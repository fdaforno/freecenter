package esxi

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"hostwarden/internal/model"
)

// Snapshots are managed via SSH (vim-cmd vmsvc/snapshot.*), like every other
// change to a host. Snapshot ids are the
// vim-cmd "Snapshot Id" values from `vim-cmd vmsvc/snapshot.get`.

var (
	snapNameRE = regexp.MustCompile(`^(-+)Snapshot Name\s*:\s*(.*)$`)
	snapIDRE   = regexp.MustCompile(`^(-+)Snapshot Id\s*:\s*(.*)$`)
	snapDescRE = regexp.MustCompile(`^(-+)Snapshot Desciption\s*:\s*(.*)$`)
	snapDateRE = regexp.MustCompile(`^(-+)Snapshot Created On\s*:\s*(.*)$`)
)

// ListSnapshots returns the flattened snapshot tree of a VM.
func (c *Client) ListSnapshots(ctx context.Context, id string) ([]model.Snapshot, error) {
	if err := c.Ensure(ctx); err != nil {
		return nil, err
	}
	if _, err := c.retrieveVM(ctx, id, []string{"name"}); err != nil {
		return nil, err
	}
	if c.ssh == nil {
		return nil, ErrSSHDisabled
	}
	out, err := c.sshRun(ctx, "vim-cmd vmsvc/snapshot.get "+shq(id))
	if err != nil {
		return nil, fmt.Errorf("snapshot.get: %w (output: %s)", err, out)
	}
	return parseSnapshotGet(out), nil
}

func parseSnapshotGet(out string) []model.Snapshot {
	type snapEntry struct {
		snap  model.Snapshot
		depth int
	}
	var entries []snapEntry
	for _, line := range strings.Split(out, "\n") {
		if m := snapNameRE.FindStringSubmatch(line); m != nil {
			depth := len(m[1]) - 2 // "--" = root (depth 0), "---" = child (depth 1)
			if depth < 0 {
				depth = 0
			}
			entries = append(entries, snapEntry{snap: model.Snapshot{Name: strings.TrimSpace(m[2])}, depth: depth})
			continue
		}
		if len(entries) == 0 {
			continue
		}
		cur := &entries[len(entries)-1]
		if m := snapIDRE.FindStringSubmatch(line); m != nil {
			cur.snap.ID = strings.TrimSpace(m[2])
		} else if m := snapDescRE.FindStringSubmatch(line); m != nil {
			cur.snap.Description = strings.TrimSpace(m[2])
		} else if m := snapDateRE.FindStringSubmatch(line); m != nil {
			cur.snap.CreateTime = strings.TrimSpace(m[2])
		}
	}
	// build paths and child flags
	snaps := make([]model.Snapshot, 0, len(entries))
	for i, e := range entries {
		path := e.snap.Name
		for j := i - 1; j >= 0; j-- {
			if entries[j].depth < e.depth {
				path = entries[j].snap.Name + "/" + path
				break
			}
		}
		e.snap.Path = path
		for j := i + 1; j < len(entries); j++ {
			if entries[j].depth <= e.depth {
				break
			}
			e.snap.HasChildren = true
		}
		snaps = append(snaps, e.snap)
	}
	return snaps
}

// CreateSnapshot creates a new snapshot.
func (c *Client) CreateSnapshot(ctx context.Context, id string, req *model.CreateSnapshotRequest) ([]model.Snapshot, error) {
	if err := c.Ensure(ctx); err != nil {
		return nil, err
	}
	if _, err := c.retrieveVM(ctx, id, []string{"name"}); err != nil {
		return nil, err
	}
	if c.ssh == nil {
		return nil, ErrSSHDisabled
	}
	mem := 0
	if req.Memory {
		mem = 1
	}
	qui := 0
	if req.Quiesce {
		qui = 1
	}
	cmd := fmt.Sprintf("vim-cmd vmsvc/snapshot.create %s %s %s %d %d", shq(id), shq(req.Name), shq(req.Description), mem, qui)
	if out, err := c.sshRun(ctx, cmd); err != nil {
		return nil, fmt.Errorf("snapshot.create: %w (output: %s)", err, out)
	}
	return c.ListSnapshots(ctx, id)
}

// RevertSnapshot reverts the VM to the given snapshot (id from ListSnapshots).
func (c *Client) RevertSnapshot(ctx context.Context, vmid, snapID string, suppressPowerOn bool) error {
	if err := c.Ensure(ctx); err != nil {
		return err
	}
	if c.ssh == nil {
		return ErrSSHDisabled
	}
	sup := 0
	if suppressPowerOn {
		sup = 1
	}
	cmd := fmt.Sprintf("vim-cmd vmsvc/snapshot.revert %s %s %d", shq(vmid), shq(snapID), sup)
	if out, err := c.sshRun(ctx, cmd); err != nil {
		return fmt.Errorf("snapshot.revert: %w (output: %s)", err, out)
	}
	return nil
}

// DeleteSnapshot removes a snapshot (and children when removeChildren).
func (c *Client) DeleteSnapshot(ctx context.Context, vmid, snapID string, removeChildren bool) error {
	if err := c.Ensure(ctx); err != nil {
		return err
	}
	if c.ssh == nil {
		return ErrSSHDisabled
	}
	rc := 0
	if removeChildren {
		rc = 1
	}
	cmd := fmt.Sprintf("vim-cmd vmsvc/snapshot.remove %s %s %d", shq(vmid), shq(snapID), rc)
	if out, err := c.sshRun(ctx, cmd); err != nil {
		return fmt.Errorf("snapshot.remove: %w (output: %s)", err, out)
	}
	return nil
}

// RemoveAllSnapshots removes every snapshot of the VM.
func (c *Client) RemoveAllSnapshots(ctx context.Context, vmid string) error {
	if err := c.Ensure(ctx); err != nil {
		return err
	}
	if c.ssh == nil {
		return ErrSSHDisabled
	}
	if out, err := c.sshRun(ctx, "vim-cmd vmsvc/snapshot.removeall "+shq(vmid)); err != nil {
		return fmt.Errorf("snapshot.removeall: %w (output: %s)", err, out)
	}
	return nil
}
