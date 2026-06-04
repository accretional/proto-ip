package geoip

import (
	"net/netip"
	"strings"
	"testing"
)

func TestParsePrefixList(t *testing.T) {
	const list = `# bgp.tools anycast prefixes
1.1.1.0/24
8.8.8.0/24    # inline comment

2620:fe::/48
not-a-prefix
`
	ps, err := parsePrefixList(strings.NewReader(list))
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 3 {
		t.Fatalf("got %d prefixes, want 3: %v", len(ps), ps)
	}
}

func TestAnycastContains(t *testing.T) {
	a, err := NewAnycastSet("", "")
	if err != nil {
		t.Fatal(err)
	}
	v4, _ := parsePrefixList(strings.NewReader("1.1.1.0/24\n"))
	v6, _ := parsePrefixList(strings.NewReader("2606:4700::/32\n"))
	a.v4, a.v6 = v4, v6

	cases := []struct {
		ip   string
		want bool
	}{
		{"1.1.1.1", true},
		{"1.1.2.1", false},
		{"2606:4700::1111", true},
		{"2001:db8::1", false},
		{"8.8.8.8", false},
	}
	for _, c := range cases {
		if got := a.Contains(netip.MustParseAddr(c.ip)); got != c.want {
			t.Errorf("Contains(%s) = %v, want %v", c.ip, got, c.want)
		}
	}
}

// TestAnycastReal checks the downloaded bgp.tools list flags a known anycast IP.
func TestAnycastReal(t *testing.T) {
	v4, v6, ok := FindAnycastFiles("../data/geoip")
	if !ok {
		t.Skip("anycast lists not present")
	}
	a, err := NewAnycastSet(v4, v6)
	if err != nil {
		t.Fatal(err)
	}
	// 1.1.1.1 (Cloudflare) is anycast; a typical unicast host is not.
	if !a.Contains(netip.MustParseAddr("1.1.1.1")) {
		t.Errorf("1.1.1.1 should be flagged anycast")
	}
}
