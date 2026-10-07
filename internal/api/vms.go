package api

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"

	"freecenter/internal/model"
)

var vmNameRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

func (s *Server) handleListVMs(w http.ResponseWriter, r *http.Request) {
	c, err := s.client(r)
	if err != nil {
		apiError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	hid := r.PathValue("hid")
	vms, err := cached(s.cache, ctx, hid+"/vms", func(ctx context.Context) ([]*model.VM, error) {
		vms, err := c.ListVMs(ctx)
		for _, vm := range vms {
			vm.HostID = hid
		}
		return vms, err
	})
	if err != nil {
		apiError(w, err)
		return
	}
	writeJSON(w, 200, vms)
}

func (s *Server) handleCreateVM(w http.ResponseWriter, r *http.Request) {
	c, err := s.client(r)
	if err != nil {
		apiError(w, err)
		return
	}
	var req model.CreateVMRequest
	if !decodeBody(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || !vmNameRE.MatchString(req.Name) {
		errJSON(w, http.StatusBadRequest, "name is required (alphanumeric, dots, dashes, underscores)")
		return
	}
	if req.GuestID == "" {
		errJSON(w, http.StatusBadRequest, "guest_id is required (e.g. ubuntu64Guest)")
		return
	}
	if req.Datastore == "" {
		errJSON(w, http.StatusBadRequest, "datastore is required")
		return
	}
	if req.NumCPUs < 1 || req.NumCPUs > 128 {
		errJSON(w, http.StatusBadRequest, "num_cpus must be between 1 and 128")
		return
	}
	if req.MemoryMB < 4 || req.MemoryMB > 2097152 {
		errJSON(w, http.StatusBadRequest, "memory_mb must be between 4 and 2097152")
		return
	}
	for _, d := range req.Disks {
		if d.SizeGB < 1 || d.SizeGB > 65536 {
			errJSON(w, http.StatusBadRequest, "disk size_gb must be between 1 and 65536")
			return
		}
	}
	if req.ISOPath != "" && !isoPathRE.MatchString(req.ISOPath) {
		errJSON(w, http.StatusBadRequest, "iso_path must look like [datastore] dir/file.iso")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	vm, err := c.CreateVM(ctx, &req)
	if err != nil {
		apiError(w, err)
		return
	}
	vm.HostID = r.PathValue("hid")
	writeJSON(w, 201, vm)
}

var isoPathRE = regexp.MustCompile(`^\[[^\]]+\] .+\.(iso|ISO)$`)

func (s *Server) handleGetVM(w http.ResponseWriter, r *http.Request) {
	c, err := s.client(r)
	if err != nil {
		apiError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	vm, err := c.GetVM(ctx, r.PathValue("vmid"))
	if err != nil {
		apiError(w, err)
		return
	}
	vm.HostID = r.PathValue("hid")
	writeJSON(w, 200, vm)
}

func (s *Server) handleUpdateVM(w http.ResponseWriter, r *http.Request) {
	c, err := s.client(r)
	if err != nil {
		apiError(w, err)
		return
	}
	var req model.UpdateVMRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.NumCPUs != nil && (*req.NumCPUs < 1 || *req.NumCPUs > 128) {
		errJSON(w, http.StatusBadRequest, "num_cpus must be between 1 and 128")
		return
	}
	if req.MemoryMB != nil && (*req.MemoryMB < 4 || *req.MemoryMB > 2097152) {
		errJSON(w, http.StatusBadRequest, "memory_mb must be between 4 and 2097152")
		return
	}
	for _, d := range req.ResizeDisks {
		if d.SizeGB < 1 || d.SizeGB > 65536 {
			errJSON(w, http.StatusBadRequest, "resize size_gb must be between 1 and 65536")
			return
		}
	}
	for _, d := range req.AddDisks {
		if d.SizeGB < 1 || d.SizeGB > 65536 {
			errJSON(w, http.StatusBadRequest, "disk size_gb must be between 1 and 65536")
			return
		}
	}
	if req.ISOPath != nil && *req.ISOPath != "" && !isoPathRE.MatchString(*req.ISOPath) {
		errJSON(w, http.StatusBadRequest, "iso_path must look like [datastore] dir/file.iso")
		return
	}
	if msg := validateVMSettings(&req); msg != "" {
		errJSON(w, http.StatusBadRequest, msg)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	vm, err := c.UpdateVM(ctx, r.PathValue("vmid"), &req)
	if err != nil {
		apiError(w, err)
		return
	}
	vm.HostID = r.PathValue("hid")
	writeJSON(w, 200, vm)
}

func (s *Server) handleDeleteVM(w http.ResponseWriter, r *http.Request) {
	c, err := s.client(r)
	if err != nil {
		apiError(w, err)
		return
	}
	deleteFiles := r.URL.Query().Get("delete_files") != "false"
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	if err := c.DeleteVM(ctx, r.PathValue("vmid"), deleteFiles); err != nil {
		apiError(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "deleted"})
}

func (s *Server) handlePowerVM(w http.ResponseWriter, r *http.Request) {
	c, err := s.client(r)
	if err != nil {
		apiError(w, err)
		return
	}
	var req model.PowerRequest
	if !decodeBody(w, r, &req) {
		return
	}
	switch req.Action {
	case "on", "off", "reset", "suspend", "shutdown_guest", "reboot_guest":
	default:
		errJSON(w, http.StatusBadRequest, "action must be one of on, off, reset, suspend, shutdown_guest, reboot_guest")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	state, err := c.SetPower(ctx, r.PathValue("vmid"), req.Action)
	if err != nil {
		apiError(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"power_state": state})
}

func (s *Server) handleListSnapshots(w http.ResponseWriter, r *http.Request) {
	c, err := s.client(r)
	if err != nil {
		apiError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	snaps, err := c.ListSnapshots(ctx, r.PathValue("vmid"))
	if err != nil {
		apiError(w, err)
		return
	}
	writeJSON(w, 200, snaps)
}

func (s *Server) handleCreateSnapshot(w http.ResponseWriter, r *http.Request) {
	c, err := s.client(r)
	if err != nil {
		apiError(w, err)
		return
	}
	var req model.CreateSnapshotRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		errJSON(w, http.StatusBadRequest, "name is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	snaps, err := c.CreateSnapshot(ctx, r.PathValue("vmid"), &req)
	if err != nil {
		apiError(w, err)
		return
	}
	writeJSON(w, 201, snaps)
}

func (s *Server) handleRevertSnapshot(w http.ResponseWriter, r *http.Request) {
	c, err := s.client(r)
	if err != nil {
		apiError(w, err)
		return
	}
	suppress := r.URL.Query().Get("suppress_power_on") == "true"
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	if err := c.RevertSnapshot(ctx, r.PathValue("vmid"), r.PathValue("snapid"), suppress); err != nil {
		apiError(w, err)
		return
	}
	snaps, _ := c.ListSnapshots(ctx, r.PathValue("vmid"))
	writeJSON(w, 200, snaps)
}

func (s *Server) handleDeleteSnapshot(w http.ResponseWriter, r *http.Request) {
	c, err := s.client(r)
	if err != nil {
		apiError(w, err)
		return
	}
	children := r.URL.Query().Get("children") == "true"
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	if err := c.DeleteSnapshot(ctx, r.PathValue("vmid"), r.PathValue("snapid"), children); err != nil {
		apiError(w, err)
		return
	}
	snaps, _ := c.ListSnapshots(ctx, r.PathValue("vmid"))
	writeJSON(w, 200, snaps)
}

func (s *Server) handleRemoveAllSnapshots(w http.ResponseWriter, r *http.Request) {
	c, err := s.client(r)
	if err != nil {
		apiError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	if err := c.RemoveAllSnapshots(ctx, r.PathValue("vmid")); err != nil {
		apiError(w, err)
		return
	}
	snaps, _ := c.ListSnapshots(ctx, r.PathValue("vmid"))
	writeJSON(w, 200, snaps)
}

// vmxValueRE accepts names that are safe as a VMX value and as a datastore
// folder: no quotes, slashes or control characters.
var vmxValueRE = regexp.MustCompile(`^[^"'/\\\x00-\x1f]{1,80}$`)

// validateVMSettings checks the option and device fields of an update request
// and returns a user-facing message, or "" when they are valid.
func validateVMSettings(req *model.UpdateVMRequest) string {
	if req.Name != nil && !vmxValueRE.MatchString(strings.TrimSpace(*req.Name)) {
		return "name must be 1-80 characters without quotes or slashes"
	}
	if req.GuestID != nil && !vmxValueRE.MatchString(*req.GuestID) {
		return "invalid guest_id"
	}
	if req.Firmware != nil && *req.Firmware != "bios" && *req.Firmware != "efi" {
		return "firmware must be bios or efi"
	}
	if req.BootDelayMs != nil && (*req.BootDelayMs < 0 || *req.BootDelayMs > 10000) {
		return "boot_delay_ms must be between 0 and 10000"
	}
	if req.USBController != nil && *req.USBController != "" && *req.USBController != "usb2" && *req.USBController != "usb3" {
		return "usb_controller must be empty, usb2 or usb3"
	}
	for _, d := range req.AddDisks {
		if d.Datastore != "" && !vmxValueRE.MatchString(d.Datastore) {
			return "invalid disk datastore"
		}
	}
	for _, n := range req.UpdateNICs {
		if n.Network != nil && !vmxValueRE.MatchString(*n.Network) {
			return "invalid network name"
		}
	}
	for _, c := range req.AddCdroms {
		if c.ISOPath != "" && !isoPathRE.MatchString(c.ISOPath) {
			return "cd/dvd iso_path must look like [datastore] dir/file.iso"
		}
	}
	for _, c := range req.UpdateCdroms {
		if c.ISOPath != "" && !isoPathRE.MatchString(c.ISOPath) {
			return "cd/dvd iso_path must look like [datastore] dir/file.iso"
		}
	}
	return ""
}
