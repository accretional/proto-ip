package geoip

import (
	"context"
	"net/netip"
	"testing"

	pb "github.com/accretional/proto-ip/proto/ippb"
)

// TestDBIPLookup exercises the real DB-IP City Lite database when present
// (downloaded by setup.sh into data/geoip). It is skipped on machines that
// have not fetched the DB so offline `go test` still passes.
func TestDBIPLookup(t *testing.T) {
	path, err := FindDBIPDatabase("../data/geoip")
	if err != nil {
		t.Skipf("DB-IP database not present: %v", err)
	}
	src, err := NewDBIPSource(path)
	if err != nil {
		t.Fatalf("NewDBIPSource: %v", err)
	}
	defer src.Close()

	res, err := src.Lookup(context.Background(), netip.MustParseAddr("8.8.8.8"))
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if res == nil {
		t.Fatal("8.8.8.8 not found in DB-IP database")
	}
	if res.GetSource() != pb.GeoSource_GEO_SOURCE_DBIP_LITE {
		t.Errorf("source = %v", res.GetSource())
	}
	loc := res.GetLocation()
	if loc.GetCountry() == "" {
		t.Errorf("expected a country for 8.8.8.8, got empty (%+v)", loc)
	}
	if loc.Latitude == nil || loc.Longitude == nil {
		t.Errorf("expected coordinates for 8.8.8.8, got none (%+v)", loc)
	}
	if res.GetMatchedPrefix() == "" {
		t.Errorf("expected a matched prefix")
	}
}
