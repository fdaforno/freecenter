package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"freecenter/internal/esxi"
	"freecenter/internal/model"
)

// jobTimeout bounds a background job; large disks over a slow link take hours.
const jobTimeout = 24 * time.Hour

// job is a long operation running in the background, polled by the UI.
type job struct {
	mu     sync.Mutex
	status model.JobStatus
}

func (j *job) snapshot() model.JobStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.status
}

func (j *job) update(fn func(st *model.JobStatus)) {
	j.mu.Lock()
	defer j.mu.Unlock()
	fn(&j.status)
}

// jobResult is what a finished job produced.
type jobResult struct {
	NewVMID  string
	Warnings []string
}

// startJob runs fn in the background and returns its initial status. The job
// outlives the HTTP request: closing the browser must not interrupt it. When
// it ends, the inventory cache of the hosts involved is dropped.
func (s *Server) startJob(r *http.Request, init model.JobStatus, fn func(ctx context.Context, j *job) (jobResult, error)) model.JobStatus {
	taskProgress, taskFinish := s.detachTask(r)
	idBytes := make([]byte, 8)
	_, _ = rand.Read(idBytes)
	init.ID = hex.EncodeToString(idBytes)
	init.Status, init.Phase, init.StartedAt = "running", "Starting", time.Now()
	if init.Warnings == nil {
		init.Warnings = []string{}
	}
	j := &job{status: init}
	s.jobs.Store(init.ID, j)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), jobTimeout)
		defer cancel()
		// Mirror the job phase into its task while it runs.
		stopMirror := make(chan struct{})
		go func() {
			for {
				select {
				case <-stopMirror:
					return
				case <-time.After(2 * time.Second):
					taskProgress(progressText(j.snapshot()))
				}
			}
		}()
		res, err := fn(ctx, j)
		close(stopMirror)
		taskFinish(err, strings.Join(res.Warnings, ". "))
		for _, hid := range []string{init.SourceHostID, init.TargetHostID} {
			if hid != "" {
				s.cache.invalidateHost(hid)
			}
		}
		now := time.Now()
		j.update(func(st *model.JobStatus) {
			st.FinishedAt = &now
			st.Warnings = append(st.Warnings, res.Warnings...)
			st.NewVMID = res.NewVMID
			if err != nil {
				st.Status, st.Error = "error", err.Error()
				return
			}
			st.Status, st.Phase = "ok", "Completed"
		})
		if err != nil {
			s.logger.Error("job failed", "kind", init.Kind, "vm", init.VMName, "err", err)
		} else {
			s.logger.Info("job completed", "kind", init.Kind, "vm", init.VMName, "new_id", res.NewVMID, "dur", now.Sub(init.StartedAt).Round(time.Second))
		}
	}()
	return j.snapshot()
}

// progressText renders a running job for the Details column of its task.
func progressText(st model.JobStatus) string {
	if st.BytesTotal > 0 && strings.HasPrefix(st.Phase, "Copying") {
		pct := min(100, st.BytesDone*100/st.BytesTotal)
		return fmt.Sprintf("%s (%d%%)", st.Phase, pct)
	}
	return st.Phase
}

func (s *Server) handleJobStatus(w http.ResponseWriter, r *http.Request) {
	v, ok := s.jobs.Load(r.PathValue("id"))
	if !ok {
		apiError(w, errors.Join(esxi.ErrNotFound, errors.New("job "+r.PathValue("id"))))
		return
	}
	writeJSON(w, 200, v.(*job).snapshot())
}
