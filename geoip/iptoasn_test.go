package geoip

import (
	"context"
	"net/netip"
	"strings"
	"testing"

	pb "github.com/accretional/proto-ip/proto/ippb"
)

func TestParseIPtoASN(t *testing.T) {
	const tsv = "1.0.0.0\t1.0.0.255\t13335\tUS\tCLOUDFLARENET\n" +
		"1.0.1.0\t1.0.3.255\t0\tNone\tNot routed\n" + // AS 0 → skipped
		"2001:db8::\t2001:db8::ffff\t64500\tJP\tEXAMPLE-V6\n"
	rs, err := parseIPtoASN(strings.NewReader(tsv))
	if err != nil {
		t.Fatal(err)
	}
	// Mixed v4 + v6 rows both parse; the AS-0 row is dropped → 2 ranges.
	if len(rs) != 2 {
		t.Fatalf("got %d ranges, want 2: %+v", len(rs), rs)
	}
	r, ok := searchASNRange(rs, netip.MustParseAddr("1.0.0.50"))
	if !ok || r.asn != 13335 || r.country != "US" || r.network != "CLOUDFLARENET" {
		t.Errorf("v4 lookup wrong: %+v ok=%v", r, ok)
	}
	if _, ok := searchASNRange(rs, netip.MustParseAddr("1.0.2.1")); ok {
		t.Errorf("AS-0 (not routed) range should have been skipped")
	}
}

func TestIPtoASNLookup(t *testing.T) {
	rs, err := parseIPtoASN(strings.NewReader("8.8.8.0\t8.8.8.255\t15169\tUS\tGOOGLE\n"))
	if err != nil {
		t.Fatal(err)
	}
	src := &IPtoASNSource{v4: rs}
	got, err := src.Lookup(context.Background(), netip.MustParseAddr("8.8.8.8"))
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.GetAsn() != 15169 || got.GetNetwork() != "GOOGLE" {
		t.Fatalf("lookup: %+v", got)
	}
	if got.GetLocation().GetCountry() != "US" ||
		got.GetLocation().GetGranularity() != pb.GeoGranularity_GEO_GRANULARITY_COUNTRY {
		t.Errorf("expected US country floor: %+v", got.GetLocation())
	}
	if got.GetConfidence() != pb.GeoConfidence_GEO_CONFIDENCE_LOW {
		t.Errorf("confidence = %v, want low", got.GetConfidence())
	}
}

// TestIPtoASNReal exercises the downloaded tables when present.
func TestIPtoASNReal(t *testing.T) {
	v4, v6, ok := FindIPtoASNDatabases("../data/geoip")
	if !ok {
		t.Skip("iptoasn tables not present")
	}
	src, err := NewIPtoASNSource(v4, v6)
	if err != nil {
		t.Fatalf("NewIPtoASNSource: %v", err)
	}
	res, err := src.Lookup(context.Background(), netip.MustParseAddr("8.8.8.8"))
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.GetAsn() != 15169 {
		t.Errorf("8.8.8.8 expected AS15169 (Google), got %+v", res)
	}
}
