package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func modelFixture(t *testing.T) (string, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "model")
	b := []byte("bounded model fixture")
	if e := os.WriteFile(p, b, 0600); e != nil {
		t.Fatal(e)
	}
	s := sha256.Sum256(b)
	return p, hex.EncodeToString(s[:])
}
func TestValidateModel(t *testing.T) {
	p, s := modelFixture(t)
	if e := validateModel(p, s); e != nil {
		t.Fatal(e)
	}
	if validateModel(p, testSHA) == nil {
		t.Fatal("digest mismatch accepted")
	}
	link := p + "link"
	os.Symlink(p, link)
	if validateModel(link, s) == nil {
		t.Fatal("symlink accepted")
	}
	if validateModel(filepath.Dir(p), s) == nil {
		t.Fatal("directory accepted")
	}
}
func TestPrivateSocket(t *testing.T) {
	d := t.TempDir()
	os.Chmod(d, 0700)
	p := filepath.Join(d, "worker.sock")
	l, e := listenPrivate(p)
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0600 {
		t.Fatal("socket permissions")
	}
	if l2, e := listenPrivate(p); e == nil {
		l2.Close()
		t.Fatal("existing socket accepted")
	}
	os.Chmod(d, 0750)
	if l2, e := listenPrivate(filepath.Join(d, "bad.sock")); e == nil {
		l2.Close()
		t.Fatal("unsafe parent accepted")
	}
	os.Chmod(d, 0700)
	link := filepath.Join(t.TempDir(), "link")
	os.Symlink(d, link)
	if l2, e := listenPrivate(filepath.Join(link, "x.sock")); e == nil {
		l2.Close()
		t.Fatal("symlink parent accepted")
	}
}

type fakeExtractor struct{ delay time.Duration }

func (f *fakeExtractor) Dimension() int { return 2 }
func (f *fakeExtractor) Extract([]byte) ([]float32, error) {
	time.Sleep(f.delay)
	return []float32{1, 2}, nil
}
func (f *fakeExtractor) Close() {}
func TestWorkerConnectionAndCleanShutdown(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); serveConnection(ctx, b, &fakeExtractor{}, testSHA); b.Close() }()
	dim, e := readReady(a, testSHA)
	if e != nil || dim != 2 {
		t.Fatal(e)
	}
	req := sampleRequest()
	if e = writeRequest(a, req, make([]byte, req.PCMBytes)); e != nil {
		t.Fatal(e)
	}
	if _, _, e = readResponse(a, req, dim); e != nil {
		t.Fatal(e)
	}
	req.ID++
	req.Session = "session2"
	req.Scope = "scope2"
	req.Generation++
	if e = writeRequest(a, req, make([]byte, req.PCMBytes)); e != nil {
		t.Fatal(e)
	}
	if _, _, e = readResponse(a, req, dim); e != nil {
		t.Fatal("cross-session request", e)
	}
	a.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
}
func TestCLINoDefaultListenOrEcho(t *testing.T) {
	for _, args := range [][]string{nil, {"unknown", "secret-raw-text"}, {"worker", "--model", "secret-raw-text"}} {
		var out, errOut bytes.Buffer
		if runCLI(context.Background(), args, &out, &errOut) == 0 {
			t.Fatal("invalid command succeeded")
		}
		if strings.Contains(out.String()+errOut.String(), "secret") || strings.Contains(out.String()+errOut.String(), "embedding") {
			t.Fatal("unsafe output")
		}
	}
}
func TestHelperWorker(t *testing.T) {
	if os.Getenv("SPEAKER_LAB_TEST_HELPER") != "1" {
		return
	}
	socket := os.Getenv("SPEAKER_LAB_HELPER_SOCKET")
	l, e := listenPrivate(socket)
	if e != nil {
		os.Exit(2)
	}
	defer l.Close()
	c, e := l.Accept()
	if e != nil {
		os.Exit(3)
	}
	defer c.Close()
	serveConnection(context.Background(), c, &fakeExtractor{delay: 10 * time.Second}, testSHA)
	os.Exit(0)
}
func TestParentTimeoutKillsAndReapsChild(t *testing.T) {
	d := t.TempDir()
	os.Chmod(d, 0700)
	socket := filepath.Join(d, "worker.sock")
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperWorker$")
	cmd.Env = append(os.Environ(), "SPEAKER_LAB_TEST_HELPER=1", "SPEAKER_LAB_HELPER_SOCKET="+socket)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	w, e := launchWorker(ctx, cmd, socket, testSHA, time.Second)
	if e != nil {
		t.Fatal(e)
	}
	_, _, e = w.request(ctx, sampleRequest(), make([]byte, 32000))
	if e == nil {
		t.Fatal("missing timeout")
	}
	if w.cmd.ProcessState == nil {
		t.Fatal("child not reaped")
	}
	status, ok := w.cmd.ProcessState.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatal("child was not killed by watchdog")
	}
	w.Close()
}
func TestRealModelProbeOptIn(t *testing.T) {
	model, sha, wav := os.Getenv("SPEAKER_LAB_TEST_MODEL"), os.Getenv("SPEAKER_LAB_TEST_SHA256"), os.Getenv("SPEAKER_LAB_TEST_WAV")
	if model == "" || sha == "" || wav == "" {
		t.Skip("explicit model+digest+synthetic WAV opt-in not set")
	}
	exe := os.Getenv("SPEAKER_LAB_TEST_BINARY")
	if exe == "" {
		t.Fatal("explicit built binary required")
	}
	cmd := exec.Command(exe, "probe", "--model", model, "--sha256", sha, "--wav", wav)
	b, e := cmd.Output()
	if e != nil {
		t.Fatal("real probe failed")
	}
	var result probeResult
	if json.Unmarshal(b, &result) != nil || result.Dimension < 1 || result.Samples < 1 || result.OverlapAssessment != "unavailable" || result.QualityEvidence != "fixture-only" {
		t.Fatal("invalid safe result")
	}
	if bytes.Contains(b, []byte(model)) || bytes.Contains(b, []byte(wav)) || bytes.Contains(b, []byte("embedding")) {
		t.Fatal("unsafe probe result")
	}
}

