package geoip

import (
	pb "github.com/accretional/proto-ip/proto/ippb"
)

// DBIPAttribution is the credit string required by the DB-IP City Lite
// CC BY 4.0 license wherever results are displayed.
const DBIPAttribution = "IP Geolocation by DB-IP (https://db-ip.com), CC BY 4.0"

// NewDBIPSource opens the DB-IP City Lite MMDB at path. DB-IP ships in the
// GeoIP2-City schema, so it is served by the shared MMDBCitySource.
// FindDBIPDatabase (in cache.go) locates the newest cached file.
func NewDBIPSource(path string) (*MMDBCitySource, error) {
	return openMMDBCity(path, pb.GeoSource_GEO_SOURCE_DBIP_LITE, DBIPAttribution)
}
