package geoip

import (
	"google.golang.org/protobuf/proto"

	pb "github.com/accretional/proto-ip/proto/ippb"
)

// Merge combines per-source results into one best-effort location and reports
// which source contributed its granularity.
//
// Policy:
//   - Base = the result with the highest granularity (COORDINATES > CITY > …).
//   - Administrative fields (country/region/city/postal) prefer the first
//     authoritative (self-published geofeed) result, since the operator knows
//     its own network better than an aggregated estimate.
//   - Coordinates and time zone are filled from the first coordinate-bearing
//     result when the base lacks them (geofeeds never carry coordinates, so in
//     practice this pulls lat/lon from DB-IP).
//   - best_source is whichever source supplied the chosen granularity: the
//     coordinate provider when the merged result has coordinates, else the base.
func Merge(results []*pb.GeoSourceResult) (*pb.GeoLocation, pb.GeoSource) {
	var base *pb.GeoSourceResult
	for _, r := range results {
		if r == nil || r.GetLocation() == nil {
			continue
		}
		if base == nil || r.GetLocation().GetGranularity() > base.GetLocation().GetGranularity() {
			base = r
		}
	}
	if base == nil {
		return nil, pb.GeoSource_GEO_SOURCE_UNKNOWN
	}

	merged := proto.Clone(base.GetLocation()).(*pb.GeoLocation)
	bestSource := base.GetSource()

	// Prefer authoritative geofeed administrative fields where present.
	for _, r := range results {
		if !r.GetAuthoritative() || r.GetLocation() == nil {
			continue
		}
		loc := r.GetLocation()
		if loc.GetCountry() != "" {
			merged.Country = loc.GetCountry()
		}
		if loc.GetRegion() != "" {
			merged.Region = loc.GetRegion()
		}
		if loc.GetCity() != "" {
			merged.City = loc.GetCity()
		}
		if loc.GetPostalCode() != "" {
			merged.PostalCode = loc.GetPostalCode()
		}
		break // first authoritative source wins
	}

	// Fill coordinates from a coordinate-bearing source if the base lacked them.
	if merged.Latitude == nil {
		for _, r := range results {
			loc := r.GetLocation()
			if loc == nil || loc.Latitude == nil {
				continue
			}
			lat, lon := loc.GetLatitude(), loc.GetLongitude()
			merged.Latitude = &lat
			merged.Longitude = &lon
			if merged.GetTimeZone() == "" {
				merged.TimeZone = loc.GetTimeZone()
			}
			merged.Granularity = pb.GeoGranularity_GEO_GRANULARITY_COORDINATES
			bestSource = r.GetSource()
			break
		}
	}

	return merged, bestSource
}
