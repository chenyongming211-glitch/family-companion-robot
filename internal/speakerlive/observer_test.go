//go:build linux

package speakerlive

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Test subprocesses use the exact production argv; no inherited test marker or
// credential is required. Only synthetic bytes and process IDs are used.
func init() {
	if len(os.Args) > 1 && os.Args[1] == "session" {
		os.Exit(fakeSession(os.Args[2:]))
	}
}
func fakeSession(args []string) int {
	if len(args) != 6 || args[0] != "--model" || args[2] != "--sha256" || args[4] != "--declared-single-speaker" || args[5] != "--realtime-test" {
		return 30
	}
	model := args[1]
	mode := filepath.Base(model)
	_ = os.WriteFile(model+".pid", []byte(strconv.Itoa(os.Getpid())), 0600)
	if mode == "startup-stall" {
		for {
			time.Sleep(time.Hour)
		}
	}
	if mode == "startup-eof" {
		return 0
	}
	if mode == "startup-bad" {
		fmt.Println(`{"event":"ready","evidence":"private-secret"}`)
		return 0
	}
	if mode == "env" {
		for _, s := range os.Environ() {
			if !strings.HasPrefix(s, "PATH=") && !strings.HasPrefix(s, "LANG=") {
				return 31
			}
		}
	}
	fmt.Println(`{"event":"ready","evidence":"turn_taking_test"}`)
	if mode == "descendant" {
		child := exec.Command("/bin/sleep", "60")
		if child.Start() != nil {
			return 32
		}
		_ = os.WriteFile(model+".child", []byte(strconv.Itoa(child.Process.Pid)), 0600)
	}
	if mode == "blocked" || mode == "copy" {
		_ = os.WriteFile(model+".waiting", nil, 0600)
		for {
			if _, err := os.Stat(model + ".release"); err == nil {
				break
			}
			time.Sleep(time.Millisecond)
		}
	}
	for count := 0; ; count++ {
		var size [4]byte
		if _, err := io.ReadFull(os.Stdin, size[:]); err != nil {
			return 0
		}
		n := binary.BigEndian.Uint32(size[:])
		if n > 4096 {
			return 33
		}
		data := make([]byte, n)
		if _, err := io.ReadFull(os.Stdin, data); err != nil {
			return 34
		}
		var h struct {
			Version    int    `json:"version"`
			ID         int64  `json:"id"`
			Session    string `json:"session"`
			Scope      string `json:"scope"`
			Generation int    `json:"generation"`
			Start      int64  `json:"start"`
			End        int64  `json:"end"`
			SHA        string `json:"model_sha256"`
			Rate       int    `json:"sample_rate"`
			Bytes      int    `json:"pcm_bytes"`
		}
		d := json.NewDecoder(bytes.NewReader(data))
		d.DisallowUnknownFields()
		if d.Decode(&h) != nil {
			return 35
		}
		if h.Version != 1 || h.Session != "test-session" || h.Scope != "realtime-test" || h.Generation != 1 || h.SHA != args[3] || h.Rate != 16000 || h.Bytes != int(h.End-h.Start)*2 {
			return 36
		}
		pcm := make([]byte, h.Bytes)
		if _, err := io.ReadFull(os.Stdin, pcm); err != nil {
			return 37
		}
		_ = os.WriteFile(model+".read", nil, 0600)
		switch mode {
		case "stall", "descendant":
			for {
				time.Sleep(time.Hour)
			}
		case "eof":
			return 0
		case "closed":
			fmt.Println(`{"event":"closed","evidence":"turn_taking_test"}`)
			return 0
		case "oversize":
			fmt.Println(strings.Repeat("x", 4097))
			return 0
		case "malformed":
			fmt.Println("not JSON private-secret")
			return 0
		}
		id, duration, evidence, label := h.ID, (h.End-h.Start)*1000/16000, "turn_taking_test", "guest_2"
		if mode == "copy" {
			for _, b := range pcm {
				if b != 3 {
					label = "unknown"
					break
				}
			}
		}
		switch mode {
		case "id":
			id++
		case "duration":
			duration++
		case "evidence":
			evidence = "user_declared_test"
		case "label":
			label = "guest_9"
		case "unknown":
			label = "unknown"
		case "uncertain":
			label = "uncertain"
		}
		line := fmt.Sprintf(`{"event":"observation","id":%d,"label":%q,"duration_ms":%d,"evidence":%q}`, id, label, duration, evidence)
		switch mode {
		case "extra":
			line = strings.TrimSuffix(line, "}") + `,"text":"private-secret"}`
		case "duplicate":
			line = strings.TrimSuffix(line, "}") + `,"id":1}`
		case "null":
			line = strings.Replace(line, `"label":"guest_2"`, `"label":null`, 1)
		case "missing":
			line = strings.Replace(line, `"duration_ms":1500,`, "", 1)
		case "negative":
			line = strings.Replace(line, `"duration_ms":1500`, `"duration_ms":-1`, 1)
		}
		fmt.Println(line)
		if count == 31 {
			fmt.Println(`{"event":"closed","evidence":"turn_taking_test"}`)
			return 0
		}
	}
}
func fakeConfig(t *testing.T, mode string) Config {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	model := filepath.Join(t.TempDir(), mode)
	if err := os.WriteFile(model, []byte("synthetic-model"), 0600); err != nil {
		t.Fatal(err)
	}
	return Config{Binary: binary, Model: model, SHA256: strings.Repeat("a", 64)}
}
func testJob(id int64) Job {
	return Job{ID: id, Start: (id-1)*30000 + 100, End: (id-1)*30000 + 24100, PCM: bytes.Repeat([]byte{3}, 48000)}
}
func receive(t *testing.T, ch <-chan Result) Result {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(2 * time.Second):
		t.Fatal("missing observer result")
		return Result{}
	}
}
func eventually(t *testing.T, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not reached")
}
func observerForTest(t *testing.T, cfg Config, emit func(Result), limits observerLimits) *Observer {
	t.Helper()
	o, err := newObserver(context.Background(), "test-session", cfg, emit, limits)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(o.Close)
	return o
}
func shortLimits() observerLimits {
	return observerLimits{startup: 150 * time.Millisecond, request: 150 * time.Millisecond, lifetime: 3 * time.Second}
}
func processGone(t *testing.T, path string, allowZombie ...bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		if syscall.Kill(pid, 0) == syscall.ESRCH {
			return true
		}
		data, _ := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		fields := strings.Fields(string(data))
		return len(allowZombie) > 0 && allowZombie[0] && len(fields) > 2 && fields[2] == "Z"
	})
}

