package esxi

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"freecenter/internal/model"
)

// Backups.
//
// A backup is a self-contained copy of a VM in
//
//	/vmfs/volumes/<datastore>/freecenter-backups/<vm name>/<YYYYMMDD-HHMMSS>/
//
// with every disk consolidated into a single thin VMDK and the VMX pointing
// at those copies, so the folder can be registered (restored) as a VM. A
// running VM is backed up through a temporary snapshot, without stopping it;
// the copy is then crash-consistent, like a machine that lost power.

const (
	backupRoot     = "freecenter-backups"
	backupSnapshot = "freecenter-backup"
	stampLayout    = "20060102-150405"
)

var stampRE = regexp.MustCompile(`^\d{8}-\d{6}$`)

// backupNameRE restricts the VM folder name inside the backup root.
var backupNameRE = regexp.MustCompile(`^[^"'/\\\x00-\x1f]{1,80}$`)

func backupDir(datastore, vmName, stamp string) string {
	return "/vmfs/volumes/" + datastore + "/" + backupRoot + "/" + vmName + "/" + stamp
}

// BackupVM writes a backup of VM id to datastore and returns its stamp. keep
// > 0 then prunes the oldest backups of that VM on the datastore.
func (c *Client) BackupVM(ctx context.Context, id, datastore string, keep int, progress CloneProgress) (stamp string, err error) {
	if c.ssh == nil {
		return "", ErrSSHDisabled
	}
	report := func(format string, a ...any) {
		if progress != nil {
			progress(fmt.Sprintf(format, a...))
		}
	}
	layout, vm, err := c.vmLayout(ctx, id)
	if err != nil {
		return "", err
	}
	if !backupNameRE.MatchString(vm.Name) {
		return "", fmt.Errorf("%w: the name of the virtual machine cannot be used as a folder name", ErrInvalidState)
	}
	if out, _ := c.sshRun(ctx, "[ -d "+shq("/vmfs/volumes/"+datastore)+" ] && echo ok"); strings.TrimSpace(out) != "ok" {
		return "", fmt.Errorf("%w: datastore %s", ErrNotFound, datastore)
	}
	// The VMX as it is now: after the snapshot it points at the delta disks.
	content, err := c.sshRun(ctx, "cat "+shq(layout.vmxFS))
	if err != nil {
		return "", fmt.Errorf("read vmx: %w", err)
	}
	doc := parseVMX(content)
	diskKeys := vmxDiskKeys(doc)

	stamp = time.Now().UTC().Format(stampLayout)
	target := backupDir(datastore, vm.Name, stamp)
	if out, err := c.sshRun(ctx, "mkdir -p "+shq(target)); err != nil {
		return "", fmt.Errorf("create the backup folder: %s", remoteFailure(err, out))
	}
	cleanup := context.WithoutCancel(ctx)
	fail := func(err error) (string, error) {
		_, _ = c.sshRun(cleanup, "rm -rf "+shq(target))
		return "", err
	}

	// A running VM locks its disks: freeze them with a snapshot, which vmkfstools can clone.
	if layout.power == "poweredOn" {
		report("Taking a temporary snapshot")
		if _, err := c.CreateSnapshot(ctx, id, &model.CreateSnapshotRequest{Name: backupSnapshot, Description: "Temporary snapshot taken by FreeCenter for a backup"}); err != nil {
			return fail(err)
		}
		defer func() {
			report("Removing the temporary snapshot")
			if !removeSnapshotNamed(cleanup, c, id, backupSnapshot) && err == nil {
				err = fmt.Errorf("the backup was written, but the temporary snapshot %q could not be removed: delete it from the Snapshots tab", backupSnapshot)
			}
		}()
	}

	for i, key := range diskKeys {
		ref, _ := doc.get(key)
		src := ref
		if !strings.HasPrefix(src, "/") {
			src = layout.dir + "/" + ref
		}
		name := diskFileName(vm.Name, i)
		report("Copying disk %d of %d", i+1, len(diskKeys))
		if out, err := c.sshRun(ctx, fmt.Sprintf("vmkfstools -i %s -d thin %s", shq(src), shq(target+"/"+name))); err != nil {
			return fail(fmt.Errorf("copy disk %s: %s", path.Base(src), remoteFailure(err, lastLine(out))))
		}
		doc.set(key, name)
	}

	report("Saving the configuration")
	for _, k := range []string{"sched.swap.derivedName", "checkpoint.vmState", "migrate.hostlog"} {
		doc.removePrefix(k)
	}
	if nvram, ok := doc.get("nvram"); ok && !strings.HasPrefix(nvram, "/") {
		_, _ = c.sshRun(ctx, fmt.Sprintf("cp %s %s 2>/dev/null", shq(layout.dir+"/"+nvram), shq(target+"/"+nvram)))
	}
	cmd := fmt.Sprintf("cat > %s <<'FCVMXEOF'\n%sFCVMXEOF\n", shq(target+"/"+vm.Name+".vmx"), doc.String())
	if out, err := c.sshRun(ctx, cmd); err != nil {
		return fail(fmt.Errorf("write the backup configuration: %w (output: %s)", err, out))
	}
	if keep > 0 {
		report("Removing old backups")
		if err := c.PruneBackups(ctx, datastore, vm.Name, keep); err != nil {
			return stamp, err
		}
	}
	return stamp, nil
}

