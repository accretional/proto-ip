package geoip

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"math/big"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	pb "github.com/accretional/proto-ip/proto/ippb"
)

// IP2LocationAttribution is the credit required by the IP2Location LITE
// CC BY-SA 4.0 license wherever results are displayed.
const IP2LocationAttribution = "IP geolocation by IP2Location LITE (https://lite.ip2location.com), CC BY-SA 4.0"

// Cache filenames for the (opt-in) IP2Location LITE DB5 CSVs.
const (
	IP2LocationV4File = "ip2location-lite-db5.csv"
	IP2LocationV6File = "ip2location-lite-db5-ipv6.csv"
)

// ip2Loc is the subset of an IP2Location row we surface.
type ip2Loc struct {
	country string // ISO 3166-1 alpha-2
	city    string
	lat     float64
	lon     float64
}

// ip2Range is one CSV row: an inclusive address range and its location.
type ip2Range struct {
	start netip.Addr
	end   netip.Addr
	loc   ip2Loc
}

// IP2LocationSource answers from the IP2Location LITE DB5 CSV. The CSV uses
// inclusive integer address ranges (not CIDRs), so ranges are held sorted and
// resolved by binary search. v4 and v6 are kept in separate slices and chosen
// by the query's family.
type IP2LocationSource struct {
	v4 []ip2Range
	v6 []ip2Range
}

// FindIP2LocationDatabases reports the cached DB5 CSV paths in dir. ok is true
// when at least one family is present (the source is opt-in, so absence is
// normal). A missing family yields an empty path and is simply not loaded.
func FindIP2LocationDatabases(dir string) (v4, v6 string, ok bool) {
	if p := filepath.Join(dir, IP2LocationV4File); fileExists(p) {
		v4 = p
	}
	if p := filepath.Join(dir, IP2LocationV6File); fileExists(p) {
		v6 = p
	}
	return v4, v6, v4 != "" || v6 != ""
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// NewIP2LocationSource loads whichever of the two CSV paths are non-empty.
func NewIP2LocationSource(v4Path, v6Path string) (*IP2LocationSource, error) {
	s := &IP2LocationSource{}
	if v4Path != "" {
		r, err := loadIP2LocationFile(v4Path, false)
		if err != nil {
			return nil, err
		}
		s.v4 = r
	}
	if v6Path != "" {
		r, err := loadIP2LocationFile(v6Path, true)
		if err != nil {
			return nil, err
		}
		s.v6 = r
	}
	if len(s.v4) == 0 && len(s.v6) == 0 {
		return nil, fmt.Errorf("IP2Location: no usable rows in %q/%q", v4Path, v6Path)
	}
	return s, nil
}

func loadIP2LocationFile(path string, v6 bool) ([]ip2Range, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rs, err := parseIP2LocationCSV(f, v6)
	if err != nil {
		return nil, fmt.Errorf("parse IP2Location %s: %w", path, err)
	}
	return rs, nil
}

// parseIP2LocationCSV reads an IP2Location LITE DB5 CSV. Columns are:
//
//	ip_from,ip_to,country_code,country_name,region_name,city_name,latitude,longitude
//
// All fields are double-quoted; ip_from/ip_to are decimal integers (32-bit for
// the IPv4 file, up to 128-bit for the IPv6 file). region_name is a name, not
// an ISO 3166-2 code, so it is dropped (left for DB-IP/geofeeds to supply).
func parseIP2LocationCSV(r io.Reader, v6 bool) ([]ip2Range, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1 // DB5 has 8; tolerate DB11's 10
	cr.ReuseRecord = true

	// country codes (~250) and city names (tens of thousands) repeat across
	// millions of rows; intern them so each distinct value is stored once.
	pool := make(map[string]string)
	intern := func(s string) string {
		if s == "" || s == "-" { // "-" is IP2Location's "unknown" placeholder
			return ""
		}
		if v, ok := pool[s]; ok {
			return v
		}
		pool[s] = s
		return s
	}

	var out []ip2Range
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(rec) < 8 {
			continue
		}
		start, ok1 := decimalToAddr(rec[0], v6)
		end, ok2 := decimalToAddr(rec[1], v6)
		if !ok1 || !ok2 {
			continue
		}
		lat, _ := strconv.ParseFloat(rec[6], 64)
		lon, _ := strconv.ParseFloat(rec[7], 64)
		out = append(out, ip2Range{
			start: start,
			end:   end,
			loc:   ip2Loc{country: intern(rec[2]), city: intern(rec[5]), lat: lat, lon: lon},
		})
	}
	// The CSV ships sorted by ip_from, but sort defensively so the binary
	// search is correct regardless.
	sort.Slice(out, func(i, j int) bool { return out[i].start.Less(out[j].start) })
	return out, nil
}

