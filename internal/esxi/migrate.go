package esxi

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"freecenter/internal/model"
)

// VM migration between two standalone ESXi hosts.
//
// This is a cold (or suspended) migration, not a live one: the VM is stopped
// (or suspended), its folder is copied host-to-host over SSH, and it is
// registered and started on the target.
// The data never passes through FreeCenter: the source host pushes it directly
// to the target with a key pair created for that purpose.

const (
	// migrateKeyPath is the private key the source host uses to reach other
	// hosts. It is generated on the host and never leaves it.
	migrateKeyPath = "/etc/ssh/keys-root/freecenter_migrate"
	authorizedKeys = "/etc/ssh/keys-root/authorized_keys"

	guestShutdownTimeout = 3 * time.Minute
	powerOnTimeout       = 2 * time.Minute
	progressInterval     = 3 * time.Second
)

// MigrationProgress receives updates while a migration runs.
type MigrationProgress func(phase string, bytesDone, bytesTotal int64)

// MigrationResult is what a successful migration produced.
type MigrationResult struct {
	NewVMID  string
	Warnings []string
}

// vmLayout is where a VM lives on its host.
type vmLayout struct {
	name    string
	vmxFS   string // /vmfs/volumes/<ds>/<dir>/<name>.vmx
	dir     string // /vmfs/volumes/<ds>/<dir>
	dirName string
	power   string
}

