package esxi

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"freecenter/internal/model"
)

// vmxFileRE accepts a configuration file inside a datastore: no quotes or
// control characters, as the path ends up in a shell command and in messages.
var vmxFileRE = regexp.MustCompile(`^[^"'\\\x00-\x1f]{1,240}\.vmx$`)

// RegisterVM adds an existing virtual machine to the inventory of the host
// from its configuration file on a datastore: a VM that was unregistered, or
// one whose folder was copied or restored by other means. Nothing is copied
// or modified.
func (c *Client) RegisterVM(ctx context.Context, datastore, file string) (*model.VM, error) {
	if err := c.Ensure(ctx); err != nil {
		return nil, err
	}
	if c.ssh == nil {
		return nil, ErrSSHDisabled
	}
	file = strings.TrimPrefix(path.Clean("/"+file), "/")
	if !backupNameRE.MatchString(datastore) || !vmxFileRE.MatchString(file) {
		return nil, fmt.Errorf("the file to register must be a .vmx file on a datastore")
	}
	dsPath := fmt.Sprintf("[%s] %s", datastore, file)
	vmxFS := dsPathToFS(dsPath)

	vms, err := c.allVMRefs(ctx)
	if err != nil {
		return nil, err
	}
	for i := range vms {
		if cfg := vms[i].Summary.Config; cfg.VmPathName == dsPath {
			return nil, fmt.Errorf("%w: %s is already registered as %s", ErrInvalidState, dsPath, cfg.Name)
		}
	}
	if out, err := c.sshRun(ctx, "[ -f "+shq(vmxFS)+" ]"); err != nil {
		return nil, fmt.Errorf("%w: %s (%s)", ErrNotFound, dsPath, strings.TrimSpace(out))
	}

	out, err := c.sshRun(ctx, "vim-cmd solo/registervm "+shq(vmxFS))
	if err != nil {
		return nil, fmt.Errorf("register virtual machine: %s", remoteFailure(err, out))
	}
	id, err := parseFirstInt(out)
	if err != nil {
		return nil, fmt.Errorf("register virtual machine: %w", err)
	}
	vmid := strconv.Itoa(id)
	// A freshly registered VM may not be readable right away.
	for i := 0; ; i++ {
		vm, err := c.GetVM(ctx, vmid)
		if err == nil {
			return vm, nil
		}
		if i == 10 {
			return nil, fmt.Errorf("the virtual machine was registered with id %s but cannot be read yet: %w", vmid, err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-after(500 * time.Millisecond):
		}
	}
}
