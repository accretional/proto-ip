package geoip

import (
	"fmt"
	"os"
	"path/filepath"

	pb "github.com/accretional/proto-ip/proto/ippb"
)

// IP2LocationAttribution is the credit required by the IP2Location LITE
// CC BY-SA 4.0 license wherever results are displayed.
const IP2LocationAttribution = "IP geolocation by IP2Location LITE (https://lite.ip2location.com), CC BY-SA 4.0"

// IP2LocationFile is the on-disk name of the cached IP2Location LITE DB9 MMDB.
const IP2LocationFile = "ip2location-lite-db9.mmdb"

// NewIP2LocationSource opens the IP2Location LITE DB9 MMDB at path. The DB9
// MMDB ships in the GeoIP2-City schema (its metadata even reports DatabaseType
// "GeoLite2-City"), so it is served by the shared MMDBCitySource — a single
// mmap'd file covering both IPv4 and IPv6 with negligible memory use.
func NewIP2LocationSource(path string) (*MMDBCitySource, error) {
	return openMMDBCity(path, pb.GeoSource_GEO_SOURCE_IP2LOCATION_LITE, IP2LocationAttribution)
}

// FindIP2LocationDatabase returns the cached DB9 MMDB path in dir. The source
// is opt-in, so a missing file is normal (the caller simply skips it).
func FindIP2LocationDatabase(dir string) (string, error) {
	p := filepath.Join(dir, IP2LocationFile)
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("IP2Location LITE DB9 MMDB not found in %s", dir)
	}
	return p, nil
}
