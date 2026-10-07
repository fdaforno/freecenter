package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"freecenter/internal/esxi"
	"freecenter/internal/model"
	"freecenter/internal/store"
)

var hostNameRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

type createHostRequest struct {
	Name       string `json:"name"`
	Address    string `json:"address"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	SSHPort    int    `json:"ssh_port"`
	Insecure   *bool  `json:"insecure"`
	SSHEnabled *bool  `json:"ssh_enabled"` // omitted on update = unchanged
}

func (s *Server) handleListHosts(w http.ResponseWriter, r *http.Request) {
	hosts := s.store.Hosts()
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	type hostWithStatus struct {
		model.HostRecord
		Status *model.HostStatus `json:"status"`
	}
	// Hosts are queried concurrently: one slow or unreachable host must not delay the others.
	out := make([]hostWithStatus, len(hosts))
	var wg sync.WaitGroup
	for i, h := range hosts {
		wg.Go(func() {
			row := hostWithStatus{HostRecord: public(h)}
			c, err := s.mgr.Client(h.ID)
			if err != nil {
				row.Status = &model.HostStatus{ID: h.ID, Name: h.Name, Address: h.Address, Connected: false, LastError: err.Error()}
			} else {
				var sshc *esxi.SSHClient
				if h.SSHEnabled {
					sshc = esxi.NewSSHClient(hostOnly(h.Address), h.SSHPort, h.Username, h.Password, s.sshKnownHosts)
				}
				cachedSt, err := cached(s.cache, ctx, h.ID+"/status", func(ctx context.Context) (*model.HostStatus, error) {
					return c.Status(ctx, sshc)
				})
				if err != nil {
					row.Status = &model.HostStatus{ID: h.ID, Name: h.Name, Address: h.Address, Connected: false, LastError: err.Error()}
				} else {
					st := *cachedSt // copy: the cached value is shared between requests
					st.ID, st.Name, st.Address = h.ID, h.Name, h.Address
					st.Connected = true
					if h.SSHEnabled && sshc != nil {
						st.SSHOk = true
					}
					row.Status = &st
				}
			}
			out[i] = row
		})
	}
	wg.Wait()
	writeJSON(w, 200, out)
}

// public returns the host record as clients may see it: the ESXi password
// never leaves the server.
func public(h model.HostRecord) model.HostRecord {
	h.Password = ""
	return h
}

func hostOnly(addr string) string {
	if i := strings.LastIndex(addr, ":"); i > 0 && !strings.Contains(addr[i:], "]") {
		return addr[:i]
	}
	return addr
}

func (s *Server) handleCreateHost(w http.ResponseWriter, r *http.Request) {
	var req createHostRequest
	if !decodeBody(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Address = strings.TrimSpace(req.Address)
	if req.Name == "" || !hostNameRE.MatchString(req.Name) {
		errJSON(w, http.StatusBadRequest, "name is required (alphanumeric, dots, dashes, underscores)")
		return
	}
	if req.Address == "" {
		errJSON(w, http.StatusBadRequest, "address is required (host or host:port)")
		return
	}
	if req.Username == "" || req.Password == "" {
		errJSON(w, http.StatusBadRequest, "username and password are required")
		return
	}
	if req.SSHPort == 0 {
		req.SSHPort = 22
	}
	rec := model.HostRecord{
		Name:       req.Name,
		Address:    req.Address,
		Username:   req.Username,
		Password:   req.Password,
		SSHPort:    req.SSHPort,
		Insecure:   req.Insecure,
		SSHEnabled: req.SSHEnabled != nil && *req.SSHEnabled,
	}
	if rec.Insecure == nil {
		t := true
		rec.Insecure = &t
	}
	created, err := s.store.AddHost(rec)
	if err != nil {
		apiError(w, err)
		return
	}
	// verify connectivity before returning success
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	c, err := s.mgr.Client(created.ID)
	if err != nil {
		_ = s.store.DeleteHost(created.ID)
		errJSON(w, http.StatusBadRequest, "cannot connect to ESXi host: "+err.Error())
		return
	}
	if _, err := c.Status(ctx, nil); err != nil {
		_ = s.store.DeleteHost(created.ID)
		errJSON(w, http.StatusBadRequest, "cannot connect to ESXi host: "+err.Error())
		return
	}
	writeJSON(w, 201, public(*created))
}

func (s *Server) handleGetHost(w http.ResponseWriter, r *http.Request) {
	h, err := s.store.Host(r.PathValue("hid"))
	if err != nil {
		apiError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	c, err := s.mgr.Client(h.ID)
	if err != nil {
		apiError(w, err)
		return
	}
	var sshc *esxi.SSHClient
	if h.SSHEnabled {
		sshc = esxi.NewSSHClient(hostOnly(h.Address), h.SSHPort, h.Username, h.Password, s.sshKnownHosts)
	}
	st, err := c.Status(ctx, sshc)
	if err != nil {
		apiError(w, err)
		return
	}
	st.ID, st.Name, st.Address = h.ID, h.Name, h.Address
	st.Connected = true
	writeJSON(w, 200, st)
}

func (s *Server) handleUpdateHost(w http.ResponseWriter, r *http.Request) {
	h, err := s.store.Host(r.PathValue("hid"))
	if err != nil {
		apiError(w, err)
		return
	}
	var req createHostRequest
	if !decodeBody(w, r, &req) {
		return
	}
	_, err = s.store.UpdateHost(h.ID, func(rec *model.HostRecord) {
		if req.Name != "" {
			rec.Name = req.Name
		}
		if req.Address != "" {
			rec.Address = req.Address
		}
		if req.Username != "" {
			rec.Username = req.Username
		}
		if req.Password != "" {
			rec.Password = req.Password
		}
		if req.SSHPort != 0 {
			rec.SSHPort = req.SSHPort
		}
		if req.Insecure != nil {
			rec.Insecure = req.Insecure
		}
		if req.SSHEnabled != nil {
			rec.SSHEnabled = *req.SSHEnabled
		}
	})
	if err != nil {
		apiError(w, err)
		return
	}
	s.mgr.Drop(h.ID) // force reconnect with new credentials
	updated, _ := s.store.Host(h.ID)
	writeJSON(w, 200, public(*updated))
}

func (s *Server) handleDeleteHost(w http.ResponseWriter, r *http.Request) {
	hid := r.PathValue("hid")
	if _, err := s.store.Host(hid); err != nil {
		apiError(w, err)
		return
	}
	s.mgr.Drop(hid)
	if err := s.store.DeleteHost(hid); err != nil {
		apiError(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "deleted"})
}

func (s *Server) handleTestHost(w http.ResponseWriter, r *http.Request) {
	h, err := s.store.Host(r.PathValue("hid"))
	if err != nil {
		apiError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	result := map[string]any{"api": "ok", "ssh": "disabled"}
	c, err := s.mgr.Client(h.ID)
	if err != nil {
		result["api"] = "fail: " + err.Error()
		writeJSON(w, 200, result)
		return
	}
	if _, err := c.Status(ctx, nil); err != nil {
		result["api"] = "fail: " + err.Error()
	}
	if h.SSHEnabled {
		sshc := esxi.NewSSHClient(hostOnly(h.Address), h.SSHPort, h.Username, h.Password, s.sshKnownHosts)
		out, err := sshc.Run(ctx, "echo ok")
		if err != nil {
			result["ssh"] = "fail: " + err.Error()
		} else {
			result["ssh"] = "ok"
			result["ssh_output"] = strings.TrimSpace(out)
		}
	}
	writeJSON(w, 200, result)
}

func (s *Server) handleListDatastores(w http.ResponseWriter, r *http.Request) {
	c, err := s.client(r)
	if err != nil {
		apiError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	ds, err := cached(s.cache, ctx, r.PathValue("hid")+"/datastores", c.Datastores)
	if err != nil {
		apiError(w, err)
		return
	}
	writeJSON(w, 200, ds)
}

func (s *Server) handleListNetworks(w http.ResponseWriter, r *http.Request) {
	c, err := s.client(r)
	if err != nil {
		apiError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	ns, err := cached(s.cache, ctx, r.PathValue("hid")+"/networks", c.Networks)
	if err != nil {
		apiError(w, err)
		return
	}
	writeJSON(w, 200, ns)
}

func (s *Server) handleListGuestOS(w http.ResponseWriter, r *http.Request) {
	c, err := s.client(r)
	if err != nil {
		apiError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	gs, err := c.GuestOSList(ctx)
	if err != nil {
		apiError(w, err)
		return
	}
	writeJSON(w, 200, gs)
}

func (s *Server) handleDatastoreFiles(w http.ResponseWriter, r *http.Request) {
	c, err := s.client(r)
	if err != nil {
		apiError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	p := r.URL.Query().Get("path")
	if p == "" {
		p = "/"
	}
	files, err := c.DatastoreFiles(ctx, r.PathValue("ds"), p)
	if err != nil {
		apiError(w, err)
		return
	}
	writeJSON(w, 200, files)
}

type downloadISORequest struct {
	URL    string `json:"url"`
	Folder string `json:"folder"`
	Name   string `json:"name"`
}

func isoStatusKey(hid, ds, folder, name string) string {
	return hid + "/" + ds + "/" + folder + "/" + name
}

func (s *Server) handleDownloadISO(w http.ResponseWriter, r *http.Request) {
	h, err := s.store.Host(r.PathValue("hid"))
	if err != nil {
		apiError(w, err)
		return
	}
	if !h.SSHEnabled {
		errJSON(w, http.StatusBadRequest, "ssh is required to download ISOs")
		return
	}
	var req downloadISORequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.URL == "" || !strings.HasPrefix(req.URL, "http") {
		errJSON(w, http.StatusBadRequest, "valid http(s) url is required")
		return
	}
	if strings.Contains(req.Folder, "..") || strings.Contains(req.Name, "..") {
		errJSON(w, http.StatusBadRequest, "invalid folder or name")
		return
	}
	if req.Folder == "" {
		req.Folder = "ISO"
	}
	if req.Name == "" {
		idx := strings.LastIndex(req.URL, "/")
		if idx >= 0 && idx < len(req.URL)-1 {
			req.Name = req.URL[idx+1:]
		}
		if req.Name == "" {
			errJSON(w, http.StatusBadRequest, "name is required (could not derive from URL)")
			return
		}
	}
	ds := r.PathValue("ds")
	hid := h.ID
	key := isoStatusKey(hid, ds, req.Folder, req.Name)
	status := &isoStatus{
		URL: req.URL, Name: req.Name, Folder: req.Folder,
		Datastore: ds, Status: "running", StartedAt: time.Now(),
	}
	s.isoStatuses.Store(key, status)

	// The request returns at once: the download owns the task from here.
	_, finishTask := s.detachTask(r)
	go func() {
		defer func() {
			if status.Status == "ok" {
				finishTask(nil, "")
			} else {
				finishTask(errors.New(status.Error), "")
			}
		}()
		bgCtx := context.Background()
		// try SSH wget first (only for HTTP, not HTTPS — BusyBox can't do TLS)
		if strings.HasPrefix(req.URL, "http://") {
			sshc := esxi.NewSSHClient(hostOnly(h.Address), h.SSHPort, h.Username, h.Password, s.sshKnownHosts)
			sshCtx, sshCancel := context.WithTimeout(bgCtx, 30*time.Second)
			defer sshCancel()
			if err := sshc.DownloadISO(sshCtx, ds, req.Folder, req.Name, req.URL); err == nil {
				status.Status = "ok"
				status.DoneAt = time.Now()
				s.logger.Info("iso download complete (ssh)", "name", req.Name)
				return
			}
		}
		// fallback: download on FreeCenter server + upload via SSH cat pipe
		c, cerr := s.mgr.Client(hid)
		if cerr != nil {
			status.Status = "error"
			status.Error = fmt.Errorf("govmomi client: %v", cerr).Error()
			status.DoneAt = time.Now()
			s.logger.Error("iso download", "name", req.Name, "err", status.Error)
			return
		}
		httpCtx, httpCancel := context.WithTimeout(bgCtx, 30*time.Minute)
		defer httpCancel()
		if ferr := c.DownloadISOHTTP(httpCtx, ds, req.Folder, req.Name, req.URL); ferr != nil {
			status.Status = "error"
			status.Error = ferr.Error()
			status.DoneAt = time.Now()
			s.logger.Error("iso download", "name", req.Name, "err", ferr.Error())
			return
		}
		status.Status = "ok"
		status.DoneAt = time.Now()
		s.logger.Info("iso download complete (http fallback)", "name", req.Name, "datastore", ds, "folder", req.Folder)
	}()

	writeJSON(w, 202, map[string]any{
		"status":    "accepted",
		"key":       key,
		"name":      req.Name,
		"datastore": ds,
		"folder":    req.Folder,
	})
}

func (s *Server) handleDownloadISOStatus(w http.ResponseWriter, r *http.Request) {
	folder := r.URL.Query().Get("folder")
	if folder == "" {
		folder = "ISO"
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		errJSON(w, http.StatusBadRequest, "name query parameter is required")
		return
	}
	key := isoStatusKey(r.PathValue("hid"), r.PathValue("ds"), folder, name)
	v, ok := s.isoStatuses.Load(key)
	if !ok {
		errJSON(w, http.StatusNotFound, "no download found for this name")
		return
	}
	writeJSON(w, 200, v.(*isoStatus))
}

func (s *Server) handleUploadFile(w http.ResponseWriter, r *http.Request) {
	c, err := s.client(r)
	if err != nil {
		apiError(w, err)
		return
	}
	p := r.URL.Query().Get("path")
	if p == "" || p == "/" || strings.Contains(p, "..") {
		errJSON(w, http.StatusBadRequest, "path query parameter required (target file name, no ..)")
		return
	}
	tmp, err := os.CreateTemp("", "fc-upload-*")
	if err != nil {
		apiError(w, err)
		return
	}
	defer os.Remove(tmp.Name())
	size, err := io.Copy(tmp, r.Body)
	tmp.Close()
	if err != nil {
		errJSON(w, http.StatusBadRequest, "reading body: "+err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()
	err = c.UploadDatastoreFile(ctx, r.PathValue("ds"), p, tmp.Name())
	if err != nil {
		apiError(w, err)
		return
	}
	writeJSON(w, 201, map[string]any{"path": p, "size": size})
}

// handleDownloadFile streams a file of a datastore to the client.
// Administrators only: disks and configuration files hold the guests' data.
func (s *Server) handleDownloadFile(w http.ResponseWriter, r *http.Request) {
	c, err := s.client(r)
	if err != nil {
		apiError(w, err)
		return
	}
	p := r.URL.Query().Get("path")
	if p == "" || p == "/" || strings.Contains(p, "..") {
		errJSON(w, http.StatusBadRequest, "path query parameter required")
		return
	}
	body, size, err := c.DownloadDatastoreFile(r.Context(), r.PathValue("ds"), p)
	if err != nil {
		apiError(w, err)
		return
	}
	defer body.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(p)}))
	if size >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	}
	_, _ = io.Copy(w, body)
}

func (s *Server) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	c, err := s.client(r)
	if err != nil {
		apiError(w, err)
		return
	}
	p := r.URL.Query().Get("path")
	if p == "" || strings.Contains(p, "..") {
		errJSON(w, http.StatusBadRequest, "path query parameter required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	if err := c.DeleteDatastoreFile(ctx, r.PathValue("ds"), p); err != nil {
		apiError(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "deleted"})
}

type esxcliRequest struct {
	Command string `json:"command"`
}

func (s *Server) handleEsxcli(w http.ResponseWriter, r *http.Request) {
	h, err := s.store.Host(r.PathValue("hid"))
	if err != nil {
		apiError(w, err)
		return
	}
	var req esxcliRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if !esxi.CommandAllowed(req.Command) {
		errJSON(w, http.StatusForbidden, "command not allowed; permitted prefixes: esxcli/vim-cmd read-only commands")
		return
	}
	if !h.SSHEnabled {
		errJSON(w, http.StatusConflict, "ssh is not enabled for this host")
		return
	}
	sshc := esxi.NewSSHClient(hostOnly(h.Address), h.SSHPort, h.Username, h.Password, s.sshKnownHosts)
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	out, err := sshc.Run(ctx, req.Command)
	if err != nil {
		apiError(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"output": out})
}

type hostPowerRequest struct {
	Action string `json:"action"` // reboot | shutdown
	Force  bool   `json:"force"`
	Reason string `json:"reason"`
}

func (s *Server) handleHostPower(w http.ResponseWriter, r *http.Request) {
	h, err := s.store.Host(r.PathValue("hid"))
	if err != nil {
		apiError(w, err)
		return
	}
	var req hostPowerRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Action != "reboot" && req.Action != "shutdown" {
		errJSON(w, http.StatusBadRequest, "action must be reboot or shutdown")
		return
	}
	if req.Reason == "" {
		req.Reason = "freecenter API request"
	}
	if !h.SSHEnabled {
		errJSON(w, http.StatusBadRequest, "host power operations require ssh_enabled host (uses esxcli system shutdown)")
		return
	}
	sshc := esxi.NewSSHClient(hostOnly(h.Address), h.SSHPort, h.Username, h.Password, s.sshKnownHosts)
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	cmd := fmt.Sprintf("esxcli system shutdown %s -d 10 -r %q", req.Action, req.Reason)
	out, err := sshc.Run(ctx, cmd)
	if err != nil {
		apiJSON(w, 202, map[string]string{"status": "command sent, host is going down", "output": out})
		return
	}
	writeJSON(w, 202, map[string]string{"status": "command sent, host is going down", "output": out})
}

func apiJSON(w http.ResponseWriter, code int, v any) { writeJSON(w, code, v) }

func (s *Server) client(r *http.Request) (*esxi.Client, error) {
	hid := r.PathValue("hid")
	if _, err := s.store.Host(hid); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, fmt.Errorf("%w: host %s", store.ErrNotFound, hid)
		}
		return nil, err
	}
	return s.mgr.Client(hid)
}

func (s *Server) hostAndClient(r *http.Request) (*model.HostRecord, *esxi.Client, error) {
	hid := r.PathValue("hid")
	h, err := s.store.Host(hid)
	if err != nil {
		return nil, nil, err
	}
	c, err := s.mgr.Client(hid)
	if err != nil {
		return h, nil, err
	}
	return h, c, nil
}

type downloadCloudImageRequest struct {
	URL  string `json:"url"`
	Name string `json:"name"` // vmdk name on datastore (e.g. "cloud-images/rocky-9.vmdk")
}

func (s *Server) handleDownloadCloudImage(w http.ResponseWriter, r *http.Request) {
	h, err := s.store.Host(r.PathValue("hid"))
	if err != nil {
		apiError(w, err)
		return
	}
	if !h.SSHEnabled {
		errJSON(w, http.StatusBadRequest, "ssh is required to download cloud images")
		return
	}
	var req downloadCloudImageRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.URL == "" || !strings.HasPrefix(req.URL, "http") {
		errJSON(w, http.StatusBadRequest, "valid http(s) url is required")
		return
	}
	if req.Name == "" {
		idx := strings.LastIndex(req.URL, "/")
		if idx >= 0 && idx < len(req.URL)-1 {
			raw := req.URL[idx+1:]
			raw = strings.TrimSuffix(raw, ".qcow2")
			raw = strings.TrimSuffix(raw, ".img")
			req.Name = "cloud-images/" + raw + ".vmdk"
		}
		if req.Name == "" {
			errJSON(w, http.StatusBadRequest, "name is required (could not derive from URL)")
			return
		}
	}
	if strings.Contains(req.Name, "..") {
		errJSON(w, http.StatusBadRequest, "invalid name")
		return
	}
	ds := r.PathValue("ds")
	hid := h.ID
	// Same key scheme as ISO downloads, so the status endpoint serves both
	// (folder=cloud-images&name=<name>).
	key := isoStatusKey(hid, ds, "cloud-images", req.Name)
	status := &isoStatus{
		URL: req.URL, Name: req.Name, Folder: "cloud-images",
		Datastore: ds, Status: "running", StartedAt: time.Now(),
	}
	s.isoStatuses.Store(key, status)

	// The request returns at once: the download owns the task from here.
	_, finishTask := s.detachTask(r)
	go func() {
		defer func() {
			if status.Status == "ok" {
				finishTask(nil, "")
			} else {
				finishTask(errors.New(status.Error), "")
			}
		}()
		c, cerr := s.mgr.Client(hid)
		if cerr != nil {
			status.Status = "error"
			status.Error = fmt.Errorf("govmomi client: %v", cerr).Error()
			status.DoneAt = time.Now()
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		if ferr := c.DownloadCloudImage(ctx, ds, req.Name, req.URL); ferr != nil {
			status.Status = "error"
			status.Error = ferr.Error()
			status.DoneAt = time.Now()
			s.logger.Error("cloud image download", "name", req.Name, "err", ferr.Error())
			return
		}
		status.Status = "ok"
		status.DoneAt = time.Now()
		s.logger.Info("cloud image download complete", "name", req.Name, "datastore", ds)
	}()

	writeJSON(w, 202, map[string]any{
		"status":    "accepted",
		"key":       key,
		"name":      req.Name,
		"datastore": ds,
	})
}

func (s *Server) handleListCloudImages(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, esxi.CloudImageCatalog)
}