func (c *Client) vmLayout(ctx context.Context, id string) (*vmLayout, *model.VM, error) {
	vm, err := c.GetVM(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	vmxFS := dsPathToFS(vm.VMXPath)
	dir := path.Dir(vmxFS)
	return &vmLayout{name: vm.Name, vmxFS: vmxFS, dir: dir, dirName: path.Base(dir), power: vm.PowerState}, vm, nil
}

// CheckMigration runs the compatibility checks for moving VM id from src to
// targetDS on dst. Problems are reported in the result, not as an error.
func CheckMigration(ctx context.Context, src, dst *Client, id, targetDS string) (*model.MigrationCheck, error) {
	check := &model.MigrationCheck{Blockers: []string{}, Warnings: []string{}}
	block := func(format string, a ...any) { check.Blockers = append(check.Blockers, fmt.Sprintf(format, a...)) }
	warn := func(format string, a ...any) { check.Warnings = append(check.Warnings, fmt.Sprintf(format, a...)) }

	if src.ssh == nil || dst.ssh == nil {
		block("SSH must be enabled on both hosts")
		return check, nil
	}
	if src.rec.ID == dst.rec.ID {
		block("Source and target are the same host")
		return check, nil
	}
	layout, vm, err := src.vmLayout(ctx, id)
	if err != nil {
		return nil, err
	}
	check.PowerState = layout.power

	// --- CPU ---
	srcStatus, err := src.Status(ctx, nil)
	if err != nil {
		return nil, err
	}
	dstStatus, err := dst.Status(ctx, nil)
	if err != nil {
		return nil, err
	}
	check.SourceCPU = strings.Join(strings.Fields(srcStatus.CPUModel), " ")
	check.TargetCPU = strings.Join(strings.Fields(dstStatus.CPUModel), " ")
	check.CPUCompatible = check.SourceCPU != "" && check.SourceCPU == check.TargetCPU
	switch {
	case layout.power == "poweredOff":
		check.RecommendedMode = model.MigrateCold
	case check.CPUCompatible:
		check.RecommendedMode = model.MigrateSuspend
	default:
		check.RecommendedMode = model.MigrateStopStart
	}
	if layout.power == "suspended" && !check.CPUCompatible {
		block("The virtual machine is suspended and cannot be resumed on a host with a different CPU; power it on and shut it down first")
	}

	// --- files ---
	for _, d := range vm.Disks {
		if path.Dir(dsPathToFS(d.Path)) != layout.dir {
			block("%s is stored outside the virtual machine folder (%s); only virtual machines kept in a single folder can be migrated", d.Label, d.Path)
		}
		check.ProvisionedGB += d.CapacityGB
	}
	if vm.SnapshotCount > 0 {
		warn("The virtual machine has snapshots: they are migrated too, but thin disks cannot be re-thinned on the target")
	}
	size, err := dirSize(ctx, src, layout.dir)
	if err != nil {
		return nil, err
	}
	check.SizeBytes = size

	// --- target storage ---
	stores, err := dst.Datastores(ctx)
	if err != nil {
		return nil, err
	}
	var target *model.Datastore
	for i := range stores {
		if stores[i].Name == targetDS {
			target = &stores[i]
		}
	}
	// The same volume mounted on both hosts (shared VMFS/NFS) has the same URL on each.
	if target != nil && target.URL != "" {
		if srcStores, err := src.Datastores(ctx); err == nil {
			for _, d := range srcStores {
				if d.Name == dsFromPath(vm.VMXPath) && d.URL == target.URL {
					check.Shared = true
				}
			}
		}
	}
	switch {
	case target == nil:
		block("Datastore %q does not exist on the target host", targetDS)
	case !target.Accessible:
		block("Datastore %q is not accessible on the target host", targetDS)
	case check.Shared:
		check.TargetFreeBytes = target.FreeB
		check.SizeBytes = 0 // the files stay where they are
	default:
		check.TargetFreeBytes = target.FreeB
		if size > target.FreeB {
			block("Not enough free space on %s: %d GB needed, %d GB free", targetDS, toGB(size), toGB(target.FreeB))
		}
		targetDir := "/vmfs/volumes/" + targetDS + "/" + layout.dirName
		if out, err := dst.sshRun(ctx, "[ -e "+shq(targetDir)+" ] && echo exists || echo free"); err != nil {
			return nil, err
		} else if strings.TrimSpace(out) != "free" {
			block("A folder named %q already exists on %s", layout.dirName, targetDS)
		}
	}

	// --- target networks ---
	nets, err := dst.Networks(ctx)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, n := range nets {
		known[n.Name] = true
	}
	for _, n := range vm.Networks {
		if !known[n] {
			block("Network %q does not exist on the target host", n)
		}
	}

	// --- informational ---
	for _, cd := range vm.Cdroms {
		if cd.ISO != "" {
			warn("%s uses %s: if that image is not on the target host the drive is disconnected", cd.Label, cd.ISO)
		}
	}
	if others, err := dst.ListVMs(ctx); err == nil {
		for _, o := range others {
			if o.Name == vm.Name {
				warn("The target host already has a virtual machine named %q", vm.Name)
			}
		}
	}
	return check, nil
}

func toGB(b int64) int64 { return b / (1 << 30) }

// dirSize is the sum of the file sizes in dir (what a copy has to transfer).
func dirSize(ctx context.Context, c *Client, dir string) (int64, error) {
	out, err := c.sshRun(ctx, "ls -l "+shq(dir)+" | awk '{s+=$5} END {printf \"%.0f\\n\", s}'")
	if err != nil {
		return 0, fmt.Errorf("measure %s: %w (output: %s)", dir, err, out)
	}
	return strconv.ParseInt(strings.TrimSpace(out), 10, 64)
}

// MigrateVM moves VM id from src to targetDS on dst. The source VM is only
// removed after the target has been registered (and started, if it was
// running); on failure the copy is removed and the source is put back in the
// state it was in.
func MigrateVM(ctx context.Context, src, dst *Client, id string, req *model.MigrateRequest, progress MigrationProgress) (*MigrationResult, error) {
	check, err := CheckMigration(ctx, src, dst, id, req.TargetDatastore)
	if err != nil {
		return nil, err
	}
	if len(check.Blockers) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrInvalidState, strings.Join(check.Blockers, "; "))
	}
	layout, vm, err := src.vmLayout(ctx, id)
	if err != nil {
		return nil, err
	}
	running := layout.power == "poweredOn"
	switch {
	case running && req.Mode == model.MigrateCold:
		return nil, fmt.Errorf("%w: the virtual machine is powered on; choose suspend or stop_start", ErrInvalidState)
	case req.Mode == model.MigrateSuspend && !check.CPUCompatible:
		return nil, fmt.Errorf("%w: the hosts have different CPUs (%s / %s); a suspended virtual machine cannot be resumed on the target, use stop_start", ErrInvalidState, check.SourceCPU, check.TargetCPU)
	}
	if check.Shared {
		return migrateShared(ctx, src, dst, id, layout, req, progress)
	}
	// With older snapshots the disk chain cannot be rebuilt on the target: copy everything after stopping instead.
	precopy := req.Precopy && running && vm.SnapshotCount == 0

	result := &MigrationResult{}
	total := check.SizeBytes
	report := func(phase string, done int64) {
		if progress != nil {
			progress(phase, done, total)
		}
	}
	targetParent := "/vmfs/volumes/" + req.TargetDatastore
	targetDir := targetParent + "/" + layout.dirName
	targetVMX := targetDir + "/" + path.Base(layout.vmxFS)

	// --- 1. direct channel between the hosts ---
	report("Preparing the connection between the hosts", 0)
	sshOpts, err := prepareHostChannel(ctx, src, dst)
	if err != nil {
		return nil, err
	}

	// --- 1b. pre-copy: freeze the disks with a snapshot and copy them while the VM keeps running ---
	//
	// A running VM keeps its disk files locked, so they cannot be read directly.
	// vmkfstools can clone a disk that a snapshot has frozen: each base disk is
	// cloned into a staging folder next to the VM, and the staging copy is what
	// travels to the target. The changes made meanwhile stay in the snapshot
	// delta, copied after the VM is stopped.
	var frozen []string // disk files that no longer change once the snapshot exists
	if precopy {
		report("Taking a temporary snapshot", 0)
		if frozen, err = listFiles(ctx, src, layout.dir, `\.vmdk$`); err != nil {
			return nil, err
		}
		stagingDir := layout.dir + ".fc-migrate"
		if out, err := src.sshRun(ctx, "mkdir "+shq(stagingDir)); err != nil {
			return nil, fmt.Errorf("create the staging folder: %s", remoteFailure(err, out))
		}
		// Until the VM is stopped, undoing the pre-copy only means dropping the copies and the snapshot.
		undo := func(cause error) error {
			rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Minute)
			defer cancel()
			_, _ = src.sshRun(rctx, "rm -rf "+shq(stagingDir))
			_, _ = dst.sshRun(rctx, "rm -rf "+shq(targetDir))
			removeMigrateSnapshot(rctx, src, id)
			return fmt.Errorf("%w; the virtual machine was not stopped", cause)
		}
		if _, err := src.CreateSnapshot(ctx, id, &model.CreateSnapshotRequest{Name: migrateSnapshot, Description: "Temporary snapshot taken by FreeCenter for a migration"}); err != nil {
			return nil, undo(err)
		}
		var descriptors []string
		for _, f := range frozen {
			if !diskExtentRE.MatchString(f) {
				descriptors = append(descriptors, f)
			}
		}
		for i, d := range descriptors {
			report(fmt.Sprintf("Cloning disk %d of %d while the virtual machine is running", i+1, len(descriptors)), 0)
			// Thin keeps the staging copy as small as the data in the disk.
			cmd := fmt.Sprintf("vmkfstools -i %s -d thin %s", shq(layout.dir+"/"+d), shq(stagingDir+"/"+d))
			if out, err := src.sshRun(ctx, cmd); err != nil {
				return nil, undo(fmt.Errorf("clone %s for the pre-copy: %s", d, remoteFailure(err, lastLine(out))))
			}
		}
		staged, err := listFiles(ctx, src, stagingDir, "")
		if err != nil {
			return nil, undo(err)
		}
		if out, err := dst.sshRun(ctx, "mkdir "+shq(targetDir)); err != nil {
			return nil, undo(fmt.Errorf("create the target folder: %s", remoteFailure(err, out)))
		}
		report("Copying the disks while the virtual machine is running", 0)
		stop := watchCopy(ctx, dst, targetDir, func(done int64) { report("Copying the disks while the virtual machine is running", done) })
		err = copyFiles(ctx, src, dst, sshOpts, stagingDir, staged, targetDir)
		stop()
		if err != nil {
			return nil, undo(err)
		}
		_, _ = src.sshRun(ctx, "rm -rf "+shq(stagingDir))
		// The copied disks are not in use yet: re-thin them now, with the VM still in service.
		if req.ReclaimThin {
			for _, d := range descriptors {
				report("Reclaiming unused space of "+d, 0)
				if out, err := dst.sshRun(ctx, "vmkfstools -K "+shq(targetDir+"/"+d)); err != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("%s could not be re-thinned: %s", d, remoteFailure(err, lastLine(out))))
				}
			}
		}
	}

	// --- 2. stop the source ---
	if running {
		if req.Mode == model.MigrateSuspend {
			report("Suspending the virtual machine", 0)
			if _, err := src.SetPower(ctx, id, "suspend"); err != nil {
				return nil, err
			}
			if err := waitPower(ctx, src, id, "suspended", powerOnTimeout); err != nil {
				return nil, err
			}
		} else {
			report("Shutting down the guest operating system", 0)
			if err := shutDown(ctx, src, id); err != nil {
				return nil, err
			}
		}
		// The suspend state adds files, the shutdown removes the swap file.
		if total, err = dirSize(ctx, src, layout.dir); err != nil {
			total = check.SizeBytes
		}
	}

	// From here on a failure must leave the source as we found it.
	newID := ""
	rollback := func(cause error) error {
		// Detached from ctx: the rollback must also run when the job was cancelled.
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
		defer cancel()
		if newID != "" {
			_, _ = dst.sshRun(rctx, "vim-cmd vmsvc/power.off "+shq(newID)+" >/dev/null 2>&1; vim-cmd vmsvc/unregister "+shq(newID))
		}
		// targetDir did not exist before (checked above) and its name is never empty, so this only removes our copy.
		if layout.dirName != "" && layout.dirName != "." && layout.dirName != "/" {
			_, _ = dst.sshRun(rctx, "rm -rf "+shq(targetDir))
		}
		if running {
			if _, err := src.SetPower(rctx, id, "on"); err != nil {
				return fmt.Errorf("%w; the copy was removed but the source virtual machine could not be started again: %v", cause, err)
			}
		}
		if precopy {
			removeMigrateSnapshot(rctx, src, id)
		}
		return fmt.Errorf("%w; the source virtual machine was restored", cause)
	}

	// --- 3. copy, host to host ---
	report("Copying the virtual machine files", 0)
	stopProgress := watchCopy(ctx, dst, targetDir, func(done int64) { report("Copying the virtual machine files", done) })
	if precopy {
		// Only what the pre-copy did not cover: the snapshot delta, the configuration and the suspend state.
		var rest []string
		if rest, err = listFiles(ctx, src, layout.dir, ""); err == nil {
			rest = without(rest, frozen)
			err = copyFiles(ctx, src, dst, sshOpts, layout.dir, rest, targetDir)
		}
	} else {
		copyCmd := fmt.Sprintf("scp -O -r -q %s -P %d %s %s", sshOpts, sshPort(dst.rec), shq(layout.dir), shq(dst.rec.Username+"@"+hostOf(dst.rec.Address)+":"+targetParent+"/"))
		var out string
		if out, err = src.sshRun(ctx, copyCmd); err != nil {
			err = fmt.Errorf("copy to the target host failed: %s", remoteFailure(err, out))
		}
	}
	stopProgress()
	if err != nil {
		return nil, rollback(err)
	}
	report("Copying the virtual machine files", total)

	if precopy {
		// The staged disks are clones: point each snapshot delta at the content id of the parent it now has.
		relink := `cd ` + shq(targetDir) + ` && for f in *-[0-9][0-9][0-9][0-9][0-9][0-9].vmdk; do [ -f "$f" ] || continue; ` +
			`p=$(sed -n 's/^parentFileNameHint="\(.*\)"/\1/p' "$f"); [ -f "$p" ] || continue; ` +
			`cid=$(sed -n 's/^CID=//p' "$p"); [ -n "$cid" ] && sed -i "s/^parentCID=.*/parentCID=$cid/" "$f"; done; true`
		if out, err := dst.sshRun(ctx, relink); err != nil {
			return nil, rollback(fmt.Errorf("relink the snapshot disks on the target: %s", remoteFailure(err, out)))
		}
	}

	// --- 4. adapt the copy to the target host ---
	report("Configuring the virtual machine on the target host", total)
	warns, err := adaptVMX(ctx, dst, targetVMX)
	if err != nil {
		return nil, rollback(err)
	}
	result.Warnings = append(result.Warnings, warns...)

	if req.ReclaimThin && vm.SnapshotCount == 0 && !precopy {
		for _, d := range vm.Disks {
			if !d.Thin {
				continue
			}
			report("Reclaiming unused space of "+d.Label, total)
			disk := targetDir + "/" + path.Base(dsPathToFS(d.Path))
			if out, err := dst.sshRun(ctx, "vmkfstools -K "+shq(disk)); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("%s could not be re-thinned: %v (%s)", d.Label, err, strings.TrimSpace(out)))
			}
		}
	}

	// --- 5. register and start on the target ---
	report("Registering the virtual machine on the target host", total)
	out, err := dst.sshRun(ctx, "vim-cmd solo/registervm "+shq(targetVMX))
	if err != nil {
		return nil, rollback(fmt.Errorf("register on the target host: %w (output: %s)", err, strings.TrimSpace(out)))
	}
	newID = strings.TrimSpace(out)
	if _, err := strconv.Atoi(newID); err != nil {
		newID = ""
		return nil, rollback(fmt.Errorf("register on the target host: unexpected output %q", strings.TrimSpace(out)))
	}
	// A VM that was already suspended before the migration stays suspended.
	if running {
		report("Powering on the virtual machine on the target host", total)
		if _, err := dst.SetPower(ctx, newID, "on"); err != nil {
			return nil, rollback(fmt.Errorf("power on on the target host: %w", err))
		}
		if err := waitPower(ctx, dst, newID, "poweredOn", powerOnTimeout); err != nil {
			return nil, rollback(fmt.Errorf("power on on the target host: %w", err))
		}
	}
	result.NewVMID = newID
	if precopy {
		// The VM is already back in service: fold the temporary snapshot into its disks.
		report("Consolidating the temporary snapshot", total)
		if !removeMigrateSnapshot(ctx, dst, newID) {
			result.Warnings = append(result.Warnings, fmt.Sprintf("The temporary snapshot %q could not be removed on the target: delete it from the Snapshots tab", migrateSnapshot))
		}
		if !req.DeleteSource {
			removeMigrateSnapshot(ctx, src, id)
		}
	}

	// --- 6. retire the source (the target is up: failures here are only warnings) ---
	report("Removing the virtual machine from the source host", total)
	if req.DeleteSource {
		if err := src.DeleteVM(ctx, id, true); err != nil {
			result.Warnings = append(result.Warnings, "The source virtual machine could not be deleted: "+err.Error())
		}
	} else {
		// A suspended source must be powered off (discarding its now stale state) before it can be unregistered.
		if out, err := src.sshRun(ctx, "vim-cmd vmsvc/power.off "+shq(id)+" >/dev/null 2>&1; vim-cmd vmsvc/unregister "+shq(id)); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("The source virtual machine could not be unregistered: %v (%s)", err, strings.TrimSpace(out)))
		} else {
			result.Warnings = append(result.Warnings, "The source files were kept in "+vm.VMXPath[:strings.LastIndex(vm.VMXPath, "/")])
		}
	}
	return result, nil
}

