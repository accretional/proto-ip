package geoip

import "testing"

// TestGeofeedURLRe proves the single discovery regex extracts a geofeed URL
// from every channel/form we encounter: the RPSL geofeed: attribute (RIPE
// whois), an ARIN-style remarks line, the bracketed form, and an RDAP JSON
// remark. A line with no geofeed must not match.
func TestGeofeedURLRe(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "rpsl attribute",
			in:   "inet6num:       2a05:b0c6:a200::/39\ngeofeed:        https://api.geofeed.space/pfcloud/geofeed.csv\n",
			want: "https://api.geofeed.space/pfcloud/geofeed.csv",
		},
		{
			name: "arin remarks",
			in:   "Geofeed https://example.net/geofeed.csv",
			want: "https://example.net/geofeed.csv",
		},
		{
			name: "bracketed",
			in:   "remarks: Geofeed <https://example.org/feed.csv>",
			want: "https://example.org/feed.csv",
		},
		{
			name: "rdap json description",
			in:   `{"remarks":[{"description":["Geofeed https://cdn.example/g.csv"]}]}`,
			want: "https://cdn.example/g.csv",
		},
		{
			name: "no geofeed",
			in:   "remarks: For abuse contact noc@example.net",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := geofeedURLRe.FindStringSubmatch(tt.in)
			got := ""
			if m != nil {
				got = m[1]
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
