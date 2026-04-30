// Package localip enumerates the host's network interfaces and their
// addresses, returning them as ippb messages ready for the
// LocalLookup gRPC service.
//
// The implementation rides on Go stdlib's net.Interfaces() / Addrs(),
// which on Linux reads /proc/net and on Darwin calls getifaddrs(3).
// Raw /proc parsing or raw sysctl(NET_RT_IFLIST) walking would
// duplicate stdlib's work and add ~300 lines of fragile parsing for
// no extra capability the LocalLookup proto exposes today. If we
// ever need data stdlib doesn't surface (FIB LOCAL vs link, IPv6
// scope IDs beyond the zone name, …), split into procfs/ and
// sysctlip/ packages then.
package localip

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"

	pb "github.com/accretional/proto-ip/proto/ippb"
)

// List returns every local interface that passes filter, with each
// interface's bound CIDRs attached.
func List(filter *pb.LookupFilter) ([]*pb.Interface, error) {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("net.Interfaces: %w", err)
	}
	out := make([]*pb.Interface, 0, len(ifs))
	for _, ni := range ifs {
		iface, err := convertInterface(ni)
		if err != nil {
			return nil, err
		}
		if !accepts(filter, iface) {
			continue
		}
		out = append(out, iface)
	}
	return out, nil
}

// convertInterface turns a net.Interface (with its bound addresses)
// into the proto shape. Class detection uses Flags + name patterns
// since stdlib doesn't expose link type — same heuristic ifconfig
// and ip(8) print to humans, applied programmatically.
func convertInterface(ni net.Interface) (*pb.Interface, error) {
	addrs, err := ni.Addrs()
	if err != nil {
		// An interface with unreadable addrs is still a valid
		// interface; surface it without addresses rather than
		// failing the whole listing.
		addrs = nil
	}
	cidrs := make([]*pb.CIDR, 0, len(addrs))
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		cidrs = append(cidrs, toCIDR(ipnet))
	}
	return &pb.Interface{
		Class:           classify(ni),
		Name:            ni.Name,
		HardwareAddress: append([]byte(nil), ni.HardwareAddr...),
		Addresses:       cidrs,
		Up:              ni.Flags&net.FlagUp != 0,
	}, nil
}

// classify picks an InterfaceClass for ni. Loopback comes from
// stdlib's flag; everything else is name-pattern guesswork.
func classify(ni net.Interface) pb.InterfaceClass {
	if ni.Flags&net.FlagLoopback != 0 {
		return pb.InterfaceClass_INTERFACE_CLASS_LOOPBACK
	}
	name := strings.ToLower(ni.Name)
	switch {
	case strings.HasPrefix(name, "utun"),
		strings.HasPrefix(name, "tun"),
		strings.HasPrefix(name, "tap"),
		strings.HasPrefix(name, "wg"),
		strings.HasPrefix(name, "ipsec"),
		strings.HasPrefix(name, "gif"),
		strings.HasPrefix(name, "stf"):
		return pb.InterfaceClass_INTERFACE_CLASS_TUNNEL
	case strings.HasPrefix(name, "bridge"),
		strings.HasPrefix(name, "br"),
		strings.HasPrefix(name, "awdl"),
		strings.HasPrefix(name, "llw"),
		strings.HasPrefix(name, "anpi"),
		strings.HasPrefix(name, "ap"),
		strings.HasPrefix(name, "vmnet"),
		strings.HasPrefix(name, "veth"),
		strings.HasPrefix(name, "docker"),
		strings.HasPrefix(name, "vlan"):
		return pb.InterfaceClass_INTERFACE_CLASS_VIRTUAL
	case strings.HasPrefix(name, "wlan"),
		strings.HasPrefix(name, "wifi"),
		strings.HasPrefix(name, "wlp"):
		return pb.InterfaceClass_INTERFACE_CLASS_WIRELESS
	case strings.HasPrefix(name, "eth"),
		strings.HasPrefix(name, "en"),
		strings.HasPrefix(name, "enp"),
		strings.HasPrefix(name, "ens"):
		// On Darwin "en0" is usually wired and "en1+" is wifi, but
		// reliably distinguishing them needs CoreWLAN. Treat the
		// whole en* family as ethernet and let callers refine via
		// name if they care.
		return pb.InterfaceClass_INTERFACE_CLASS_ETHERNET
	}
	return pb.InterfaceClass_INTERFACE_CLASS_UNKNOWN
}

