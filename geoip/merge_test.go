package geoip

import (
	"testing"

	pb "github.com/accretional/proto-ip/proto/ippb"
)

func f64(v float64) *float64 { return &v }

func dbipResult(lat, lon float64, country, region, city string) *pb.GeoSourceResult {
	return &pb.GeoSourceResult{
		Source:        pb.GeoSource_GEO_SOURCE_DBIP_LITE,
		Authoritative: false,
		Location: &pb.GeoLocation{
			Latitude:    f64(lat),
			Longitude:   f64(lon),
			Country:     country,
			Region:      region,
			City:        city,
			Granularity: pb.GeoGranularity_GEO_GRANULARITY_COORDINATES,
		},
	}
}

func geofeedResult(country, region, city string) *pb.GeoSourceResult {
	g := granularityFromFields(&pb.GeoLocation{Country: country, Region: region, City: city})
	return &pb.GeoSourceResult{
		Source:        pb.GeoSource_GEO_SOURCE_GEOFEED,
		Authoritative: true,
		Location: &pb.GeoLocation{
			Country:     country,
			Region:      region,
			City:        city,
			Granularity: g,
		},
	}
}

func TestMergeEmpty(t *testing.T) {
	best, src := Merge(nil)
	if best != nil || src != pb.GeoSource_GEO_SOURCE_UNKNOWN {
		t.Fatalf("empty merge: best=%v src=%v", best, src)
	}
}

func TestMergeDBIPOnly(t *testing.T) {
	best, src := Merge([]*pb.GeoSourceResult{dbipResult(37.4, -122.0, "US", "US-CA", "Mountain View")})
	if best.Latitude == nil || best.GetLatitude() != 37.4 {
		t.Errorf("missing coordinates: %+v", best)
	}
	if src != pb.GeoSource_GEO_SOURCE_DBIP_LITE {
		t.Errorf("best_source = %v, want dbip", src)
	}
	if best.GetGranularity() != pb.GeoGranularity_GEO_GRANULARITY_COORDINATES {
		t.Errorf("granularity = %v", best.GetGranularity())
	}
}

// The key behaviour: authoritative geofeed admin fields win, coordinates are
// pulled from DB-IP, and best_source reflects the coordinate provider.
func TestMergeGeofeedPlusDBIP(t *testing.T) {
	results := []*pb.GeoSourceResult{
		dbipResult(40.71, -74.00, "US", "US-NY", "New York"),
		geofeedResult("US", "US-CA", "San Francisco"),
	}
	best, src := Merge(results)

	if best.GetCity() != "San Francisco" || best.GetRegion() != "US-CA" {
		t.Errorf("authoritative admin fields not preferred: %+v", best)
	}
	if best.Latitude == nil || best.GetLatitude() != 40.71 {
		t.Errorf("coordinates not filled from DB-IP: %+v", best)
	}
	if best.GetGranularity() != pb.GeoGranularity_GEO_GRANULARITY_COORDINATES {
		t.Errorf("granularity = %v, want coordinates", best.GetGranularity())
	}
	if src != pb.GeoSource_GEO_SOURCE_DBIP_LITE {
		t.Errorf("best_source = %v, want dbip (coordinate provider)", src)
	}
}

func TestMergeGeofeedOnly(t *testing.T) {
	best, src := Merge([]*pb.GeoSourceResult{geofeedResult("DE", "DE-BE", "Berlin")})
	if best.Latitude != nil {
		t.Errorf("geofeed must not synthesize coordinates: %+v", best)
	}
	if best.GetCity() != "Berlin" {
		t.Errorf("city = %q", best.GetCity())
	}
	if src != pb.GeoSource_GEO_SOURCE_GEOFEED {
		t.Errorf("best_source = %v, want geofeed", src)
	}
	if best.GetGranularity() != pb.GeoGranularity_GEO_GRANULARITY_CITY {
		t.Errorf("granularity = %v, want city", best.GetGranularity())
	}
}

// Merge must not mutate the input results (it clones the base location).
func TestMergeDoesNotMutate(t *testing.T) {
	df := dbipResult(1, 2, "US", "", "")
	gf := geofeedResult("US", "US-TX", "Austin")
	Merge([]*pb.GeoSourceResult{df, gf})
	if df.GetLocation().GetCity() != "" {
		t.Errorf("DB-IP input mutated: %+v", df.GetLocation())
	}
}
