package api

import (
	"context"
	"net/http"
	"sync"
	"time"

	"freecenter/internal/model"
)

// Disk health probes are expensive: each datastore means several SSH
// round trips (vmfs extents, device list, smart). They run on a fixed
// 10-minute background timer — never on UI request — and the result is
// served from cache. The first probe starts shortly after boot.
const (
	healthProbeEvery = 10 * time.Minute
	healthProbeDelay = 15 * time.Second // let the inventory settle first
)

// healthCache holds the latest disk-health probe for every datastore,
// keyed by "<hostID>/<datastore>". Populated by healthRefreshLoop.
type healthCache struct {
	mu      sync.Mutex
	entries map[string]*model.DatastoreHealth
}

func newHealthCache() *healthCache {
	return &healthCache{entries: map[string]*model.DatastoreHealth{}}
}

func (h *healthCache) get(key string) (*model.DatastoreHealth, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	v, ok := h.entries[key]
	return v, ok
}

func (h *healthCache) set(key string, v *model.DatastoreHealth) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.entries[key] = v
}

func (h *healthCache) snapshot() []model.DatastoreHealth {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]model.DatastoreHealth, 0, len(h.entries))
	for _, v := range h.entries {
		out = append(out, *v)
	}
	return out
}

// handleDatastoreHealth serves the cached health of the devices backing
// one datastore. Never triggers a probe: if no data is available yet
// (first 10-minute window after boot) it returns an empty result.
func (s *Server) handleDatastoreHealth(w http.ResponseWriter, r *http.Request) {
	ds := r.PathValue("ds")
	key := r.PathValue("hid") + "/" + ds
	if h, ok := s.health.get(key); ok {
		writeJSON(w, 200, h)
		return
	}
	// No cached data yet — return an empty health so the UI can render.
	writeJSON(w, 200, &model.DatastoreHealth{
		Datastore: ds,
		HostID:    r.PathValue("hid"),
		Devices:   []model.DiskHealth{},
	})
}

// handleListAlarms serves the cached alarm list, also refreshed by
// healthRefreshLoop every 10 minutes.
func (s *Server) handleListAlarms(w http.ResponseWriter, r *http.Request) {
	s.alarmMu.Lock()
	defer s.alarmMu.Unlock()
	val := s.alarmVal
	if val == nil {
		val = []model.Alarm{}
	}
	writeJSON(w, 200, val)
}

// handleRefreshAlarms triggers an immediate disk-health probe across
// all connected hosts and returns the fresh alarm list.
func (s *Server) handleRefreshAlarms(w http.ResponseWriter, r *http.Request) {
	s.probeAllHealth()
	s.alarmMu.Lock()
	defer s.alarmMu.Unlock()
	val := s.alarmVal
	if val == nil {
		val = []model.Alarm{}
	}
	writeJSON(w, 200, val)
}

// healthRefreshLoop probes every connected host's datastore health once
// every healthProbeEvery, caches each datastore result, and computes the
// aggregated alarm list from the fresh data.
func (s *Server) healthRefreshLoop() {
	time.Sleep(healthProbeDelay)
	for {
		s.probeAllHealth()
		time.Sleep(healthProbeEvery)
	}
}

// probeAllHealth probes all connected hosts, caches per-datastore
// health, and recomputes the alarm list.
func (s *Server) probeAllHealth() {
	hosts := s.store.Hosts()
	type dsEntry struct {
		key    string
		health *model.DatastoreHealth
	}
	var allHealth []dsEntry
	var alarms []model.Alarm

	for _, h := range hosts {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		c, err := s.mgr.Client(h.ID)
		if err != nil {
			cancel()
			continue
		}
		if _, err := c.Status(ctx, nil); err != nil {
			cancel()
			continue
		}
		if !h.SSHEnabled {
			cancel()
			continue
		}
		dss, err := c.Datastores(ctx)
		if err != nil {
			cancel()
			continue
		}
		for _, ds := range dss {
			if !ds.Accessible {
				continue
			}
			health, err := c.DatastoreHealth(ctx, ds.Name)
			if err != nil {
				continue
			}
			allHealth = append(allHealth, dsEntry{
				key:    h.ID + "/" + ds.Name,
				health: health,
			})
			for _, d := range health.Devices {
				sev := d.Severity()
				if sev == "" {
					continue
				}
				alarms = append(alarms, model.Alarm{
					Kind:     "disk",
					Severity: sev,
					HostID:   h.ID,
					HostName: h.Name,
					Target:   ds.Name,
					Message:  d.ShortDescription(),
				})
			}
		}
		cancel()
	}

	for _, e := range allHealth {
		s.health.set(e.key, e.health)
	}
	s.alarmMu.Lock()
	s.alarmVal = alarms
	s.alarmAt = time.Now()
	s.alarmMu.Unlock()
}
