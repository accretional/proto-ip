package geoip

import (
	"net/netip"
	"strings"
	"testing"

	pb "github.com/accretional/proto-ip/proto/ippb"
)

// vrpsJSON is a minimal rpki-client-style dump exercising both the string
// ("AS13335") and integer (15169) asn encodings plus an IPv6 ROA. The metadata
// block carries its own integer "roas" count (as the real dump does) to ensure
// the parser skips it and finds the real top-level array.
const vrpsJSON = `{
  "metadata": {"buildtime": "2026-06-02T00:00:00Z", "roas": 3, "failedroas": 0},
  "roas": [
    {"asn": "AS13335", "prefix": "1.1.1.0/24", "maxLength": 24, "ta": "apnic"},
    {"asn": 15169, "prefix": "8.8.8.0/24", "maxLength": 24, "ta": "arin"},
    {"asn": "AS64500", "prefix": "2606:4700::/32", "maxLength": 48, "ta": "apnic"}
  ]
}`

func mustRPKI(t *testing.T) *RPKISet {
	t.Helper()
	s, err := parseVRPs(strings.NewReader(vrpsJSON))
	if err != nil {
		t.Fatalf("parseVRPs: %v", err)
	}
	if got := s.Len(); got != 3 {
		t.Fatalf("Len() = %d, want 3", got)
	}
	return s
}

func TestRPKIValidate(t *testing.T) {
	s := mustRPKI(t)

	cases := []struct {
		name   string
		addr   string
		origin uint32
		want   pb.RPKIStatus
	}{
		{"valid string-asn v4", "1.1.1.1", 13335, pb.RPKIStatus_RPKI_STATUS_VALID},
		{"valid int-asn v4", "8.8.8.8", 15169, pb.RPKIStatus_RPKI_STATUS_VALID},
		{"valid v6", "2606:4700::1", 64500, pb.RPKIStatus_RPKI_STATUS_VALID},
		{"invalid wrong origin", "1.1.1.1", 99999, pb.RPKIStatus_RPKI_STATUS_INVALID},
		{"unknown no origin", "1.1.1.1", 0, pb.RPKIStatus_RPKI_STATUS_UNKNOWN},
		{"not found uncovered", "9.9.9.9", 19281, pb.RPKIStatus_RPKI_STATUS_NOT_FOUND},
		{"not found v6", "2001:db8::1", 64500, pb.RPKIStatus_RPKI_STATUS_NOT_FOUND},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			addr := netip.MustParseAddr(c.addr)
			got, covering := s.Validate(addr, c.origin)
			if got != c.want {
				t.Errorf("Validate(%s, %d) = %v, want %v", c.addr, c.origin, got, c.want)
			}
			// NOT_FOUND must carry no covering ROAs; every other verdict must.
			if c.want == pb.RPKIStatus_RPKI_STATUS_NOT_FOUND && len(covering) != 0 {
				t.Errorf("NOT_FOUND should have no covering ROAs, got %d", len(covering))
			}
			if c.want != pb.RPKIStatus_RPKI_STATUS_NOT_FOUND && len(covering) == 0 {
				t.Errorf("%v should have covering ROAs, got 0", c.want)
			}
		})
	}
}

func TestRPKICoveringRoaToProto(t *testing.T) {
	s := mustRPKI(t)
	_, covering := s.Validate(netip.MustParseAddr("1.1.1.1"), 13335)
	roas := vrpsToProto(covering)
	if len(roas) != 1 {
		t.Fatalf("vrpsToProto len = %d, want 1", len(roas))
	}
	got := roas[0]
	if got.GetPrefix() != "1.1.1.0/24" || got.GetMaxLength() != 24 || got.GetAsn() != 13335 {
		t.Errorf("RpkiRoa = {%s %d AS%d}, want {1.1.1.0/24 24 AS13335}",
			got.GetPrefix(), got.GetMaxLength(), got.GetAsn())
	}
}

func TestParseVRPASN(t *testing.T) {
	cases := []struct {
		in   string
		want uint32
		ok   bool
	}{
		{`13335`, 13335, true},
		{`"AS13335"`, 13335, true},
		{`"as13335"`, 13335, true},
		{`"13335"`, 13335, true},
		{`"AS"`, 0, false},
		{`"garbage"`, 0, false},
	}
	for _, c := range cases {
		got, ok := parseVRPASN([]byte(c.in))
		if ok != c.ok || got != c.want {
			t.Errorf("parseVRPASN(%s) = (%d,%t), want (%d,%t)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestParseVRPsEmptyRoas(t *testing.T) {
	s, err := parseVRPs(strings.NewReader(`{"metadata":{},"roas":[]}`))
	if err != nil {
		t.Fatalf("parseVRPs empty: %v", err)
	}
	if s.Len() != 0 {
		t.Errorf("Len() = %d, want 0", s.Len())
	}
	if st, _ := s.Validate(netip.MustParseAddr("1.1.1.1"), 13335); st != pb.RPKIStatus_RPKI_STATUS_NOT_FOUND {
		t.Errorf("empty set Validate = %v, want NOT_FOUND", st)
	}
}