func TestConfigValidation(t *testing.T) {
	cfg := fakeConfig(t, "ok")
	tests := []struct {
		name, session string
		edit          func(*Config)
	}{
		{"relative binary", "test-session", func(c *Config) { c.Binary = "speaker-lab" }},
		{"binary directory", "test-session", func(c *Config) { c.Binary = filepath.Dir(c.Model) }},
		{"binary not executable", "test-session", func(c *Config) { c.Binary = c.Model }},
		{"missing model", "test-session", func(c *Config) { c.Model += "-missing" }},
		{"model directory", "test-session", func(c *Config) { c.Model = filepath.Dir(c.Model) }},
		{"uppercase hash", "test-session", func(c *Config) { c.SHA256 = strings.Repeat("A", 64) }},
		{"bad session", "bad/session", func(c *Config) {}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := cfg
			tt.edit(&c)
			o, err := New(context.Background(), tt.session, c, nil)
			if err == nil {
				if o != nil {
					o.Close()
				}
				t.Fatal("invalid config accepted")
			}
			if strings.Contains(err.Error(), c.Model) {
				t.Fatal("error leaked model path")
			}
		})
	}
}
func TestNewDoesNotWaitForReady(t *testing.T) {
	cfg := fakeConfig(t, "startup-stall")
	ch := make(chan Result, 2)
	start := time.Now()
	o := observerForTest(t, cfg, func(r Result) { ch <- r }, shortLimits())
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("New waited for helper ready")
	}
	if !o.Offer(testJob(1)) {
		t.Fatal("first initializing job rejected")
	}
	if o.Offer(testJob(2)) {
		t.Fatal("initialization queue exceeded one")
	}
	if r := receive(t, ch); r.State != "unavailable" || r.ID != 1 {
		t.Fatalf("startup failure=%+v", r)
	}
	o.Close()
	processGone(t, cfg.Model+".pid")
	if o.Offer(testJob(3)) {
		t.Fatal("dead tracker restarted")
	}
}
func TestOfferCopiesAndBoundsQueue(t *testing.T) {
	cfg := fakeConfig(t, "copy")
	ch := make(chan Result, 4)
	o := observerForTest(t, cfg, func(r Result) { ch <- r }, observerLimits{startup: time.Second, request: time.Second, lifetime: 3 * time.Second})
	eventually(t, func() bool { _, e := os.Stat(cfg.Model + ".waiting"); return e == nil })
	job := testJob(1)
	start := time.Now()
	if !o.Offer(job) {
		t.Fatal("offer 1")
	}
	clear(job.PCM)
	eventually(t, func() bool { return len(o.jobs) == 0 })
	if !o.Offer(testJob(2)) {
		t.Fatal("queued offer")
	}
	if o.Offer(testJob(3)) {
		t.Fatal("accepted third resident job")
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("offer blocked on helper")
	}
	if err := os.WriteFile(cfg.Model+".release", nil, 0600); err != nil {
		t.Fatal(err)
	}
	for id := int64(1); id <= 2; id++ {
		r := receive(t, ch)
		want := testJob(id)
		if r.ID != id || r.State != "temporary" || r.Label != "guest_2" || r.Start != want.Start || r.End != want.End || r.ElapsedMS < 0 {
			t.Fatalf("result %+v", r)
		}
	}
}
func TestOfferRejectsInputBeforeQueueing(t *testing.T) {
	cfg := fakeConfig(t, "startup-stall")
	o := observerForTest(t, cfg, nil, shortLimits())
	bad := []Job{{}, testJob(0), {ID: 1, Start: -1, End: 23999, PCM: make([]byte, 48000)}, {ID: 1, Start: 10, End: 10, PCM: []byte{1, 2}}, {ID: 1, Start: 0, End: 128001, PCM: make([]byte, 256002)}, {ID: 1, Start: 0, End: 24000, PCM: make([]byte, 47999)}}
	for i, j := range bad {
		if o.Offer(j) {
			t.Fatalf("accepted invalid input %d", i)
		}
	}
	if !o.Offer(testJob(1)) {
		t.Fatal("bad input changed queue")
	}
}
func TestWrongOutputIsUnavailableAndReaped(t *testing.T) {
	for _, mode := range []string{"id", "duration", "evidence", "label", "extra", "duplicate", "null", "missing", "negative", "oversize", "malformed", "eof", "closed", "stall"} {
		t.Run(mode, func(t *testing.T) {
			cfg := fakeConfig(t, mode)
			ch := make(chan Result, 4)
			o := observerForTest(t, cfg, func(r Result) { ch <- r }, shortLimits())
			if !o.Offer(testJob(1)) {
				t.Fatal("offer")
			}
			r := receive(t, ch)
			if r.State != "unavailable" || r.Label != "" || r.ID != 1 {
				t.Fatalf("bad result=%+v", r)
			}
			if o.Offer(testJob(2)) {
				t.Fatal("failed observer restarted")
			}
			o.Close()
			processGone(t, cfg.Model+".pid")
		})
	}
}
func TestLabelMappingAndEnvironment(t *testing.T) {
	t.Setenv("SPEAKERLIVE_PRIVATE_SECRET", "do-not-inherit")
	for _, mode := range []string{"unknown", "uncertain", "env"} {
		t.Run(mode, func(t *testing.T) {
			cfg := fakeConfig(t, mode)
			ch := make(chan Result, 2)
			o := observerForTest(t, cfg, func(r Result) { ch <- r }, shortLimits())
			if !o.Offer(testJob(1)) {
				t.Fatal("offer")
			}
			r := receive(t, ch)
			want := mode
			if mode == "env" {
				want = "temporary"
			}
			if r.State != want {
				t.Fatalf("state=%+v", r)
			}
			if mode != "env" && r.Label != "" {
				t.Fatal("uncertain label retained")
			}
		})
	}
}
func TestCloseKillsBlockedWriterAndClearsQueue(t *testing.T) {
	cfg := fakeConfig(t, "blocked")
	ch := make(chan Result, 4)
	o := observerForTest(t, cfg, func(r Result) { ch <- r }, observerLimits{startup: time.Second, request: time.Second, lifetime: 3 * time.Second})
	eventually(t, func() bool { _, e := os.Stat(cfg.Model + ".waiting"); return e == nil })
	job := Job{ID: 1, Start: 20, End: 128020, PCM: bytes.Repeat([]byte{3}, 256000)}
	if !o.Offer(job) {
		t.Fatal("offer 1")
	}
	eventually(t, func() bool { return len(o.jobs) == 0 })
	job.ID = 2
	job.Start = 128020
	job.End = 256020
	if !o.Offer(job) {
		t.Fatal("offer 2")
	}
	queued := <-o.jobs
	o.jobs <- queued
	start := time.Now()
	o.Close()
	o.Close()
	if !bytes.Equal(queued.PCM, make([]byte, len(queued.PCM))) {
		t.Fatal("queued PCM not cleared")
	}

	if time.Since(start) > time.Second {
		t.Fatal("Close blocked")
	}
	if len(o.jobs) != 0 {
		t.Fatal("queued PCM retained")
	}
	processGone(t, cfg.Model+".pid")
	select {
	case r := <-ch:
		t.Fatalf("callback after cancellation %+v", r)
	default:
	}
}
func TestCancellationKillsDescendants(t *testing.T) {
	cfg := fakeConfig(t, "descendant")
	ctx, cancel := context.WithCancel(context.Background())
	o, err := newObserver(ctx, "test-session", cfg, nil, shortLimits())
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	eventually(t, func() bool { _, e := os.Stat(cfg.Model + ".child"); return e == nil })
	if !o.Offer(testJob(1)) {
		t.Fatal("offer")
	}
	cancel()
	o.Close()
	processGone(t, cfg.Model+".pid")
	processGone(t, cfg.Model+".child", true)
}
func TestFiniteSessionAndMonotonicCoordinates(t *testing.T) {
	cfg := fakeConfig(t, "ok")
	ch := make(chan Result, 40)
	o := observerForTest(t, cfg, func(r Result) { ch <- r }, observerLimits{startup: time.Second, request: time.Second, lifetime: 4 * time.Second})
	for id := int64(1); id <= 32; id++ {
		if !o.Offer(testJob(id)) {
			t.Fatalf("offer %d", id)
		}
		if r := receive(t, ch); r.ID != id || r.State != "temporary" {
			t.Fatalf("result %+v", r)
		}
		if o.Offer(testJob(id)) {
			t.Fatal("replayed ID accepted")
		}
		overlap := testJob(id + 1)
		overlap.Start = testJob(id).Start
		if o.Offer(overlap) {
			t.Fatal("overlap accepted")
		}
	}
	if o.Offer(testJob(33)) {
		t.Fatal("accepted clip beyond bound")
	}
	o.Close()
}
func TestLifetimeAndStartupFailures(t *testing.T) {
	for _, mode := range []string{"startup-eof", "startup-bad", "ok"} {
		t.Run(mode, func(t *testing.T) {
			cfg := fakeConfig(t, mode)
			ch := make(chan Result, 2)
			o := observerForTest(t, cfg, func(r Result) { ch <- r }, observerLimits{startup: time.Second, request: time.Second, lifetime: 100 * time.Millisecond})
			r := receive(t, ch)
			if r.State != "unavailable" || r.ID != 0 {
				t.Fatalf("global failure %+v", r)
			}
			o.Close()
			processGone(t, cfg.Model+".pid")
		})
	}
}

