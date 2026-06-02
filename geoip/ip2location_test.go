package geoip

import (
	"context"
	"fmt"
	"math/big"
	"net/netip"
	"strconv"
	"strings"
	"testing"

	pb "github.com/accretional/proto-ip/proto/ippb"
)

// addrToDecimal renders an address as IP2Location's decimal integer form.
func addrToDecimal(a netip.Addr, v6 bool) string {
	if !v6 {
		b := a.As4()
		n := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
		return strconv.FormatUint(uint64(n), 10)
	}
	b := a.As16()
	return new(big.Int).SetBytes(b[:]).String()
}

func TestParseIP2LocationV4(t *testing.T) {
	lo := netip.MustParseAddr("1.0.0.0")
	hi := netip.MustParseAddr("1.0.0.255")
	csv := fmt.Sprintf(`"%s","%s","US","United States","California","Los Angeles","34.052230","-118.243680"
"%s","%s","AU","Australia","Victoria","Melbourne","0.000000","0.000000"
`,
		addrToDecimal(lo, false), addrToDecimal(hi, false),
		addrToDecimal(netip.MustParseAddr("1.0.1.0"), false), addrToDecimal(netip.MustParseAddr("1.0.1.255"), false))

	rs, err := parseIP2LocationCSV(strings.NewReader(csv), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 {
		t.Fatalf("got %d ranges, want 2", len(rs))
	}

	// In-range lookup with coordinates.
	got, ok := searchRange(rs, netip.MustParseAddr("1.0.0.100"))
	if !ok || got.loc.country != "US" || got.loc.city != "Los Angeles" || got.loc.lat != 34.05223 {
		t.Errorf("1.0.0.100 lookup wrong: %+v ok=%v", got.loc, ok)
	}
	// Boundary (last address of range) must match.
	if _, ok := searchRange(rs, hi); !ok {
		t.Errorf("range end %s should match", hi)
	}
	// Gap between ranges must miss.
	if _, ok := searchRange(rs, netip.MustParseAddr("1.0.0.255")); !ok {
		t.Errorf("end boundary should match")
	}
	if _, ok := searchRange(rs, netip.MustParseAddr("2.0.0.1")); ok {
		t.Errorf("2.0.0.1 should not match any range")
	}
}

func TestIP2LocationLookupV4ZeroCoords(t *testing.T) {
	lo := netip.MustParseAddr("9.0.0.0")
	hi := netip.MustParseAddr("9.0.0.255")
	csv := fmt.Sprintf(`"%s","%s","AU","Australia","Victoria","Melbourne","0.000000","0.000000"`+"\n",
		addrToDecimal(lo, false), addrToDecimal(hi, false))
	rs, err := parseIP2LocationCSV(strings.NewReader(csv), false)
	if err != nil {
		t.Fatal(err)
	}
	src := &IP2LocationSource{v4: rs}
	res, err := src.Lookup(context.Background(), netip.MustParseAddr("9.0.0.5"))
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("expected a hit")
	}
	if res.GetLocation().Latitude != nil {
		t.Errorf("0,0 must be treated as no coordinates: %+v", res.GetLocation())
	}
	if res.GetLocation().GetGranularity() != pb.GeoGranularity_GEO_GRANULARITY_CITY {
		t.Errorf("granularity = %v, want city (no coords)", res.GetLocation().GetGranularity())
	}
}

func TestIP2LocationLookupV6(t *testing.T) {
	lo := netip.MustParseAddr("2001:db8::")
	hi := netip.MustParseAddr("2001:db8::ffff")
	csv := fmt.Sprintf(`"%s","%s","JP","Japan","Tokyo","Tokyo","35.689500","139.691700"`+"\n",
		addrToDecimal(lo, true), addrToDecimal(hi, true))
	rs, err := parseIP2LocationCSV(strings.NewReader(csv), true)
	if err != nil {
		t.Fatal(err)
	}
	src := &IP2LocationSource{v6: rs}

	res, err := src.Lookup(context.Background(), netip.MustParseAddr("2001:db8::42"))
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.GetSource() != pb.GeoSource_GEO_SOURCE_IP2LOCATION_LITE {
		t.Fatalf("v6 hit: %+v", res)
	}
	loc := res.GetLocation()
	if loc.GetCity() != "Tokyo" || loc.Latitude == nil || loc.GetLatitude() != 35.6895 {
		t.Errorf("v6 location wrong: %+v", loc)
	}
	if res.GetMatchedPrefix() != "2001:db8::-2001:db8::ffff" {
		t.Errorf("matched_prefix = %q", res.GetMatchedPrefix())
	}

	// Outside the range misses.
	if r, _ := src.Lookup(context.Background(), netip.MustParseAddr("2001:db8::1:0")); r != nil {
		t.Errorf("2001:db8::1:0 should be outside the range")
	}
}
