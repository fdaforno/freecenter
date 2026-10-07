package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"freecenter/internal/esxi"
	"freecenter/internal/model"
)

func validateBackupRequest(req *model.BackupRequest) string {
	switch {
	case !vmxValueRE.MatchString(req.Datastore):
		return "datastore is required"
	case req.Keep < 0 || req.Keep > 365:
		return "keep must be between 0 and 365"
	case req.TargetHostID != "" && !vmxValueRE.MatchString(req.TargetDatastore):
		return "target_datastore is required with target_host_id"
	}
	return ""
}

// backupVM backs a VM up and, when the request names another host, ships the
// backup there. It is shared by the manual action and the scheduler.
func (s *Server) backupVM(ctx context.Context, hostID, vmID string, req *model.BackupRequest, progress esxi.CloneProgress) error {
	c, err := s.mgr.Client(hostID)
	if err != nil {
		return err
	}
	vm, err := c.GetVM(ctx, vmID)
	if err != nil {
		return err
	}
	remote := req.TargetHostID != "" && req.TargetHostID != hostID
	keep := req.Keep
	if remote {
		keep = 0 // retention applies at the destination
	}
	stamp, err := c.BackupVM(ctx, vmID, req.Datastore, keep, progress)
	if err != nil || !remote {
		return err
	}
	if _, err := s.store.Host(req.TargetHostID); err != nil {
		return fmt.Errorf("target host: %w", err)
	}
	dst, err := s.mgr.Client(req.TargetHostID)
	if err != nil {
		return err
	}
	return esxi.ShipBackup(ctx, c, dst, req.Datastore, vm.Name, stamp, req.TargetDatastore, req.Keep, progress)
}

func (s *Server) handleBackupVM(w http.ResponseWriter, r *http.Request) {
	c, err := s.client(r)
	if err != nil {
		apiError(w, err)
		return
	}
	req := &model.BackupRequest{}
	if !decodeBody(w, r, req) {
		return
	}
	if msg := validateBackupRequest(req); msg != "" {
		errJSON(w, http.StatusBadRequest, msg)
		return
	}
	hid, vmid := r.PathValue("hid"), r.PathValue("vmid")
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	vm, err := c.GetVM(ctx, vmid)
	cancel()
	if err != nil {
		apiError(w, err)
		return
	}
	init := model.JobStatus{Kind: "backup", VMName: vm.Name, SourceHostID: hid, TargetHostID: req.TargetHostID}
	writeJSON(w, http.StatusAccepted, s.startJob(r, init, func(ctx context.Context, j *job) (jobResult, error) {
		return jobResult{}, s.backupVM(ctx, hid, vmid, req, func(phase string) {
			j.update(func(st *model.JobStatus) { st.Phase = phase })
		})
	}))
}

// handleListBackups lists the backups found on every connected host.
func (s *Server) handleListBackups(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	hosts := s.store.Hosts()
	perHost := make([][]model.Backup, len(hosts))
	var wg sync.WaitGroup
	for i, h := range hosts {
		wg.Go(func() {
			if !h.SSHEnabled {
				return
			}
			c, err := s.mgr.Client(h.ID)
			if err != nil {
				return // an unreachable host simply contributes nothing
			}
			list, err := cached(s.cache, ctx, h.ID+"/backups", c.ListBackups)
			if err != nil {
				return
			}
			for _, b := range list {
				b.HostID = h.ID
				perHost[i] = append(perHost[i], b)
			}
		})
	}
	wg.Wait()
	all := []model.Backup{}
	for _, l := range perHost {
		all = append(all, l...)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Stamp > all[j].Stamp })
	writeJSON(w, 200, all)
}

func (s *Server) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	s.hostOp(w, r, func(ctx context.Context, c *esxi.Client) (any, error) {
		return nil, c.DeleteBackup(ctx, r.PathValue("ds"), r.PathValue("vm"), r.PathValue("stamp"))
	})
}

func (s *Server) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	c, err := s.client(r)
	if err != nil {
		apiError(w, err)
		return
	}
	req := &model.RestoreRequest{}
	if !decodeBody(w, r, req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if !vmxValueRE.MatchString(req.Name) || strings.HasPrefix(req.Name, ".") {
		errJSON(w, http.StatusBadRequest, "name must be 1-80 characters without quotes or slashes")
		return
	}
	if req.Datastore != "" && !vmxValueRE.MatchString(req.Datastore) {
		errJSON(w, http.StatusBadRequest, "invalid datastore")
		return
	}
	hid, ds, vm, stamp := r.PathValue("hid"), r.PathValue("ds"), r.PathValue("vm"), r.PathValue("stamp")
	init := model.JobStatus{Kind: "restore", VMName: req.Name, SourceHostID: hid, TargetHostID: hid}
	writeJSON(w, http.StatusAccepted, s.startJob(r, init, func(ctx context.Context, j *job) (jobResult, error) {
		newID, err := c.RestoreBackup(ctx, ds, vm, stamp, req, func(phase string) {
			j.update(func(st *model.JobStatus) { st.Phase = phase })
		})
		return jobResult{NewVMID: newID}, err
	}))
}