// diskExtentRE matches the data files of a virtual disk, as opposed to its descriptor.
var diskExtentRE = regexp.MustCompile(`-(flat|delta|sesparse|ctk|rdm|rdmp)\.vmdk$`)

// migrateSnapshot names the temporary snapshot a pre-copy migration takes.
const migrateSnapshot = "freecenter-migrate"

// removeMigrateSnapshot deletes the temporary migration snapshot of a VM, if
// it has one, and reports whether none is left.
func removeMigrateSnapshot(ctx context.Context, c *Client, id string) bool {
	return removeSnapshotNamed(ctx, c, id, migrateSnapshot)
}

// listFiles returns the names of the plain files in dir matching the grep
// pattern ("" = all), skipping lock and swap files that cannot be copied.
func listFiles(ctx context.Context, c *Client, dir, pattern string) ([]string, error) {
	cmd := "ls -p " + shq(dir) + " | grep -v '/$' | grep -v -e '\\.lck$' -e '\\.vswp$'"
	if pattern != "" {
		cmd += " | grep -e " + shq(pattern)
	}
	out, err := c.sshRun(ctx, cmd+"; true")
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", dir, err)
	}
	// One name per line: file names may contain spaces.
	var files []string
	for _, l := range strings.Split(strings.ReplaceAll(out, "\r", ""), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			files = append(files, l)
		}
	}
	return files, nil
}

