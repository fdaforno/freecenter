package api

import (
	"context"
	"net/http"
	"time"

	"hostwarden/internal/esxi"
	"hostwarden/internal/model"
)

// hostOp runs a host configuration call with the usual plumbing: resolve the
// client, bound the time, map the error. A nil result answers {"status":"ok"}.
func (s *Server) hostOp(w http.ResponseWriter, r *http.Request, op func(ctx context.Context, c *esxi.Client) (any, error)) {
	c, err := s.client(r)
	if err != nil {
		apiError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	res, err := op(ctx, c)
	if err != nil {
		apiError(w, err)
		return
	}
	if res == nil {
		res = map[string]string{"status": "ok"}
	}
	writeJSON(w, 200, res)
}

func (s *Server) handleListServices(w http.ResponseWriter, r *http.Request) {
	s.hostOp(w, r, func(ctx context.Context, c *esxi.Client) (any, error) { return c.Services(ctx) })
}

func (s *Server) handleSetService(w http.ResponseWriter, r *http.Request) {
	var req model.HostServiceRequest
	if !decodeBody(w, r, &req) {
		return
	}
	s.hostOp(w, r, func(ctx context.Context, c *esxi.Client) (any, error) {
		return nil, c.SetService(ctx, r.PathValue("key"), &req)
	})
}

func (s *Server) handleMaintenance(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	s.hostOp(w, r, func(ctx context.Context, c *esxi.Client) (any, error) {
		return nil, c.SetMaintenance(ctx, req.Enabled)
	})
}

func (s *Server) handleGetNetwork(w http.ResponseWriter, r *http.Request) {
	s.hostOp(w, r, func(ctx context.Context, c *esxi.Client) (any, error) { return c.Network(ctx) })
}

func (s *Server) handleAddPortGroup(w http.ResponseWriter, r *http.Request) {
	var req model.PortGroupRequest
	if !decodeBody(w, r, &req) {
		return
	}
	s.hostOp(w, r, func(ctx context.Context, c *esxi.Client) (any, error) { return nil, c.AddPortGroup(ctx, &req) })
}

func (s *Server) handleSetPortGroup(w http.ResponseWriter, r *http.Request) {
	var req model.PortGroupRequest
	if !decodeBody(w, r, &req) {
		return
	}
	s.hostOp(w, r, func(ctx context.Context, c *esxi.Client) (any, error) {
		return nil, c.SetPortGroupVLAN(ctx, r.PathValue("name"), req.VLAN)
	})
}

func (s *Server) handleRemovePortGroup(w http.ResponseWriter, r *http.Request) {
	s.hostOp(w, r, func(ctx context.Context, c *esxi.Client) (any, error) {
		return nil, c.RemovePortGroup(ctx, r.PathValue("name"))
	})
}

func (s *Server) handleAddVSwitch(w http.ResponseWriter, r *http.Request) {
	var req model.VSwitchRequest
	if !decodeBody(w, r, &req) {
		return
	}
	s.hostOp(w, r, func(ctx context.Context, c *esxi.Client) (any, error) { return nil, c.AddVSwitch(ctx, &req) })
}

func (s *Server) handleRemoveVSwitch(w http.ResponseWriter, r *http.Request) {
	s.hostOp(w, r, func(ctx context.Context, c *esxi.Client) (any, error) {
		return nil, c.RemoveVSwitch(ctx, r.PathValue("name"))
	})
}

func (s *Server) handleGetTime(w http.ResponseWriter, r *http.Request) {
	s.hostOp(w, r, func(ctx context.Context, c *esxi.Client) (any, error) { return c.Time(ctx) })
}

func (s *Server) handleSetTime(w http.ResponseWriter, r *http.Request) {
	var req model.HostTimeRequest
	if !decodeBody(w, r, &req) {
		return
	}
	s.hostOp(w, r, func(ctx context.Context, c *esxi.Client) (any, error) { return nil, c.SetTime(ctx, &req) })
}
