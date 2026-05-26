// Command rdap-client is a CLI driver for the RDAPLookup gRPC service.
// Used by LET_IT_RIP.sh to verify the server returns RDAP data.
//
// Usage:
//
//	rdap-client [-addr HOST:PORT] ip   <address>      # e.g. 8.8.8.8
//	rdap-client [-addr HOST:PORT] cidr <prefix>       # e.g. 2001:db8::/32
package main

import (
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/accretional/proto-ip/proto/ippb"
)

func main() {
	addr := flag.String("addr", "localhost:50098", "RDAPLookup server address")
	timeout := flag.Duration("timeout", 15*time.Second, "request timeout")
	flag.Parse()

	args := flag.Args()
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: rdap-client [-addr HOST:PORT] (ip <addr> | cidr <prefix>)")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("dial %s: %v", *addr, err)
	}
	defer conn.Close()
	client := pb.NewRDAPLookupClient(conn)

	switch args[0] {
	case "ip":
		resp, err := client.LookupIP(ctx, parseIP(args[1]))
		if err != nil {
			log.Fatalf("LookupIP(%s): %v", args[1], err)
		}
		printResponse(resp)
	case "cidr":
		resp, err := client.LookupCIDR(ctx, parseCIDR(args[1]))
		if err != nil {
			log.Fatalf("LookupCIDR(%s): %v", args[1], err)
		}
		printResponse(resp)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", args[0])
		os.Exit(2)
	}
}

func printResponse(resp *pb.RDAPResponse) {
	n := resp.GetNetwork()
	fmt.Printf("handle:          %s\n", n.GetHandle())
	fmt.Printf("name:            %s\n", n.GetName())
	fmt.Printf("type:            %s\n", n.GetType())
	fmt.Printf("range:           %s – %s\n", n.GetStartAddress(), n.GetEndAddress())
	for _, cb := range n.GetCidrBlocks() {
		fmt.Printf("cidr_block:      %s/%d\n", cb.GetPrefix(), cb.GetLength())
	}
	fmt.Printf("ip_version:      %s\n", shortEnum(n.GetIpVersion().String(), "RDAP_IP_VERSION_"))
	fmt.Printf("country:         %s\n", n.GetCountry())
	statuses := make([]string, len(n.GetStatus()))
	for i, s := range n.GetStatus() {
		statuses[i] = shortEnum(s.String(), "RDAP_STATUS_")
	}
	fmt.Printf("status:          %s\n", strings.Join(statuses, ", "))
	fmt.Printf("parent_handle:   %s\n", n.GetParentHandle())
	fmt.Printf("rdap_server:     %s\n", n.GetRdapServer())
	fmt.Printf("conformance:     %s\n", strings.Join(n.GetRdapConformance(), ", "))
	for _, e := range n.GetEntities() {
		roles := make([]string, len(e.GetRoles()))
		for i, r := range e.GetRoles() {
			roles[i] = shortEnum(r.String(), "RDAP_ROLE_")
		}
		kind := shortEnum(e.GetKind().String(), "RDAP_ENTITY_KIND_")
		fmt.Printf("entity:          handle=%s kind=%s fn=%q org=%q roles=%s\n",
			e.GetHandle(), kind, e.GetFn(), e.GetOrg(), strings.Join(roles, ","))
		if addr := e.GetAddress(); addr != "" {
			fmt.Printf("  address:       %s\n", strings.ReplaceAll(addr, "\n", " | "))
		}
		if phone := e.GetPhone(); phone != "" {
			fmt.Printf("  phone:         %s\n", phone)
		}
		if emails := e.GetEmails(); len(emails) > 0 {
			fmt.Printf("  emails:        %s\n", strings.Join(emails, ", "))
		}
	}
	for _, ev := range n.GetEvents() {
		fmt.Printf("event:           %s @ %s\n",
			shortEnum(ev.GetAction().String(), "RDAP_EVENT_ACTION_"), ev.GetDate())
	}
}

// shortEnum strips the generated enum prefix and lowercases the result
// to match the original RDAP JSON value form (e.g. "active", "v4").
func shortEnum(s, prefix string) string {
	return strings.ToLower(strings.TrimPrefix(s, prefix))
}

func parseIP(s string) *pb.IP {
	ip := net.ParseIP(s)
	if ip == nil {
		log.Fatalf("invalid IP address: %q", s)
	}
	return netIPToProto(ip)
}

func parseCIDR(s string) *pb.CIDR {
	ip, ipnet, err := net.ParseCIDR(s)
	if err != nil {
		log.Fatalf("invalid CIDR: %q: %v", s, err)
	}
	// Use the host address (not the network address) so LookupCIDR
	// receives exactly what the user typed.
	ones, _ := ipnet.Mask.Size()
	return &pb.CIDR{
		Ip: netIPToProto(ip),
		Subnet: &pb.Subnet{
			Format: &pb.Subnet_PrefixLength{PrefixLength: uint32(ones)},
		},
	}
}

func netIPToProto(ip net.IP) *pb.IP {
	out := &pb.IP{}
	if v4 := ip.To4(); v4 != nil {
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
	out.NetworkPrefix = int64(binary.BigEndian.Uint64(v6[0:8]))
	out.InterfaceIdentifier = int64(binary.BigEndian.Uint64(v6[8:16]))
	out.Version = &pb.IP_V6{V6: &pb.IPv6Address{
		Format: &pb.IPv6Address_Text{Text: v6.String()},
	}}
	return out
}
