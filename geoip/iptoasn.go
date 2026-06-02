package geoip

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	pb "github.com/accretional/proto-ip/proto/ippb"
)

// IPtoASNAttribution credits the iptoasn.com dataset (public domain).
const IPtoASNAttribution = "BGP origin-AS data from iptoasn.com (RouteViews/RIPE RIS), Public Domain (PDDL)"

// Cache filenames for the (gunzipped) iptoasn TSV tables.
const (
	IPtoASNv4File = "ip2asn-v4.tsv"
	IPtoASNv6File = "ip2asn-v6.tsv"
)

// asnRange is one iptoasn row: an inclusive address range mapped to an origin
// AS, with the registry country and AS description.
type asnRange struct {
	start   netip.Addr
	end     netip.Addr
	asn     uint32
	country string
	network string
}

// IPtoASNSource answers from the iptoasn.com IP-to-ASN tables (derived from
// public BGP route collectors). It contributes a country floor plus origin-AS
// metadata; it carries no coordinates. Ranges are held per-family sorted by
// start and resolved by binary search.
type IPtoASNSource struct {
	v4 []asnRange
	v6 []asnRange
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// FindIPtoASNDatabases reports the cached TSV paths in dir. ok is true when at
// least one family is present.
func FindIPtoASNDatabases(dir string) (v4, v6 string, ok bool) {
	if p := filepath.Join(dir, IPtoASNv4File); fileExists(p) {
		v4 = p
	}
	if p := filepath.Join(dir, IPtoASNv6File); fileExists(p) {
		v6 = p
	}
	return v4, v6, v4 != "" || v6 != ""
}

// NewIPtoASNSource loads whichever of the two TSV paths are non-empty.
func NewIPtoASNSource(v4Path, v6Path string) (*IPtoASNSource, error) {
	s := &IPtoASNSource{}
	if v4Path != "" {
		r, err := loadIPtoASNFile(v4Path)
		if err != nil {
			return nil, err
		}
		s.v4 = r
	}
	if v6Path != "" {
		r, err := loadIPtoASNFile(v6Path)
		if err != nil {
			return nil, err
		}
		s.v6 = r
	}
	if len(s.v4) == 0 && len(s.v6) == 0 {
		return nil, fmt.Errorf("iptoasn: no usable rows in %q/%q", v4Path, v6Path)
	}
	return s, nil
}

func loadIPtoASNFile(path string) ([]asnRange, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rs, err := parseIPtoASN(f)
	if err != nil {
		return nil, fmt.Errorf("parse iptoasn %s: %w", path, err)
	}
	return rs, nil
}

// parseIPtoASN reads an iptoasn TSV. Columns (tab-separated, text IPs):
//
//	range_start  range_end  AS_number  country_code  AS_description
//
// Rows with AS_number 0 (country "None", description "Not routed") are skipped.
func parseIPtoASN(r io.Reader) ([]asnRange, error) {
	pool := make(map[string]string)
	intern := func(s string) string {
		if s == "" {
			return ""
		}
		if v, ok := pool[s]; ok {
			return v
		}
		pool[s] = s
		return s
	}

	var out []asnRange
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 5 {
			continue
		}
		asn, err := strconv.ParseUint(f[2], 10, 32)
		if err != nil || asn == 0 { // 0 == not routed / unknown
			continue
		}
		start, err1 := netip.ParseAddr(f[0])
		end, err2 := netip.ParseAddr(f[1])
		if err1 != nil || err2 != nil {
			continue
		}
		country := f[3]
		if country == "None" {
			country = ""
		}
		out = append(out, asnRange{
			start:   start,
			end:     end,
			asn:     uint32(asn),
			country: intern(country),
			network: intern(f[4]),
		})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].start.Less(out[j].start) })
	return out, nil
}

func (s *IPtoASNSource) Kind() pb.GeoSource { return pb.GeoSource_GEO_SOURCE_IPTOASN }

// Summary reports loaded range counts, for startup logging.
func (s *IPtoASNSource) Summary() string {
	return fmt.Sprintf("v4=%d v6=%d ranges", len(s.v4), len(s.v6))
}

func (s *IPtoASNSource) Lookup(_ context.Context, ip netip.Addr) (*pb.GeoSourceResult, error) {
	rs := s.v6
	if ip.Is4() {
		rs = s.v4
	}
	rng, ok := searchASNRange(rs, ip)
	if !ok {
		return nil, nil
	}

	loc := &pb.GeoLocation{Country: rng.country}
	loc.Granularity = granularityFromFields(loc) // COUNTRY when country set, else UNKNOWN

	return &pb.GeoSourceResult{
		Source:        pb.GeoSource_GEO_SOURCE_IPTOASN,
		Location:      loc,
		MatchedPrefix: fmt.Sprintf("%s-%s", rng.start, rng.end),
		Authoritative: false,
		Attribution:   IPtoASNAttribution,
		Confidence:    pb.GeoConfidence_GEO_CONFIDENCE_LOW, // coarse country floor
		Asn:           rng.asn,
		Network:       rng.network,
	}, nil
}

// searchASNRange finds the range covering ip in a slice sorted ascending by
// start (ranges are non-overlapping).
func searchASNRange(rs []asnRange, ip netip.Addr) (asnRange, bool) {
	i := sort.Search(len(rs), func(i int) bool { return ip.Less(rs[i].start) })
	if i == 0 {
		return asnRange{}, false
	}
	cand := rs[i-1]
	if !ip.Less(cand.start) && !cand.end.Less(ip) { // cand.start <= ip <= cand.end
		return cand, true
	}
	return asnRange{}, false
}