// removeSnapshotNamed deletes the snapshots of a VM with the given name and
// reports whether none is left.
func removeSnapshotNamed(ctx context.Context, c *Client, id, name string) bool {
	snaps, err := c.ListSnapshots(ctx, id)
	if err != nil {
		return false
	}
	ok := true
	for _, s := range snaps {
		if s.Name == name {
			ok = c.DeleteSnapshot(ctx, id, s.ID, false) == nil && ok
		}
	}
	return ok
}

// backupStamps lists the backup stamps of one VM on a datastore, oldest first.
func (c *Client) backupStamps(ctx context.Context, datastore, vmName string) ([]string, error) {
	out, err := c.sshRun(ctx, "ls "+shq("/vmfs/volumes/"+datastore+"/"+backupRoot+"/"+vmName)+" 2>/dev/null; true")
	if err != nil {
		return nil, err
	}
	var stamps []string
	for _, l := range strings.Fields(out) {
		if stampRE.MatchString(l) {
			stamps = append(stamps, l)
		}
	}
	sort.Strings(stamps)
	return stamps, nil
}

// PruneBackups keeps the newest keep backups of a VM on a datastore.
func (c *Client) PruneBackups(ctx context.Context, datastore, vmName string, keep int) error {
	stamps, err := c.backupStamps(ctx, datastore, vmName)
	if err != nil {
		return err
	}
	for len(stamps) > keep {
		if err := c.DeleteBackup(ctx, datastore, vmName, stamps[0]); err != nil {
			return err
		}
		stamps = stamps[1:]
	}
	return nil
}

// DeleteBackup removes one backup. Only folders with the backup layout are
// ever removed, so a wrong argument cannot reach other data.
func (c *Client) DeleteBackup(ctx context.Context, datastore, vmName, stamp string) error {
	if c.ssh == nil {
		return ErrSSHDisabled
	}
	if !backupNameRE.MatchString(datastore) || !backupNameRE.MatchString(vmName) || !stampRE.MatchString(stamp) || vmName == "." || vmName == ".." {
		return fmt.Errorf("invalid backup reference")
	}
	dir := backupDir(datastore, vmName, stamp)
	// Drop the VM's folder, and the backup root, once they are empty (rmdir refuses otherwise).
	vmDir := path.Dir(dir)
	cmd := fmt.Sprintf("[ -d %s ] || { echo missing; exit 0; }; rm -rf %s; rmdir %s 2>/dev/null; rmdir %s 2>/dev/null; true", shq(dir), shq(dir), shq(vmDir), shq(path.Dir(vmDir)))
	out, err := c.sshRun(ctx, cmd)
	if err != nil {
		return fmt.Errorf("delete backup: %s", remoteFailure(err, out))
	}
	if strings.Contains(out, "missing") {
		return fmt.Errorf("%w: backup %s of %s on %s", ErrNotFound, stamp, vmName, datastore)
	}
	return nil
}

// ListBackups finds the backups on every datastore of the host, newest first.
func (c *Client) ListBackups(ctx context.Context) ([]model.Backup, error) {
	if c.ssh == nil {
		return nil, ErrSSHDisabled
	}
	stores, err := c.Datastores(ctx)
	if err != nil {
		return nil, err
	}
	var script strings.Builder
	for _, ds := range stores {
		if !ds.Accessible {
			continue
		}
		root := "/vmfs/volumes/" + ds.Name + "/" + backupRoot
		// One line per backup: datastore|vm|stamp|allocated KB.
		fmt.Fprintf(&script, "for d in %s/*/*/; do [ -d \"$d\" ] || continue; s=$(basename \"$d\"); v=$(basename \"$(dirname \"$d\")\"); echo %s\"|$v|$s|$(du -sk \"$d\" | cut -f1)\"; done 2>/dev/null\n", shq(root), shq(ds.Name))
	}
	script.WriteString("true\n")
	out, err := c.sshRun(ctx, script.String())
	if err != nil {
		return nil, fmt.Errorf("list backups: %w", err)
	}
	backups := []model.Backup{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(strings.TrimSpace(line), "|")
		if len(f) != 4 || !stampRE.MatchString(f[2]) {
			continue
		}
		created, _ := time.Parse(stampLayout, f[2])
		kb, _ := strconv.ParseInt(f[3], 10, 64)
		backups = append(backups, model.Backup{Datastore: f[0], VMName: f[1], Stamp: f[2], Created: created, SizeBytes: kb * 1024})
	}
	sort.Slice(backups, func(i, j int) bool { return backups[i].Stamp > backups[j].Stamp })
	return backups, nil
}

