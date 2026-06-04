package geoip

import (
	"context"
	"net/netip"
	"testing"

	pb "github.com/accretional/proto-ip/proto/ippb"
)

// TestIP2LocationDB9 exercises the real IP2Location LITE DB9 MMDB when present
// (downloaded by setup.sh given IP2LOCATION_TOKEN). The DB9 MMDB uses the
// GeoIP2-City schema, so it flows through the shared MMDBCitySource. Skipped
// when absent so offline `go test` still passes.
func TestIP2LocationDB9(t *testing.T) {
	path, err := FindIP2LocationDatabase("../data/geoip")
	if err != nil {
		t.Skipf("IP2Location DB9 MMDB not present: %v", err)
	}
	src, err := NewIP2LocationSource(path)
	if err != nil {
		t.Fatalf("NewIP2LocationSource: %v", err)
	}
	defer src.Close()

	if src.Kind() != pb.GeoSource_GEO_SOURCE_IP2LOCATION_LITE {
		t.Errorf("Kind() = %v", src.Kind())
	}

	// IPv4 and IPv6 both resolve from the single MMDB.
	for _, ip := range []string{"8.8.8.8", "2001:4860:4860::8888"} {
		res, err := src.Lookup(context.Background(), netip.MustParseAddr(ip))
		if err != nil {
			t.Fatalf("Lookup(%s): %v", ip, err)
		}
		if res == nil {
			t.Fatalf("%s not found in DB9", ip)
		}
		loc := res.GetLocation()
		if loc.GetCountry() == "" {
			t.Errorf("%s: empty country (%+v)", ip, loc)
		}
		if loc.Latitude == nil || loc.Longitude == nil {
			t.Errorf("%s: expected coordinates (%+v)", ip, loc)
		}
		if res.GetAttribution() != IP2LocationAttribution {
			t.Errorf("%s: attribution = %q", ip, res.GetAttribution())
		}
	}
}