func TestProbeRefusesNonRegularInputWithoutOpening(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "input.wav")
	if e := syscall.Mkfifo(fifo, 0600); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, e := runProbe(context.Background(), "unused", testSHA, fifo); done <- e }()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("accepted FIFO")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("blocked opening FIFO")
	}
}
func TestSocketCleanupPreservesReplacement(t *testing.T) {
	d := t.TempDir()
	os.Chmod(d, 0700)
	p := filepath.Join(d, "worker.sock")
	l, e := listenPrivate(p)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Remove(p); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(p, []byte("replacement"), 0600); e != nil {
		t.Fatal(e)
	}
	l.Close()
	b, e := os.ReadFile(p)
	if e != nil || string(b) != "replacement" {
		t.Fatal("removed replacement file")
	}
}
func TestModelRejectsOversizeBeforeRead(t *testing.T) {
	p, s := modelFixture(t)
	f, e := os.OpenFile(p, os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	e = f.Truncate(maxModelBytes + 1)
	f.Close()
	if e != nil {
		t.Fatal(e)
	}
	if validateModel(p, s) == nil {
		t.Fatal("oversize model accepted")
	}
}

func TestParentIODeadlineKillsAndReapsChild(t *testing.T) {
	d := t.TempDir()
	os.Chmod(d, 0700)
	socket := filepath.Join(d, "worker.sock")
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperWorker$")
	cmd.Env = append(os.Environ(), "SPEAKER_LAB_TEST_HELPER=1", "SPEAKER_LAB_HELPER_SOCKET="+socket)
	w, e := launchWorker(context.Background(), cmd, socket, testSHA, time.Second)
	if e != nil {
		t.Fatal(e)
	}
	defer w.Close()
	started := time.Now()
	if _, _, e = w.request(context.Background(), sampleRequest(), make([]byte, 32000)); e == nil {
		t.Fatal("inference hang accepted")
	}
	if time.Since(started) > 3*time.Second || w.cmd.ProcessState == nil {
		t.Fatal("deadline did not reap child")
	}
}
func TestRealWorkerCleanShutdownOptIn(t *testing.T) {
	model, sha, exe := os.Getenv("SPEAKER_LAB_TEST_MODEL"), os.Getenv("SPEAKER_LAB_TEST_SHA256"), os.Getenv("SPEAKER_LAB_TEST_BINARY")
	if model == "" || sha == "" || exe == "" {
		t.Skip("explicit model+digest+binary opt-in not set")
	}
	t.Setenv("FAMILY_ROBOT_DB_KEY", "SENTINEL-NO-LEAK")
	w, e := startWorkerBinary(context.Background(), exe, model, sha)
	if e != nil {
		t.Fatal(e)
	}
	defer w.Close()
	env, e := os.ReadFile(fmt.Sprintf("/proc/%d/environ", w.cmd.Process.Pid))
	if e != nil || bytes.Contains(env, []byte("SENTINEL-NO-LEAK")) {
		t.Fatal("worker inherited parent credential environment")
	}
	if e = w.cmd.Process.Signal(syscall.SIGTERM); e != nil {
		t.Fatal(e)
	}
	select {
	case <-w.done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not exit cleanly")
	}
	if !w.cmd.ProcessState.Success() {
		t.Fatal("worker shutdown failed")
	}
	socket := w.conn.RemoteAddr().String()
	if _, e = os.Lstat(socket); !os.IsNotExist(e) {
		t.Fatal("worker left socket")
	}
}

func TestRealWorkerCleanupPreservesReplacementOptIn(t *testing.T) {
	model, sha, exe := os.Getenv("SPEAKER_LAB_TEST_MODEL"), os.Getenv("SPEAKER_LAB_TEST_SHA256"), os.Getenv("SPEAKER_LAB_TEST_BINARY")
	if model == "" || sha == "" || exe == "" {
		t.Skip("explicit model+digest+binary opt-in not set")
	}
	w, e := startWorkerBinary(context.Background(), exe, model, sha)
	if e != nil {
		t.Fatal(e)
	}
	defer w.Close()
	socket := w.conn.RemoteAddr().String()
	dir := filepath.Dir(socket)
	if e = os.Remove(socket); e != nil {
		t.Fatal(e)
	}
	replacement, e := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if e != nil {
		t.Fatal(e)
	}
	replacement.SetUnlinkOnClose(false)
	defer replacement.Close()
	defer os.Remove(dir)
	defer os.Remove(socket)
	identity, e := os.Lstat(socket)
	if e != nil {
		t.Fatal(e)
	}
	w.Close()
	current, e := os.Lstat(socket)
	if e != nil || !os.SameFile(identity, current) {
		t.Fatal("parent cleanup removed replacement socket")
	}
}

func TestParentCleanupWithoutVerifiedIdentityPreservesSocket(t *testing.T) {
	d := t.TempDir()
	os.Chmod(d, 0700)
	socket := filepath.Join(d, "worker.sock")
	l, e := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if e != nil {
		t.Fatal(e)
	}
	l.SetUnlinkOnClose(false)
	defer l.Close()
	identity, e := os.Lstat(socket)
	if e != nil {
		t.Fatal(e)
	}
	cleanupWorkerDirectory(d, socket, nil)
	current, e := os.Lstat(socket)
	if e != nil || !os.SameFile(identity, current) {
		t.Fatal("unverified startup socket removed")
	}
	cleanupWorkerDirectory(d, socket, identity)
	if _, e = os.Lstat(d); !os.IsNotExist(e) {
		t.Fatal("owned socket and empty directory not removed")
	}
}

func TestColdStartupHelper(t *testing.T) {
	mode := os.Getenv("SPEAKER_LAB_COLD_HELPER")
	if mode == "" {
		return
	}
	if mode == "never_listen" {
		time.Sleep(10 * time.Second)
		os.Exit(0)
	}
	socket := os.Getenv("SPEAKER_LAB_HELPER_SOCKET")
	l, e := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if e != nil {
		os.Exit(2)
	}
	l.SetUnlinkOnClose(false)
	c, e := l.Accept()
	if e != nil {
		os.Exit(3)
	}
	// A valid prefix followed by incomplete JSON must remain inside coldLimit.
	c.Write([]byte{0, 0, 0, 20, '{'})
	if os.Remove(socket) != nil || os.WriteFile(socket, []byte("unknown replacement"), 0600) != nil {
		os.Exit(4)
	}
	time.Sleep(10 * time.Second)
	os.Exit(0)
}
func TestColdStartupDeadlineKillsAndReaps(t *testing.T) {
	for _, mode := range []string{"never_listen", "partial_ready"} {
		t.Run(mode, func(t *testing.T) {
			d := t.TempDir()
			os.Chmod(d, 0700)
			socket := filepath.Join(d, "worker.sock")
			cmd := exec.Command(os.Args[0], "-test.run=^TestColdStartupHelper$")
			cmd.Env = append(os.Environ(), "SPEAKER_LAB_COLD_HELPER="+mode, "SPEAKER_LAB_HELPER_SOCKET="+socket)
			started := time.Now()
			w, e := launchWorker(context.Background(), cmd, socket, testSHA, 300*time.Millisecond)
			if w != nil {
				w.Close()
				t.Fatal("hung startup accepted")
			}
			if e == nil || time.Since(started) > 2*time.Second || cmd.ProcessState == nil {
				t.Fatal("cold deadline did not bound and reap child")
			}
			status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
			if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
				t.Fatal("actual child not killed")
			}
			if mode == "partial_ready" {
				cleanupWorkerDirectory(d, socket, nil)
				b, e := os.ReadFile(socket)
				if e != nil || string(b) != "unknown replacement" {
					t.Fatal("startup cleanup removed unknown replacement")
				}
			}
		})
	}
}

func TestEvaluateCLI(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code string
	}{
		{[]string{"evaluate"}, "usage"},
		{[]string{"evaluate", "--model", "/secret/model", "--sha256", testSHA}, "usage"},
		{[]string{"evaluate", "--model", "/secret/model", "--sha256", testSHA, "--manifest", "/secret/missing.json"}, "evaluate_failed"},
		{[]string{"evaluate", "--model", "/secret/model", "--sha256", testSHA, "--manifest", "/secret/missing.json", "extra"}, "usage"},
	} {
		var out, errOut bytes.Buffer
		if runCLI(context.Background(), tc.args, &out, &errOut) != 1 || out.Len() != 0 || errOut.String() != fmt.Sprintf("{\"error\":\"%s\"}\n", tc.code) {
			t.Fatalf("unsafe CLI output %q %q", out.String(), errOut.String())
		}
	}
}