// RestoreBackup creates a new VM from a backup on this host. The backup is
// left untouched; the new VM gets its own identity (UUID, MAC addresses), so
// it can coexist with the original.
func (c *Client) RestoreBackup(ctx context.Context, datastore, vmName, stamp string, req *model.RestoreRequest, progress CloneProgress) (string, error) {
	if c.ssh == nil {
		return "", ErrSSHDisabled
	}
	if !backupNameRE.MatchString(datastore) || !backupNameRE.MatchString(vmName) || !stampRE.MatchString(stamp) {
		return "", fmt.Errorf("invalid backup reference")
	}
	vmx := backupDir(datastore, vmName, stamp) + "/" + vmName + ".vmx"
	if out, _ := c.sshRun(ctx, "[ -f "+shq(vmx)+" ] && echo ok"); strings.TrimSpace(out) != "ok" {
		return "", fmt.Errorf("%w: backup %s of %s on %s", ErrNotFound, stamp, vmName, datastore)
	}
	clone := &model.CloneRequest{Name: req.Name, Datastore: req.Datastore, PowerOn: req.PowerOn}
	if clone.Datastore == "" {
		clone.Datastore = datastore
	}
	// Backup disks are always thin.
	return c.cloneFolder(ctx, vmx, func(string) bool { return true }, clone, progress)
}

// ShipBackup moves a backup from src to a datastore of dst, host to host
// (same channel as a migration), then prunes the target to keep backups.
func ShipBackup(ctx context.Context, src, dst *Client, srcDS, vmName, stamp, dstDS string, keep int, progress CloneProgress) error {
	report := func(phase string) {
		if progress != nil {
			progress(phase)
		}
	}
	if !backupNameRE.MatchString(dstDS) {
		return fmt.Errorf("invalid target datastore")
	}
	local := backupDir(srcDS, vmName, stamp)
	remoteParent := path.Dir(backupDir(dstDS, vmName, stamp))
	if out, _ := dst.sshRun(ctx, "[ -d "+shq("/vmfs/volumes/"+dstDS)+" ] && echo ok"); strings.TrimSpace(out) != "ok" {
		return fmt.Errorf("%w: datastore %s on %s", ErrNotFound, dstDS, dst.rec.Name)
	}
	report("Preparing the connection between the hosts")
	opts, err := prepareHostChannel(ctx, src, dst)
	if err != nil {
		return err
	}
	if out, err := dst.sshRun(ctx, "mkdir -p "+shq(remoteParent)); err != nil {
		return fmt.Errorf("create the backup folder on %s: %s", dst.rec.Name, remoteFailure(err, out))
	}
	report("Copying the backup to " + dst.rec.Name)
	cmd := fmt.Sprintf("scp -O -r -q %s -P %d %s %s", opts, sshPort(dst.rec), shq(local), shq(dst.rec.Username+"@"+hostOf(dst.rec.Address)+":"+remoteParent+"/"))
	if out, err := src.sshRun(ctx, cmd); err != nil {
		_, _ = dst.sshRun(context.WithoutCancel(ctx), "rm -rf "+shq(remoteParent+"/"+stamp))
		return fmt.Errorf("copy the backup to %s: %s (the backup is still on %s)", dst.rec.Name, remoteFailure(err, out), srcDS)
	}
	// The copy travelled fully allocated: make its disks thin again.
	_, _ = dst.sshRun(ctx, fmt.Sprintf("for f in %s/*.vmdk; do case \"$f\" in *-flat.vmdk) ;; *) vmkfstools -K \"$f\" >/dev/null 2>&1 ;; esac; done; true", shq(remoteParent+"/"+stamp)))
	if err := src.DeleteBackup(ctx, srcDS, vmName, stamp); err != nil {
		return err
	}
	if keep > 0 {
		return dst.PruneBackups(ctx, dstDS, vmName, keep)
	}
	return nil
}
