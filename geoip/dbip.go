package geoip

import (
	"context"
	"fmt"
	"net/netip"

	"github.com/oschwald/maxminddb-golang/v2"

	pb "github.com/accretional/proto-ip/proto/ippb"
)

// DBIPAttribution is the credit string required by the DB-IP City Lite
// CC BY 4.0 license wherever results are displayed.
const DBIPAttribution = "IP Geolocation by DB-IP (https://db-ip.com), CC BY 4.0"

// DBIPSource resolves coordinates and city data from a DB-IP City Lite MMDB.
type DBIPSource struct {
	db   *maxminddb.Reader
	path string
}

// dbipRecord mirrors the subset of the GeoIP2-City schema that DB-IP City
// Lite populates.
type dbipRecord struct {
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

// NewDBIPSource opens the MMDB at path.
func NewDBIPSource(path string) (*DBIPSource, error) {
	db, err := maxminddb.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open DB-IP database %s: %w", path, err)
	}
	return &DBIPSource{db: db, path: path}, nil
}

func (s *DBIPSource) Kind() pb.GeoSource { return pb.GeoSource_GEO_SOURCE_DBIP_LITE }

// Close releases the underlying memory-mapped database.
func (s *DBIPSource) Close() error { return s.db.Close() }

func (s *DBIPSource) Lookup(_ context.Context, ip netip.Addr) (*pb.GeoSourceResult, error) {
	res := s.db.Lookup(ip)
	if err := res.Err(); err != nil {
		return nil, err
	}
	if !res.Found() {
		return nil, nil
	}
	var rec dbipRecord
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
		Source:        pb.GeoSource_GEO_SOURCE_DBIP_LITE,
		Location:      loc,
		MatchedPrefix: res.Prefix().String(),
		Authoritative: false,
		Attribution:   DBIPAttribution,
	}, nil
}
