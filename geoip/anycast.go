package geoip

import (
	"bufio"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
)

// Cache filenames for the bgp.tools anycast prefix lists.
const (
	AnycastV4File = "anycast-v4-prefixes.txt"
	AnycastV6File = "anycast-v6-prefixes.txt"
)

// AnycastSet answers whether an address falls in a known anycast prefix. It is
// not a geolocation Source: anycast prefixes have no single physical location,
// so this is consulted as a quality signal that forces confidence to LOW.
type AnycastSet struct {
	v4 []netip.Prefix
	v6 []netip.Prefix
}

// FindAnycastFiles reports the cached anycast list paths in dir. ok is true
// when at least one family file is present.
func FindAnycastFiles(dir string) (v4, v6 string, ok bool) {
	if p := filepath.Join(dir, AnycastV4File); fileExists(p) {
		v4 = p
	}
	if p := filepath.Join(dir, AnycastV6File); fileExists(p) {
		v6 = p
	}
	return v4, v6, v4 != "" || v6 != ""
}

// NewAnycastSet loads whichever of the prefix-list paths are non-empty. Each
// file is one CIDR per line ('#' comments and blanks ignored).
func NewAnycastSet(v4Path, v6Path string) (*AnycastSet, error) {
	a := &AnycastSet{}
	if v4Path != "" {
		p, err := loadPrefixFile(v4Path)
		if err != nil {
			return nil, err
		}
		a.v4 = p
	}
	if v6Path != "" {
		p, err := loadPrefixFile(v6Path)
		if err != nil {
			return nil, err
		}
		a.v6 = p
	}
	return a, nil
}

func loadPrefixFile(path string) ([]netip.Prefix, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parsePrefixList(f)
}

func parsePrefixList(r io.Reader) ([]netip.Prefix, error) {
	var out []netip.Prefix
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		p, err := netip.ParsePrefix(line)
		if err != nil {
			continue
		}
		out = append(out, p.Masked())
	}
	return out, sc.Err()
}

// Len reports total loaded prefixes, for startup logging.
func (a *AnycastSet) Len() int { return len(a.v4) + len(a.v6) }

// Contains reports whether ip is inside any known anycast prefix.
func (a *AnycastSet) Contains(ip netip.Addr) bool {
	prefixes := a.v6
	if ip.Is4() {
		prefixes = a.v4
	}
	for _, p := range prefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}
