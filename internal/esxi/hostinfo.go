package esxi

import (
	"context"
	"strings"
	"time"

	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"

	"hostwarden/internal/model"
)

// Status collects live host info via the API (and SSH for license details).
func (c *Client) Status(ctx context.Context, ssh *SSHClient) (*model.HostStatus, error) {
	if err := c.Ensure(ctx); err != nil {
		return nil, err
	}
	st := &model.HostStatus{}
	about := c.vc.ServiceContent.About
	st.Product = about.Name
	st.Version = about.Version
	st.Build = about.Build
	st.OSType = about.OsType

	hsRef, err := c.hostSystemRef(ctx)
	if err != nil {
		return nil, err
	}
	var hs mo.HostSystem
	if err := c.pc.RetrieveOne(ctx, hsRef, []string{"summary", "runtime"}, &hs); err != nil {
		return nil, err
	}
	if hw := hs.Summary.Hardware; hw != nil {
		st.CPUModel = hw.CpuModel
		st.CPUCores = int32(hw.NumCpuCores)
		st.CPUs = int32(hw.NumCpuPkgs)
		st.CPUMHz = int32(hw.CpuMhz)
		st.MemoryTotalMB = hw.MemorySize / (1024 * 1024)
	}
	st.MemoryUsedMB = int64(hs.Summary.QuickStats.OverallMemoryUsage)
	st.ConnectionState = string(hs.Runtime.ConnectionState)
	st.MaintenanceMode = hs.Runtime.InMaintenanceMode
	st.BootTime = hs.Runtime.BootTime
	if hs.Runtime.BootTime != nil {
		st.UptimeSeconds = int64(time.Since(*hs.Runtime.BootTime).Seconds())
	}

	// count VMs and running VMs
	vms, err := c.allVMRefs(ctx)
	if err == nil {
		st.VMsTotal = len(vms)
		for i := range vms {
			if vms[i].Summary.Runtime.PowerState == types.VirtualMachinePowerStatePoweredOn {
				st.VMsRunning++
			}
		}
	}

	if ssh != nil {
		st.LicenseName = c.licenseName(ctx, ssh)
	}
	return st, nil
}

// licenseTTL bounds how stale the cached license name may be. The license
// practically never changes, while reading it costs a full SSH session.
const licenseTTL = 10 * time.Minute

// licenseName returns the host license name via SSH (best effort), cached
// so that status polling does not open an SSH session on every call.
func (c *Client) licenseName(ctx context.Context, ssh *SSHClient) string {
	c.licMu.Lock()
	defer c.licMu.Unlock()
	if c.lic != "" && time.Since(c.licTime) < licenseTTL {
		return c.lic
	}
	out, err := ssh.Run(ctx, "vim-cmd vimsvc/license --show")
	if err != nil {
		return c.lic // keep the last known value on a transient SSH failure
	}
	c.lic, c.licTime = parseLicenseName(out), time.Now()
	return c.lic
}

func parseLicenseName(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "name:"))
		}
	}
	return ""
}
