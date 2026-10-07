package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"freecenter/internal/model"
)

// Scheduled snapshots and backups.

const (
	schedulerTick = 30 * time.Second
	// autoSnapshotPrefix marks the snapshots a schedule owns (and may prune).
	autoSnapshotPrefix = "fc-auto-"
	scheduleTimeout    = 12 * time.Hour
)

// nextRun returns the first run time of sc strictly after t, in sc's time zone.
func nextRun(sc *model.Schedule, t time.Time) time.Time {
	loc, err := time.LoadLocation(sc.Timezone)
	if err != nil {
		loc = time.UTC
	}
	t = t.In(loc)
	switch sc.Frequency {
	case "hourly":
		n := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), sc.Minute, 0, 0, loc)
		if !n.After(t) {
			n = n.Add(time.Hour)
		}
		return n
	case "weekly":
		n := time.Date(t.Year(), t.Month(), t.Day(), sc.Hour, sc.Minute, 0, 0, loc)
		for int(n.Weekday()) != sc.Weekday || !n.After(t) {
			n = n.AddDate(0, 0, 1)
		}
		return n
	default: // daily
		n := time.Date(t.Year(), t.Month(), t.Day(), sc.Hour, sc.Minute, 0, 0, loc)
		if !n.After(t) {
			n = n.AddDate(0, 0, 1)
		}
		return n
	}
}

func (s *Server) schedulerLoop() {
	for range time.Tick(schedulerTick) {
		now := time.Now()
		for _, sc := range s.store.Schedules() {
			if sc.Enabled && !sc.NextRun.IsZero() && !sc.NextRun.After(now) {
				s.launchSchedule(sc)
			}
		}
	}
}

// launchSchedule runs sc in the background unless it is still running from a
// previous launch. It reports whether a run was started.
func (s *Server) launchSchedule(sc model.Schedule) bool {
	if _, busy := s.running.LoadOrStore(sc.ID, struct{}{}); busy {
		return false
	}
	// Move the next run on right away, so a long run is not launched twice.
	_ = s.store.UpdateSchedule(sc.ID, func(st *model.Schedule) { st.NextRun = nextRun(st, time.Now()) })
	go func() {
		defer s.running.Delete(sc.ID)
		name := "Scheduled snapshot"
		if sc.Kind == model.ScheduleBackup {
			name = "Scheduled backup"
		}
		task := &model.Task{Name: name, Target: sc.VMName, HostID: sc.HostID, Initiator: "scheduler", Status: "running", StartedAt: time.Now()}
		s.tasks.add(task)

		ctx, cancel := context.WithTimeout(context.Background(), scheduleTimeout)
		defer cancel()
		err := s.runSchedule(ctx, &sc, func(phase string) {
			s.tasks.update(task, func(t *model.Task) { t.Details = phase })
		})
		s.cache.invalidateHost(sc.HostID)

		now := time.Now()
		s.tasks.update(task, func(t *model.Task) {
			t.FinishedAt = &now
			t.Status, t.Details = "ok", ""
			if err != nil {
				t.Status, t.Details = "error", err.Error()
			}
		})
		_ = s.store.UpdateSchedule(sc.ID, func(st *model.Schedule) {
			st.LastRun = &now
			st.LastStatus, st.LastError = "ok", ""
			if err != nil {
				st.LastStatus, st.LastError = "error", err.Error()
			}
		})
		if err != nil {
			s.logger.Error("schedule failed", "kind", sc.Kind, "vm", sc.VMName, "err", err)
		}
	}()
	return true
}

