package geoip

import (
	"bufio"
	"io"
	"net/netip"
	"strings"
)

// GeofeedRecord is one row of an RFC 8805 geofeed: a prefix and the
// administrative location fields that apply to it. Geofeeds never carry
// coordinates (that is by design in RFC 8805).
type GeofeedRecord struct {
	Prefix  netip.Prefix
	Country string // ISO 3166-1 alpha-2
	Region  string // ISO 3166-2
	City    string
	Postal  string
}

// ParseGeofeed parses an RFC 8805 self-published geolocation feed.
//
// The format is CSV with up to five columns:
//
//	ip_prefix,country_code,region_code,city,postal_code
//
// Per RFC 8805: the file is UTF-8; text from a '#' to end-of-line is a
// comment and is ignored; blank lines are ignored. We additionally tolerate
// missing trailing fields and skip any row whose prefix does not parse,
// since a single malformed line should not discard an otherwise valid feed.
func ParseGeofeed(r io.Reader) ([]GeofeedRecord, error) {
	var recs []GeofeedRecord
	sc := bufio.NewScanner(r)
	// Geofeeds can be large; raise the line buffer well above the default 64 KiB.
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		// Strip an inline comment ("from a '#' character to end of line").
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, ",")
		pfx, err := netip.ParsePrefix(strings.TrimSpace(fields[0]))
		if err != nil {
			continue
		}
		rec := GeofeedRecord{Prefix: pfx.Masked()}
		if len(fields) > 1 {
			rec.Country = strings.TrimSpace(fields[1])
		}
		if len(fields) > 2 {
			rec.Region = strings.TrimSpace(fields[2])
		}
		if len(fields) > 3 {
			rec.City = strings.TrimSpace(fields[3])
		}
		if len(fields) > 4 {
			rec.Postal = strings.TrimSpace(fields[4])
		}
		recs = append(recs, rec)
	}
	return recs, sc.Err()
}

// longestMatch returns the most-specific (longest-prefix) record covering ip.
func longestMatch(recs []GeofeedRecord, ip netip.Addr) (GeofeedRecord, bool) {
	best := GeofeedRecord{}
	bestBits := -1
	for _, rec := range recs {
		if !rec.Prefix.Contains(ip) {
			continue
		}
		if rec.Prefix.Bits() > bestBits {
			bestBits = rec.Prefix.Bits()
			best = rec
		}
	}
	return best, bestBits >= 0
}
