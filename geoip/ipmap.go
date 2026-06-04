package geoip

import (
	"bufio"
	"compress/bzip2"
	"context"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	pb "github.com/accretional/proto-ip/proto/ippb"
)

// IPMapAttribution credits RIPE IPmap. Its data is published under the RIPE
// NCC Terms of Service (not a Creative Commons license).
const IPMapAttribution = "Infrastructure geolocation by RIPE IPmap (https://ipmap.ripe.net), RIPE NCC ToS"

// IPMapCacheFile is the on-disk name of the cached RIPE IPmap daily dump
// (kept bzip2-compressed; decoded in-process at load).
const IPMapCacheFile = "ipmap-geolocations-latest.csv.bz2"

// ipmapEntry is the subset of an IPmap row we surface.
type ipmapEntry struct {
	city    string
	country string // ISO 3166-1 alpha-2
	lat     float64
	lon     float64
}

// IPMapSource answers from the RIPE IPmap daily dump: measured locations for
// core Internet infrastructure, keyed by exact address (the dump contains only
// /32 and /128 prefixes). It therefore returns data only when the queried IP
// is itself a known node, but for those IPs the data is measured rather than
// estimated.
type IPMapSource struct {
	byAddr map[netip.Addr]ipmapEntry
}

// FindIPMapDatabase returns the path to the cached IPmap dump in dir.
func FindIPMapDatabase(dir string) (string, error) {
	p := filepath.Join(dir, IPMapCacheFile)
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("RIPE IPmap dump not found in %s", dir)
	}
	return p, nil
}

// NewIPMapSource loads the bzip2'd IPmap dump at path into memory.
func NewIPMapSource(path string) (*IPMapSource, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	m, err := parseIPMap(bzip2.NewReader(f))
	if err != nil {
		return nil, fmt.Errorf("parse RIPE IPmap %s: %w", path, err)
	}
	return &IPMapSource{byAddr: m}, nil
}

// parseIPMap reads the IPmap CSV. Columns are:
//
//	prefix,geolocation_id,city,state,country_name,cc_alpha2,cc_alpha3,lat,lon,score
//
// country_name is unquoted and may itself contain commas (e.g. "Bonaire,
// Saint Eustatius and Saba"), so the trailing numeric/code columns are read by
// offset from the END of the row, which is unambiguous; city stays at index 2
// (geolocation_id never contains a comma).
func parseIPMap(r io.Reader) (map[netip.Addr]ipmapEntry, error) {
	m := make(map[netip.Addr]ipmapEntry, 1<<19)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		f := strings.Split(line, ",")
		if len(f) < 9 {
			continue
		}
		n := len(f)
		pfx, err := netip.ParsePrefix(strings.TrimSpace(f[0]))
		if err != nil {
			continue
		}
		lat, err1 := strconv.ParseFloat(strings.TrimSpace(f[n-3]), 64)
		lon, err2 := strconv.ParseFloat(strings.TrimSpace(f[n-2]), 64)
		if err1 != nil || err2 != nil {
			continue
		}
		m[pfx.Addr()] = ipmapEntry{
			city:    strings.TrimSpace(f[2]),
			country: strings.TrimSpace(f[n-5]), // cc_alpha2
			lat:     lat,
			lon:     lon,
		}
	}
	return m, sc.Err()
}

// Len reports how many addresses were loaded (used by tests/diagnostics).
func (s *IPMapSource) Len() int { return len(s.byAddr) }

func (s *IPMapSource) Kind() pb.GeoSource { return pb.GeoSource_GEO_SOURCE_IPMAP }

func (s *IPMapSource) Lookup(_ context.Context, ip netip.Addr) (*pb.GeoSourceResult, error) {
	e, ok := s.byAddr[ip]
	if !ok {
		return nil, nil // IPmap only knows infrastructure addresses
	}
	lat, lon := e.lat, e.lon
	loc := &pb.GeoLocation{
		Latitude:  &lat,
		Longitude: &lon,
		Country:   e.country,
		City:      e.city,
		// IPmap's "state" is a name, not an ISO 3166-2 code, so region is left
		// for DB-IP / geofeeds to supply.
		Granularity: pb.GeoGranularity_GEO_GRANULARITY_COORDINATES,
	}
	return &pb.GeoSourceResult{
		Source:        pb.GeoSource_GEO_SOURCE_IPMAP,
		Location:      loc,
		MatchedPrefix: netip.PrefixFrom(ip, ip.BitLen()).String(),
		Authoritative: false,
		Attribution:   IPMapAttribution,
		Confidence:    pb.GeoConfidence_GEO_CONFIDENCE_HIGH, // measured (RIPE Atlas)
	}, nil
}
