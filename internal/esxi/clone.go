package esxi

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"

	"hostwarden/internal/model"
)

// CloneProgress receives the current phase of a clone.
type CloneProgress func(phase string)

// identityKeys are VMX entries that identify one particular VM instance or
// its runtime state; a clone must not inherit them.
var identityKeys = []string{
	"uuid.bios", "uuid.location", "vc.uuid", "vm.genid", "vm.genidX",
	"sched.swap.derivedName", "migrate.hostlog", "checkpoint.vmState",
	"vmxstats.filename", "extendedConfigFile",
}

// CloneVM copies a powered-off VM into a new VM on the same host: every disk
// is cloned with vmkfstools (keeping its provisioning and folding snapshots
// into the copy) and the VMX is rewritten with a new identity. It returns the
// id of the new VM.
func (c *Client) CloneVM(ctx context.Context, id string, req *model.CloneRequest, progress CloneProgress) (string, error) {
	if c.ssh == nil {
		return "", ErrSSHDisabled
	}
	layout, vm, err := c.vmLayout(ctx, id)
	if err != nil {
		return "", err
	}
	if layout.power != "poweredOff" {
		return "", fmt.Errorf("%w: %s must be powered off to be cloned", ErrInvalidState, vm.Name)
	}
	if req.Datastore == "" {
		req.Datastore = dsFromPath(vm.VMXPath)
	}
	// Provisioning of each source disk, by file.
	thin := map[string]bool{}
	for _, d := range vm.Disks {
		thin[dsPathToFS(d.Path)] = d.Thin
	}
	return c.cloneFolder(ctx, layout.vmxFS, func(disk string) bool { return thin[disk] }, req, progress)
}

