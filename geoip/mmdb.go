package geoip

import (
	"context"
	"fmt"
	"net/netip"

	"github.com/oschwald/maxminddb-golang/v2"

	pb "github.com/accretional/proto-ip/proto/ippb"
)

// cityRecord mirrors the subset of the GeoIP2-City schema we surface. Both
// DB-IP City Lite and IP2Location LITE DB9 ship in this exact schema (the
// latter even reports DatabaseType "GeoLite2-City"), so one decoder serves both.
type cityRecord struct {
	Country struct {
		ISOCode string `maxminddb:"iso_code"`
	} `maxminddb:"country"`
	Subdivisions []struct {
		ISOCode string `maxminddb:"iso_code"`
	} `maxminddb:"subdivisions"`
	City struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"city"`
	Location struct {
		Latitude  float64 `maxminddb:"latitude"`
		Longitude float64 `maxminddb:"longitude"`
		TimeZone  string  `maxminddb:"time_zone"`
	} `maxminddb:"location"`
	Postal struct {
		Code string `maxminddb:"code"`
	} `maxminddb:"postal"`
}

// MMDBCitySource is a GeoIP2-City-schema MMDB geolocation source. It is mmap'd
// by the reader, so memory use is negligible regardless of database size. Used
// for both DB-IP City Lite and IP2Location LITE DB9; instances differ only in
// their reported source identity and attribution string.
type MMDBCitySource struct {
	db          *maxminddb.Reader
	kind        pb.GeoSource
	attribution string
	path        string
}

// openMMDBCity opens a GeoIP2-City-schema MMDB at path.
func openMMDBCity(path string, kind pb.GeoSource, attribution string) (*MMDBCitySource, error) {
	db, err := maxminddb.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open MMDB %s: %w", path, err)
	}
	return &MMDBCitySource{db: db, kind: kind, attribution: attribution, path: path}, nil
}

func (s *MMDBCitySource) Kind() pb.GeoSource { return s.kind }

// Close releases the underlying memory-mapped database.
func (s *MMDBCitySource) Close() error { return s.db.Close() }

func (s *MMDBCitySource) Lookup(_ context.Context, ip netip.Addr) (*pb.GeoSourceResult, error) {
	res := s.db.Lookup(ip)
	if err := res.Err(); err != nil {
		return nil, err
	}
	if !res.Found() {
		return nil, nil
	}
	var rec cityRecord
	if err := res.Decode(&rec); err != nil {
		return nil, err
	}

	loc := &pb.GeoLocation{
		Country:    rec.Country.ISOCode,
		City:       rec.City.Names["en"],
		PostalCode: rec.Postal.Code,
		TimeZone:   rec.Location.TimeZone,
	}
	// Build an ISO 3166-2 region from country + the most-specific subdivision.
	if n := len(rec.Subdivisions); n > 0 && rec.Subdivisions[n-1].ISOCode != "" {
		sub := rec.Subdivisions[n-1].ISOCode
		if loc.Country != "" {
			loc.Region = loc.Country + "-" + sub
		} else {
			loc.Region = sub
		}
	}
	// Treat (0,0) as "no coordinates" — it is Null Island, not a real estimate.
	if rec.Location.Latitude != 0 || rec.Location.Longitude != 0 {
		lat, lon := rec.Location.Latitude, rec.Location.Longitude
		loc.Latitude = &lat
		loc.Longitude = &lon
		loc.Granularity = pb.GeoGranularity_GEO_GRANULARITY_COORDINATES
	} else {
		loc.Granularity = granularityFromFields(loc)
	}

	return &pb.GeoSourceResult{
		Source:        s.kind,
		Location:      loc,
		MatchedPrefix: res.Prefix().String(),
		Authoritative: false,
		Attribution:   s.attribution,
	}, nil
}
