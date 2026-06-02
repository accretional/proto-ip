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
		Confidence:    pb.GeoConfidence_GEO_CONFIDENCE_MEDIUM,
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
		Confidence:    pb.GeoConfidence_GEO_CONFIDENCE_HIGH,
		Location: &pb.GeoLocation{
			Country:     country,
			Region:      region,
			City:        city,
			Granularity: g,
		},
	}
}

func iptoasnResult(country string, asn uint32, network string) *pb.GeoSourceResult {
	loc := &pb.GeoLocation{Country: country}
	loc.Granularity = granularityFromFields(loc)
	return &pb.GeoSourceResult{
		Source:      pb.GeoSource_GEO_SOURCE_IPTOASN,
		Confidence:  pb.GeoConfidence_GEO_CONFIDENCE_LOW,
		Location:    loc,
		Asn:         asn,
		Network:     network,
	}
}

func TestMergeEmpty(t *testing.T) {
	resp := Merge(nil)
	if resp.GetBest() != nil || resp.GetBestSource() != pb.GeoSource_GEO_SOURCE_UNKNOWN {
		t.Fatalf("empty merge: %+v", resp)
	}
	if resp.GetConfidence() != pb.GeoConfidence_GEO_CONFIDENCE_UNKNOWN {
		t.Errorf("empty confidence = %v", resp.GetConfidence())
	}
}

func TestMergeDBIPOnly(t *testing.T) {
	resp := Merge([]*pb.GeoSourceResult{dbipResult(37.4, -122.0, "US", "US-CA", "Mountain View")})
	if resp.GetBest().GetLatitude() != 37.4 {
		t.Errorf("missing coordinates: %+v", resp.GetBest())
	}
	if resp.GetBestSource() != pb.GeoSource_GEO_SOURCE_DBIP_LITE {
		t.Errorf("best_source = %v, want dbip", resp.GetBestSource())
	}
	if resp.GetConfidence() != pb.GeoConfidence_GEO_CONFIDENCE_MEDIUM {
		t.Errorf("confidence = %v, want medium", resp.GetConfidence())
	}
}

// Authoritative geofeed admin fields win, coordinates are pulled from DB-IP,
// and best_source + confidence reflect the coordinate provider (DB-IP).
func TestMergeGeofeedPlusDBIP(t *testing.T) {
	resp := Merge([]*pb.GeoSourceResult{
		dbipResult(40.71, -74.00, "US", "US-NY", "New York"),
		geofeedResult("US", "US-CA", "San Francisco"),
	})
	best := resp.GetBest()
	if best.GetCity() != "San Francisco" || best.GetRegion() != "US-CA" {
		t.Errorf("authoritative admin fields not preferred: %+v", best)
	}
	if best.GetLatitude() != 40.71 {
		t.Errorf("coordinates not filled from DB-IP: %+v", best)
	}
	if resp.GetBestSource() != pb.GeoSource_GEO_SOURCE_DBIP_LITE {
		t.Errorf("best_source = %v, want dbip (coordinate provider)", resp.GetBestSource())
	}
	if resp.GetConfidence() != pb.GeoConfidence_GEO_CONFIDENCE_MEDIUM {
		t.Errorf("confidence = %v, want medium (from DB-IP)", resp.GetConfidence())
	}
}

func TestMergeGeofeedOnly(t *testing.T) {
	resp := Merge([]*pb.GeoSourceResult{geofeedResult("DE", "DE-BE", "Berlin")})
	if resp.GetBest().Latitude != nil {
		t.Errorf("geofeed must not synthesize coordinates: %+v", resp.GetBest())
	}
	if resp.GetBestSource() != pb.GeoSource_GEO_SOURCE_GEOFEED {
		t.Errorf("best_source = %v, want geofeed", resp.GetBestSource())
	}
	if resp.GetConfidence() != pb.GeoConfidence_GEO_CONFIDENCE_HIGH {
		t.Errorf("confidence = %v, want high (geofeed)", resp.GetConfidence())
	}
}

// iptoasn supplies ASN/network metadata and a country floor without becoming
// the location winner when a coordinate source is present.
func TestMergeLiftsASN(t *testing.T) {
	resp := Merge([]*pb.GeoSourceResult{
		dbipResult(37.4, -122.0, "US", "US-CA", "Mountain View"),
		iptoasnResult("US", 15169, "GOOGLE"),
	})
	if resp.GetAsn() != 15169 || resp.GetNetwork() != "GOOGLE" {
		t.Errorf("asn/network not lifted: asn=%d network=%q", resp.GetAsn(), resp.GetNetwork())
	}
	if resp.GetBestSource() != pb.GeoSource_GEO_SOURCE_DBIP_LITE {
		t.Errorf("iptoasn should not win over a coordinate source: %v", resp.GetBestSource())
	}
}

// With only iptoasn, the response still carries ASN + a country floor.
func TestMergeIPtoASNOnly(t *testing.T) {
	resp := Merge([]*pb.GeoSourceResult{iptoasnResult("AU", 13335, "CLOUDFLARENET")})
	if resp.GetAsn() != 13335 || resp.GetBest().GetCountry() != "AU" {
		t.Errorf("iptoasn-only merge wrong: %+v", resp)
	}
	if resp.GetConfidence() != pb.GeoConfidence_GEO_CONFIDENCE_LOW {
		t.Errorf("confidence = %v, want low", resp.GetConfidence())
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
