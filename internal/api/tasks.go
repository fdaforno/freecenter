package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"freecenter/internal/model"
)

// Recent Tasks.
//
// Every modifying API request is recorded as a task, so the UI can show what
// was done, by whom and with what outcome - for all users and API clients, and
// across page reloads. The log lives in memory and is cleared by a restart.
const maxTasks = 200

type taskLog struct {
	mu    sync.Mutex
	seq   int64
	tasks []*model.Task // newest first
}

func (l *taskLog) add(t *model.Task) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	t.ID = l.seq
	l.tasks = append([]*model.Task{t}, l.tasks...)
	if len(l.tasks) > maxTasks {
		l.tasks = l.tasks[:maxTasks]
	}
}

func (l *taskLog) update(t *model.Task, fn func(t *model.Task)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fn(t)
}

func (l *taskLog) list() []model.Task {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]model.Task, len(l.tasks))
	for i, t := range l.tasks {
		out[i] = *t
	}
	return out
}

// taskRef travels in the request context so that the handler chain can
// complete the task record: auth fills in the user, and handlers that continue
// in the background take the task over (see detachTask).
type taskRef struct {
	task     *model.Task
	detached bool
}

const taskRefKey ctxKey = "task"

// routeTaskNames gives a readable name to requests that do not carry one
// (API clients such as the Terraform provider). Keyed by "METHOD suffix".
var routeTaskNames = []struct{ method, suffix, name string }{
	{"POST", "/snapshots/remove-all", "Remove all snapshots"},
	{"POST", "/revert", "Revert to snapshot"},
	{"POST", "/snapshots", "Create virtual machine snapshot"},
	{"POST", "/power", "Change power state"},
	{"POST", "/migrate", "Migrate virtual machine"},
	{"POST", "/clone", "Clone virtual machine"},
	{"POST", "/vms", "Create virtual machine"},
	{"POST", "/files/upload", "Upload file to datastore"},
	{"POST", "/iso/download", "Download ISO to datastore"},
	{"POST", "/cloud-image/download", "Download cloud image to datastore"},
	{"POST", "/hosts", "Add standalone host"},
}

func defaultTaskName(r *http.Request) string {
	for _, n := range routeTaskNames {
		if r.Method == n.method && strings.HasSuffix(r.URL.Path, n.suffix) {
			return n.name
		}
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/"), "/"), "/")
	kind := "object"
	switch {
	case len(parts) >= 5 && parts[4] == "snapshots":
		kind = "snapshot"
	case len(parts) >= 3 && parts[2] == "vms":
		kind = "virtual machine"
	case len(parts) >= 3 && parts[2] == "datastores":
		kind = "datastore file"
	case len(parts) >= 1 && parts[0] == "hosts":
		kind = "host"
	}
	switch r.Method {
	case http.MethodDelete:
		return "Delete " + kind
	case http.MethodPut:
		return "Reconfigure " + kind
	}
	return r.Method + " " + r.URL.Path
}

// untracked requests are modifying by method only: they are queries, or session handling.
func untrackedRequest(r *http.Request) bool {
	p := r.URL.Path
	return r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions ||
		strings.HasPrefix(p, "/api/v1/auth/") || strings.HasPrefix(p, "/api/v1/users") || strings.HasSuffix(p, "/esxcli") || strings.HasSuffix(p, "/migrate/check")
}

// recordTasks logs each modifying request as a task. The UI names its tasks
// with the X-Task-Name / X-Task-Target headers (URL-encoded).
func (s *Server) recordTasks(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if untrackedRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		name, _ := url.QueryUnescape(r.Header.Get("X-Task-Name"))
		target, _ := url.QueryUnescape(r.Header.Get("X-Task-Target"))
		if name == "" {
			name = defaultTaskName(r)
		}
		t := &model.Task{Name: name, Target: target, Status: "running", StartedAt: time.Now()}
		if rest, ok := strings.CutPrefix(r.URL.Path, "/api/v1/hosts/"); ok {
			t.HostID, _, _ = strings.Cut(rest, "/")
		}
		s.tasks.add(t)
		ref := &taskRef{task: t}
		rec := &errorRecorder{ResponseWriter: w, code: 200}
		next.ServeHTTP(rec, r.WithContext(context.WithValue(r.Context(), taskRefKey, ref)))

		if ref.detached && rec.code < 400 {
			return // a background job finishes this task
		}
		now := time.Now()
		s.tasks.update(t, func(t *model.Task) {
			t.FinishedAt = &now
			if rec.code >= 400 {
				t.Status, t.Details = "error", rec.message()
			} else {
				t.Status = "ok"
			}
		})
	})
}

// taskOf returns the task of the request being served, if it is tracked.
func taskOf(r *http.Request) *taskRef {
	ref, _ := r.Context().Value(taskRefKey).(*taskRef)
	return ref
}

// detachTask hands the request's task over to a background operation, which
// must call the returned function when it ends. It returns nil-safe closures
// when the request is not tracked.
func (s *Server) detachTask(r *http.Request) (progress func(details string), finish func(err error, details string)) {
	ref := taskOf(r)
	if ref == nil {
		return func(string) {}, func(error, string) {}
	}
	ref.detached = true
	t := ref.task
	progress = func(details string) {
		s.tasks.update(t, func(t *model.Task) { t.Details = details })
	}
	finish = func(err error, details string) {
		now := time.Now()
		s.tasks.update(t, func(t *model.Task) {
			t.FinishedAt = &now
			if err != nil {
				t.Status, t.Details = "error", err.Error()
				return
			}
			t.Status, t.Details = "ok", details
		})
	}
	return progress, finish
}

// errorRecorder keeps the status code and, for failures, the (small JSON) body
// so the error message can be shown in the task.
type errorRecorder struct {
	http.ResponseWriter
	code int
	body bytes.Buffer
}

func (w *errorRecorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *errorRecorder) WriteHeader(code int) {
	w.code = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *errorRecorder) Write(b []byte) (int, error) {
	if w.code >= 400 && w.body.Len() < 4096 {
		w.body.Write(b)
	}
	return w.ResponseWriter.Write(b)
}

func (w *errorRecorder) message() string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(w.body.Bytes(), &e) == nil && e.Error != "" {
		return e.Error
	}
	return http.StatusText(w.code)
}

func (s *Server) handleListTasks(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, s.tasks.list())
}
