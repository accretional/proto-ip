package localip

import (
	"net"
	"testing"

	pb "github.com/accretional/proto-ip/proto/ippb"
)

// TestListIncludesLoopback asserts the local host has at least one
// loopback interface with at least one bound address. This holds on
// every Linux + Darwin install the test will run on.
func TestListIncludesLoopback(t *testing.T) {
	ifs, err := List(nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var lo *pb.Interface
	for _, iface := range ifs {
		if iface.GetClass() == pb.InterfaceClass_INTERFACE_CLASS_LOOPBACK {
			lo = iface
			break
		}
	}
	if lo == nil {
		t.Fatal("no loopback interface found")
	}
	if len(lo.GetAddresses()) == 0 {
		t.Errorf("loopback %q has no addresses", lo.GetName())
	}
}

// TestToIPRoundTrips encodes a known v4 and v6 address through toIP
// and decodes it back via ipFromProto. The 128-bit halves should
// round-trip exactly.
func TestToIPRoundTrips(t *testing.T) {
	cases := []net.IP{
		net.ParseIP("127.0.0.1"),
		net.ParseIP("0.0.0.0"),
		net.ParseIP("255.255.255.255"),
		net.ParseIP("::1"),
		net.ParseIP("fe80::1"),
		net.ParseIP("2001:db8::1"),
	}
	for _, ip := range cases {
		got := ipFromProto(toIP(ip))
		want := ip.To16()
		if !got.Equal(want) {
			t.Errorf("roundtrip(%s): got %s, want %s", ip, got, want)
		}
	}
}

// TestFilterByClass restricts to loopback only and verifies the
// non-loopback interfaces are dropped.
func TestFilterByClass(t *testing.T) {
	ifs, err := List(&pb.LookupFilter{
		Classes: []pb.InterfaceClass{pb.InterfaceClass_INTERFACE_CLASS_LOOPBACK},
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(ifs) == 0 {
		t.Fatal("filter returned no interfaces")
	}
	for _, iface := range ifs {
		if iface.GetClass() != pb.InterfaceClass_INTERFACE_CLASS_LOOPBACK {
			t.Errorf("got non-loopback %q in loopback-only filter", iface.GetName())
		}
	}
}
