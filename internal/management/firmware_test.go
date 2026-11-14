package management

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const firmwareTestBoot = "0123456789abcdef0123456789abcdef"
const firmwareTestOldELF = "bebc4dc82ab5c40d7d10cd9fdf72a6ae911ab8711318d612c73c6f6bd6155ca3"

func TestFirmwareRoutesAvailable(t *testing.T) {
	s, b, c, _ := runtimeFixture(t)
	if w := request(s, "GET", "/api/family/device-firmware/"+b.DeviceID, "", c); w.Code == 404 {
		t.Fatal("new firmware route missing")
	}
	if w := deviceRequest(s, "POST", "/api/device/firmware", `{}`, b); w.Code == 404 {
		t.Fatal("device firmware route missing")
	}
}

func firmwareInputs(t *testing.T) (string, string, string, FirmwareManifest) {
	t.Helper()
	dir := t.TempDir()
	app := make([]byte, 304)
	app[0] = 0xe9
	app[1] = 1
	binary.LittleEndian.PutUint16(app[12:], 9)
	app[23] = 1
	binary.LittleEndian.PutUint32(app[28:], 256)
	binary.LittleEndian.PutUint32(app[32:], 0xabcd5432)
	copy(app[48:80], "2.5.3")
	copy(app[80:112], "xiaozhi")
	elf := sha256.Sum256([]byte("new ELF"))
	copy(app[176:208], elf[:])
	sum := byte(0xef)
	for _, v := range app[32:288] {
		sum ^= v
	}
	app[303] = sum
	imageHash := sha256.Sum256(app)
	app = append(app, imageHash[:]...)
	table := bytes.Repeat([]byte{0xff}, 3072)
	for i, p := range firmwarePartitions {
		q := table[i*32 : (i+1)*32]
		clear(q)
		binary.LittleEndian.PutUint16(q, 0x50aa)
		q[2] = byte(p.Type)
		q[3] = byte(p.Subtype)
		binary.LittleEndian.PutUint32(q[4:], uint32(p.Offset))
		binary.LittleEndian.PutUint32(q[8:], uint32(p.Size))
		copy(q[12:28], p.Label)
	}
	pos := len(firmwarePartitions) * 32
	table[pos] = 0xeb
	table[pos+1] = 0xeb
	h := md5.Sum(table[:pos])
	copy(table[pos+16:pos+32], h[:])
	appHash := sha256.Sum256(app)
	partHash := sha256.Sum256(table)
	m := FirmwareManifest{SchemaVersion: 1, Board: "waveshare-s3", Chip: "esp32s3", Project: "xiaozhi", Layout: firmwareLayout, SlotBytes: firmwareSlotBytes, Version: "2.5.3", SourceCommit: strings.Repeat("a", 40), Size: int64(len(app)), SHA256: hex.EncodeToString(appHash[:]), ElfSHA256: hex.EncodeToString(elf[:]), PartitionSHA256: hex.EncodeToString(partHash[:]), CompatibleFromElfSHA256: []string{firmwareTestOldELF}}
	ap, pp, mp := filepath.Join(dir, "app.bin"), filepath.Join(dir, "partition.bin"), filepath.Join(dir, "manifest.json")
	raw, _ := json.Marshal(m)
	for p, v := range map[string][]byte{ap: app, pp: table, mp: raw} {
		if e := os.WriteFile(p, v, 0600); e != nil {
			t.Fatal(e)
		}
	}
	return mp, ap, pp, m
}
func firmwareFixture(t *testing.T) (*Server, FactoryBundle, *http.Cookie, *time.Time, FirmwareManifest) {
	t.Helper()
	s, b, c, now := runtimeFixture(t)
	s.opts.FirmwareRoot = filepath.Join(t.TempDir(), "firmware")
	mp, ap, pp, m := firmwareInputs(t)
	if _, e := PublishFirmware(s.opts.FirmwareRoot, mp, ap, pp); e != nil {
		t.Fatal(e)
	}
	firmwareOrdinary(t, s, b, firmwareTestBoot, 1)
	return s, b, c, now, m
}
func firmwareOrdinary(t *testing.T, s *Server, b FactoryBundle, boot string, seq int) {
	t.Helper()
	w := deviceRequest(s, "POST", "/api/device/state", fmt.Sprintf(`{"boot":%q,"seq":%d,"volume":25,"brightness":40}`, boot, seq), b)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
}
func firmwareReportRequest(s *Server, b FactoryBundle, boot string, seq int, elf, phase, job string) *httptest.ResponseRecorder {
	return deviceRequest(s, "POST", "/api/device/firmware", fmt.Sprintf(`{"capability":1,"boot":%q,"seq":%d,"version":%q,"elfSha256":%q,"layout":"waveshare-s3-16m-v1","slotBytes":3604480,"phase":%q,"jobId":%q,"progress":0,"error":""}`, boot, seq, func() string {
		if elf == firmwareTestOldELF {
			return "2.5.2"
		}
		return "2.5.3"
	}(), elf, phase, job), b)
}
func firmwareView(t *testing.T, w *httptest.ResponseRecorder) FirmwareDTO {
	t.Helper()
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var v FirmwareDTO
	if e := json.Unmarshal(w.Body.Bytes(), &v); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(w.Body.String(), "ticket") || strings.Contains(w.Body.String(), "https://") {
		t.Fatal("browser leaked download authority")
	}
	return v
}
func firmwareCreate(t *testing.T, s *Server, b FactoryBundle, c *http.Cookie, m FirmwareManifest) FirmwareDTO {
	t.Helper()
	return firmwareView(t, request(s, "POST", "/api/family/device-firmware/"+b.DeviceID, fmt.Sprintf(`{"releaseId":%q,"boot":%q}`, "fw_"+m.SHA256, firmwareTestBoot), c))
}