// decimalToAddr converts an IP2Location decimal integer to a netip.Addr.
func decimalToAddr(s string, v6 bool) (netip.Addr, bool) {
	if !v6 {
		n, err := strconv.ParseUint(s, 10, 32)
		if err != nil {
			return netip.Addr{}, false
		}
		return netip.AddrFrom4([4]byte{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}), true
	}
	bi, ok := new(big.Int).SetString(s, 10)
	if !ok || bi.Sign() < 0 || bi.BitLen() > 128 {
		return netip.Addr{}, false
	}
	var b [16]byte
	bi.FillBytes(b[:]) // big-endian, left-zero-padded
	return netip.AddrFrom16(b), true
}

func (s *IP2LocationSource) Kind() pb.GeoSource {
	return pb.GeoSource_GEO_SOURCE_IP2LOCATION_LITE
}

// Summary reports loaded range counts, for startup logging.
func (s *IP2LocationSource) Summary() string {
	return fmt.Sprintf("v4=%d v6=%d ranges", len(s.v4), len(s.v6))
}

func (s *IP2LocationSource) Lookup(_ context.Context, ip netip.Addr) (*pb.GeoSourceResult, error) {
	rs := s.v6
	if ip.Is4() {
		rs = s.v4
	}
	rng, ok := searchRange(rs, ip)
	if !ok {
		return nil, nil
	}
	// Many ranges in the LITE data are all-"unknown" (country/city blank,
	// coordinates 0,0). Treat such a match as "no data" rather than emitting an
	// empty result that adds noise to the merged response.
	if rng.loc.country == "" && rng.loc.city == "" && rng.loc.lat == 0 && rng.loc.lon == 0 {
		return nil, nil
	}

	loc := &pb.GeoLocation{
		Country: rng.loc.country,
		City:    rng.loc.city,
	}
	// IP2Location uses 0,0 for "unknown" — treat it as no coordinates.
	if rng.loc.lat != 0 || rng.loc.lon != 0 {
		lat, lon := rng.loc.lat, rng.loc.lon
		loc.Latitude = &lat
		loc.Longitude = &lon
		loc.Granularity = pb.GeoGranularity_GEO_GRANULARITY_COORDINATES
	} else {
		loc.Granularity = granularityFromFields(loc)
	}

	return &pb.GeoSourceResult{
		Source:        pb.GeoSource_GEO_SOURCE_IP2LOCATION_LITE,
		Location:      loc,
		MatchedPrefix: fmt.Sprintf("%s-%s", rng.start, rng.end), // a range, not a CIDR
		Authoritative: false,
		Attribution:   IP2LocationAttribution,
	}, nil
}

// searchRange finds the range covering ip in a slice sorted ascending by start
// (ranges are non-overlapping).
func searchRange(rs []ip2Range, ip netip.Addr) (ip2Range, bool) {
	// First index whose start is strictly greater than ip; the candidate is the
	// one before it.
	i := sort.Search(len(rs), func(i int) bool { return ip.Less(rs[i].start) })
	if i == 0 {
		return ip2Range{}, false
	}
	cand := rs[i-1]
	if !ip.Less(cand.start) && !cand.end.Less(ip) { // cand.start <= ip <= cand.end
		return cand, true
	}
	return ip2Range{}, false
}