func (s *Server) runSchedule(ctx context.Context, sc *model.Schedule, progress func(string)) error {
	if sc.Kind == model.ScheduleBackup {
		if sc.Backup == nil {
			return errors.New("the schedule has no backup destination")
		}
		req := *sc.Backup
		req.Keep = sc.Keep
		return s.backupVM(ctx, sc.HostID, sc.VMID, &req, progress)
	}
	c, err := s.mgr.Client(sc.HostID)
	if err != nil {
		return err
	}
	progress("Taking the snapshot")
	name := autoSnapshotPrefix + time.Now().UTC().Format("20060102-150405")
	snaps, err := c.CreateSnapshot(ctx, sc.VMID, &model.CreateSnapshotRequest{Name: name, Description: "Scheduled snapshot (FreeCenter)"})
	if err != nil {
		return err
	}
	if sc.Keep <= 0 {
		return nil
	}
	// Prune only the snapshots this mechanism created, oldest first (the name carries the time).
	var auto []model.Snapshot
	for _, sn := range snaps {
		if strings.HasPrefix(sn.Name, autoSnapshotPrefix) {
			auto = append(auto, sn)
		}
	}
	sort.Slice(auto, func(i, j int) bool { return auto[i].Name < auto[j].Name })
	for len(auto) > sc.Keep {
		progress("Removing old scheduled snapshots")
		if err := c.DeleteSnapshot(ctx, sc.VMID, auto[0].ID, false); err != nil {
			return fmt.Errorf("the snapshot was taken, but an old one could not be removed: %w", err)
		}
		auto = auto[1:]
	}
	return nil
}

// ---- API ----

func (s *Server) handleListSchedules(w http.ResponseWriter, _ *http.Request) {
	list := s.store.Schedules()
	sort.SliceStable(list, func(i, j int) bool { return list[i].VMName < list[j].VMName })
	writeJSON(w, 200, list)
}

func validateSchedule(sc *model.Schedule) string {
	switch {
	case sc.HostID == "" || sc.VMID == "":
		return "host_id and vm_id are required"
	case sc.Kind != model.ScheduleSnapshot && sc.Kind != model.ScheduleBackup:
		return "kind must be snapshot or backup"
	case sc.Frequency != "hourly" && sc.Frequency != "daily" && sc.Frequency != "weekly":
		return "frequency must be hourly, daily or weekly"
	case sc.Hour < 0 || sc.Hour > 23 || sc.Minute < 0 || sc.Minute > 59 || sc.Weekday < 0 || sc.Weekday > 6:
		return "invalid time of day or weekday"
	case sc.Keep < 0 || sc.Keep > 365:
		return "keep must be between 0 and 365"
	}
	if _, err := time.LoadLocation(sc.Timezone); err != nil {
		return "unknown time zone"
	}
	if sc.Kind == model.ScheduleBackup {
		if sc.Backup == nil {
			return "a backup schedule needs a destination"
		}
		return validateBackupRequest(sc.Backup)
	}
	return ""
}

// handlePutSchedule creates (POST) or replaces (PUT) a schedule.
func (s *Server) handlePutSchedule(w http.ResponseWriter, r *http.Request) {
	var sc model.Schedule
	if !decodeBody(w, r, &sc) {
		return
	}
	sc.ID = r.PathValue("id") // empty on POST
	if msg := validateSchedule(&sc); msg != "" {
		errJSON(w, http.StatusBadRequest, msg)
		return
	}
	c, err := s.mgr.Client(sc.HostID)
	if err != nil {
		apiError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	vm, err := c.GetVM(ctx, sc.VMID)
	if err != nil {
		apiError(w, err)
		return
	}
	sc.VMName = vm.Name
	if sc.Kind == model.ScheduleSnapshot {
		sc.Backup = nil
	}
	// Run bookkeeping is the server's: keep it across edits.
	sc.LastRun, sc.LastStatus, sc.LastError = nil, "", ""
	for _, old := range s.store.Schedules() {
		if old.ID == sc.ID {
			sc.LastRun, sc.LastStatus, sc.LastError = old.LastRun, old.LastStatus, old.LastError
		}
	}
	sc.NextRun = nextRun(&sc, time.Now())
	saved, err := s.store.PutSchedule(sc)
	if err != nil {
		apiError(w, err)
		return
	}
	writeJSON(w, 200, saved)
}

func (s *Server) handleDeleteSchedule(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteSchedule(r.PathValue("id")); err != nil {
		apiError(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "deleted"})
}

// handleRunSchedule starts a schedule now, without changing its timetable.
func (s *Server) handleRunSchedule(w http.ResponseWriter, r *http.Request) {
	for _, sc := range s.store.Schedules() {
		if sc.ID != r.PathValue("id") {
			continue
		}
		next := sc.NextRun
		if !s.launchSchedule(sc) {
			errJSON(w, http.StatusConflict, "this schedule is already running")
			return
		}
		_ = s.store.UpdateSchedule(sc.ID, func(st *model.Schedule) { st.NextRun = next })
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
		return
	}
	errJSON(w, http.StatusNotFound, "schedule not found")
}