func without(all, drop []string) []string {
	skip := map[string]bool{}
	for _, f := range drop {
		skip[f] = true
	}
	var out []string
	for _, f := range all {
		if !skip[f] {
			out = append(out, f)
		}
	}
	return out
}

// copyFiles pushes the named files of dir on src into targetDir on dst.
func copyFiles(ctx context.Context, src, dst *Client, sshOpts, dir string, files []string, targetDir string) error {
	if len(files) == 0 {
		return nil
	}
	quoted := make([]string, len(files))
	for i, f := range files {
		quoted[i] = shq(f)
	}
	cmd := fmt.Sprintf("cd %s && scp -O -q %s -P %d %s %s", shq(dir), sshOpts, sshPort(dst.rec), strings.Join(quoted, " "), shq(dst.rec.Username+"@"+hostOf(dst.rec.Address)+":"+targetDir+"/"))
	if out, err := src.sshRun(ctx, cmd); err != nil {
		return fmt.Errorf("copy to the target host failed: %s", remoteFailure(err, out))
	}
	return nil
}

// migrateShared moves a VM whose files are on a datastore both hosts mount:
// nothing is copied, the VM is unregistered on src and registered on dst. On
// failure it is registered on src again. The files are never deleted.
func migrateShared(ctx context.Context, src, dst *Client, id string, layout *vmLayout, req *model.MigrateRequest, progress MigrationProgress) (*MigrationResult, error) {
	report := func(phase string) {
		if progress != nil {
			progress(phase, 0, 0)
		}
	}
	running := layout.power == "poweredOn"
	targetVMX := "/vmfs/volumes/" + req.TargetDatastore + "/" + layout.dirName + "/" + path.Base(layout.vmxFS)
	if out, _ := dst.sshRun(ctx, "[ -f "+shq(targetVMX)+" ] && echo ok"); strings.TrimSpace(out) != "ok" {
		return nil, fmt.Errorf("%w: %s is not visible on the target host", ErrInvalidState, targetVMX)
	}

	if running {
		if req.Mode == model.MigrateSuspend {
			report("Suspending the virtual machine")
			if _, err := src.SetPower(ctx, id, "suspend"); err != nil {
				return nil, err
			}
			if err := waitPower(ctx, src, id, "suspended", powerOnTimeout); err != nil {
				return nil, err
			}
		} else {
			report("Shutting down the guest operating system")
			if err := shutDown(ctx, src, id); err != nil {
				return nil, err
			}
		}
	}
	restart := func(c *Client, vmid string) error {
		if !running {
			return nil
		}
		if _, err := c.SetPower(ctx, vmid, "on"); err != nil {
			return err
		}
		return waitPower(ctx, c, vmid, "poweredOn", powerOnTimeout)
	}

	report("Unregistering the virtual machine from the source host")
	if out, err := src.sshRun(ctx, "vim-cmd vmsvc/unregister "+shq(id)); err != nil {
		cause := fmt.Errorf("unregister on the source host: %s", remoteFailure(err, out))
		if rerr := restart(src, id); rerr != nil {
			return nil, fmt.Errorf("%w; the virtual machine could not be started again: %v", cause, rerr)
		}
		return nil, cause
	}
	// From here the VM is registered nowhere: any failure must register it on the source again.
	backToSource := func(cause error) error {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
		defer cancel()
		out, err := src.sshRun(rctx, "vim-cmd solo/registervm "+shq(layout.vmxFS))
		if err != nil {
			return fmt.Errorf("%w; the virtual machine could NOT be registered on the source host again (%s): register %s manually", cause, remoteFailure(err, out), layout.vmxFS)
		}
		if running {
			if _, err := src.SetPower(rctx, strings.TrimSpace(out), "on"); err != nil {
				return fmt.Errorf("%w; the virtual machine is registered on the source host again but could not be started: %v", cause, err)
			}
		}
		return fmt.Errorf("%w; the virtual machine was restored on the source host", cause)
	}

	report("Registering the virtual machine on the target host")
	warnings, err := adaptVMX(ctx, dst, targetVMX)
	if err != nil {
		return nil, backToSource(err)
	}
	out, err := dst.sshRun(ctx, "vim-cmd solo/registervm "+shq(targetVMX))
	newID := strings.TrimSpace(out)
	if err == nil {
		_, err = strconv.Atoi(newID)
	}
	if err != nil {
		return nil, backToSource(fmt.Errorf("register on the target host: %s", remoteFailure(err, out)))
	}
	if running {
		report("Powering on the virtual machine on the target host")
		if err := restart(dst, newID); err != nil {
			rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
			_, _ = dst.sshRun(rctx, "vim-cmd vmsvc/power.off "+shq(newID)+" >/dev/null 2>&1; vim-cmd vmsvc/unregister "+shq(newID))
			cancel()
			return nil, backToSource(fmt.Errorf("power on on the target host: %w", err))
		}
	}
	return &MigrationResult{NewVMID: newID, Warnings: warnings}, nil
}

