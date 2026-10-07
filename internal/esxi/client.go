package esxi

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/session"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"

	"freecenter/internal/model"
)

var (
	ErrNotFound     = errors.New("not found")
	ErrInvalidState = errors.New("invalid state")
)

// Client wraps a govmomi connection (reads) and an SSH connection (writes)
// to a single ESXi host. ESXi free 8.0.3 gates external API writes with
// vim.fault.RestrictedVersion, so all mutating operations go through SSH
// (vim-cmd / vmkfstools / VMX editing) which is not license-gated.
type Client struct {
	mu     sync.Mutex
	rec    *model.HostRecord
	vc     *vim25.Client
	finder *find.Finder
	pc     *property.Collector
	ssh    *SSHClient

	// license name, fetched over SSH and cached (see licenseName)
	licMu   sync.Mutex
	lic     string
	licTime time.Time
}

func insecure(rec *model.HostRecord) bool {
	return rec.Insecure == nil || *rec.Insecure
}

func dial(ctx context.Context, rec *model.HostRecord, sshKnownHosts string) (*Client, error) {
	u := &url.URL{
		Scheme: "https",
		Host:   rec.Address,
		Path:   "/sdk",
		User:   url.UserPassword(rec.Username, rec.Password),
	}
	c, err := govmomi.NewClient(ctx, u, insecure(rec))
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", rec.Address, err)
	}
	cl := &Client{rec: rec, vc: c.Client}
	cl.finder = find.NewFinder(c.Client)
	cl.pc = property.DefaultCollector(c.Client)
	if rec.SSHEnabled {
		cl.ssh = NewSSHClient(hostOf(rec.Address), rec.SSHPort, rec.Username, rec.Password, sshKnownHosts)
	}
	return cl, nil
}

func hostOf(addr string) string {
	if i := strings.LastIndex(addr, ":"); i > 0 && !strings.Contains(addr[i:], "]") {
		return addr[:i]
	}
	return addr
}

// Ensure the session is alive, re-dialing when expired.
func (c *Client) Ensure(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.vc == nil {
		return c.redialLocked(ctx)
	}
	// UserSession reads the current session of this connection (nil once it has
	// expired). SessionIsActive cannot be used: standalone ESXi does not
	// implement it, which made every call log in again.
	if us, err := session.NewManager(c.vc).UserSession(ctx); err == nil && us != nil {
		return nil
	}
	return c.redialLocked(ctx)
}

func (c *Client) redialLocked(ctx context.Context) error {
	ctx2, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	nc, err := dial(ctx2, c.rec, c.sshKnownHosts())
	if err != nil {
		return err
	}
	if old := c.vc; old != nil {
		// Do not leave the replaced session behind on the host.
		go func() { _ = session.NewManager(old).Logout(context.Background()) }()
	}
	c.vc = nc.vc
	c.finder = nc.finder
	c.pc = nc.pc
	return nil
}

func (c *Client) sshKnownHosts() string {
	if c.ssh != nil {
		return c.ssh.knownHosts
	}
	return ""
}

func (c *Client) Logout(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.vc != nil {
		_ = session.NewManager(c.vc).Logout(ctx)
		c.vc = nil
	}
}

// ref builds a ManagedObjectReference from "vm-123" style IDs.
func vmRef(id string) types.ManagedObjectReference {
	return types.ManagedObjectReference{Type: "VirtualMachine", Value: id}
}

func (c *Client) newVM(id string) *object.VirtualMachine {
	return object.NewVirtualMachine(c.vc, vmRef(id))
}

// retrieveVM fetches properties for a VM MoRef; returns ErrNotFound if gone.
func (c *Client) retrieveVM(ctx context.Context, id string, props []string) (*mo.VirtualMachine, error) {
	var m mo.VirtualMachine
	err := c.pc.RetrieveOne(ctx, vmRef(id), props, &m)
	if err != nil {
		if isMoNotFound(err) {
			return nil, fmt.Errorf("%w: vm %s", ErrNotFound, id)
		}
		return nil, err
	}
	return &m, nil
}

func isMoNotFound(err error) bool {
	if !soap.IsSoapFault(err) {
		return false
	}
	fault := soap.ToSoapFault(err)
	_, ok := fault.VimFault().(*types.ManagedObjectNotFound)
	return ok
}

// vmFolderPoolHost resolves (folder, resourcePool, host) for VM creation.
func (c *Client) vmFolderPoolHost(ctx context.Context) (*object.Folder, *object.ResourcePool, *object.HostSystem, error) {
	hosts, err := c.finder.HostSystemList(ctx, "*")
	if err != nil || len(hosts) == 0 {
		return nil, nil, nil, fmt.Errorf("no host system found: %w", err)
	}
	hs := hosts[0]

	var mhs mo.HostSystem
	if err := c.pc.RetrieveOne(ctx, hs.Reference(), []string{"parent"}, &mhs); err != nil {
		return nil, nil, nil, err
	}
	if mhs.Parent == nil {
		return nil, nil, nil, fmt.Errorf("host %s has no parent compute resource", hs.Name())
	}

	var mcr mo.ComputeResource
	if err := c.pc.RetrieveOne(ctx, *mhs.Parent, []string{"resourcePool"}, &mcr); err != nil {
		return nil, nil, nil, err
	}
	if mcr.ResourcePool == nil {
		return nil, nil, nil, fmt.Errorf("compute resource has no resource pool")
	}
	pool := object.NewResourcePool(c.vc, *mcr.ResourcePool)

	dcs, err := c.finder.DatacenterList(ctx, "*")
	if err != nil || len(dcs) == 0 {
		return nil, nil, nil, fmt.Errorf("no datacenter found: %w", err)
	}
	var mdc mo.Datacenter
	if err := c.pc.RetrieveOne(ctx, dcs[0].Reference(), []string{"vmFolder"}, &mdc); err != nil {
		return nil, nil, nil, err
	}
	folder := object.NewFolder(c.vc, mdc.VmFolder)

	return folder, pool, hs, nil
}

// Manager caches one Client per registered host.
type Manager struct {
	mu            sync.Mutex
	clients       map[string]*Client
	getRec        func(id string) (*model.HostRecord, error)
	sshKnownHosts string
}

func NewManager(getRec func(id string) (*model.HostRecord, error), sshKnownHosts string) *Manager {
	return &Manager{clients: map[string]*Client{}, getRec: getRec, sshKnownHosts: sshKnownHosts}
}

func (m *Manager) Client(hostID string) (*Client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.clients[hostID]; ok {
		return c, nil
	}
	rec, err := m.getRec(hostID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := dial(ctx, rec, m.sshKnownHosts)
	if err != nil {
		return nil, err
	}
	m.clients[hostID] = c
	return c, nil
}

// Drop removes the cached client (e.g. after credentials change or host delete).
func (m *Manager) Drop(hostID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.clients[hostID]; ok {
		go c.Logout(context.Background())
		delete(m.clients, hostID)
	}
}