func TestBlockedWriteDeadlineClearsBothJobs(t *testing.T) {
	cfg := fakeConfig(t, "blocked")
	ch := make(chan Result, 4)
	o := observerForTest(t, cfg, func(r Result) { ch <- r }, shortLimits())
	eventually(t, func() bool { _, err := os.Stat(cfg.Model + ".waiting"); return err == nil })
	first := Job{ID: 1, Start: 10, End: 128010, PCM: bytes.Repeat([]byte{3}, 256000)}
	if !o.Offer(first) {
		t.Fatal("first offer")
	}
	eventually(t, func() bool { return len(o.jobs) == 0 })
	second := first
	second.ID = 2
	second.Start = 128010
	second.End = 256010
	if !o.Offer(second) {
		t.Fatal("queued offer")
	}
	queued := <-o.jobs
	o.jobs <- queued
	for id := int64(1); id <= 2; id++ {
		if r := receive(t, ch); r.ID != id || r.State != "unavailable" {
			t.Fatalf("timeout result %+v", r)
		}
	}
	o.Close()
	processGone(t, cfg.Model+".pid")
	if !bytes.Equal(queued.PCM, make([]byte, len(queued.PCM))) {
		t.Fatal("timed out queued PCM not cleared")
	}
	if o.Offer(testJob(3)) {
		t.Fatal("timed out helper restarted")
	}
}
func TestRequestTimeoutKillsDescendants(t *testing.T) {
	cfg := fakeConfig(t, "descendant")
	ch := make(chan Result, 2)
	o := observerForTest(t, cfg, func(r Result) { ch <- r }, shortLimits())
	eventually(t, func() bool { _, err := os.Stat(cfg.Model + ".child"); return err == nil })
	if !o.Offer(testJob(1)) {
		t.Fatal("offer")
	}
	if r := receive(t, ch); r.ID != 1 || r.State != "unavailable" {
		t.Fatalf("timeout result %+v", r)
	}
	o.Close()
	processGone(t, cfg.Model+".pid")
	processGone(t, cfg.Model+".child", true)
}
