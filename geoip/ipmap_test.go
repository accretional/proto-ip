package geoip

import (
	"context"
	"net/netip"
	"strings"
	"testing"

	pb "github.com/accretional/proto-ip/proto/ippb"
)

func TestParseIPMap(t *testing.T) {
	const dump = `1.1.1.1/32,JOHANNESBURG-ZA-06-X,Johannesburg,Gauteng,South Africa,ZA,ZAF,-26.20227,28.04363,32.4
2001:1900::3:32f/128,BOSTON-US-MA-Y,Boston,Massachusetts,United States,US,USA,42.35843,-71.05977,29.64
190.2.130.14/32,KRALENDIJK-BQ-BO-Z,Kralendijk,Bonaire,Bonaire, Saint Eustatius and Saba ,BQ,BES,12.15,-68.26667,10
8.8.8.8/32,BROKEN,City,State,Country,US,USA,not-a-number,oops,5

99.99.99.99/32,SHORT,too,few,fields`
	m, err := parseIPMap(strings.NewReader(dump))
	if err != nil {
		t.Fatalf("parseIPMap: %v", err)
	}
	// Valid: 1.1.1.1, the v6 row, and the comma-in-country row. Broken &
	// short rows are skipped → 3 entries.
	if len(m) != 3 {
		t.Fatalf("got %d entries, want 3: %v", len(m), m)
	}

	za := m[netip.MustParseAddr("1.1.1.1")]
	if za.country != "ZA" || za.city != "Johannesburg" || za.lat != -26.20227 {
		t.Errorf("1.1.1.1 parsed wrong: %+v", za)
	}
	// The embedded-comma country row must still parse cc2/lat/lon from the end.
	bq := m[netip.MustParseAddr("190.2.130.14")]
	if bq.country != "BQ" || bq.lat != 12.15 || bq.lon != -68.26667 {
		t.Errorf("comma-in-country row parsed wrong: %+v", bq)
	}
	if _, ok := m[netip.MustParseAddr("8.8.8.8")]; ok {
		t.Errorf("row with non-numeric coords should be skipped")
	}
}

func TestIPMapLookup(t *testing.T) {
	src := &IPMapSource{byAddr: map[netip.Addr]ipmapEntry{
		netip.MustParseAddr("1.1.1.1"): {city: "Johannesburg", country: "ZA", lat: -26.2, lon: 28.0},
	}}

	got, err := src.Lookup(context.Background(), netip.MustParseAddr("1.1.1.1"))
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.GetSource() != pb.GeoSource_GEO_SOURCE_IPMAP {
		t.Fatalf("hit: %+v", got)
	}
	if got.GetMatchedPrefix() != "1.1.1.1/32" {
		t.Errorf("matched_prefix = %q", got.GetMatchedPrefix())
	}
	if got.GetLocation().Latitude == nil || got.GetLocation().GetGranularity() != pb.GeoGranularity_GEO_GRANULARITY_COORDINATES {
		t.Errorf("expected coordinates granularity: %+v", got.GetLocation())
	}

	miss, err := src.Lookup(context.Background(), netip.MustParseAddr("2.2.2.2"))
	if err != nil || miss != nil {
		t.Errorf("miss should be (nil,nil), got (%v,%v)", miss, err)
	}
}

// TestIPMapRealDump exercises the downloaded dump when present (setup.sh fetches
// it into data/geoip). It asserts the whole bzip2 stream decodes (Go's
// compress/bzip2 must read all concatenated blocks) and that a known row
// resolves. Skipped when the dump is absent so offline `go test` still passes.
func TestIPMapRealDump(t *testing.T) {
	path, err := FindIPMapDatabase("../data/geoip")
	if err != nil {
		t.Skipf("RIPE IPmap dump not present: %v", err)
	}
	src, err := NewIPMapSource(path)
	if err != nil {
		t.Fatalf("NewIPMapSource: %v", err)
	}
	// The full dump has ~600k rows; require a high floor to catch a truncated
	// bzip2 decode (would otherwise silently load only the first block).
	if src.Len() < 400000 {
		t.Fatalf("loaded only %d IPmap entries; expected >400k (truncated decode?)", src.Len())
	}
	res, err := src.Lookup(context.Background(), netip.MustParseAddr("1.1.1.1"))
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Skip("1.1.1.1 not in this dump revision")
	}
	if res.GetLocation().Latitude == nil || res.GetLocation().GetCountry() == "" {
		t.Errorf("1.1.1.1 missing coords/country: %+v", res.GetLocation())
	}
}
