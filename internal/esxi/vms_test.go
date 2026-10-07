package esxi

import (
	"testing"

	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

// A VM that was never powered on can have no UUID yet: its devices must be
// reported all the same.
func TestMapVMWithoutUUID(t *testing.T) {
	m := &mo.VirtualMachine{Config: &types.VirtualMachineConfigInfo{
		Version: "vmx-21",
		Files:   types.VirtualMachineFileInfo{VmPathName: "[ds] vm/vm.vmx"},
		Hardware: types.VirtualHardware{Device: []types.BaseVirtualDevice{
			&types.VirtualDisk{CapacityInKB: 2 * 1024 * 1024, VirtualDevice: types.VirtualDevice{Key: 2000}},
		}},
	}}
	vm := mapVM(m)
	if len(vm.Disks) != 1 || vm.Disks[0].CapacityGB != 2 {
		t.Fatalf("disks = %+v, want one disk of 2 GB", vm.Disks)
	}
	if vm.HardwareVersion != "vmx-21" {
		t.Errorf("hardware version = %q, want vmx-21", vm.HardwareVersion)
	}
}

// The list view fetches config.extraConfig only: it carries the template flag
// and nothing else.
func TestMapVMListView(t *testing.T) {
	m := &mo.VirtualMachine{Config: &types.VirtualMachineConfigInfo{
		ExtraConfig: []types.BaseOptionValue{&types.OptionValue{Key: templateKey, Value: "TRUE"}},
	}}
	vm := mapVM(m)
	if !vm.Template {
		t.Error("template flag not read from extraConfig")
	}
	if vm.Disks != nil || vm.HardwareVersion != "" {
		t.Errorf("list view should not map the full config: %+v", vm)
	}
}
