// Package rdap implements an RDAP client backed by the IANA bootstrap
// registry (RFC 7484). Bootstrap data is fetched once on construction
// and held in memory for the lifetime of the process.
package rdap

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
)

const (
	ianaIPv4Bootstrap = "https://data.iana.org/rdap/ipv4.json"
	ianaIPv6Bootstrap = "https://data.iana.org/rdap/ipv6.json"
)

// Bootstrap maps IP prefixes to RDAP server base URLs.
type Bootstrap struct {
	v4 []bsEntry
	v6 []bsEntry
}

type bsEntry struct {
	prefix *net.IPNet
	urls   []string
}

// bootstrapFile mirrors the RFC 7484 JSON structure.
type bootstrapFile struct {
	Services [][]json.RawMessage `json:"services"`
}

// NewBootstrap fetches the IANA IPv4 and IPv6 bootstrap files and
// returns a ready-to-use Bootstrap. Cancelling ctx aborts the fetch.
func NewBootstrap(ctx context.Context) (*Bootstrap, error) {
	v4, err := fetchBootstrap(ctx, ianaIPv4Bootstrap)
	if err != nil {
		return nil, fmt.Errorf("rdap bootstrap ipv4: %w", err)
	}
	v6, err := fetchBootstrap(ctx, ianaIPv6Bootstrap)
	if err != nil {
		return nil, fmt.Errorf("rdap bootstrap ipv6: %w", err)
	}
	return &Bootstrap{v4: v4, v6: v6}, nil
}

func fetchBootstrap(ctx context.Context, url string) ([]bsEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d fetching %s", resp.StatusCode, url)
	}

	var bf bootstrapFile
	if err := json.NewDecoder(resp.Body).Decode(&bf); err != nil {
		return nil, fmt.Errorf("decode %s: %w", url, err)
	}

	var entries []bsEntry
	for _, svc := range bf.Services {
		if len(svc) < 2 {
			continue
		}
		var prefixes []string
		if err := json.Unmarshal(svc[0], &prefixes); err != nil {
			continue
		}
		var urls []string
		if err := json.Unmarshal(svc[1], &urls); err != nil {
			continue
		}
		if len(urls) == 0 {
			continue
		}
		for _, p := range prefixes {
			_, ipnet, err := net.ParseCIDR(p)
			if err != nil {
				continue
			}
			entries = append(entries, bsEntry{prefix: ipnet, urls: urls})
		}
	}
	return entries, nil
}

// Resolve returns the base URL of the RDAP server responsible for ip.
// It picks the most-specific (longest prefix) matching entry.
// The returned URL always ends with "/".
func (b *Bootstrap) Resolve(ip net.IP) (string, error) {
	entries := b.v4
	if ip.To4() == nil {
		entries = b.v6
	}

	best := -1
	bestLen := -1
	for i, e := range entries {
		if !e.prefix.Contains(ip) {
			continue
		}
		ones, _ := e.prefix.Mask.Size()
		if ones > bestLen {
			bestLen = ones
			best = i
		}
	}
	if best < 0 {
		return "", fmt.Errorf("no RDAP server found for %s", ip)
	}
	u := entries[best].urls[0]
	if !strings.HasSuffix(u, "/") {
		u += "/"
	}
	return u, nil
}
