package esxi

import (
	"context"
	"fmt"
	"strings"

	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"

	"hostwarden/internal/model"
)

var vmPropsDetail = []string{"name", "summary", "config", "snapshot"}

// ListVMs returns all VMs on the host.
func (c *Client) ListVMs(ctx context.Context) ([]*model.VM, error) {
	if err := c.Ensure(ctx); err != nil {
		return nil, err
	}
	vms, err := c.allVMRefs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*model.VM, 0, len(vms))
	for i := range vms {
		out = append(out, mapVM(&vms[i]))
	}
	return out, nil
}

// allVMRefs retrieves VM summaries via the host's Vm property (standalone-safe).
func (c *Client) allVMRefs(ctx context.Context) ([]mo.VirtualMachine, error) {
	var mhs mo.HostSystem
	hsRef, err := c.hostSystemRef(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.pc.RetrieveOne(ctx, hsRef, []string{"vm"}, &mhs); err != nil {
		return nil, err
	}
	if len(mhs.Vm) == 0 {
		return nil, nil
	}
	var vms []mo.VirtualMachine
	if err := c.pc.Retrieve(ctx, mhs.Vm, []string{"summary", "snapshot", "config.extraConfig"}, &vms); err != nil {
		return nil, err
	}
	return vms, nil
}

func (c *Client) hostSystemRef(ctx context.Context) (types.ManagedObjectReference, error) {
	hosts, err := c.finder.HostSystemList(ctx, "*")
	if err != nil || len(hosts) == 0 {
		return types.ManagedObjectReference{}, fmt.Errorf("no host system found: %w", err)
	}
	return hosts[0].Reference(), nil
}

// GetVM returns full VM details.
func (c *Client) GetVM(ctx context.Context, id string) (*model.VM, error) {
	if err := c.Ensure(ctx); err != nil {
		return nil, err
	}
	m, err := c.retrieveVM(ctx, id, vmPropsDetail)
	if err != nil {
		return nil, err
	}
	return mapVM(m), nil
}

// CreateVM creates a VM and optionally powers it on.
// ---- mapping ----

func dsFromPath(p string) string {
	if !strings.HasPrefix(p, "[") {
		return ""
	}
	end := strings.Index(p, "]")
	if end < 0 {
		return ""
	}
	return p[1:end]
}

func labelOf(d types.BaseDescription) string {
	if d != nil {
		return d.GetDescription().Label
	}
	return ""
}

func adapterTypeName(d any) string {
	switch d.(type) {
	case *types.VirtualE1000:
		return "e1000"
	case *types.VirtualE1000e:
		return "e1000e"
	case *types.VirtualVmxnet2:
		return "vmxnet2"
	case *types.VirtualVmxnet3:
		return "vmxnet3"
	case *types.VirtualPCNet32:
		return "pcnet32"
	}
	return "unknown"
}

func mapVM(m *mo.VirtualMachine) *model.VM {
	vm := &model.VM{
		ID: m.Self.Value,
	}
	cfg := m.Summary.Config
	vm.Name = cfg.Name
	vm.NumCPUs = int(cfg.NumCpu)
	vm.MemoryMB = int64(cfg.MemorySizeMB)
	vm.GuestID = cfg.GuestId
	vm.GuestFullName = cfg.GuestFullName
	vm.VMXPath = cfg.VmPathName
	vm.Annotation = cfg.Annotation
	vm.PowerState = string(m.Summary.Runtime.PowerState)
	qs := m.Summary.QuickStats
	vm.CPUUsageMHz = qs.OverallCpuUsage
	vm.UptimeSeconds = qs.UptimeSeconds
	vm.GuestMemoryMB = qs.GuestMemoryUsage
	vm.HostMemoryMB = qs.HostMemoryUsage
	if g := m.Summary.Guest; g != nil {
		vm.IPAddress = g.IpAddress
		vm.GuestHostname = g.HostName
		if g.ToolsStatus != "" {
			vm.ToolsStatus = string(g.ToolsStatus)
		}
	}
	if m.Config != nil {
		vm.Template = isTemplate(m.Config.ExtraConfig)
	}
	// The list view only fetches config.extraConfig: the rest needs the full
	// config. Its file layout tells the two apart (the UUID does not: a VM that
	// was never powered on may not have one yet).
	if m.Config != nil && m.Config.Files.VmPathName != "" {
		vm.UUID = m.Config.Uuid
		vm.HardwareVersion = m.Config.Version
		if cps := m.Config.Hardware.NumCoresPerSocket; cps != nil {
			vm.CoresPerSocket = int(*cps)
		}
		vm.Firmware = m.Config.Firmware
		if bo := m.Config.BootOptions; bo != nil {
			vm.BootDelayMs = bo.BootDelay
		}
		vm.CPUHotAdd = m.Config.CpuHotAddEnabled != nil && *m.Config.CpuHotAddEnabled
		vm.MemoryHotAdd = m.Config.MemoryHotAddEnabled != nil && *m.Config.MemoryHotAddEnabled
		vm.NestedHV = m.Config.NestedHVEnabled != nil && *m.Config.NestedHVEnabled
		if vm.Annotation == "" {
			vm.Annotation = m.Config.Annotation
		}
		if vm.Name == "" {
			vm.Name = m.Config.Name
		}
		dsSet := map[string]bool{}
		for _, d := range m.Config.Hardware.Device {
			switch dev := d.(type) {
			case *types.VirtualDisk:
				disk := model.VMDisk{
					Key:        dev.VirtualDevice.Key,
					Label:      labelOf(dev.VirtualDevice.DeviceInfo),
					CapacityKB: dev.CapacityInKB,
					CapacityGB: dev.CapacityInKB / (1024 * 1024),
				}
				if b, ok := dev.Backing.(*types.VirtualDiskFlatVer2BackingInfo); ok {
					disk.Path = b.FileName
					disk.Thin = b.ThinProvisioned != nil && *b.ThinProvisioned
					disk.Mode = b.DiskMode
					disk.Datastore = dsFromPath(b.FileName)
					if disk.Datastore != "" {
						dsSet[disk.Datastore] = true
					}
				}
				vm.Disks = append(vm.Disks, disk)
			case types.BaseVirtualEthernetCard:
				ec := dev.GetVirtualEthernetCard()
				nic := model.VMNIC{
					Key:            ec.VirtualDevice.Key,
					Label:          labelOf(ec.VirtualDevice.DeviceInfo),
					AdapterType:    adapterTypeName(dev),
					MACAddress:     ec.MacAddress,
					Connected:      ec.Connectable != nil && ec.Connectable.Connected,
					StartConnected: ec.Connectable != nil && ec.Connectable.StartConnected,
				}
				switch b := ec.Backing.(type) {
				case *types.VirtualEthernetCardNetworkBackingInfo:
					nic.Network = b.DeviceName
				case *types.VirtualEthernetCardDistributedVirtualPortBackingInfo:
					nic.Network = "dvport:" + b.Port.PortKey
				case *types.VirtualEthernetCardOpaqueNetworkBackingInfo:
					nic.Network = "opaque:" + b.OpaqueNetworkId
				}
				if nic.Network != "" {
					vm.Networks = append(vm.Networks, nic.Network)
				}
				vm.NICs = append(vm.NICs, nic)
			case *types.VirtualCdrom:
				cd := model.VMCdrom{
					Key:            dev.VirtualDevice.Key,
					Label:          labelOf(dev.VirtualDevice.DeviceInfo),
					StartConnected: dev.Connectable != nil && dev.Connectable.StartConnected,
				}
				if b, ok := dev.Backing.(*types.VirtualCdromIsoBackingInfo); ok {
					cd.ISO = b.FileName
					if vm.CdromISO == "" {
						vm.CdromISO = b.FileName
					}
				}
				vm.Cdroms = append(vm.Cdroms, cd)
			case *types.VirtualUSBXHCIController:
				vm.USBController = "usb3"
			case *types.VirtualUSBController:
				if vm.USBController == "" {
					vm.USBController = "usb2"
				}
			}
		}
		for ds := range dsSet {
			vm.Datastores = append(vm.Datastores, ds)
		}
	}
	if m.Snapshot != nil {
		vm.SnapshotCount = countSnapshotTree(m.Snapshot.RootSnapshotList)
	}
	if len(vm.Datastores) == 0 && vm.VMXPath != "" {
		if ds := dsFromPath(vm.VMXPath); ds != "" {
			vm.Datastores = []string{ds}
		}
	}
	return vm
}

// templateKey is the VMX entry that marks a VM as a template. Standalone
// ESXi has no template concept, so Hostwarden keeps its own flag in the VMX.
const templateKey = "hostwarden.template"

// legacyTemplateKey is the flag written by versions released as FreeCenter.
// It is still recognised, and removed whenever the flag is rewritten.
const legacyTemplateKey = "freecenter.template"

func isTemplate(extra []types.BaseOptionValue) bool {
	for _, o := range extra {
		if ov := o.GetOptionValue(); ov.Key == templateKey || ov.Key == legacyTemplateKey {
			v, _ := ov.Value.(string)
			return strings.EqualFold(v, "TRUE")
		}
	}
	return false
}

func countSnapshotTree(trees []types.VirtualMachineSnapshotTree) int {
	n := 0
	for _, t := range trees {
		n++
		n += countSnapshotTree(t.ChildSnapshotList)
	}
	return n
}