// accepts applies a LookupFilter. A nil/empty filter accepts
// everything.
func accepts(f *pb.LookupFilter, iface *pb.Interface) bool {
	if f == nil {
		return true
	}
	if classes := f.GetClasses(); len(classes) > 0 {
		ok := false
		for _, c := range classes {
			if c == iface.GetClass() {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if names := f.GetNames(); len(names) > 0 {
		ok := false
		for _, n := range names {
			if n == iface.GetName() {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if f.GetOnlyRoutable() {
		// Drop the whole interface if every address is
		// loopback/link-local/private. Otherwise keep all addresses
		// — finer-grained per-address filtering would require a
		// different proto shape.
		anyRoutable := false
		for _, c := range iface.GetAddresses() {
			ip := ipFromProto(c.GetIp())
			if ip != nil && isRoutable(ip) {
				anyRoutable = true
				break
			}
		}
		if !anyRoutable {
			return false
		}
	}
	return true
}

func isRoutable(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsUnspecified() {
		return false
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	if ip.IsPrivate() {
		return false
	}
	return true
}

// toCIDR builds the proto-shape CIDR for a net.IPNet.
func toCIDR(ipnet *net.IPNet) *pb.CIDR {
	prefix, _ := ipnet.Mask.Size()
	return &pb.CIDR{
		Ip: toIP(ipnet.IP),
		Subnet: &pb.Subnet{
			Format: &pb.Subnet_PrefixLength{PrefixLength: uint32(prefix)},
		},
	}
}

// toIP encodes a net.IP into the 128-bit canonical form used by
// ippb.IP. IPv4 is stored as IPv4-mapped IPv6 (::ffff:0:0/96), so
// the wire type is uniform; the original family is preserved in the
// version oneof as the dotted-decimal / RFC 5952 textual form.
func toIP(ip net.IP) *pb.IP {
	out := &pb.IP{}
	if v4 := ip.To4(); v4 != nil {
		// IPv4-mapped IPv6: high64 = 0, low64 = 0x0000_FFFF_<v4>.
		low := uint64(0xFFFF)<<32 |
			uint64(v4[0])<<24 | uint64(v4[1])<<16 |
			uint64(v4[2])<<8 | uint64(v4[3])
		out.NetworkPrefix = 0
		out.InterfaceIdentifier = int64(low)
		out.Version = &pb.IP_V4{V4: &pb.IPv4Address{
			Format: &pb.IPv4Address_DottedDecimal{DottedDecimal: v4.String()},
		}}
		return out
	}
	v6 := ip.To16()
	if v6 == nil {
		// Shouldn't happen for valid net.IP, but fall through with
		// zero halves rather than crashing.
		return out
	}
	out.NetworkPrefix = int64(binary.BigEndian.Uint64(v6[0:8]))
	out.InterfaceIdentifier = int64(binary.BigEndian.Uint64(v6[8:16]))
	out.Version = &pb.IP_V6{V6: &pb.IPv6Address{
		Format: &pb.IPv6Address_Text{Text: v6.String()},
	}}
	return out
}

// ipFromProto rebuilds a net.IP from the 128-bit halves so we can
// apply standard predicates (loopback, link-local, …) without
// re-parsing the textual form.
func ipFromProto(ip *pb.IP) net.IP {
	if ip == nil {
		return nil
	}
	buf := make(net.IP, 16)
	binary.BigEndian.PutUint64(buf[0:8], uint64(ip.GetNetworkPrefix()))
	binary.BigEndian.PutUint64(buf[8:16], uint64(ip.GetInterfaceIdentifier()))
	return buf
}
