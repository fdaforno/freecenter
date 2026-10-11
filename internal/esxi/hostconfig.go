package esxi

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/vmware/govmomi/vim25/mo"

	"freecenter/internal/model"
)

// Host configuration: reads go through the SOAP API, changes through SSH
// (esxcli / init scripts), like every other change to a host.

// sshServiceKey is the service FreeCenter itself depends on for all writes.
const sshServiceKey = "TSM-SSH"

func (c *Client) hostConfig(ctx context.Context, props []string) (*mo.HostSystem, error) {
	if err := c.Ensure(ctx); err != nil {
		return nil, err
	}
	ref, err := c.hostSystemRef(ctx)
	if err != nil {
		return nil, err
	}
	var hs mo.HostSystem
	if err := c.pc.RetrieveOne(ctx, ref, props, &hs); err != nil {
		return nil, err
	}
	return &hs, nil
}

// ---- services ----

func (c *Client) Services(ctx context.Context) ([]model.HostService, error) {
	hs, err := c.hostConfig(ctx, []string{"config.service"})
	if err != nil {
		return nil, err
	}
	out := []model.HostService{}
	if hs.Config == nil || hs.Config.Service == nil {
		return out, nil
	}
	for _, s := range hs.Config.Service.Service {
		out = append(out, model.HostService{Key: s.Key, Label: s.Label, Running: s.Running, Policy: s.Policy})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out, nil
}

// serviceKeyRE restricts service keys to what an init script name can be.
var serviceKeyRE = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

// serviceCommands gives the commands for services that are not plain init scripts.
var serviceCommands = map[string]map[string]string{
	"TSM-SSH": {"start": "vim-cmd hostsvc/start_ssh", "stop": "vim-cmd hostsvc/stop_ssh", "on": "vim-cmd hostsvc/enable_ssh", "off": "vim-cmd hostsvc/disable_ssh"},
	"TSM":     {"start": "vim-cmd hostsvc/start_esx_shell", "stop": "vim-cmd hostsvc/stop_esx_shell", "on": "vim-cmd hostsvc/enable_esx_shell", "off": "vim-cmd hostsvc/disable_esx_shell"},
}

// SetService starts, stops or restarts a host service and/or sets whether it
// starts with the host. The key must be one reported by Services.
func (c *Client) SetService(ctx context.Context, key string, req *model.HostServiceRequest) error {
	if c.ssh == nil {
		return ErrSSHDisabled
	}
	if !serviceKeyRE.MatchString(key) {
		return fmt.Errorf("invalid service key")
	}
	services, err := c.Services(ctx)
	if err != nil {
		return err
	}
	known := false
	for _, s := range services {
		known = known || s.Key == key
	}
	if !known {
		return fmt.Errorf("%w: service %s", ErrNotFound, key)
	}
	if key == sshServiceKey && (req.Action == "stop" || req.Policy == "off") {
		return fmt.Errorf("%w: FreeCenter manages this host through SSH; stopping or disabling it would lock FreeCenter out", ErrInvalidState)
	}
	special := serviceCommands[key]
	run := func(what, cmd string) error {
		if out, err := c.sshRun(ctx, cmd); err != nil {
			return fmt.Errorf("%s %s: %s", what, key, remoteFailure(err, out))
		}
		return nil
	}
	switch req.Action {
	case "":
	case "start", "stop":
		cmd := "/etc/init.d/" + key + " " + req.Action
		if special != nil {
			cmd = special[req.Action]
		}
		if err := run(req.Action, cmd); err != nil {
			return err
		}
	case "restart":
		cmd := "/etc/init.d/" + key + " restart"
		if special != nil {
			cmd = special["stop"] + "; " + special["start"]
		}
		if err := run("restart", cmd); err != nil {
			return err
		}
	default:
		return fmt.Errorf("action must be start, stop or restart")
	}
	switch req.Policy {
	case "":
	case "on", "off":
		cmd := "chkconfig " + key + " " + req.Policy
		if special != nil {
			cmd = special[req.Policy]
		}
		if err := run("set startup policy of", cmd); err != nil {
			return err
		}
	default:
		return fmt.Errorf("policy must be on or off")
	}
	return nil
}

// ---- maintenance mode ----

// SetMaintenance enters or exits maintenance mode. Entering fails (after a
// short wait) while virtual machines are running: nothing is powered off here.
func (c *Client) SetMaintenance(ctx context.Context, enabled bool) error {
	if c.ssh == nil {
		return ErrSSHDisabled
	}
	cmd := "esxcli system maintenanceMode set --enable false"
	if enabled {
		cmd = "esxcli system maintenanceMode set --enable true --timeout 20"
	}
	if out, err := c.sshRun(ctx, cmd); err != nil {
		msg := remoteFailure(err, out)
		if enabled {
			return fmt.Errorf("%w: the host could not enter maintenance mode (power off or migrate its running virtual machines first): %s", ErrInvalidState, msg)
		}
		return fmt.Errorf("exit maintenance mode: %s", msg)
	}
	return nil
}

// ---- networking ----

func (c *Client) Network(ctx context.Context) (*model.HostNetwork, error) {
	hs, err := c.hostConfig(ctx, []string{"config.network"})
	if err != nil {
		return nil, err
	}
	out := &model.HostNetwork{VSwitches: []model.VSwitch{}, PortGroups: []model.PortGroup{}, Nics: []model.HostNic{}}
	if hs.Config == nil || hs.Config.Network == nil {
		return out, nil
	}
	n := hs.Config.Network
	nicByKey := map[string]string{}
	for _, p := range n.Pnic {
		nic := model.HostNic{Device: p.Device, MAC: p.Mac, Driver: p.Driver}
		if p.LinkSpeed != nil {
			nic.SpeedMb = p.LinkSpeed.SpeedMb
		}
		nicByKey[p.Key] = p.Device
		out.Nics = append(out.Nics, nic)
	}
	vmkernel := map[string]bool{} // port groups used by VMkernel interfaces
	for _, v := range n.Vnic {
		vmkernel[v.Portgroup] = true
	}
	vmCount := map[string]int{}
	if vms, err := c.ListVMsDetailedNetworks(ctx); err == nil {
		vmCount = vms
	}
	for _, pg := range n.Portgroup {
		out.PortGroups = append(out.PortGroups, model.PortGroup{
			Name: pg.Spec.Name, VSwitch: pg.Spec.VswitchName, VLAN: pg.Spec.VlanId,
			Management: vmkernel[pg.Spec.Name], VMs: vmCount[pg.Spec.Name],
		})
	}
	for _, vs := range n.Vswitch {
		sw := model.VSwitch{Name: vs.Name, MTU: vs.Mtu, Uplinks: []string{}, PortGroups: []string{}}
		for _, k := range vs.Pnic {
			sw.Uplinks = append(sw.Uplinks, nicByKey[k])
		}
		for _, pg := range out.PortGroups {
			if pg.VSwitch == vs.Name {
				sw.PortGroups = append(sw.PortGroups, pg.Name)
			}
		}
		out.VSwitches = append(out.VSwitches, sw)
	}
	sort.Slice(out.PortGroups, func(i, j int) bool { return out.PortGroups[i].Name < out.PortGroups[j].Name })
	return out, nil
}

// ListVMsDetailedNetworks counts, per port group name, the VMs attached to it.
func (c *Client) ListVMsDetailedNetworks(ctx context.Context) (map[string]int, error) {
	var mhs mo.HostSystem
	ref, err := c.hostSystemRef(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.pc.RetrieveOne(ctx, ref, []string{"network"}, &mhs); err != nil {
		return nil, err
	}
	counts := map[string]int{}
	if len(mhs.Network) == 0 {
		return counts, nil
	}
	var nets []mo.Network
	if err := c.pc.Retrieve(ctx, mhs.Network, []string{"name", "vm"}, &nets); err != nil {
		return nil, err
	}
	for _, n := range nets {
		counts[n.Name] = len(n.Vm)
	}
	return counts, nil
}

// netNameRE restricts switch and port group names to characters that are safe
// in esxcli arguments (spaces are allowed and always quoted).
var netNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _.-]{0,63}$`)

func (c *Client) netRun(ctx context.Context, what, cmd string) error {
	if c.ssh == nil {
		return ErrSSHDisabled
	}
	if out, err := c.sshRun(ctx, cmd); err != nil {
		return fmt.Errorf("%s: %s", what, remoteFailure(err, out))
	}
	// hostd learns of a change made through esxcli only at its next refresh:
	// without this the new switch or port group is not reported for a while.
	_, _ = c.sshRun(ctx, "vim-cmd hostsvc/net/refresh")
	return nil
}

// portGroup looks a port group up by name, with its usage (VMkernel, VMs).
func (c *Client) portGroup(ctx context.Context, name string) (*model.PortGroup, error) {
	net, err := c.Network(ctx)
	if err != nil {
		return nil, err
	}
	for i := range net.PortGroups {
		if net.PortGroups[i].Name == name {
			return &net.PortGroups[i], nil
		}
	}
	return nil, fmt.Errorf("%w: port group %s", ErrNotFound, name)
}

func (c *Client) AddPortGroup(ctx context.Context, req *model.PortGroupRequest) error {
	if !netNameRE.MatchString(req.Name) || !netNameRE.MatchString(req.VSwitch) {
		return fmt.Errorf("invalid port group or virtual switch name")
	}
	if req.VLAN < 0 || req.VLAN > 4095 {
		return fmt.Errorf("vlan must be between 0 and 4095")
	}
	if err := c.netRun(ctx, "add port group", fmt.Sprintf("esxcli network vswitch standard portgroup add -p %s -v %s", shq(req.Name), shq(req.VSwitch))); err != nil {
		return err
	}
	if req.VLAN == 0 {
		return nil
	}
	// Set directly: the host does not report the new port group right away,
	// and one that was just created cannot carry a VMkernel interface.
	return c.setVLAN(ctx, req.Name, req.VLAN)
}

func (c *Client) setVLAN(ctx context.Context, name string, vlan int32) error {
	return c.netRun(ctx, "set vlan", fmt.Sprintf("esxcli network vswitch standard portgroup set -p %s --vlan-id %d", shq(name), vlan))
}

func (c *Client) SetPortGroupVLAN(ctx context.Context, name string, vlan int32) error {
	if vlan < 0 || vlan > 4095 {
		return fmt.Errorf("vlan must be between 0 and 4095")
	}
	pg, err := c.portGroup(ctx, name)
	if err != nil {
		return err
	}
	if pg.Management {
		return fmt.Errorf("%w: %s carries a VMkernel interface of the host; change it from the host console", ErrInvalidState, name)
	}
	return c.setVLAN(ctx, name, vlan)
}

func (c *Client) RemovePortGroup(ctx context.Context, name string) error {
	pg, err := c.portGroup(ctx, name)
	if err != nil {
		return err
	}
	switch {
	case pg.Management:
		return fmt.Errorf("%w: %s carries a VMkernel interface of the host and cannot be removed here", ErrInvalidState, name)
	case pg.VMs > 0:
		return fmt.Errorf("%w: %d virtual machine(s) are connected to %s", ErrInvalidState, pg.VMs, name)
	}
	return c.netRun(ctx, "remove port group", fmt.Sprintf("esxcli network vswitch standard portgroup remove -p %s -v %s", shq(pg.Name), shq(pg.VSwitch)))
}

func (c *Client) AddVSwitch(ctx context.Context, req *model.VSwitchRequest) error {
	if !netNameRE.MatchString(req.Name) {
		return fmt.Errorf("invalid virtual switch name")
	}
	net, err := c.Network(ctx)
	if err != nil {
		return err
	}
	inUse := map[string]string{}
	for _, sw := range net.VSwitches {
		for _, u := range sw.Uplinks {
			inUse[u] = sw.Name
		}
	}
	known := map[string]bool{}
	for _, n := range net.Nics {
		known[n.Device] = true
	}
	for _, u := range req.Uplinks {
		if !known[u] {
			return fmt.Errorf("%w: physical adapter %s", ErrNotFound, u)
		}
		// Moving an uplink away from its switch could cut the host off.
		if sw := inUse[u]; sw != "" {
			return fmt.Errorf("%w: %s is already an uplink of %s", ErrInvalidState, u, sw)
		}
	}
	if err := c.netRun(ctx, "add virtual switch", "esxcli network vswitch standard add -v "+shq(req.Name)); err != nil {
		return err
	}
	for _, u := range req.Uplinks {
		if err := c.netRun(ctx, "add uplink", fmt.Sprintf("esxcli network vswitch standard uplink add -u %s -v %s", shq(u), shq(req.Name))); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) RemoveVSwitch(ctx context.Context, name string) error {
	net, err := c.Network(ctx)
	if err != nil {
		return err
	}
	for _, sw := range net.VSwitches {
		if sw.Name != name {
			continue
		}
		if len(sw.PortGroups) > 0 {
			return fmt.Errorf("%w: %s still has port groups (%s); remove them first", ErrInvalidState, name, strings.Join(sw.PortGroups, ", "))
		}
		return c.netRun(ctx, "remove virtual switch", "esxcli network vswitch standard remove -v "+shq(name))
	}
	return fmt.Errorf("%w: virtual switch %s", ErrNotFound, name)
}

// ---- time ----

var ntpServerRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.:-]{0,253}$`)

