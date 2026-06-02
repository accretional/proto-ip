package geoip

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"time"

	"github.com/accretional/proto-ip/rdap"

	pb "github.com/accretional/proto-ip/proto/ippb"
)

// GeofeedAttribution credits the operator-published nature of geofeed data.
const GeofeedAttribution = "Geofeed (RFC 8805), self-published by the network operator"

// defaultGeofeedTTL is how long a fetched geofeed is reused before refetching.
const defaultGeofeedTTL = time.Hour

// geofeedURLRe extracts a geofeed URL advertised in an RDAP/whois object per
// RFC 9632. It matches all common forms across channels:
//   - the RPSL `geofeed:        https://example.net/geofeed.csv` attribute
//   - a `remarks: Geofeed https://…` line (ARIN-style)
//   - the bracketed `Geofeed <https://…>` form
// Angle-bracket and quote/JSON delimiters are excluded from the captured URL.
var geofeedURLRe = regexp.MustCompile(`(?i)geofeed[\s:<]+\s*(https?://[^\s"'<>,\]}]+)`)

// GeofeedSource discovers an RFC 8805 geofeed for an address via RDAP
// (RFC 9632), fetches and caches the CSV, and returns the longest-prefix match.
type GeofeedSource struct {
	rdap  *rdap.Client
	http  *http.Client
	cache *geofeedCache
}

// NewGeofeedSource builds a geofeed source that discovers feeds through rc.
func NewGeofeedSource(rc *rdap.Client) *GeofeedSource {
	return &GeofeedSource{
		rdap:  rc,
		http:  &http.Client{Timeout: 20 * time.Second},
		cache: newGeofeedCache(defaultGeofeedTTL),
	}
}

func (g *GeofeedSource) Kind() pb.GeoSource { return pb.GeoSource_GEO_SOURCE_GEOFEED }

func (g *GeofeedSource) Lookup(ctx context.Context, ip netip.Addr) (*pb.GeoSourceResult, error) {
	url, err := g.discover(ctx, ip)
	if err != nil {
		return nil, err
	}
	if url == "" {
		return nil, nil // no geofeed published for this network
	}

	recs, ok := g.cache.get(url)
	if !ok {
		recs, err = g.fetch(ctx, url)
		if err != nil {
			return nil, err
		}
		g.cache.put(url, recs)
	}

	rec, found := longestMatch(recs, ip)
	if !found {
		return nil, nil
	}

	loc := &pb.GeoLocation{
		Country:    rec.Country,
		Region:     rec.Region,
		City:       rec.City,
		PostalCode: rec.Postal,
	}
	loc.Granularity = granularityFromFields(loc)

	return &pb.GeoSourceResult{
		Source:        pb.GeoSource_GEO_SOURCE_GEOFEED,
		Location:      loc,
		MatchedPrefix: rec.Prefix.String(),
		Authoritative: true,
		Attribution:   GeofeedAttribution,
		Confidence:    pb.GeoConfidence_GEO_CONFIDENCE_HIGH, // operator self-published
	}, nil
}

// discover finds the geofeed URL advertised for ip per RFC 9632. An empty
// string (with nil error) means no geofeed is published.
//
// Two channels are tried, because RIRs differ:
//  1. The RDAP response body — ARIN and others surface a `Geofeed <url>`
//     remark inline in JSON.
//  2. RPSL whois (port 43) — RFC 9632's normative location is the
//     inetnum `geofeed:` attribute, which RIPE/APNIC serve over whois and
//     do NOT echo into RDAP. RDAP conveniently advertises the whois host in
//     its `port43` field, so we reuse the RDAP lookup we already did.
func (g *GeofeedSource) discover(ctx context.Context, ip netip.Addr) (string, error) {
	resp, err := g.rdap.LookupIP(ctx, protoFromAddr(ip))
	if err != nil {
		return "", fmt.Errorf("rdap discovery: %w", err)
	}
	raw := resp.GetRawJson()

	// Channel 1: geofeed reference embedded in the RDAP body.
	if m := geofeedURLRe.FindStringSubmatch(raw); m != nil {
		return m[1], nil
	}

	// Channel 2: the RPSL geofeed: attribute via whois.
	var meta struct {
		Port43 string `json:"port43"`
	}
	_ = json.Unmarshal([]byte(raw), &meta)
	if meta.Port43 == "" {
		return "", nil
	}
	text, err := whoisQuery(ctx, meta.Port43, ip.String())
	if err != nil {
		return "", fmt.Errorf("whois discovery: %w", err)
	}
	if m := geofeedURLRe.FindStringSubmatch(text); m != nil {
		return m[1], nil
	}
	return "", nil
}

// whoisQuery performs a single RFC 3912 whois request and returns the raw
// response text. The context's deadline bounds the whole exchange.
func whoisQuery(ctx context.Context, server, query string) (string, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(server, "43"))
	if err != nil {
		return "", err
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	if _, err := io.WriteString(conn, query+"\r\n"); err != nil {
		return "", err
	}
	b, err := io.ReadAll(io.LimitReader(conn, 1<<20))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// fetch downloads and parses the geofeed CSV at url.
func (g *GeofeedSource) fetch(ctx context.Context, url string) ([]GeofeedRecord, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := g.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("geofeed GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("geofeed %s: HTTP %d", url, resp.StatusCode)
	}
	// Cap the body to a sane size to avoid a hostile feed exhausting memory.
	return ParseGeofeed(io.LimitReader(resp.Body, 64*1024*1024))
}

// protoFromAddr converts a netip.Addr to the proto-ip IP wire form (two
// sint64 halves of the 128-bit IPv4-mapped IPv6 value), matching
// rdap.ipFromProto's reconstruction.
func protoFromAddr(ip netip.Addr) *pb.IP {
	a := ip.As16() // IPv4 addresses become their ::ffff:0:0/96 mapped form.
	return &pb.IP{
		NetworkPrefix:       int64(binary.BigEndian.Uint64(a[0:8])),
		InterfaceIdentifier: int64(binary.BigEndian.Uint64(a[8:16])),
	}
}
