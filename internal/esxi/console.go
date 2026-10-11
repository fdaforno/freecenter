package esxi

import (
	"context"
	"fmt"
	"net"
)

// ConsoleTicket is a one-time WebMKS ticket for a VM's remote console.
// The console is a WebSocket at wss://Host:Port/ticket/<Ticket>.
type ConsoleTicket struct {
	Ticket      string
	Host        string
	Port        int
	InsecureTLS bool // skip TLS verification (ESXi self-signed certificate)
}

// ConsoleTicket acquires a WebMKS ticket. It is a read-type SOAP call; the VM
// must be powered on.
func (c *Client) ConsoleTicket(ctx context.Context, id string) (*ConsoleTicket, error) {
	if err := c.Ensure(ctx); err != nil {
		return nil, err
	}
	t, err := c.newVM(id).AcquireTicket(ctx, "webmks")
	if err != nil {
		return nil, fmt.Errorf("acquire console ticket: %w", err)
	}
	// A standalone host leaves Host empty: the console is on the host we are connected to.
	host := t.Host
	if host == "" {
		host = c.rec.Address
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
	}
	port := int(t.Port)
	if port == 0 {
		port = 443
	}
	return &ConsoleTicket{Ticket: t.Ticket, Host: host, Port: port, InsecureTLS: insecure(c.rec)}, nil
}