func (c *Client) Time(ctx context.Context) (*model.HostTime, error) {
	if c.ssh == nil {
		return nil, ErrSSHDisabled
	}
	out, err := c.sshRun(ctx, "esxcli system time get; echo ---; esxcli system ntp get")
	if err != nil {
		return nil, fmt.Errorf("read host time: %s", remoteFailure(err, out))
	}
	clock, ntp, _ := strings.Cut(out, "---")
	t := &model.HostTime{Now: strings.TrimSpace(clock), NTPServers: []string{}}
	for _, line := range strings.Split(ntp, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch strings.TrimSpace(k) {
		case "Enabled":
			t.NTPEnabled = v == "true"
		case "Servers":
			for _, s := range strings.Split(v, ",") {
				if s = strings.TrimSpace(s); s != "" {
					t.NTPServers = append(t.NTPServers, s)
				}
			}
		}
	}
	return t, nil
}

func (c *Client) SetTime(ctx context.Context, req *model.HostTimeRequest) error {
	if c.ssh == nil {
		return ErrSSHDisabled
	}
	if req.NTPEnabled && len(req.NTPServers) == 0 {
		return fmt.Errorf("at least one NTP server is required to enable NTP")
	}
	cmd := fmt.Sprintf("esxcli system ntp set --enabled %t", req.NTPEnabled)
	for _, s := range req.NTPServers {
		if !ntpServerRE.MatchString(s) {
			return fmt.Errorf("invalid NTP server %q", s)
		}
		cmd += " --server " + shq(s)
	}
	if out, err := c.sshRun(ctx, cmd); err != nil {
		return fmt.Errorf("set NTP configuration: %s", remoteFailure(err, out))
	}
	return nil
}
