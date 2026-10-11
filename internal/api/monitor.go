package api

import (
	"context"
	"net/http"
	"time"

	"hostwarden/internal/model"
)

// maxEvents caps the event list sent to the UI.
const maxEvents = 200

// handlePerformance serves the real-time statistics of a host or, when the
// route has a vmid, of one VM. Cached like the inventory: ESXi only adds a
// sample every 20 seconds.
func (s *Server) handlePerformance(w http.ResponseWriter, r *http.Request) {
	c, err := s.client(r)
	if err != nil {
		apiError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	vmid := r.PathValue("vmid")
	data, err := cached(s.cache, ctx, r.PathValue("hid")+"/performance/"+vmid, func(ctx context.Context) (*model.PerfData, error) {
		return c.Performance(ctx, vmid)
	})
	if err != nil {
		apiError(w, err)
		return
	}
	writeJSON(w, 200, data)
}

// handleEvents serves the newest entries of the host event log (of one VM when the route has a vmid).
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	c, err := s.client(r)
	if err != nil {
		apiError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	vmid := r.PathValue("vmid")
	events, err := cached(s.cache, ctx, r.PathValue("hid")+"/events/"+vmid, func(ctx context.Context) ([]model.Event, error) {
		return c.Events(ctx, vmid, maxEvents)
	})
	if err != nil {
		apiError(w, err)
		return
	}
	writeJSON(w, 200, events)
}