func TestFirmwarePublishPrivateImmutableAndVerified(t *testing.T) {
	mp, ap, pp, m := firmwareInputs(t)
	root := filepath.Join(t.TempDir(), "store")
	id, e := PublishFirmware(root, mp, ap, pp)
	if e != nil || id != "fw_"+m.SHA256 {
		t.Fatal(id, e)
	}
	for _, p := range []string{root, filepath.Join(root, "releases"), filepath.Join(root, "devices"), filepath.Join(root, "releases", id)} {
		st, e := os.Lstat(p)
		if e != nil || !st.IsDir() || st.Mode().Perm() != 0700 {
			t.Fatal(p, e)
		}
	}
	for _, name := range []string{"manifest.json", "app.bin"} {
		st, e := os.Stat(filepath.Join(root, "releases", id, name))
		if e != nil || st.Mode().Perm() != 0600 {
			t.Fatal(name, e)
		}
	}
	if _, e = PublishFirmware(root, mp, ap, pp); e == nil {
		t.Fatal("overwritten immutable release")
	}
	for _, scenario := range []string{"version", "elf", "sha", "partition", "size", "chip", "project", "source", "unknown", "symlink"} {
		t.Run(scenario, func(t *testing.T) {
			mp, ap, pp, m := firmwareInputs(t)
			switch scenario {
			case "version":
				m.Version = "2.5.4"
			case "elf":
				m.ElfSHA256 = strings.Repeat("0", 64)
			case "sha":
				m.SHA256 = strings.Repeat("0", 64)
			case "partition":
				os.WriteFile(pp, []byte("wrong"), 0600)
			case "size":
				m.Size = firmwareMaxApp + 1
			case "chip":
				m.Chip = "esp32"
			case "project":
				m.Project = "other"
			case "source":
				m.SourceCommit = "short"
			case "symlink":
				os.Rename(ap, ap+".real")
				os.Symlink(ap+".real", ap)
			}
			raw, _ := json.Marshal(m)
			if scenario == "unknown" {
				raw = append(raw[:len(raw)-1], []byte(`,"unexpected":1}`)...)
			}
			os.WriteFile(mp, raw, 0600)
			if _, e := PublishFirmware(filepath.Join(t.TempDir(), "private"), mp, ap, pp); e == nil {
				t.Fatal("accepted", scenario)
			}
		})
	}
	bad := filepath.Join(t.TempDir(), "link")
	os.Symlink(root, bad)
	if _, e = PublishFirmware(bad, mp, ap, pp); e == nil {
		t.Fatal("followed root symlink")
	}
}
func TestFirmwareOnlineLifecycleAndPersistence(t *testing.T) {
	s, b, c, _, m := firmwareFixture(t)
	path := "/api/family/device-firmware/" + b.DeviceID
	if w := firmwareReportRequest(s, b, firmwareTestBoot, 1, firmwareTestOldELF, "ready", ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	v := firmwareCreate(t, s, b, c, m)
	if v.Job == nil || v.Job.Status != "queued" || !v.CanUpgrade {
		t.Fatal(v)
	}
	dup := firmwareCreate(t, s, b, c, m)
	if dup.Job.ID != v.Job.ID {
		t.Fatal("duplicate")
	}
	// Task survives a management restart, while online status deliberately does not.
	next, e := NewServer(s.cfg, s.opts)
	if e != nil {
		t.Fatal(e)
	}
	nc := login(t, next, "family", "alice")
	persist := firmwareView(t, request(next, "GET", path, "", nc))
	if persist.Job.ID != v.Job.ID || persist.Connection != "unknown" {
		t.Fatal(persist)
	}
	w := firmwareReportRequest(s, b, firmwareTestBoot, 2, firmwareTestOldELF, "ready", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var cmd struct {
		Command *firmwareCommand `json:"command"`
	}
	json.Unmarshal(w.Body.Bytes(), &cmd)
	if cmd.Command == nil || cmd.Command.ID != v.Job.ID {
		t.Fatal("missing command", w.Body)
	}
	download := deviceRequest(s, "GET", "/api/device/firmware/"+v.Job.ID, "", b)
	if download.Code != 200 || download.Body.Len() != int(m.Size) {
		t.Fatal(download.Code, download.Body.Len())
	}
	if w := firmwareReportRequest(s, b, firmwareTestBoot, 3, m.ElfSHA256, "ready", v.Job.ID); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if v := firmwareView(t, request(s, "GET", path, "", c)); v.Job.Status == "confirmed" {
		t.Fatal("same boot false confirmation")
	}
	newboot := strings.Repeat("b", 32)
	firmwareOrdinary(t, s, b, newboot, 1)
	if w := firmwareReportRequest(s, b, newboot, 1, m.ElfSHA256, "checking", v.Job.ID); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if v := firmwareView(t, request(s, "GET", path, "", c)); v.Job.Status == "confirmed" {
		t.Fatal("unhealthy boot confirmed")
	}
	if w := firmwareReportRequest(s, b, newboot, 2, m.ElfSHA256, "ready", v.Job.ID); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if v := firmwareView(t, request(s, "GET", path, "", c)); v.Job.Status != "confirmed" {
		t.Fatal(v)
	}
	if w := firmwareReportRequest(s, b, firmwareTestBoot, 4, m.ElfSHA256, "ready", v.Job.ID); w.Code != 409 {
		t.Fatal("retired boot", w.Code)
	}
}
func TestFirmwareAuthorizationConflictsAndExpiry(t *testing.T) {
	for _, scenario := range []string{"family", "readonly", "admin", "revoked", "expired", "boot", "busy", "ordinary", "concurrent", "corrupt"} {
		t.Run(scenario, func(t *testing.T) {
			s, b, c, now, m := firmwareFixture(t)
			path := "/api/family/device-firmware/" + b.DeviceID
			if w := firmwareReportRequest(s, b, firmwareTestBoot, 1, firmwareTestOldELF, "ready", ""); w.Code != 200 {
				t.Fatal(w.Code, w.Body)
			}
			switch scenario {
			case "family":
				other := login(t, s, "family", "bob")
				if w := request(s, "GET", path, "", other); w.Code != 403 {
					t.Fatal(w.Code)
				}
				return
			case "readonly":
				ConfigStore{s.opts.ConfigPath}.Transaction(func(c *Config) error { c.Accounts[0].Memberships[0].CanAddDevices = false; return nil })
				v := firmwareView(t, request(s, "GET", path, "", c))
				if v.CanUpgrade {
					t.Fatal("readonly")
				}
				if w := request(s, "POST", path, fmt.Sprintf(`{"releaseId":%q,"boot":%q}`, "fw_"+m.SHA256, firmwareTestBoot), c); w.Code != 403 {
					t.Fatal(w.Code)
				}
				return
			case "admin":
				ConfigStore{s.opts.ConfigPath}.Transaction(func(c *Config) error {
					c.Accounts[0].InternalAdmin = true
					c.Accounts[0].Memberships[0].Devices = nil
					return nil
				})
				ac := login(t, s, "admin", "alice")
				if w := request(s, "GET", strings.Replace(path, "/family/", "/admin/", 1), "", ac); w.Code != 403 {
					t.Fatal(w.Code)
				}
				return
			case "busy":
				firmwareReportRequest(s, b, firmwareTestBoot, 2, firmwareTestOldELF, "busy", "")
			case "ordinary":
				if w := request(s, "PUT", "/api/family/device-runtime/"+b.DeviceID, `{"field":"volume","value":50,"boot":"`+firmwareTestBoot+`"}`, c); w.Code != 200 {
					t.Fatal(w.Code)
				}
			case "boot":
				firmwareOrdinary(t, s, b, strings.Repeat("b", 32), 2)
			}
			body := fmt.Sprintf(`{"releaseId":%q,"boot":%q}`, "fw_"+m.SHA256, firmwareTestBoot)
			if scenario == "busy" || scenario == "ordinary" || scenario == "boot" {
				if w := request(s, "POST", path, body, c); w.Code != 409 {
					t.Fatal(w.Code, w.Body)
				}
				return
			}
			if scenario == "concurrent" {
				var wg sync.WaitGroup
				ids := make(chan string, 8)
				for i := 0; i < 8; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						w := request(s, "POST", path, body, c)
						if w.Code != 200 {
							ids <- fmt.Sprint(w.Code)
							return
						}
						var v FirmwareDTO
						json.Unmarshal(w.Body.Bytes(), &v)
						ids <- v.Job.ID
					}()
				}
				wg.Wait()
				close(ids)
				id := ""
				for got := range ids {
					if id == "" {
						id = got
					}
					if got != id || !strings.HasPrefix(got, "ota_") {
						t.Fatal("non-idempotent", got)
					}
				}
				return
			}
			v := firmwareCreate(t, s, b, c, m)
			if scenario == "revoked" {
				ConfigStore{s.opts.ConfigPath}.Transaction(func(c *Config) error { c.Accounts[0].Memberships[0].CanAddDevices = false; return nil })
			}
			if scenario == "expired" {
				*now = now.Add(16 * time.Minute)
				firmwareOrdinary(t, s, b, firmwareTestBoot, 2)
			}
			if scenario == "corrupt" {
				os.WriteFile(filepath.Join(s.opts.FirmwareRoot, "releases", "fw_"+m.SHA256, "app.bin"), []byte("corrupt"), 0600)
			}
			w := firmwareReportRequest(s, b, firmwareTestBoot, 2, firmwareTestOldELF, "ready", "")
			if scenario == "corrupt" {
				if w.Code != 503 {
					t.Fatal(w.Code, w.Body)
				}
				return
			}
			if w.Code != 200 || w.Body.String() != "{\"command\":null}\n" {
				t.Fatal(w.Code, w.Body)
			}
			if w := deviceRequest(s, "GET", "/api/device/firmware/"+v.Job.ID, "", b); w.Code == 200 {
				t.Fatal("download after authority/expiry")
			}
		})
	}
}
func firmwareLegacyMetadata(elf string) string {
	parts := make([]map[string]any, 0)
	for _, p := range firmwarePartitions {
		parts = append(parts, map[string]any{"label": p.Label, "type": p.Type, "subtype": p.Subtype, "address": p.Offset, "size": p.Size})
	}
	raw, _ := json.Marshal(map[string]any{"application": map[string]any{"name": "xiaozhi", "version": "2.5.2", "elf_sha256": elf}, "chip_model_name": "esp32s3", "partition_table": parts, "ota": map[string]any{"label": "ota_0"}, "board": map[string]any{"type": "wifi", "name": "family-s3"}})
	return string(raw)
}
func TestFirmwareLegacyOneTimeTicketAndNoTask(t *testing.T) {
	s, b, c, now, m := firmwareFixture(t)
	body := firmwareLegacyMetadata(firmwareTestOldELF)
	if w := deviceRequest(s, "POST", "/api/device/bootstrap", body, b); w.Code != 200 || strings.Contains(w.Body.String(), "firmware") {
		t.Fatal("normal bootstrap changed", w.Code, w.Body)
	}
	v := firmwareCreate(t, s, b, c, m)
	if v.Mode != "restart" {
		t.Fatal(v.Mode)
	}
	if w := deviceRequest(s, "POST", "/api/device/bootstrap", firmwareLegacyMetadata(strings.Repeat("0", 64)), b); w.Code != 409 {
		t.Fatal("wrong legacy baseline", w.Code, w.Body)
	}
	w := deviceRequest(s, "POST", "/api/device/bootstrap", body, b)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var out struct {
		Firmware struct{ Version, URL string } `json:"firmware"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if out.Firmware.Version != "2.5.3" || out.Firmware.URL == "" {
		t.Fatal("missing legacy command")
	}
	u := strings.TrimPrefix(out.Firmware.URL, onboardingOrigin)
	r := httptest.NewRequest("GET", "http://localhost:18791"+u, nil)
	r.RemoteAddr = "127.0.0.1:31000"
	download := httptest.NewRecorder()
	s.ServeHTTP(download, r)
	if download.Code != 200 || download.Body.Len() != int(m.Size) {
		t.Fatal(download.Code, download.Body.Len())
	}
	if w := deviceRequest(s, "POST", "/api/device/bootstrap", body, b); w.Code != 200 || strings.Contains(w.Body.String(), "firmware") {
		t.Fatal("repeat dispatch")
	}
	r = httptest.NewRequest("GET", "http://localhost:18791"+strings.Replace(u, v.Job.ID, "ota_"+strings.Repeat("f", 32), 1), nil)
	download = httptest.NewRecorder()
	s.ServeHTTP(download, r)
	if download.Code == 200 {
		t.Fatal("ticket for another job")
	}
	*now = now.Add(2 * time.Minute)
	download = httptest.NewRecorder()
	s.ServeHTTP(download, httptest.NewRequest("GET", "http://localhost:18791"+u, nil))
	if download.Code == 200 {
		t.Fatal("expired ticket")
	}
}

func TestFirmwareStrictProtocolAndDeviceAuthentication(t *testing.T) {
	s, b, _, _, _ := firmwareFixture(t)
	valid := fmt.Sprintf(`{"capability":1,"boot":%q,"seq":1,"version":"2.5.2","elfSha256":%q,"layout":"waveshare-s3-16m-v1","slotBytes":3604480,"phase":"ready","jobId":"","progress":0,"error":""}`, firmwareTestBoot, firmwareTestOldELF)
	for _, body := range []string{`{}`, strings.Replace(valid, `"seq":1`, `"seq":9007199254740992`, 1), strings.Replace(valid, `"seq":1`, `"seq":1.5`, 1), strings.Replace(valid, `"seq":1`, `"seq":null`, 1), strings.Replace(valid, `"seq":1`, `"seq":1,"seq":2`, 1), strings.Replace(valid, `"error":""`, `"error":"https://secret"`, 1), strings.Replace(valid, `"phase":"ready"`, `"phase":"idle"`, 1), strings.Replace(valid, `"slotBytes":3604480`, `"slotBytes":3604481`, 1), strings.Replace(valid, `"capability":1`, `"capability":1,"extra":0`, 1)} {
		if w := deviceRequest(s, "POST", "/api/device/firmware", body, b); w.Code != 400 {
			t.Fatal(w.Code, w.Body)
		}
	}
	wrong := b
	wrong.DeviceKey = strings.Repeat("a", 43)
	if w := deviceRequest(s, "POST", "/api/device/firmware", valid, wrong); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := deviceRequest(s, "POST", "/api/device/firmware", valid, b); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if w := deviceRequest(s, "POST", "/api/device/firmware", valid, b); w.Code != 409 {
		t.Fatal("duplicate sequence", w.Code)
	}
}
func TestFirmwareControlExclusionAndFailure(t *testing.T) {
	s, b, c, _, m := firmwareFixture(t)
	firmwareReportRequest(s, b, firmwareTestBoot, 1, firmwareTestOldELF, "ready", "")
	v := firmwareCreate(t, s, b, c, m)
	if w := request(s, "PUT", "/api/family/device-runtime/"+b.DeviceID, `{"field":"volume","value":50,"boot":"`+firmwareTestBoot+`"}`, c); w.Code != 409 {
		t.Fatal("control admitted during OTA", w.Code)
	}
	firmwareReportRequest(s, b, firmwareTestBoot, 2, firmwareTestOldELF, "ready", "")
	if w := firmwareReportRequest(s, b, firmwareTestBoot, 3, firmwareTestOldELF, "failed", v.Job.ID); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	out := firmwareView(t, request(s, "GET", "/api/family/device-firmware/"+b.DeviceID, "", c))
	if out.Job.Status != "failed" {
		t.Fatal(out.Job)
	}
	if w := firmwareReportRequest(s, b, firmwareTestBoot, 4, firmwareTestOldELF, "ready", ""); w.Code != 200 || w.Body.String() != "{\"command\":null}\n" {
		t.Fatal("implicit retry", w.Code, w.Body)
	}
	if w := request(s, "PUT", "/api/family/device-runtime/"+b.DeviceID, `{"field":"volume","value":50,"boot":"`+firmwareTestBoot+`"}`, c); w.Code != 200 {
		t.Fatal("terminal task still blocks", w.Code, w.Body)
	}
}
func TestFirmwareLegacyIncompatibleMetadataAndNewCapabilityNoBootstrap(t *testing.T) {
	for _, scenario := range []string{"board", "layout", "ota", "version", "newcap"} {
		t.Run(scenario, func(t *testing.T) {
			s, b, c, _, m := firmwareFixture(t)
			if scenario == "newcap" {
				firmwareReportRequest(s, b, firmwareTestBoot, 1, firmwareTestOldELF, "ready", "")
			}
			firmwareCreate(t, s, b, c, m)
			body := firmwareLegacyMetadata(firmwareTestOldELF)
			switch scenario {
			case "board":
				body = strings.Replace(body, "family-s3", "other", 1)
			case "layout":
				body = strings.Replace(body, `"address":131072`, `"address":65536`, 1)
			case "ota":
				body = strings.Replace(body, `"label":"ota_0"}`, `"label":"factory"}`, 1)
			case "version":
				body = strings.Replace(body, "2.5.2", "2.5.3", 1)
			}
			w := deviceRequest(s, "POST", "/api/device/bootstrap", body, b)
			if scenario == "newcap" {
				if w.Code != 200 || strings.Contains(w.Body.String(), "firmware") {
					t.Fatal(w.Code, w.Body)
				}
			} else if w.Code != 409 {
				t.Fatal(w.Code, w.Body)
			}
		})
	}
}
func TestFirmwareReleaseAndDeviceSymlinksFailClosed(t *testing.T) {
	for _, scenario := range []string{"release-dir", "app", "manifest", "device"} {
		t.Run(scenario, func(t *testing.T) {
			s, b, c, _, m := firmwareFixture(t)
			base := filepath.Join(s.opts.FirmwareRoot, "releases", "fw_"+m.SHA256)
			p := base
			switch scenario {
			case "app":
				p = filepath.Join(base, "app.bin")
			case "manifest":
				p = filepath.Join(base, "manifest.json")
			case "device":
				firmwareCreate(t, s, b, c, m)
				p = filepath.Join(s.opts.FirmwareRoot, "devices", b.DeviceID+".json")
			}
			if e := os.Rename(p, p+".real"); e != nil {
				t.Fatal(e)
			}
			if e := os.Symlink(p+".real", p); e != nil {
				t.Fatal(e)
			}
			if w := request(s, "GET", "/api/family/device-firmware/"+b.DeviceID, "", c); w.Code != 503 {
				t.Fatal(w.Code, w.Body)
			}
		})
	}
}

type firmwareSlowWriter struct {
	*httptest.ResponseRecorder
	started, release chan struct{}
	deadline         time.Time
}

func (w *firmwareSlowWriter) Write(b []byte) (int, error) {
	close(w.started)
	<-w.release
	return w.ResponseRecorder.Write(b)
}
func (w *firmwareSlowWriter) SetWriteDeadline(t time.Time) error { w.deadline = t; return nil }
func TestFirmwareSlowDownloadDoesNotBlockHeartbeat(t *testing.T) {
	s, b, c, _, m := firmwareFixture(t)
	firmwareReportRequest(s, b, firmwareTestBoot, 1, firmwareTestOldELF, "ready", "")
	v := firmwareCreate(t, s, b, c, m)
	firmwareReportRequest(s, b, firmwareTestBoot, 2, firmwareTestOldELF, "ready", "")
	writer := &firmwareSlowWriter{ResponseRecorder: httptest.NewRecorder(), started: make(chan struct{}), release: make(chan struct{})}
	done := make(chan struct{})
	defer func() { close(writer.release); <-done }()
	r := httptest.NewRequest("GET", "http://localhost:18791/api/device/firmware/"+v.Job.ID, nil)
	r.Header.Set("Authorization", "Bearer "+b.DeviceKey)
	r.Header.Set("Device-Id", b.MAC)
	go func() { s.ServeHTTP(writer, r); close(done) }()
	select {
	case <-writer.started:
	case <-time.After(2 * time.Second):
		t.Fatal("download not started")
	}
	heartbeat := make(chan int, 1)
	go func() {
		w := deviceRequest(s, "POST", "/api/device/state", `{"boot":"`+firmwareTestBoot+`","seq":2,"volume":25,"brightness":40}`, b)
		heartbeat <- w.Code
	}()
	select {
	case code := <-heartbeat:
		if code != 200 {
			t.Fatal(code)
		}
	case <-time.After(time.Second):
		t.Fatal("slow download holds runtime lock")
	}
	if writer.deadline.IsZero() || time.Until(writer.deadline) < 30*time.Second || time.Until(writer.deadline) > 2*time.Minute {
		t.Fatal("missing bounded download deadline")
	}
}
func TestFirmwareMoreThanSixteenNormalReboots(t *testing.T) {
	s, b, _, _, _ := firmwareFixture(t)
	for i := 1; i <= 20; i++ {
		boot := fmt.Sprintf("%032x", i)
		firmwareOrdinary(t, s, b, boot, 1)
		if w := firmwareReportRequest(s, b, boot, 1, firmwareTestOldELF, "ready", ""); w.Code != 200 {
			t.Fatalf("normal reboot %d rejected: %d", i, w.Code)
		}
	}
}

func TestFirmwareRefreshesSessionAndAuthorityAfterDiskLockWait(t *testing.T) {
	for _, scenario := range []string{"revoke", "logout", "binding", "admin"} {
		t.Run(scenario, func(t *testing.T) {
			s, b, c, _, m := firmwareFixture(t)
			scope := "family"
			if scenario == "admin" {
				ConfigStore{s.opts.ConfigPath}.Transaction(func(c *Config) error { c.Accounts[0].InternalAdmin = true; return nil })
				scope = "admin"
				c = login(t, s, scope, "alice")
			}
			held, e := firmwareOpen(s.opts.FirmwareRoot)
			if e != nil {
				t.Fatal(e)
			}
			done := make(chan *httptest.ResponseRecorder, 1)
			body := fmt.Sprintf(`{"releaseId":%q,"boot":%q}`, "fw_"+m.SHA256, firmwareTestBoot)
			go func() { done <- request(s, "POST", "/api/"+scope+"/device-firmware/"+b.DeviceID, body, c) }()
			deadline := time.Now().Add(time.Second)
			waiting := false
			for time.Now().Before(deadline) {
				if !s.runtimeMu.TryLock() {
					waiting = true
					break
				}
				s.runtimeMu.Unlock()
				time.Sleep(time.Millisecond)
			}
			if !waiting {
				held.close()
				t.Fatal("handler did not reach disk wait")
			}
			switch scenario {
			case "logout":
				if w := request(s, "POST", "/api/"+scope+"/logout", "{}", c); w.Code != 200 {
					held.close()
					t.Fatal(w.Code)
				}
			case "revoke":
				ConfigStore{s.opts.ConfigPath}.Transaction(func(c *Config) error { c.Accounts[0].Memberships[0].CanAddDevices = false; return nil })
			case "binding":
				ConfigStore{s.opts.ConfigPath}.Transaction(func(c *Config) error {
					for i := range c.Bindings {
						if c.Bindings[i].DeviceID == b.DeviceID {
							c.Bindings[i].Version++
						}
					}
					return nil
				})
			case "admin":
				ConfigStore{s.opts.ConfigPath}.Transaction(func(c *Config) error { c.Accounts[0].InternalAdmin = false; return nil })
			}
			held.close()
			w := <-done
			if w.Code != 403 {
				t.Fatal("stale authority", w.Code, w.Body)
			}
		})
	}
}
func TestFirmwareDownloadRefreshesRevocationAfterDiskLockWait(t *testing.T) {
	s, b, c, _, m := firmwareFixture(t)
	firmwareReportRequest(s, b, firmwareTestBoot, 1, firmwareTestOldELF, "ready", "")
	v := firmwareCreate(t, s, b, c, m)
	firmwareReportRequest(s, b, firmwareTestBoot, 2, firmwareTestOldELF, "ready", "")
	held, e := firmwareOpen(s.opts.FirmwareRoot)
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- deviceRequest(s, "GET", "/api/device/firmware/"+v.Job.ID, "", b) }()
	deadline := time.Now().Add(time.Second)
	waiting := false
	for time.Now().Before(deadline) {
		if !s.runtimeMu.TryLock() {
			waiting = true
			break
		}
		s.runtimeMu.Unlock()
		time.Sleep(time.Millisecond)
	}
	if !waiting {
		held.close()
		t.Fatal("download did not wait")
	}
	ConfigStore{s.opts.ConfigPath}.Transaction(func(c *Config) error { c.Accounts[0].Memberships[0].CanAddDevices = false; return nil })
	held.close()
	if w := <-done; w.Code != 403 {
		t.Fatal(w.Code, w.Body)
	}
}
func TestFirmwareAuditAndDiskFailureRemainFailClosed(t *testing.T) {
	s, b, c, _, m := firmwareFixture(t)
	s.auditWrite = func(string, auditEntry) error { return ErrSource }
	body := fmt.Sprintf(`{"releaseId":%q,"boot":%q}`, "fw_"+m.SHA256, firmwareTestBoot)
	path := "/api/family/device-firmware/" + b.DeviceID
	if w := request(s, "POST", path, body, c); w.Code != 503 {
		t.Fatal(w.Code, w.Body)
	}
	v := firmwareView(t, request(s, "GET", path, "", c))
	if v.Job != nil {
		t.Fatal("created after request audit failure")
	}
	s.auditWrite = appendAudit
	v = firmwareCreate(t, s, b, c, m)
	if v.Job == nil {
		t.Fatal("missing job")
	}
	// Corrupt storage cannot let a readonly actor through the authority gate.
	os.WriteFile(filepath.Join(s.opts.FirmwareRoot, "devices", b.DeviceID+".json"), []byte("corrupt"), 0600)
	ConfigStore{s.opts.ConfigPath}.Transaction(func(c *Config) error { c.Accounts[0].Memberships[0].CanAddDevices = false; return nil })
	if w := request(s, "POST", path, body, c); w.Code != 403 {
		t.Fatal("storage error bypassed authority", w.Code)
	}
}
func TestFirmwareSameReleaseBootIsIdempotentAcrossAuthorizedScopes(t *testing.T) {
	s, b, c, _, m := firmwareFixture(t)
	v := firmwareCreate(t, s, b, c, m)
	ConfigStore{s.opts.ConfigPath}.Transaction(func(c *Config) error { c.Accounts[0].InternalAdmin = true; return nil })
	admin := login(t, s, "admin", "alice")
	out := firmwareView(t, request(s, "POST", "/api/admin/device-firmware/"+b.DeviceID, fmt.Sprintf(`{"releaseId":%q,"boot":%q}`, v.Job.ReleaseID, firmwareTestBoot), admin))
	if out.Job.ID != v.Job.ID {
		t.Fatal("duplicate upgrade")
	}
}
func TestFirmwareCandidateBootHealthFailureIsPreserved(t *testing.T) {
	s, b, c, _, m := firmwareFixture(t)
	firmwareReportRequest(s, b, firmwareTestBoot, 1, firmwareTestOldELF, "ready", "")
	v := firmwareCreate(t, s, b, c, m)
	firmwareReportRequest(s, b, firmwareTestBoot, 2, firmwareTestOldELF, "ready", "")
	boot := strings.Repeat("c", 32)
	firmwareOrdinary(t, s, b, boot, 1)
	body := fmt.Sprintf(`{"capability":1,"boot":%q,"seq":1,"version":"2.5.3","elfSha256":%q,"layout":"waveshare-s3-16m-v1","slotBytes":3604480,"phase":"failed","jobId":%q,"progress":0,"error":"health_failed"}`, boot, m.ElfSHA256, v.Job.ID)
	if w := deviceRequest(s, "POST", "/api/device/firmware", body, b); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	out := firmwareView(t, request(s, "GET", "/api/family/device-firmware/"+b.DeviceID, "", c))
	if out.Job.Status != "failed" || out.Job.Error != "health_failed" {
		t.Fatal("candidate boot failure lost", out.Job)
	}
}
func TestFirmwareRollbackToLegacyAllowsOnlyExplicitMetadataGatedRetry(t *testing.T) {
	s, b, c, now, m := firmwareFixture(t)
	firmwareReportRequest(s, b, firmwareTestBoot, 1, firmwareTestOldELF, "ready", "")
	first := firmwareCreate(t, s, b, c, m)
	firmwareReportRequest(s, b, firmwareTestBoot, 2, firmwareTestOldELF, "ready", "")
	candidateBoot := strings.Repeat("c", 32)
	firmwareOrdinary(t, s, b, candidateBoot, 1)
	firmwareReportRequest(s, b, candidateBoot, 1, m.ElfSHA256, "checking", first.Job.ID)
	// A rollback speaks only the original ordinary heartbeat/bootstrap protocol.
	legacyBoot := strings.Repeat("d", 32)
	firmwareOrdinary(t, s, b, legacyBoot, 1)
	*now = now.Add(16 * time.Minute)
	firmwareOrdinary(t, s, b, legacyBoot, 2)
	path := "/api/family/device-firmware/" + b.DeviceID
	view := firmwareView(t, request(s, "GET", path, "", c))
	if view.Mode != "restart" || view.Current != nil || !view.CanUpgrade || view.Job.Status != "unknown" {
		t.Fatal("stale candidate blocks legacy", view)
	}
	if w := deviceRequest(s, "POST", "/api/device/bootstrap", firmwareLegacyMetadata(firmwareTestOldELF), b); w.Code != 200 || strings.Contains(w.Body.String(), "firmware") {
		t.Fatal("implicit retry", w.Code)
	}
	second := firmwareView(t, request(s, "POST", path, fmt.Sprintf(`{"releaseId":%q,"boot":%q}`, first.Job.ReleaseID, legacyBoot), c))
	if second.Job.ID == first.Job.ID {
		t.Fatal("reused terminal job")
	}
	if w := deviceRequest(s, "POST", "/api/device/bootstrap", firmwareLegacyMetadata(strings.Repeat("0", 64)), b); w.Code != 409 {
		t.Fatal("retry bypassed baseline", w.Code)
	}
	if w := deviceRequest(s, "POST", "/api/device/bootstrap", firmwareLegacyMetadata(firmwareTestOldELF), b); w.Code != 200 || !strings.Contains(w.Body.String(), `"firmware"`) {
		t.Fatal("explicit legacy retry missing", w.Code)
	}
	store, e := firmwareOpen(s.opts.FirmwareRoot)
	if e != nil {
		t.Fatal(e)
	}
	defer store.close()
	d, e := store.load(b.DeviceID)
	if e != nil || len(d.Jobs) != 2 || d.Jobs[0].ID != first.Job.ID || d.Jobs[0].Status != "unknown" {
		t.Fatal("old job evidence lost", e)
	}
}
func TestFirmwarePublicationCountBoundDoesNotOverwriteReleases(t *testing.T) {
	mp, ap, pp, m := firmwareInputs(t)
	root := filepath.Join(t.TempDir(), "private")
	original, e := os.ReadFile(ap)
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i <= firmwareMaxReleases; i++ {
		app := append([]byte(nil), original...)
		app[128] = byte('0' + i)
		sum := byte(0xef)
		for _, v := range app[32:288] {
			sum ^= v
		}
		app[303] = sum
		h := sha256.Sum256(app[:304])
		copy(app[304:], h[:])
		m.SHA256 = firmwareHash(app)
		raw, _ := json.Marshal(m)
		os.WriteFile(ap, app, 0600)
		os.WriteFile(mp, raw, 0600)
		_, e := PublishFirmware(root, mp, ap, pp)
		if i < firmwareMaxReleases && e != nil {
			t.Fatal(i, e)
		}
		if i == firmwareMaxReleases && e != errLimit {
			t.Fatal("unbounded release count", e)
		}
	}
	f, e := firmwareOpen(root)
	if e != nil {
		t.Fatal(e)
	}
	defer f.close()
	releases, e := f.releases()
	if e != nil || len(releases) != firmwareMaxReleases {
		t.Fatal("existing releases changed", e)
	}
}
func TestFirmwareDownloadSaturationAndReleaseOnErrors(t *testing.T) {
	s, b, c, _, m := firmwareFixture(t)
	firmwareReportRequest(s, b, firmwareTestBoot, 1, firmwareTestOldELF, "ready", "")
	v := firmwareCreate(t, s, b, c, m)
	firmwareReportRequest(s, b, firmwareTestBoot, 2, firmwareTestOldELF, "ready", "")
	path := "/api/device/firmware/" + v.Job.ID
	for i := 0; i < 3; i++ {
		if w := deviceRequest(s, "GET", "/api/device/firmware/ota_"+strings.Repeat("f", 32), "", b); w.Code != 403 {
			t.Fatal(w.Code)
		}
		if w := deviceRequest(s, "GET", path+"?ticket="+strings.Repeat("0", 64), "", b); w.Code != 403 {
			t.Fatal(w.Code)
		}
	}
	writers := []*firmwareSlowWriter{}
	dones := []chan struct{}{}
	defer func() {
		for i, w := range writers {
			close(w.release)
			<-dones[i]
		}
	}()
	for i := 0; i < 2; i++ {
		w := &firmwareSlowWriter{ResponseRecorder: httptest.NewRecorder(), started: make(chan struct{}), release: make(chan struct{})}
		done := make(chan struct{})
		writers = append(writers, w)
		dones = append(dones, done)
		r := httptest.NewRequest("GET", "http://localhost:18791"+path, nil)
		r.Header.Set("Authorization", "Bearer "+b.DeviceKey)
		r.Header.Set("Device-Id", b.MAC)
		go func() { s.ServeHTTP(w, r); close(done) }()
		select {
		case <-w.started:
		case <-time.After(2 * time.Second):
			t.Fatal("prior errors leaked download capacity")
		}
	}
	for _, suffix := range []string{"", "?ticket=" + strings.Repeat("0", 64)} {
		if w := deviceRequest(s, "GET", path+suffix, "", b); w.Code != 429 {
			t.Fatal("unbounded concurrent downloads", w.Code)
		}
	}
	close(writers[0].release)
	<-dones[0]
	writers = writers[1:]
	dones = dones[1:]
	if w := deviceRequest(s, "GET", path, "", b); w.Code != 200 || w.Body.Len() != int(m.Size) {
		t.Fatal("finished transfer leaked capacity", w.Code)
	}
}

func firmwarePauseAuthority(t *testing.T, s *Server, now *time.Time, n int) (<-chan struct{}, func()) {
	t.Helper()
	reached, release := make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	calls := 0
	s.now = func() time.Time {
		mu.Lock()
		calls++
		pause := calls == n
		mu.Unlock()
		if pause {
			close(reached)
			<-release
		}
		return *now
	}
	return reached, func() { close(release) }
}
func TestFirmwareRefreshesBrowserAuthorityAfterReleaseReadWindow(t *testing.T) {
	for _, scenario := range []string{"revoke", "logout", "binding"} {
		t.Run(scenario, func(t *testing.T) {
			s, b, c, now, m := firmwareFixture(t)
			reached, release := firmwarePauseAuthority(t, s, now, 4)
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				done <- request(s, "POST", "/api/family/device-firmware/"+b.DeviceID, fmt.Sprintf(`{"releaseId":%q,"boot":%q}`, "fw_"+m.SHA256, firmwareTestBoot), c)
			}()
			select {
			case <-reached:
			case <-time.After(2 * time.Second):
				t.Fatal("did not reach pre-read authority")
			}
			switch scenario {
			case "logout":
				if w := request(s, "POST", "/api/family/logout", "{}", c); w.Code != 200 {
					release()
					t.Fatal(w.Code)
				}
			case "revoke":
				ConfigStore{s.opts.ConfigPath}.Transaction(func(c *Config) error { c.Accounts[0].Memberships[0].CanAddDevices = false; return nil })
			case "binding":
				ConfigStore{s.opts.ConfigPath}.Transaction(func(c *Config) error {
					for i := range c.Bindings {
						if c.Bindings[i].DeviceID == b.DeviceID {
							c.Bindings[i].Version++
						}
					}
					return nil
				})
			}
			release()
			if w := <-done; w.Code != 403 {
				t.Fatal("stale post-read browser authority", w.Code)
			}
		})
	}
}
func TestFirmwareRefreshesTaskAuthorityAfterReleaseReadWindow(t *testing.T) {
	for _, scenario := range []string{"dispatch", "confirm", "legacy"} {
		t.Run(scenario, func(t *testing.T) {
			s, b, c, now, m := firmwareFixture(t)
			if scenario != "legacy" {
				firmwareReportRequest(s, b, firmwareTestBoot, 1, firmwareTestOldELF, "ready", "")
			}
			v := firmwareCreate(t, s, b, c, m)
			boot, seq, elf, job := firmwareTestBoot, 2, firmwareTestOldELF, ""
			pauseAt := 5
			if scenario == "confirm" {
				firmwareReportRequest(s, b, firmwareTestBoot, 2, firmwareTestOldELF, "ready", "")
				boot = strings.Repeat("c", 32)
				seq = 1
				elf = m.ElfSHA256
				job = v.Job.ID
				firmwareOrdinary(t, s, b, boot, 1)
			}
			if scenario == "legacy" {
				pauseAt = 4
			}
			reached, release := firmwarePauseAuthority(t, s, now, pauseAt)
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				if scenario == "legacy" {
					done <- deviceRequest(s, "POST", "/api/device/bootstrap", firmwareLegacyMetadata(firmwareTestOldELF), b)
				} else {
					done <- firmwareReportRequest(s, b, boot, seq, elf, "ready", job)
				}
			}()
			select {
			case <-reached:
			case <-time.After(2 * time.Second):
				t.Fatal("did not reach pre-read task authority")
			}
			ConfigStore{s.opts.ConfigPath}.Transaction(func(c *Config) error { c.Accounts[0].Memberships[0].CanAddDevices = false; return nil })
			release()
			w := <-done
			if w.Code != 200 {
				t.Fatal(w.Code)
			}
			if scenario == "legacy" && strings.Contains(w.Body.String(), `"firmware"`) || scenario != "legacy" && strings.Contains(w.Body.String(), `"url"`) {
				t.Fatal("issued after authority change")
			}
			view := firmwareView(t, request(s, "GET", "/api/family/device-firmware/"+b.DeviceID, "", c))
			if view.Job.Status != "unknown" {
				t.Fatal("terminal transition used stale authority", view.Job.Status)
			}
		})
	}
}
func TestFirmwareOrdinaryControlRefreshesAfterStorageWait(t *testing.T) {
	s, b, c, _, _ := firmwareFixture(t)
	held, e := firmwareOpen(s.opts.FirmwareRoot)
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- request(s, "PUT", "/api/family/device-runtime/"+b.DeviceID, `{"field":"volume","value":50,"boot":"`+firmwareTestBoot+`"}`, c)
	}()
	deadline := time.Now().Add(time.Second)
	waiting := false
	for time.Now().Before(deadline) {
		if !s.runtimeMu.TryLock() {
			waiting = true
			break
		}
		s.runtimeMu.Unlock()
		time.Sleep(time.Millisecond)
	}
	if !waiting {
		held.close()
		t.Fatal("control did not reach storage wait")
	}
	ConfigStore{s.opts.ConfigPath}.Transaction(func(c *Config) error { c.Accounts[0].Memberships[0].CanAddDevices = false; return nil })
	held.close()
	if w := <-done; w.Code != 403 {
		t.Fatal("stale control authority after storage wait", w.Code)
	}
}
func TestFirmwareCreateRequiresOnlineAfterAuditWrite(t *testing.T) {
	s, b, c, now, m := firmwareFixture(t)
	s.auditWrite = func(p string, e auditEntry) error {
		if e.Action == "device_firmware_requested" {
			*now = now.Add(21 * time.Second)
		}
		return appendAudit(p, e)
	}
	if w := request(s, "POST", "/api/family/device-firmware/"+b.DeviceID, fmt.Sprintf(`{"releaseId":%q,"boot":%q}`, "fw_"+m.SHA256, firmwareTestBoot), c); w.Code != 409 {
		t.Fatal("created after online lease expired during audit", w.Code)
	}
}
