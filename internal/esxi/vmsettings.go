package esxi

import (
	"fmt"
	"strconv"

	"freecenter/internal/model"
)

// applyVMSettings applies the VM options and device changes of req that are
// plain VMX edits (no disk operations). It reports whether doc was modified.
// Everything here requires the VM to be powered off, as the VMX is rewritten.
func applyVMSettings(doc *vmxDoc, req *model.UpdateVMRequest, addrs map[int32]deviceAddr, poweredOn bool) (bool, error) {
	changed := false
	// touch marks a change and rejects it while the VM is running.
	touch := func(what string) error {
		if poweredOn {
			return fmt.Errorf("%w: changing %s requires the VM to be powered off", ErrInvalidState, what)
		}
		changed = true
		return nil
	}
	setBool := func(key string, v *bool, what string) error {
		if v == nil {
			return nil
		}
		if err := touch(what); err != nil {
			return err
		}
		doc.set(key, vmxBool(*v))
		return nil
	}

	// --- options ---
	if req.Name != nil {
		if err := touch("the name"); err != nil {
			return false, err
		}
		doc.set("displayName", vmxEscape(*req.Name))
	}
	if req.GuestID != nil {
		if err := touch("the guest OS"); err != nil {
			return false, err
		}
		doc.set("guestOS", guestOSToVMX(*req.GuestID))
	}
	if req.Firmware != nil {
		if err := touch("the firmware"); err != nil {
			return false, err
		}
		doc.set("firmware", *req.Firmware)
	}
	if req.BootDelayMs != nil {
		if err := touch("boot options"); err != nil {
			return false, err
		}
		doc.set("bios.bootDelay", strconv.FormatInt(*req.BootDelayMs, 10))
	}
	if err := setBool("bios.forceSetupOnce", req.EnterBIOSSetup, "boot options"); err != nil {
		return false, err
	}
	if err := setBool("vcpu.hotadd", req.CPUHotAdd, "CPU hot add"); err != nil {
		return false, err
	}
	if err := setBool("mem.hotadd", req.MemoryHotAdd, "memory hot add"); err != nil {
		return false, err
	}
	if err := setBool("vhv.enable", req.NestedHV, "hardware virtualization"); err != nil {
		return false, err
	}

	if req.Template != nil {
		if err := touch("the template flag"); err != nil {
			return false, err
		}
		if *req.Template {
			doc.set(templateKey, "TRUE")
		} else {
			doc.removePrefix(templateKey)
		}
	}

	// --- usb controller ---
	if req.USBController != nil {
		if err := touch("the USB controller"); err != nil {
			return false, err
		}
		doc.removePrefix("usb")
		doc.removePrefix("ehci")
		doc.removePrefix("usb_xhci")
		switch *req.USBController {
		case "usb2":
			doc.set("usb.present", "TRUE")
			doc.set("ehci.present", "TRUE")
		case "usb3":
			doc.set("usb_xhci.present", "TRUE")
		}
	}

	// --- existing network adapters ---
	for _, un := range req.UpdateNICs {
		if err := touch("network adapters"); err != nil {
			return false, err
		}
		idx, err := ethernetIndex(doc, addrs, un.Key)
		if err != nil {
			return false, err
		}
		if un.Network != nil {
			doc.set(fmt.Sprintf("ethernet%d.networkName", idx), *un.Network)
		}
		if un.StartConnected != nil {
			doc.set(fmt.Sprintf("ethernet%d.startConnected", idx), vmxBool(*un.StartConnected))
		}
	}

	// --- cd/dvd drives: remove, then change media, then add (so freed slots can be reused) ---
	cdromPrefix := func(key int32) (string, error) {
		addr, ok := addrs[key]
		if !ok || addr.ctrl == "" || addr.unit < 0 {
			return "", fmt.Errorf("%w: cd/dvd drive with key %d", ErrNotFound, key)
		}
		return addr.vmxPrefix(), nil
	}
	for _, key := range req.RemoveCdromKeys {
		if err := touch("CD/DVD drives"); err != nil {
			return false, err
		}
		prefix, err := cdromPrefix(key)
		if err != nil {
			return false, err
		}
		doc.removePrefix(prefix)
	}
	for _, uc := range req.UpdateCdroms {
		if err := touch("CD/DVD drives"); err != nil {
			return false, err
		}
		prefix, err := cdromPrefix(uc.Key)
		if err != nil {
			return false, err
		}
		setCdrom(doc, prefix, uc.ISOPath)
	}
	for _, ac := range req.AddCdroms {
		if err := touch("CD/DVD drives"); err != nil {
			return false, err
		}
		prefix, ok := freeIDESlot(doc)
		if !ok {
			return false, fmt.Errorf("%w: no free IDE slot for a new CD/DVD drive (maximum 4 IDE devices)", ErrInvalidState)
		}
		setCdrom(doc, prefix, ac.ISOPath)
	}
	return changed, nil
}

func vmxBool(v bool) string {
	if v {
		return "TRUE"
	}
	return "FALSE"
}

// setCdrom (re)defines the CD/DVD drive at prefix: backed by an ISO image, or
// by the client device (no media) when iso is empty.
func setCdrom(doc *vmxDoc, prefix, iso string) {
	doc.removePrefix(prefix)
	doc.set(prefix+".present", "TRUE")
	if iso != "" {
		doc.set(prefix+".deviceType", "cdrom-image")
		doc.set(prefix+".fileName", isoToFSPath(iso))
		doc.set(prefix+".startConnected", "TRUE")
		return
	}
	doc.set(prefix+".deviceType", "atapi-cdrom")
	doc.set(prefix+".fileName", "emptyBackingString")
	doc.set(prefix+".clientDevice", "TRUE")
	doc.set(prefix+".startConnected", "FALSE")
}

// freeIDESlot returns the first unused IDE address ("ide0:0" .. "ide1:1").
func freeIDESlot(doc *vmxDoc) (string, bool) {
	for bus := 0; bus < 2; bus++ {
		for unit := 0; unit < 2; unit++ {
			prefix := fmt.Sprintf("ide%d:%d", bus, unit)
			if len(doc.keysWithPrefix(prefix)) == 0 {
				return prefix, true
			}
		}
	}
	return "", false
}
