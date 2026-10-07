package api

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httputil"
	"time"
)

// handleVMConsole upgrades to a WebSocket and relays it to the VM's WebMKS
// console on the ESXi host. The browser therefore needs neither network access
// to the host nor trust in its self-signed certificate. Authentication uses
// the ?token= query parameter, since browsers cannot set headers on WebSockets.
func (s *Server) handleVMConsole(w http.ResponseWriter, r *http.Request) {
	c, err := s.client(r)
	if err != nil {
		apiError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	ticket, err := c.ConsoleTicket(ctx, r.PathValue("vmid"))
	cancel()
	if err != nil {
		apiError(w, err)
		return
	}

	target := fmt.Sprintf("%s:%d", ticket.Host, ticket.Port)
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = "https"
			pr.Out.URL.Host = target
			pr.Out.URL.Path = "/ticket/" + ticket.Ticket
			pr.Out.URL.RawQuery = "" // never forward our auth token to the host
			pr.Out.Host = target
			// The ESXi proxy rejects foreign origins; present its own.
			pr.Out.Header.Set("Origin", "https://"+target)
			pr.Out.Header.Del("Authorization")
			pr.Out.Header.Del("Cookie")
		},
		Transport: &http.Transport{
			TLSClientConfig:       &tls.Config{InsecureSkipVerify: ticket.InsecureTLS}, //nolint:gosec // per-host setting, ESXi certificates are self-signed
			ResponseHeaderTimeout: 20 * time.Second,
			ForceAttemptHTTP2:     false, // WebSocket upgrade requires HTTP/1.1
		},
		ErrorHandler: func(rw http.ResponseWriter, _ *http.Request, err error) {
			s.logger.Warn("console proxy", "target", target, "err", err)
			errJSON(rw, http.StatusBadGateway, "console connection failed: "+err.Error())
		},
	}
	proxy.ServeHTTP(w, r)
}
