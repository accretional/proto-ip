package geoip

import (
	"net/netip"
	"strings"
	"testing"
)

func TestParseGeofeed(t *testing.T) {
	const feed = `
# RFC 8805 example feed
# prefix,country,region,city,postal
192.0.2.0/24,US,US-CA,Mountain View,94043
198.51.100.0/24,FR,FR-IDF,Paris,75001    # inline comment ignored
203.0.113.0/24,GB,,London,                # missing region/postal
2001:db8::/32,JP,JP-13,Tokyo,100-0001

# blank line above, malformed line below (skipped):
not-a-prefix,US,US-NY,New York,10001
`
	recs, err := ParseGeofeed(strings.NewReader(feed))
	if err != nil {
		t.Fatalf("ParseGeofeed: %v", err)
	}
	if len(recs) != 4 {
		t.Fatalf("got %d records, want 4: %+v", len(recs), recs)
	}

	if recs[0].Prefix.String() != "192.0.2.0/24" ||
		recs[0].Country != "US" || recs[0].Region != "US-CA" ||
		recs[0].City != "Mountain View" || recs[0].Postal != "94043" {
		t.Errorf("record 0 mismatch: %+v", recs[0])
	}
	if recs[1].City != "Paris" || recs[1].Postal != "75001" {
		t.Errorf("inline comment not stripped: %+v", recs[1])
	}
	if recs[2].Region != "" || recs[2].Postal != "" || recs[2].City != "London" {
		t.Errorf("missing-field handling wrong: %+v", recs[2])
	}
	if recs[3].Prefix.Addr().Is6() == false || recs[3].City != "Tokyo" {
		t.Errorf("IPv6 record wrong: %+v", recs[3])
	}
}

func TestParseGeofeedEmpty(t *testing.T) {
	recs, err := ParseGeofeed(strings.NewReader("# only comments\n\n   \n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 0 {
		t.Fatalf("want 0 records, got %d", len(recs))
	}
}

func TestLongestMatch(t *testing.T) {
	recs := []GeofeedRecord{
		{Prefix: netip.MustParsePrefix("10.0.0.0/8"), City: "Broad"},
		{Prefix: netip.MustParsePrefix("10.1.0.0/16"), City: "Narrow"},
		{Prefix: netip.MustParsePrefix("192.0.2.0/24"), City: "Other"},
	}
	tests := []struct {
		ip       string
		wantCity string
		wantOK   bool
	}{
		{"10.1.2.3", "Narrow", true},     // longest prefix wins
		{"10.2.2.2", "Broad", true},      // only the /8 covers it
		{"192.0.2.5", "Other", true},     // unrelated block
		{"8.8.8.8", "", false},           // no coverage
	}
	for _, tt := range tests {
		rec, ok := longestMatch(recs, netip.MustParseAddr(tt.ip))
		if ok != tt.wantOK {
			t.Errorf("%s: ok=%v want %v", tt.ip, ok, tt.wantOK)
			continue
		}
		if ok && rec.City != tt.wantCity {
			t.Errorf("%s: city=%q want %q", tt.ip, rec.City, tt.wantCity)
		}
	}
}
