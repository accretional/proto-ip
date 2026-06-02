package geoip

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	pb "github.com/accretional/proto-ip/proto/ippb"
)

// RPKIAttribution credits the public rpki-client.org VRP dump.
const RPKIAttribution = "RPKI VRPs from rpki-client.org (public)"

// RPKIVRPFile is the cached rpki-client VRP dump (downloaded by setup.sh).
const RPKIVRPFile = "rpki-vrps.json"

// vrp is one Validated ROA Payload: an authorized (prefix, maxLength, asn).
type vrp struct {
	prefix netip.Prefix
	maxLen int
	asn    uint32
}

// RPKISet answers RFC 6811 route-origin validation queries from a loaded VRP
// set. Like AnycastSet it is not a geolocation Source: it contributes to the
// reliable NetworkInfo spine, not to a physical location. VRPs are bucketed by
// family and keyed by their (masked) prefix so covering ROAs can be found by
// probing every prefix length of the queried address.
type RPKISet struct {
	v4 map[netip.Prefix][]vrp
	v6 map[netip.Prefix][]vrp
	n  int
}

// FindRPKIDatabase reports the cached VRP dump path in dir, or an error if it
// is absent.
func FindRPKIDatabase(dir string) (string, error) {
	p := filepath.Join(dir, RPKIVRPFile)
	if !fileExists(p) {
		return "", fmt.Errorf("rpki: %s not found", p)
	}
	return p, nil
}

// NewRPKISet loads and indexes the rpki-client VRP dump at path.
func NewRPKISet(path string) (*RPKISet, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseVRPs(f)
}

// vrpJSON mirrors one element of the rpki-client vrps.json "roas" array. The
// asn field is decoded raw because rpki-client has shipped it both as a JSON
// number (13335) and as a string ("AS13335") across versions.
type vrpJSON struct {
	Prefix    string          `json:"prefix"`
	MaxLength int             `json:"maxLength"`
	ASN       json.RawMessage `json:"asn"`
}

// parseVRPs streams the top-level "roas" array of an rpki-client vrps.json,
// decoding one ROA at a time to bound memory on the large (tens of MB) file.
func parseVRPs(r io.Reader) (*RPKISet, error) {
	s := &RPKISet{
		v4: make(map[netip.Prefix][]vrp),
		v6: make(map[netip.Prefix][]vrp),
	}
	dec := json.NewDecoder(bufio.NewReaderSize(r, 1<<20))

	// Walk tokens looking for a "roas" key whose value is an array, then decode
	// its elements. NOTE: rpki-client's vrps.json also has a "roas" key *inside*
	// "metadata" whose value is an integer count (e.g. 372447) — we must skip
	// that and keep scanning for the real top-level array.
	for {
		t, err := dec.Token()
		if err == io.EOF {
			return s, nil // no roas array; empty set
		}
		if err != nil {
			return nil, fmt.Errorf("rpki: %w", err)
		}
		key, ok := t.(string)
		if !ok || key != "roas" {
			continue
		}
		// Inspect this "roas" value: the ROA payload is an array; metadata's
		// "roas" count is a scalar, which we skip by continuing the scan.
		d, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("rpki: %w", err)
		}
		if delim, ok := d.(json.Delim); !ok || delim != '[' {
			continue // not the array (e.g. metadata's roas count)
		}
		for dec.More() {
			var e vrpJSON
			if err := dec.Decode(&e); err != nil {
				return nil, fmt.Errorf("rpki: decode ROA: %w", err)
			}
			s.add(e)
		}
		return s, nil
	}
}

// add indexes one parsed ROA, skipping malformed rows.
func (s *RPKISet) add(e vrpJSON) {
	asn, ok := parseVRPASN(e.ASN)
	if !ok {
		return
	}
	pfx, err := netip.ParsePrefix(e.Prefix)
	if err != nil {
		return
	}
	pfx = pfx.Masked()
	v := vrp{prefix: pfx, maxLen: e.MaxLength, asn: asn}
	m := s.v6
	if pfx.Addr().Is4() {
		m = s.v4
	}
	m[pfx] = append(m[pfx], v)
	s.n++
}

// parseVRPASN accepts the asn field as a JSON number (13335) or a string
// ("AS13335" / "13335").
func parseVRPASN(raw json.RawMessage) (uint32, bool) {
	t := strings.TrimSpace(string(raw))
	t = strings.Trim(t, `"`)
	t = strings.TrimPrefix(t, "AS")
	t = strings.TrimPrefix(t, "as")
	n, err := strconv.ParseUint(t, 10, 32)
	if err != nil {
		return 0, false
	}
	return uint32(n), true
}

// Len reports the number of loaded VRPs, for startup logging.
func (s *RPKISet) Len() int { return s.n }

// Validate returns the RFC 6811 route-origin validation verdict for addr given
// its observed origin ASN, plus the ROAs covering the address (for
// transparency). originASN of 0 (unknown origin) yields UNKNOWN.
//
// Approximation: proper validation uses the *announced* prefix length, which we
// do not have (the origin ASN comes from iptoasn's aggregated ranges). We
// therefore validate on the covering ROA's authorized ASN only; the maxLength
// constraint is not enforced. See docs/impl-notes.md.
func (s *RPKISet) Validate(addr netip.Addr, originASN uint32) (pb.RPKIStatus, []vrp) {
	m := s.v6
	if addr.Is4() {
		m = s.v4
	}
	var covering []vrp
	for plen := 0; plen <= addr.BitLen(); plen++ {
		p, err := addr.Prefix(plen)
		if err != nil {
			continue
		}
		if vs, ok := m[p]; ok {
			covering = append(covering, vs...)
		}
	}

	if len(covering) == 0 {
		return pb.RPKIStatus_RPKI_STATUS_NOT_FOUND, nil
	}
	if originASN == 0 {
		return pb.RPKIStatus_RPKI_STATUS_UNKNOWN, covering
	}
	for _, v := range covering {
		if v.asn == originASN {
			return pb.RPKIStatus_RPKI_STATUS_VALID, covering
		}
	}
	return pb.RPKIStatus_RPKI_STATUS_INVALID, covering
}

// vrpsToProto converts covering VRPs to their proto form for the response.
func vrpsToProto(vs []vrp) []*pb.RpkiRoa {
	if len(vs) == 0 {
		return nil
	}
	out := make([]*pb.RpkiRoa, 0, len(vs))
	for _, v := range vs {
		out = append(out, &pb.RpkiRoa{
			Prefix:    v.prefix.String(),
			MaxLength: uint32(v.maxLen),
			Asn:       v.asn,
		})
	}
	return out
}
