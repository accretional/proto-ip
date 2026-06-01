package geoip

import (
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// FindDBIPDatabase returns the path to the newest DB-IP City Lite MMDB in dir
// (files named dbip-city-lite-YYYY-MM.mmdb). It returns an error if none exist
// so the caller can run geofeed-only.
func FindDBIPDatabase(dir string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "dbip-city-lite-*.mmdb"))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no dbip-city-lite-*.mmdb found in %s", dir)
	}
	// Lexicographic sort on the YYYY-MM suffix is chronological; newest last.
	sort.Strings(matches)
	return matches[len(matches)-1], nil
}

// geofeedCache is an in-memory, TTL'd cache of parsed geofeeds keyed by URL.
// A geofeed is fetched once per TTL window so repeated lookups against the
// same network do not refetch the CSV.
type geofeedCache struct {
	ttl time.Duration

	mu      sync.Mutex
	entries map[string]geofeedEntry
}

type geofeedEntry struct {
	recs    []GeofeedRecord
	fetched time.Time
}

func newGeofeedCache(ttl time.Duration) *geofeedCache {
	return &geofeedCache{ttl: ttl, entries: make(map[string]geofeedEntry)}
}

// get returns the cached records for url if present and still fresh.
func (c *geofeedCache) get(url string) ([]GeofeedRecord, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[url]
	if !ok || time.Since(e.fetched) > c.ttl {
		return nil, false
	}
	return e.recs, true
}

func (c *geofeedCache) put(url string, recs []GeofeedRecord) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[url] = geofeedEntry{recs: recs, fetched: time.Now()}
}