// remoteFailure describes a failed remote command for the user: what the
// command printed, or how it ended, without echoing the whole command line.
func remoteFailure(err error, out string) string {
	if out = strings.TrimSpace(out); out != "" {
		return out
	}
	msg := err.Error()
	if i := strings.LastIndex(msg, "\": "); i >= 0 {
		msg = msg[i+3:]
	}
	return msg
}

func sshPort(rec *model.HostRecord) int {
	if rec.SSHPort > 0 {
		return rec.SSHPort
	}
	return 22
}

// prepareHostChannel makes src able to open SSH connections to dst without a
// password: a key pair on src (created once), its public half authorised on
// dst (limited to connections coming from src), the outgoing SSH firewall rule
// on src, and dst's host key pinned to the one FreeCenter itself sees.
// It returns the ssh/scp options to use on src.
func prepareHostChannel(ctx context.Context, src, dst *Client) (string, error) {
	// FIPS-mode OpenSSH on ESXi rejects ed25519: use ECDSA.
	keygen := fmt.Sprintf("[ -f %[1]s ] || /usr/lib/vmware/openssh/bin/ssh-keygen -q -t ecdsa -b 256 -N '' -C freecenter-migrate -f %[1]s; chmod 600 %[1]s; cat %[1]s.pub", migrateKeyPath)
	pub, err := src.sshRun(ctx, keygen)
	if err != nil {
		return "", fmt.Errorf("create the migration key on %s: %w (output: %s)", src.rec.Name, err, strings.TrimSpace(pub))
	}
	fields := strings.Fields(strings.TrimSpace(pub))
	if len(fields) < 2 {
		return "", fmt.Errorf("create the migration key on %s: unexpected public key %q", src.rec.Name, pub)
	}
	if _, _, _, _, err := ssh.ParseAuthorizedKey([]byte(fields[0] + " " + fields[1])); err != nil {
		return "", fmt.Errorf("create the migration key on %s: %w", src.rec.Name, err)
	}
	entry := fields[0] + " " + fields[1] + " freecenter-migrate:" + src.rec.Name
	if ip := net.ParseIP(hostOf(src.rec.Address)); ip != nil {
		entry = `from="` + ip.String() + `" ` + entry
	}
	authorize := fmt.Sprintf("grep -qF %s %s 2>/dev/null || echo %s >> %s", shq(fields[1]), authorizedKeys, shq(entry), authorizedKeys)
	if out, err := dst.sshRun(ctx, authorize); err != nil {
		return "", fmt.Errorf("authorise the migration key on %s: %w (output: %s)", dst.rec.Name, err, strings.TrimSpace(out))
	}

	line, algo, err := dst.ssh.HostKeyLine(ctx)
	if err != nil {
		return "", fmt.Errorf("read the host key of %s: %w", dst.rec.Name, err)
	}
	if algo == ssh.KeyAlgoRSA {
		algo = ssh.KeyAlgoRSASHA512 + "," + ssh.KeyAlgoRSASHA256
	}
	knownHosts := "/tmp/freecenter_known_hosts_" + dst.rec.ID
	setup := fmt.Sprintf("echo %s > %s && esxcli network firewall ruleset set -e true -r sshClient", shq(line), shq(knownHosts))
	if out, err := src.sshRun(ctx, setup); err != nil {
		return "", fmt.Errorf("prepare %s for outgoing SSH: %w (output: %s)", src.rec.Name, err, strings.TrimSpace(out))
	}

	opts := fmt.Sprintf("-i %s -o BatchMode=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=%s -o HostKeyAlgorithms=%s -o ConnectTimeout=15", migrateKeyPath, knownHosts, algo)
	probe := fmt.Sprintf("ssh %s -p %d %s true", opts, sshPort(dst.rec), shq(dst.rec.Username+"@"+hostOf(dst.rec.Address)))
	if out, err := src.sshRun(ctx, probe); err != nil {
		return "", fmt.Errorf("%s cannot open an SSH connection to %s: %w (output: %s)", src.rec.Name, dst.rec.Name, err, strings.TrimSpace(out))
	}
	return opts, nil
}

