package api

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Inventory cache.
//
// Every read from an ESXi host is a SOAP round trip of a second or more, and
// the UI polls the whole inventory. Answers are therefore cached per host and
// kept warm in the background while somebody is looking: requests are served
// from memory, at most cacheMaxAge old. Any write to a host drops its entries,
// so the next read after an operation is always fresh.
const (
	cacheMaxAge       = 15 * time.Second // older entries are refetched on demand
	cacheRefreshEvery = 8 * time.Second  // background refresh interval
	cacheWarmFor      = 90 * time.Second // keep refreshing this long after the last read
	cacheFetchTimeout = 60 * time.Second
)

type cacheEntry struct {
	mu         sync.Mutex // held while fetching: concurrent readers share one fetch
	val        any
	at         time.Time // when val was fetched (zero = never)
	lastUse    time.Time
	refreshing bool // a background refresh is in flight
	fetch      func(ctx context.Context) (any, error)
}

type inventoryCache struct {
	mu      sync.Mutex
	entries map[string]*cacheEntry
}

func newInventoryCache() *inventoryCache {
	c := &inventoryCache{entries: map[string]*cacheEntry{}}
	go c.refreshLoop()
	return c
}

// cached returns the value for key (conventionally "<hostID>/<what>"),
// fetching it when missing or too old. Errors are returned, never cached.
func cached[T any](c *inventoryCache, ctx context.Context, key string, fetch func(ctx context.Context) (T, error)) (T, error) {
	c.mu.Lock()
	e, ok := c.entries[key]
	if !ok {
		e = &cacheEntry{}
		c.entries[key] = e
	}
	c.mu.Unlock()

	e.mu.Lock()
	defer e.mu.Unlock()
	e.lastUse = time.Now()
	e.fetch = func(ctx context.Context) (any, error) { return fetch(ctx) }
	if !e.at.IsZero() && time.Since(e.at) < cacheMaxAge {
		return e.val.(T), nil
	}
	v, err := fetch(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	e.val, e.at = v, time.Now()
	return v, nil
}

// invalidateHost drops everything cached for one host.
func (c *inventoryCache) invalidateHost(hostID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.entries {
		if strings.HasPrefix(k, hostID+"/") {
			delete(c.entries, k)
		}
	}
}

func (c *inventoryCache) refreshLoop() {
	for range time.Tick(cacheRefreshEvery) {
		c.mu.Lock()
		warm := make([]*cacheEntry, 0, len(c.entries))
		for k, e := range c.entries {
			e.mu.Lock()
			idle := time.Since(e.lastUse)
			e.mu.Unlock()
			if idle > cacheWarmFor {
				delete(c.entries, k) // nobody is looking any more
				continue
			}
			warm = append(warm, e)
		}
		c.mu.Unlock()

		for _, e := range warm {
			go func() {
				// Fetch without holding the entry lock, so readers keep getting the current value meanwhile.
				e.mu.Lock()
				fetch := e.fetch
				busy := e.refreshing
				e.refreshing = fetch != nil && !busy
				e.mu.Unlock()
				if fetch == nil || busy {
					return
				}
				ctx, cancel := context.WithTimeout(context.Background(), cacheFetchTimeout)
				defer cancel()
				v, err := fetch(ctx)
				e.mu.Lock()
				defer e.mu.Unlock()
				e.refreshing = false
				// On failure the old value simply ages out and the next reader sees the error.
				if err == nil {
					e.val, e.at = v, time.Now()
				}
			}()
		}
	}
}

// invalidateOnWrite drops a host's cached inventory after any successful
// modifying request addressed to it (/api/v1/hosts/{hid}/...).
func (s *Server) invalidateOnWrite(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			return
		}
		rest, ok := strings.CutPrefix(r.URL.Path, "/api/v1/hosts/")
		if !ok || rest == "" {
			return
		}
		hostID, _, _ := strings.Cut(rest, "/")
		s.cache.invalidateHost(hostID)
	})
}
