package geoip

import (
	"google.golang.org/protobuf/proto"

	pb "github.com/accretional/proto-ip/proto/ippb"
)

// Merge combines per-source results into a GeoResponse: one best-effort
// location, the contributing source, an overall confidence, and BGP-derived
// ASN/network metadata. The caller is responsible for the anycast flag (and
// the confidence downgrade it implies), since that is a property of the
// address rather than of any single source.
//
// Policy:
//   - Base = the result with the highest granularity (COORDINATES > CITY > …).
//   - Administrative fields (country/region/city/postal) prefer the first
//     authoritative (self-published geofeed) result.
//   - Coordinates and time zone are filled from the first coordinate-bearing
//     result when the base lacks them (in practice DB-IP / IP2Location / IPmap).
//   - best_source and confidence come from whichever result supplied the chosen
//     granularity (the coordinate provider when `best` ends up with coords).
//   - asn/network are lifted from the first source that carries them (iptoasn).
func Merge(results []*pb.GeoSourceResult) *pb.GeoResponse {
	resp := &pb.GeoResponse{Sources: results}

	var base *pb.GeoSourceResult
	for _, r := range results {
		if r == nil || r.GetLocation() == nil {
			continue
		}
		if base == nil || r.GetLocation().GetGranularity() > base.GetLocation().GetGranularity() {
			base = r
		}
	}

	// Lift BGP-derived ASN/network from whichever source provides it.
	for _, r := range results {
		if r.GetAsn() != 0 {
			resp.Asn = r.GetAsn()
			resp.Network = r.GetNetwork()
			break
		}
	}

	if base == nil {
		return resp // no source had a location (asn/network may still be set)
	}

	merged := proto.Clone(base.GetLocation()).(*pb.GeoLocation)
	bestResult := base

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
			bestResult = r
			break
		}
	}

	resp.Best = merged
	resp.BestSource = bestResult.GetSource()
	resp.Confidence = bestResult.GetConfidence()
	return resp
}