// shutDown stops a running VM: a guest shutdown when VMware Tools answer,
// otherwise (or when the guest does not stop in time) a hard power off.
func shutDown(ctx context.Context, c *Client, id string) error {
	if _, err := c.SetPower(ctx, id, "shutdown_guest"); err == nil {
		if waitPower(ctx, c, id, "poweredOff", guestShutdownTimeout) == nil {
			return nil
		}
	}
	if _, err := c.SetPower(ctx, id, "off"); err != nil {
		return err
	}
	return waitPower(ctx, c, id, "poweredOff", powerOnTimeout)
}

func waitPower(ctx context.Context, c *Client, id, want string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		st, err := readPowerState(c, ctx, id)
		if err == nil && st == want {
			return nil
		}
		if errors.Is(err, ErrNotFound) {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for the virtual machine to be %s (it is %s)", want, st)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-after(2 * time.Second):
		}
	}
}

// watchCopy reports the size of the growing copy until the returned stop function is called.
func watchCopy(ctx context.Context, dst *Client, dir string, report func(done int64)) (stop func()) {
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-time.After(progressInterval):
			}
			if n, err := dirSize(ctx, dst, dir); err == nil {
				report(n)
			}
		}
	})
	return func() {
		close(done)
		wg.Wait()
	}
}

