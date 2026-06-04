// Package geoip implements best-effort IP geolocation by combining free,
// open data sources: self-published RFC 8805 geofeeds (discovered via RDAP
// per RFC 9632) and the DB-IP City Lite database (CC BY 4.0). It returns the
// most granular location available — ideally latitude/longitude — and is
// explicit about gaps and disagreement between sources.
package geoip

import (
	"context"
	"net/netip"

	pb "github.com/accretional/proto-ip/proto/ippb"
)

// Source is a single geolocation data provider. Implementations are safe for
// concurrent use.
type Source interface {
	// Lookup returns a result for ip, or (nil, nil) when the source simply
	// has no data for that address. A non-nil error means the source itself
	// failed (network, parse, …); the server logs it and continues with the
	// other sources rather than failing the whole request.
	Lookup(ctx context.Context, ip netip.Addr) (*pb.GeoSourceResult, error)
	// Kind identifies which source this is.
	Kind() pb.GeoSource
}

// granularityFromFields derives a granularity from the administrative fields
// of loc, ignoring coordinates. Coordinate-bearing results set
// GEO_GRANULARITY_COORDINATES directly.
func granularityFromFields(loc *pb.GeoLocation) pb.GeoGranularity {
	switch {
	case loc.GetCity() != "":
		return pb.GeoGranularity_GEO_GRANULARITY_CITY
	case loc.GetRegion() != "":
		return pb.GeoGranularity_GEO_GRANULARITY_REGION
	case loc.GetCountry() != "":
		return pb.GeoGranularity_GEO_GRANULARITY_COUNTRY
	default:
		return pb.GeoGranularity_GEO_GRANULARITY_UNKNOWN
	}
}
