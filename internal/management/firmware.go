package management

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type FirmwareCurrent struct {
	Version   string `json:"version"`
	ElfSHA256 string `json:"elfSha256"`
}
type FirmwareReleaseDTO struct {
	ID           string `json:"id"`
	Version      string `json:"version"`
	SourceCommit string `json:"sourceCommit"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
	ElfSHA256    string `json:"elfSha256"`
}
type FirmwareJob struct {
	ID          string    `json:"id"`
	ReleaseID   string    `json:"releaseId"`
	Version     string    `json:"version"`
	Status      string    `json:"status"`
	Progress    *int      `json:"progress"`
	Error       string    `json:"error"`
	RequestedAt time.Time `json:"requestedAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
}
type firmwareTask struct {
	FirmwareJob
	Actor          string    `json:"actor"`
	Scope          string    `json:"scope"`
	Binding        Binding   `json:"binding"`
	Boot           string    `json:"boot"`
	DownloadUntil  time.Time `json:"downloadUntil"`
	Legacy         bool      `json:"legacy"`
	TicketHash     string    `json:"ticketHash"`
	AuditAttempted bool      `json:"auditAttempted"`
}
type firmwareReport struct {
	Capability int    `json:"capability"`
	Boot       string `json:"boot"`
	Seq        uint64 `json:"seq"`
	Version    string `json:"version"`
	ElfSHA256  string `json:"elfSha256"`
	Layout     string `json:"layout"`
	SlotBytes  int    `json:"slotBytes"`
	Phase      string `json:"phase"`
	JobID      string `json:"jobId"`
	Progress   int    `json:"progress"`
	Error      string `json:"error"`
}
type firmwareDevice struct {
	SchemaVersion int             `json:"schemaVersion"`
	DeviceID      string          `json:"deviceId"`
	Report        *firmwareReport `json:"report"`
	Retired       []string        `json:"retired"`
	Jobs          []firmwareTask  `json:"jobs"`
}
type FirmwareDTO struct {
	DeviceID   string               `json:"deviceId"`
	CanUpgrade bool                 `json:"canUpgrade"`
	Connection string               `json:"connection"`
	Boot       string               `json:"boot"`
	Mode       string               `json:"mode"`
	Current    *FirmwareCurrent     `json:"current"`
	Releases   []FirmwareReleaseDTO `json:"releases"`
	Job        *FirmwareJob         `json:"job"`
}
type firmwareCommand struct {
	ID        string `json:"id"`
	ReleaseID string `json:"releaseId"`
	Version   string `json:"version"`
	URL       string `json:"url"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	ElfSHA256 string `json:"elfSha256"`
	TTLMS     int    `json:"ttlMs"`
}

func firmwareActive(j *firmwareTask) bool {
	return j != nil && (j.Status == "queued" || j.Status == "sent" || j.Status == "downloading" || j.Status == "restarting")
}
func (d *firmwareDevice) task() *firmwareTask {
	if len(d.Jobs) == 0 {
		return nil
	}
	return &d.Jobs[len(d.Jobs)-1]
}
func (f *firmwareStore) load(id string) (firmwareDevice, error) {
	d := firmwareDevice{SchemaVersion: 1, DeviceID: id, Jobs: []firmwareTask{}, Retired: []string{}}
	if !device.MatchString(id) {
		return d, ErrInvalid
	}
	raw, e := f.read("devices/"+id+".json", 256<<10)
	if e == ErrNotFound {
		return d, nil
	}
	if e != nil {
		return d, e
	}
	if firmwareStrict(raw, &d) != nil || d.SchemaVersion != 1 || d.DeviceID != id || len(d.Jobs) > 32 || len(d.Retired) > 16 {
		return d, ErrSource
	}
	for _, j := range d.Jobs {
		if !firmwareJobID.MatchString(j.ID) || !firmwareReleaseID.MatchString(j.ReleaseID) || !runtimeBoot.MatchString(j.Boot) || !firmwareVersion.MatchString(j.Version) || j.RequestedAt.IsZero() || !j.ExpiresAt.After(j.RequestedAt) || j.ExpiresAt.Sub(j.RequestedAt) > 15*time.Minute || !device.MatchString(j.Binding.DeviceID) || j.Binding.DeviceID != id || j.Binding.ID == "" || j.Binding.Version < 1 || (j.Scope != "family" && j.Scope != "admin") || j.Actor == "" || !firmwareStatus(j.Status) || j.Progress != nil && (*j.Progress < 0 || *j.Progress > 100) || j.TicketHash != "" && !digest.MatchString(j.TicketHash) {
			return d, ErrSource
		}
	}
	if d.Report != nil && !firmwareReportValid(*d.Report) {
		return d, ErrSource
	}
	for _, boot := range d.Retired {
		if !runtimeBoot.MatchString(boot) {
			return d, ErrSource
		}
	}
	return d, nil
}
func firmwareStatus(s string) bool {
	switch s {
	case "queued", "sent", "downloading", "restarting", "confirmed", "failed", "unknown":
		return true
	}
	return false
}
func firmwareErrorCode(s string) bool {
	switch s {
	case "", "busy", "invalid_command", "invalid_url", "http_error", "download_failed", "length_mismatch", "hash_mismatch", "image_mismatch", "slot_too_small", "write_failed", "boot_failed", "health_failed", "timeout", "authority_changed", "device_restarted":
		return true
	}
	return false
}
func firmwareReportValid(p firmwareReport) bool {
	if p.Capability != 1 || !runtimeBoot.MatchString(p.Boot) || p.Seq == 0 || p.Seq > 9007199254740991 || !firmwareVersion.MatchString(p.Version) || !digest.MatchString(p.ElfSHA256) || p.Layout != firmwareLayout || p.SlotBytes != firmwareSlotBytes || p.JobID != "" && !firmwareJobID.MatchString(p.JobID) || p.Progress < 0 || p.Progress > 100 || !firmwareErrorCode(p.Error) {
		return false
	}
	switch p.Phase {
	case "checking", "ready", "busy", "downloading", "restarting", "failed":
		return true
	}
	return false
}
func (f *firmwareStore) save(d firmwareDevice) error {
	if _, e := f.root.Lstat("devices/" + d.DeviceID + ".json"); os.IsNotExist(e) {
		dir, e := f.root.Open("devices")
		if e != nil {
			return ErrSource
		}
		names, e := dir.Readdirnames(1025)
		dir.Close()
		if e != nil && e != io.EOF {
			return ErrSource
		}
		if len(names) >= 1024 {
			return ErrSource
		}
	}
	return f.atomic("devices/"+d.DeviceID+".json", d)
}
func (s *Server) firmwareAuthority(c Config, id string, j *firmwareTask) bool {
	b, can, e := runtimeScopePermission(c, j.Actor, j.Scope, id, s.now())
	return e == nil && can && sameMusicBinding(b, j.Binding)
}
func (s *Server) firmwareRefresh(c Config, d *firmwareDevice) bool {
	j := d.task()
	if !firmwareActive(j) {
		return false
	}
	if !s.now().Before(j.ExpiresAt) || !s.firmwareAuthority(c, d.DeviceID, j) {
		j.Status = "unknown"
		j.Progress = nil
		j.Error = "timeout"
		if s.now().Before(j.ExpiresAt) {
			j.Error = "authority_changed"
		}
		return true
	}
	return false
}
func (s *Server) firmwareAudit(d *firmwareDevice) {
	j := d.task()
	if j == nil || firmwareActive(j) || j.AuditAttempted {
		return
	}
	j.AuditAttempted = true
	e := s.operationAudit(j.Actor, j.Scope, "device_firmware_result", d.DeviceID, j.ID, j.Binding, "", nil)
	e.Outcome = j.Status
	e.Reason = j.Error
	_ = s.auditRecord(e)
}

// Called with runtimeMu held. This also makes ordinary control creation and OTA
// task creation/dispatch mutually exclusive without adding another lock order.
func (s *Server) firmwareBlocksControl(id string) (bool, error) {
	if s.opts.FirmwareRoot == "" {
		return false, nil
	}
	f, e := firmwareOpen(s.opts.FirmwareRoot)
	if e != nil {
		return false, e
	}
	defer f.close()
	c, e := s.snapshot()
	if e != nil {
		return false, e
	}
	d, e := f.load(id)
	if e != nil {
		return false, e
	}
	if s.firmwareRefresh(c, &d) {
		s.firmwareAudit(&d)
		if e = f.save(d); e != nil {
			return false, e
		}
	}
	return firmwareActive(d.task()), nil
}
func firmwareConflict(w http.ResponseWriter) {
	apiError(w, 409, "firmware_conflict", "设备或升级任务已变化，请刷新后重试")
}

// A persisted report is evidence for its original boot, never an identity or
// capability claim for a different current ordinary runtime boot.
func firmwareCurrentReport(d firmwareDevice, v *runtimeState) *firmwareReport {
	if v != nil && d.Report != nil && v.boot == d.Report.Boot {
		return d.Report
	}
	return nil
}
func (s *Server) firmwareView(id string, v *runtimeState, d firmwareDevice, can bool, releases []FirmwareManifest) FirmwareDTO {
	out := FirmwareDTO{DeviceID: id, Connection: "unknown", Mode: "restart", Releases: []FirmwareReleaseDTO{}}
	if v != nil {
		runtimeExpire(v, s.now())
		out.Boot = v.boot
		if !v.seen.IsZero() {
			out.Connection = "offline"
			if s.now().Sub(v.seen) < 20*time.Second {
				out.Connection = "online"
			}
		}
	}
	report := firmwareCurrentReport(d, v)
	if report != nil {
		out.Mode = "online"
		out.Current = &FirmwareCurrent{report.Version, report.ElfSHA256}
	}
	for _, m := range releases {
		out.Releases = append(out.Releases, FirmwareReleaseDTO{"fw_" + m.SHA256, m.Version, m.SourceCommit, m.Size, m.SHA256, m.ElfSHA256})
	}
	if j := d.task(); j != nil {
		copy := j.FirmwareJob
		out.Job = &copy
	}
	out.CanUpgrade = can && out.Connection == "online" && v != nil && !runtimePending(v.command) && (report == nil || report.Phase == "ready")
	return out
}

// Revalidate the actor against the latest atomic config after blocking I/O.
func (s *Server) firmwareBrowserAuthority(r *http.Request, a AccountConfig, scope, id string, expected Binding) (Config, Binding, bool, error) {
	c, e := s.snapshot()
	if e != nil {
		return c, Binding{}, false, e
	}
	fresh := r.WithContext(context.WithValue(r.Context(), configContextKey{}, c))
	current, ok := s.account(fresh, scope)
	if !ok || current.ID != a.ID || r.Context().Err() != nil {
		return c, Binding{}, false, ErrForbidden
	}
	b, can, e := runtimeScopePermission(c, a.ID, scope, id, s.now())
	if e != nil {
		return c, b, false, e
	}
	if !sameMusicBinding(expected, b) {
		return c, b, false, ErrForbidden
	}
	return c, b, can, nil
}
func (s *Server) firmwareFreshDevice(r *http.Request, id string) (Config, FactoryDevice, error) {
	c, e := s.snapshot()
	if e != nil {
		return c, FactoryDevice{}, e
	}
	auth := r.Header.Get("Authorization")
	d, ok := authenticateFactory(c, r.Header.Get("Device-Id"), strings.TrimPrefix(auth, "Bearer "))
	if !strings.HasPrefix(auth, "Bearer ") || !ok || d.DeviceID != id || !activated(c, d, s.now()) {
		return c, d, errAuthentication
	}
	return c, d, nil
}
func (s *Server) deviceFirmware(w http.ResponseWriter, r *http.Request, a AccountConfig, parts []string) {
	if len(parts) != 3 || !device.MatchString(parts[2]) || r.URL.RawQuery != "" {
		sourceError(w, ErrInvalid)
		return
	}
	if r.Method != "GET" && r.Method != "POST" {
		apiError(w, 405, "method_not_allowed", "请求方法不支持")
		return
	}
	id, scope := parts[2], parts[0]
	c, e := s.snapshot()
	if e != nil {
		sourceError(w, e)
		return
	}
	binding, can, e := runtimeScopePermission(c, a.ID, scope, id, s.now())
	if e != nil {
		sourceError(w, e)
		return
	}
	if r.Method == "POST" && !can {
		sourceError(w, ErrForbidden)
		return
	}
	var releaseID, boot string
	if r.Method == "POST" {
		fields, ok := runtimeFields(w, r, "releaseId", "boot")
		if !ok {
			return
		}
		if json.Unmarshal(fields["releaseId"], &releaseID) != nil || !firmwareReleaseID.MatchString(releaseID) || json.Unmarshal(fields["boot"], &boot) != nil || !runtimeBoot.MatchString(boot) {
			sourceError(w, ErrInvalid)
			return
		}
	}
	if s.opts.FirmwareRoot == "" {
		sourceError(w, ErrSource)
		return
	}
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	f, e := firmwareOpen(s.opts.FirmwareRoot)
	if e != nil {
		sourceError(w, e)
		return
	}
	defer f.close()
	// Refresh after body decoding AND the runtime/disk lock waits. account only
	// takes the session lock; no session-lock owner acquires runtimeMu.
	c, e = s.snapshot()
	if e != nil {
		sourceError(w, e)
		return
	}
	fresh := r.WithContext(context.WithValue(r.Context(), configContextKey{}, c))
	current, ok := s.account(fresh, scope)
	if !ok || current.ID != a.ID {
		sourceError(w, ErrForbidden)
		return
	}
	newBinding, newCan, e := runtimeScopePermission(c, a.ID, scope, id, s.now())
	if e != nil {
		sourceError(w, e)
		return
	}
	if !sameMusicBinding(binding, newBinding) || r.Method == "POST" && !newCan {
		sourceError(w, ErrForbidden)
		return
	}
	binding, can = newBinding, newCan
	releases, e := f.releases()
	if e != nil {
		sourceError(w, e)
		return
	}
	d, e := f.load(id)
	if e != nil {
		sourceError(w, e)
		return
	}
	c, binding, can, e = s.firmwareBrowserAuthority(r, a, scope, id, binding)
	if e != nil {
		sourceError(w, e)
		return
	}
	if r.Method == "POST" && !can {
		sourceError(w, ErrForbidden)
		return
	}
	if s.firmwareRefresh(c, &d) {
		s.firmwareAudit(&d)
		if e = f.save(d); e != nil {
			sourceError(w, e)
			return
		}
	}
	v := s.runtimes[id]
	out := s.firmwareView(id, v, d, can, releases)
	if r.Method == "POST" {
		if j := d.task(); firmwareActive(j) {
			if j.ReleaseID != releaseID || j.Boot != boot || !sameMusicBinding(j.Binding, binding) {
				firmwareConflict(w)
				return
			}
			writeJSON(w, out)
			return
		}
		if !out.CanUpgrade || out.Boot != boot || len(d.Jobs) >= 32 {
			firmwareConflict(w)
			return
		}
		var target *FirmwareManifest
		for i := range releases {
			if "fw_"+releases[i].SHA256 == releaseID {
				target = &releases[i]
			}
		}
		if target == nil {
			sourceError(w, ErrNotFound)
			return
		}
		if report := firmwareCurrentReport(d, v); report != nil && (report.ElfSHA256 == target.ElfSHA256 || !firmwareVersionNewer(target.Version, report.Version)) {
			firmwareConflict(w)
			return
		}
		suffix, e := randomHex(16)
		if e != nil {
			sourceError(w, e)
			return
		}
		now := s.now().UTC()
		j := firmwareTask{FirmwareJob: FirmwareJob{ID: "ota_" + suffix, ReleaseID: releaseID, Version: target.Version, Status: "queued", RequestedAt: now, ExpiresAt: now.Add(15 * time.Minute)}, Actor: a.ID, Scope: scope, Binding: binding, Boot: boot}
		if e = s.auditRecord(s.operationAudit(a.ID, scope, "device_firmware_requested", id, j.ID, binding, "", nil)); e != nil {
			sourceError(w, e)
			return
		}
		c, binding, can, e = s.firmwareBrowserAuthority(r, a, scope, id, binding)
		if e != nil {
			sourceError(w, e)
			return
		}
		if !can {
			sourceError(w, ErrForbidden)
			return
		}
		out = s.firmwareView(id, v, d, can, releases)
		if !out.CanUpgrade || out.Boot != boot || !s.now().Before(j.ExpiresAt) {
			firmwareConflict(w)
			return
		}
		d.Jobs = append(d.Jobs, j)
		if e = f.save(d); e != nil {
			sourceError(w, e)
			return
		}
		out = s.firmwareView(id, v, d, can, releases)
	}
	writeJSON(w, out)
}
func firmwareVersionNewer(a, b string) bool {
	if !firmwareVersion.MatchString(a) || !firmwareVersion.MatchString(b) {
		return false
	}
	aa, bb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range aa {
		x, _ := strconv.Atoi(aa[i])
		y, _ := strconv.Atoi(bb[i])
		if x != y {
			return x > y
		}
	}
	return false
}
func (s *Server) firmwareDeviceEndpoint(w http.ResponseWriter, r *http.Request, c Config) bool {
	if r.URL.Path != "/api/device/firmware" && !strings.HasPrefix(r.URL.Path, "/api/device/firmware/") {
		return false
	}
	if r.URL.Path != "/api/device/firmware" {
		s.firmwareDownload(w, r, c)
		return true
	}
	if r.Method != "POST" {
		apiError(w, 405, "method_not_allowed", "请求方法不支持")
		return true
	}
	if r.URL.RawQuery != "" {
		sourceError(w, ErrInvalid)
		return true
	}
	key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	dvc, ok := authenticateFactory(c, r.Header.Get("Device-Id"), key)
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || !ok || !activated(c, dvc, s.now()) {
		onboardingError(w, errAuthentication)
		return true
	}
	fields, ok := runtimeFields(w, r, "capability", "boot", "seq", "version", "elfSha256", "layout", "slotBytes", "phase", "jobId", "progress", "error")
	if !ok {
		return true
	}
	for _, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			sourceError(w, ErrInvalid)
			return true
		}
	}
	raw, _ := json.Marshal(fields)
	var report firmwareReport
	if firmwareStrict(raw, &report) != nil || !firmwareReportValid(report) {
		sourceError(w, ErrInvalid)
		return true
	}
	if s.opts.FirmwareRoot == "" {
		sourceError(w, ErrSource)
		return true
	}
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	f, e := firmwareOpen(s.opts.FirmwareRoot)
	if e != nil {
		sourceError(w, e)
		return true
	}
	defer f.close()
	c, e = s.snapshot()
	if e != nil {
		sourceError(w, e)
		return true
	}
	dvc, ok = authenticateFactory(c, r.Header.Get("Device-Id"), key)
	if !ok || !activated(c, dvc, s.now()) {
		onboardingError(w, errAuthentication)
		return true
	}
	v := s.runtimes[dvc.DeviceID]
	if v == nil || v.boot != report.Boot || v.seen.IsZero() || s.now().Sub(v.seen) >= 20*time.Second {
		firmwareConflict(w)
		return true
	}
	d, e := f.load(dvc.DeviceID)
	if e != nil {
		sourceError(w, e)
		return true
	}
	if d.Report != nil && d.Report.Boot == report.Boot && report.Seq <= d.Report.Seq {
		firmwareConflict(w)
		return true
	}
	for _, old := range d.Retired {
		if old == report.Boot {
			firmwareConflict(w)
			return true
		}
	}
	if d.Report != nil && d.Report.Boot != report.Boot {
		d.Retired = append(d.Retired, d.Report.Boot)
		if len(d.Retired) > 16 {
			d.Retired = d.Retired[len(d.Retired)-16:]
		}
	}
	d.Report = &report
	s.firmwareRefresh(c, &d)
	j := d.task()
	var command *firmwareCommand
	if firmwareActive(j) {
		m, _, e := f.release(j.ReleaseID)
		if e != nil {
			sourceError(w, e)
			return true
		}
		c, _, e = s.firmwareFreshDevice(r, dvc.DeviceID)
		if e != nil {
			onboardingError(w, e)
			return true
		}
		s.firmwareRefresh(c, &d)
		if v.boot != report.Boot || s.now().Sub(v.seen) >= 20*time.Second {
			firmwareConflict(w)
			return true
		}
		if firmwareActive(j) {
			validID := report.JobID == "" || report.JobID == j.ID
			if !validID {
				firmwareConflict(w)
				return true
			}
			if report.Boot != j.Boot {
				if j.Status != "queued" && report.ElfSHA256 == m.ElfSHA256 && report.Version == m.Version && report.Phase == "ready" {
					j.Status = "confirmed"
					j.Progress = new(int)
					*j.Progress = 100
					j.Error = ""
				} else if j.Status != "queued" && report.ElfSHA256 == m.ElfSHA256 && report.Version == m.Version && report.Phase == "failed" && report.Error != "" {
					j.Status = "failed"
					j.Progress = nil
					j.Error = report.Error
				} else if report.Phase == "ready" {
					j.Status = "unknown"
					j.Error = "device_restarted"
					j.Progress = nil
				}
			} else if report.JobID == j.ID && j.Status != "queued" {
				switch report.Phase {
				case "downloading":
					if report.ElfSHA256 != m.ElfSHA256 {
						j.Status = "downloading"
						p := report.Progress
						j.Progress = &p
					}
				case "restarting":
					j.Status = "restarting"
					p := report.Progress
					j.Progress = &p
				case "failed":
					j.Status = "failed"
					j.Error = report.Error
					j.Progress = nil
				}
			}
			runtimeExpire(v, s.now())
			if j.Status == "queued" && report.Boot == j.Boot && report.JobID == "" && report.Phase == "ready" && !runtimePending(v.command) && report.ElfSHA256 != m.ElfSHA256 && firmwareVersionNewer(m.Version, report.Version) {
				j.Status = "sent"
				j.DownloadUntil = s.now().Add(15 * time.Second).UTC()
				command = &firmwareCommand{j.ID, j.ReleaseID, m.Version, onboardingOrigin + "/api/device/firmware/" + j.ID, m.Size, m.SHA256, m.ElfSHA256, 15000}
			}
		}
	}
	s.firmwareAudit(&d)
	if e = f.save(d); e != nil {
		sourceError(w, e)
		return true
	}
	writeJSON(w, struct {
		Command *firmwareCommand `json:"command"`
	}{command})
	return true
}
func (s *Server) firmwareDownload(w http.ResponseWriter, r *http.Request, c Config) {
	if r.Method != "GET" {
		apiError(w, 405, "method_not_allowed", "请求方法不支持")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/device/firmware/")
	if !firmwareJobID.MatchString(id) {
		sourceError(w, ErrNotFound)
		return
	}
	if s.opts.FirmwareRoot == "" {
		sourceError(w, ErrSource)
		return
	}
	q, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil || len(q) > 1 || len(q) == 1 && (len(q["ticket"]) != 1 || len(q.Get("ticket")) != 64 || !digest.MatchString(q.Get("ticket"))) {
		sourceError(w, ErrInvalid)
		return
	}
	ticket := q.Get("ticket")
	key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if ticket == "" {
		d, ok := authenticateFactory(c, r.Header.Get("Device-Id"), key)
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || !ok || !activated(c, d, s.now()) {
			onboardingError(w, errAuthentication)
			return
		}
	}
	select {
	case s.firmwareDownloads <- struct{}{}:
		defer func() { <-s.firmwareDownloads }()
	default:
		apiError(w, 429, "firmware_download_busy", "固件下载正在进行，请稍后重试")
		return
	}
	s.runtimeMu.Lock()
	locked := true
	defer func() {
		if locked {
			s.runtimeMu.Unlock()
		}
	}()
	f, e := firmwareOpen(s.opts.FirmwareRoot)
	if e != nil {
		sourceError(w, e)
		return
	}
	defer f.close()
	c, e = s.snapshot()
	if e != nil {
		sourceError(w, e)
		return
	}
	var d firmwareDevice
	if ticket != "" {
		dir, e := f.root.Open("devices")
		if e != nil {
			sourceError(w, ErrSource)
			return
		}
		names, e := dir.Readdirnames(1025)
		dir.Close()
		if e != nil && e != io.EOF || len(names) > 1024 {
			sourceError(w, ErrSource)
			return
		}
		found := false
		for _, name := range names {
			if !strings.HasSuffix(name, ".json") || !device.MatchString(strings.TrimSuffix(name, ".json")) {
				sourceError(w, ErrSource)
				return
			}
			candidate, e := f.load(strings.TrimSuffix(name, ".json"))
			if e != nil {
				sourceError(w, e)
				return
			}
			if j := candidate.task(); j != nil && j.ID == id {
				d = candidate
				found = true
				break
			}
		}
		if !found {
			sourceError(w, ErrForbidden)
			return
		}
	} else {
		dvc, ok := authenticateFactory(c, r.Header.Get("Device-Id"), key)
		if !ok || !activated(c, dvc, s.now()) {
			onboardingError(w, errAuthentication)
			return
		}
		d, e = f.load(dvc.DeviceID)
		if e != nil {
			sourceError(w, e)
			return
		}
	}
	j := d.task()
	if j == nil || j.ID != id {
		sourceError(w, ErrForbidden)
		return
	}
	changed := s.firmwareRefresh(c, &d)
	if changed {
		s.firmwareAudit(&d)
		if f.save(d) != nil {
			sourceError(w, ErrSource)
			return
		}
	}
	if !firmwareActive(j) || j.Status == "queued" || !s.now().Before(j.DownloadUntil) {
		sourceError(w, ErrForbidden)
		return
	}
	// Legacy tickets carry no device credential and authorize only this pinned
	// image, task, deadline and current binding. They are never browser fields.
	if ticket != "" {
		h := sha256.Sum256([]byte(ticket))
		expected, e := hex.DecodeString(j.TicketHash)
		if !j.Legacy || e != nil || subtle.ConstantTimeCompare(h[:], expected) != 1 {
			sourceError(w, ErrForbidden)
			return
		}
	} else {
		v := s.runtimes[d.DeviceID]
		if j.Legacy || v == nil || v.boot != j.Boot || v.seen.IsZero() || s.now().Sub(v.seen) >= 20*time.Second {
			sourceError(w, ErrForbidden)
			return
		}
	}
	m, app, e := f.release(j.ReleaseID)
	if e != nil {
		sourceError(w, e)
		return
	}
	// All blocking disk reads precede the final authority/deadline check.
	c, e = s.snapshot()
	if e != nil || !s.firmwareAuthority(c, d.DeviceID, j) || !s.now().Before(j.ExpiresAt) || !s.now().Before(j.DownloadUntil) {
		sourceError(w, ErrForbidden)
		return
	}
	if ticket == "" {
		dvc, ok := authenticateFactory(c, r.Header.Get("Device-Id"), key)
		if !ok || dvc.DeviceID != d.DeviceID || !activated(c, dvc, s.now()) {
			onboardingError(w, errAuthentication)
			return
		}
	}
	// The immutable bytes have been verified and authorization checked. The
	// admission TTL governs this GET only; network transfer has its own limit.
	if e := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(2 * time.Minute)); e != nil && !errors.Is(e, http.ErrNotSupported) {
		sourceError(w, ErrSource)
		return
	}
	f.close()
	s.runtimeMu.Unlock()
	locked = false
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(m.Size, 10))
	w.WriteHeader(200)
	_, _ = w.Write(app)
}
func firmwareLegacyValid(metadata map[string]any, m FirmwareManifest) bool {
	a, ok := metadata["application"].(map[string]any)
	if !ok || a["name"] != "xiaozhi" || metadata["chip_model_name"] != "esp32s3" {
		return false
	}
	version, ok := a["version"].(string)
	if !ok || !firmwareVersionNewer(m.Version, version) || !firmwareVersionNewer("2.5.3", version) {
		return false
	}
	elf, ok := a["elf_sha256"].(string)
	if !ok {
		return false
	}
	trusted := false
	for _, h := range m.CompatibleFromElfSHA256 {
		if elf == h {
			trusted = true
		}
	}
	if !trusted {
		return false
	}
	board, ok := metadata["board"].(map[string]any)
	if !ok || board["type"] != "wifi" || board["name"] != "family-s3" {
		return false
	}
	ota, ok := metadata["ota"].(map[string]any)
	if !ok || ota["label"] != "ota_0" && ota["label"] != "ota_1" {
		return false
	}
	parts, ok := metadata["partition_table"].([]any)
	if !ok || len(parts) != len(firmwarePartitions) {
		return false
	}
	for i, want := range firmwarePartitions {
		p, ok := parts[i].(map[string]any)
		if !ok || p["label"] != want.Label || p["type"] != float64(want.Type) || p["subtype"] != float64(want.Subtype) || p["address"] != float64(want.Offset) || p["size"] != float64(want.Size) {
			return false
		}
	}
	return true
}

// Returns an optional legacy command. A bootstrap without a queued task keeps
// its original response, even when metadata is old or absent.
func (s *Server) firmwareBootstrap(r *http.Request, dvc FactoryDevice, key string, metadata map[string]any) (any, error) {
	if s.opts.FirmwareRoot == "" {
		return nil, nil
	}
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	f, e := firmwareOpen(s.opts.FirmwareRoot)
	if e != nil {
		return nil, e
	}
	defer f.close()
	c, e := s.snapshot()
	if e != nil {
		return nil, e
	}
	current, ok := authenticateFactory(c, dvc.MAC, key)
	if !ok || !activated(c, current, s.now()) {
		return nil, errAuthentication
	}
	d, e := f.load(current.DeviceID)
	if e != nil {
		return nil, e
	}
	if s.firmwareRefresh(c, &d) {
		s.firmwareAudit(&d)
		if e = f.save(d); e != nil {
			return nil, e
		}
	}
	j := d.task()
	if j == nil || j.Status != "queued" || firmwareCurrentReport(d, s.runtimes[current.DeviceID]) != nil || r.Method != "POST" {
		return nil, nil
	}
	m, _, e := f.release(j.ReleaseID)
	if e != nil {
		return nil, e
	}
	if !firmwareLegacyValid(metadata, m) {
		return nil, errConflict
	}
	v := s.runtimes[current.DeviceID]
	if v != nil {
		runtimeExpire(v, s.now())
		if runtimePending(v.command) {
			return nil, errConflict
		}
	}
	c, _, e = s.firmwareFreshDevice(r, current.DeviceID)
	if e != nil {
		return nil, e
	}
	if s.firmwareRefresh(c, &d) {
		s.firmwareAudit(&d)
		if e = f.save(d); e != nil {
			return nil, e
		}
	}
	if !firmwareActive(j) || j.Status != "queued" {
		return nil, nil
	}
	ticket, e := randomHex(32)
	if e != nil {
		return nil, e
	}
	h := sha256.Sum256([]byte(ticket))
	j.TicketHash = hex.EncodeToString(h[:])
	j.Legacy = true
	j.Status = "sent"
	j.DownloadUntil = s.now().Add(time.Minute).UTC()
	if e = f.save(d); e != nil {
		return nil, e
	}
	return map[string]string{"version": m.Version, "url": fmt.Sprintf("%s/api/device/firmware/%s?ticket=%s", onboardingOrigin, j.ID, ticket)}, nil
}