// cloneFolder creates a new VM from the VMX file srcVMX and the disks it
// references (which must not be in use): a registered, powered-off VM or a
// backup folder. thinDisk tells how to provision the copy of each source disk.
func (c *Client) cloneFolder(ctx context.Context, srcVMX string, thinDisk func(disk string) bool, req *model.CloneRequest, progress CloneProgress) (string, error) {
	report := func(format string, a ...any) {
		if progress != nil {
			progress(fmt.Sprintf(format, a...))
		}
	}
	srcDir := path.Dir(srcVMX)
	targetDir := "/vmfs/volumes/" + req.Datastore + "/" + req.Name
	if out, err := c.sshRun(ctx, "[ -d "+shq("/vmfs/volumes/"+req.Datastore)+" ] || echo nods; [ -e "+shq(targetDir)+" ] && echo exists; true"); err != nil {
		return "", err
	} else if strings.Contains(out, "nods") {
		return "", fmt.Errorf("%w: datastore %s", ErrNotFound, req.Datastore)
	} else if strings.Contains(out, "exists") {
		return "", fmt.Errorf("%w: a folder named %q already exists on %s", ErrInvalidState, req.Name, req.Datastore)
	}

	content, err := c.sshRun(ctx, "cat "+shq(srcVMX))
	if err != nil {
		return "", fmt.Errorf("read vmx: %w", err)
	}
	doc := parseVMX(content)

	if out, err := c.sshRun(ctx, "mkdir "+shq(targetDir)); err != nil {
		return "", fmt.Errorf("create %s: %w (output: %s)", targetDir, err, strings.TrimSpace(out))
	}
	// targetDir was just created by us: removing it cannot touch anything else.
	fail := func(err error) (string, error) {
		_, _ = c.sshRun(context.WithoutCancel(ctx), "rm -rf "+shq(targetDir))
		return "", err
	}

	// --- disks ---
	diskKeys := vmxDiskKeys(doc)
	for i, key := range diskKeys {
		ref, _ := doc.get(key)
		src := ref
		if !strings.HasPrefix(src, "/") {
			src = srcDir + "/" + ref
		}
		name := diskFileName(req.Name, i)
		format := "zeroedthick"
		if thinDisk(src) {
			format = "thin"
		}
		report("Cloning disk %d of %d", i+1, len(diskKeys))
		cmd := fmt.Sprintf("vmkfstools -i %s -d %s %s", shq(src), format, shq(targetDir+"/"+name))
		if out, err := c.sshRun(ctx, cmd); err != nil {
			return fail(fmt.Errorf("clone disk %s: %s", path.Base(src), remoteFailure(err, lastLine(out))))
		}
		doc.set(key, name)
	}

	// --- identity ---
	report("Configuring the new virtual machine")
	doc.set("displayName", vmxEscape(req.Name))
	doc.set("uuid.action", "create")
	for _, k := range identityKeys {
		doc.removePrefix(k)
	}
	for i := 0; i < 10; i++ {
		eth := fmt.Sprintf("ethernet%d", i)
		if _, ok := doc.get(eth + ".present"); !ok {
			continue
		}
		// A copy must get its own MAC address, also when the original had a fixed one.
		doc.removePrefix(eth + ".generatedAddress")
		doc.removePrefix(eth + ".generatedAddressOffset")
		doc.removePrefix(eth + ".address")
		doc.set(eth+".addressType", "generated")
	}
	if nvram, ok := doc.get("nvram"); ok && !strings.HasPrefix(nvram, "/") {
		// Firmware settings (EFI boot entries): copy when present.
		_, _ = c.sshRun(ctx, fmt.Sprintf("[ -f %[1]s ] && cp %[1]s %[2]s", shq(srcDir+"/"+nvram), shq(targetDir+"/"+req.Name+".nvram")))
		doc.set("nvram", req.Name+".nvram")
	}
	if req.Template {
		doc.removePrefix(legacyTemplateKey)
		doc.set(templateKey, "TRUE")
	} else {
		doc.removePrefix(legacyTemplateKey)
		doc.removePrefix(templateKey)
	}

	vmxFS := targetDir + "/" + req.Name + ".vmx"
	cmd := fmt.Sprintf("cat > %s <<'FCVMXEOF'\n%sFCVMXEOF\n", shq(vmxFS), doc.String())
	if out, err := c.sshRun(ctx, cmd); err != nil {
		return fail(fmt.Errorf("write vmx: %w (output: %s)", err, out))
	}

	report("Registering the new virtual machine")
	out, err := c.sshRun(ctx, "vim-cmd solo/registervm "+shq(vmxFS))
	if err != nil {
		return fail(fmt.Errorf("register: %s", remoteFailure(err, out)))
	}
	newID := strings.TrimSpace(out)
	if _, err := strconv.Atoi(newID); err != nil {
		return fail(fmt.Errorf("register: unexpected output %q", newID))
	}
	if req.PowerOn && !req.Template {
		report("Powering on the new virtual machine")
		if _, err := c.SetPower(ctx, newID, "on"); err != nil {
			return newID, fmt.Errorf("the virtual machine was created but could not be powered on: %w", err)
		}
	}
	return newID, nil
}

// vmxDiskKeys returns the "<device>.fileName" keys of the virtual disks in a VMX, in file order.
func vmxDiskKeys(doc *vmxDoc) []string {
	var keys []string
	for _, l := range doc.lines {
		k := vmxKeyOf(l)
		if !strings.HasSuffix(k, ".fileName") {
			continue
		}
		if v, _ := doc.get(k); strings.HasSuffix(strings.ToLower(v), ".vmdk") {
			keys = append(keys, k)
		}
	}
	return keys
}

// diskFileName names the i-th disk of a VM the way CreateVM does.
func diskFileName(vmName string, i int) string {
	if i == 0 {
		return vmName + ".vmdk"
	}
	return fmt.Sprintf("%s_%d.vmdk", vmName, i)
}

// lastLine returns the last non-empty line of out (vmkfstools prints a
// progress bar before its error message).
func lastLine(out string) string {
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(out, "\r", "\n")), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return ""
}