// adaptVMX fixes the copied VMX for its new host: keep UUID and MAC addresses
// without the "moved or copied" question, drop host-specific derived paths,
// and disconnect CD/DVD images that do not exist on this host.
func adaptVMX(ctx context.Context, dst *Client, vmxFS string) (warnings []string, err error) {
	content, err := dst.sshRun(ctx, "cat "+shq(vmxFS))
	if err != nil {
		return nil, fmt.Errorf("read the copied vmx: %w", err)
	}
	doc := parseVMX(content)
	doc.set("uuid.action", "keep")
	doc.removePrefix("sched.swap.derivedName")

	for _, key := range append(doc.keysWithPrefix("ide"), doc.keysWithPrefix("sata")...) {
		if !strings.HasSuffix(key, ".deviceType") {
			continue
		}
		if v, _ := doc.get(key); v != "cdrom-image" {
			continue
		}
		prefix := strings.TrimSuffix(key, ".deviceType")
		iso, _ := doc.get(prefix + ".fileName")
		if !strings.HasPrefix(iso, "/") {
			continue // relative to the VM folder: copied along
		}
		if out, _ := dst.sshRun(ctx, "[ -e "+shq(iso)+" ] && echo ok"); strings.TrimSpace(out) == "ok" {
			continue
		}
		setCdrom(doc, prefix, "")
		warnings = append(warnings, fmt.Sprintf("CD/DVD drive %s was disconnected: %s is not on the target host", prefix, iso))
	}

	cmd := fmt.Sprintf("cat > %s <<'FCVMXEOF'\n%sFCVMXEOF\n", shq(vmxFS), doc.String())
	if out, err := dst.sshRun(ctx, cmd); err != nil {
		return nil, fmt.Errorf("write the copied vmx: %w (output: %s)", err, out)
	}
	return warnings, nil
}
