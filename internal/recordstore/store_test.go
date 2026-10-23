package recordstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func sample() Snapshot {
	return Snapshot{Version: 1, UpdatedAt: time.Now().UTC(), Data: json.RawMessage(`{"task":"count to fifty"}`)}
}
func openTest(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Open(Options{Directory: dir, Retention: 7 * 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return s
}
func saveTest(t *testing.T, s *Store, device string, snap Snapshot) {
	t.Helper()
	r, err := s.Save(device, snap)
	if err != nil {
		t.Fatal(err)
	}
	if err = <-r; err != nil {
		t.Fatal(err)
	}
}
func devicePath(dir, device string) string {
	sum := sha256.Sum256([]byte(device))
	return filepath.Join(dir, hex.EncodeToString(sum[:])+".json")
}

func TestSaveLoadFIFOIsolationAndReopen(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir)
	snap := sample()
	r, err := s.Save("../device-a", snap)
	if err != nil {
		t.Fatal(err)
	}
	// A load queued after Save must see it, even without waiting on its receipt.
	got, err := s.Load(context.Background(), "../device-a")
	if err != nil || string(got.Data) != string(snap.Data) {
		t.Fatalf("load=%+v err=%v", got, err)
	}
	if err = <-r; err != nil {
		t.Fatal(err)
	}
	if _, err = s.Load(context.Background(), "device-b"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err = s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, dir)
	if _, err = s.Load(context.Background(), "../device-a"); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(devicePath(dir, "../device-a")); err != nil {
		t.Fatal(err)
	}
}

func TestClearFollowsQueuedSave(t *testing.T) {
	s := openTest(t, t.TempDir())
	r, err := s.Save("a", sample())
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Clear(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	if err = <-r; err != nil {
		t.Fatal(err)
	}
	if _, err = s.Load(context.Background(), "a"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err = s.Clear(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestRetentionOnReadAndOpen(t *testing.T) {
	var err error
	dir := t.TempDir()
	s := openTest(t, dir)
	snap := sample()
	saveTest(t, s, "a", snap)
	if err = s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	snap.UpdatedAt = time.Now().Add(-8 * 24 * time.Hour)
	b, _ := json.Marshal(snap)
	if err = os.WriteFile(devicePath(dir, "a"), b, 0600); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, dir)
	if _, err = s.Load(context.Background(), "a"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err = os.Stat(devicePath(dir, "a")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	snap = sample()
	saveTest(t, s, "b", snap)
	snap.UpdatedAt = time.Now().Add(-8 * 24 * time.Hour)
	b, _ = json.Marshal(snap)
	if err = os.WriteFile(devicePath(dir, "b"), b, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Load(context.Background(), "b"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestValidationAndSize(t *testing.T) {
	s := openTest(t, t.TempDir())
	for _, snap := range []Snapshot{{}, {Version: 1, UpdatedAt: time.Now(), Data: json.RawMessage(`no`)}, {Version: 0, UpdatedAt: time.Now(), Data: json.RawMessage(`{}`)}} {
		if _, err := s.Save("a", snap); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid=%v", err)
		}
	}
	if _, err := s.Save("", sample()); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	snap := sample()
	snap.Data = json.RawMessage(`"` + strings.Repeat("x", 1<<20) + `"`)
	if _, err := s.Save("a", snap); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
}

func TestCorruptAndOversizedDiskData(t *testing.T) {
	for _, body := range []string{`broken`, `{"version":1,"updated_at":"2026-01-01T00:00:00Z","data":null}`, strings.Repeat("x", (1<<20)+1)} {
		t.Run(fmt.Sprint(len(body)), func(t *testing.T) {
			dir := t.TempDir()
			s := openTest(t, dir)
			saveTest(t, s, "a", sample())
			if err := os.WriteFile(devicePath(dir, "a"), []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Load(context.Background(), "a"); err == nil || errors.Is(err, ErrNotFound) {
				t.Fatalf("corruption hidden: %v", err)
			}
			if err := s.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			if s2, err := Open(Options{Directory: dir, Retention: 7 * 24 * time.Hour}); err == nil {
				s2.Close(context.Background())
				t.Fatal("open accepted corrupt data")
			}
		})
	}
}

func TestEightDevicesAndClearCapacity(t *testing.T) {
	s := openTest(t, t.TempDir())
	for i := 0; i < 8; i++ {
		saveTest(t, s, fmt.Sprint(i), sample())
	}
	r, err := s.Save("ninth", sample())
	if err == nil {
		err = <-r
	}
	if !errors.Is(err, ErrTooManyDevices) {
		t.Fatal(err)
	}
	saveTest(t, s, "0", sample())
	if err = s.Clear(context.Background(), "0"); err != nil {
		t.Fatal(err)
	}
	saveTest(t, s, "ninth", sample())
}

func TestPermissionsAndExclusiveLock(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	s := openTest(t, dir)
	saveTest(t, s, "a", sample())
	for path, want := range map[string]os.FileMode{dir: 0700, devicePath(dir, "a"): 0600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != want {
			t.Fatalf("%s: %v %v", path, info, err)
		}
	}
	if s2, err := Open(Options{Directory: dir, Retention: 7 * 24 * time.Hour}); !errors.Is(err, ErrLocked) {
		if s2 != nil {
			s2.Close(context.Background())
		}
		t.Fatalf("second open: %v", err)
	}
}

func TestExecChildDoesNotInheritStoreFiles(t *testing.T) {
	const helperEnv = "RECORDSTORE_EXEC_HELPER"
	if raw := os.Getenv(helperEnv); raw != "" {
		var files map[int]string
		if err := json.Unmarshal([]byte(raw), &files); err != nil {
			t.Fatal(err)
		}
		inherited := []string{}
		for fd, path := range files {
			var stat syscall.Stat_t
			if err := syscall.Fstat(fd, &stat); err == nil {
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				want := info.Sys().(*syscall.Stat_t)
				if stat.Dev == want.Dev && stat.Ino == want.Ino {
					inherited = append(inherited, path)
				}
			}
		}
		if err := json.NewEncoder(os.Stdout).Encode(inherited); err != nil {
			t.Fatal(err)
		}
		// Remain alive until the parent has closed and reopened the store.
		if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
			t.Fatal(err)
		}
		return
	}

	dir := t.TempDir()
	s := openTest(t, dir)
	saveTest(t, s, "a", sample())
	// Hold the exact descriptor-opening path used by readFile across exec, so
	// snapshot inheritance is covered without racing the brief disk read.
	snapshot, err := openStoreFile(devicePath(dir, "a"), syscall.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	files, err := json.Marshal(map[int]string{
		int(s.lock.Fd()):   filepath.Join(dir, ".lock"),
		int(snapshot.Fd()): devicePath(dir, "a"),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+t.Name()+"$", "-test.count=1")
	cmd.Env = append(os.Environ(), helperEnv+"="+string(files))
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stdin.Close()
		if err := cmd.Wait(); err != nil {
			t.Error(err)
		}
	}()
	var inherited []string
	if err = json.NewDecoder(stdout).Decode(&inherited); err != nil {
		t.Fatal(err)
	}
	if len(inherited) != 0 {
		t.Errorf("exec child inherited store descriptors: %v", inherited)
	}
	if err = s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(Options{Directory: dir, Retention: 7 * 24 * time.Hour})
	if err != nil {
		t.Fatalf("reopen while exec child is alive: %v", err)
	}
	defer reopened.Close(context.Background())
	if _, err = reopened.Load(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
}

func TestDiskFailureIsReceiptError(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir)
	// A directory at the target path reliably breaks rename even for root.
	if err := os.Mkdir(devicePath(dir, "a"), 0700); err != nil {
		t.Fatal(err)
	}
	r, err := s.Save("a", sample())
	if err != nil {
		t.Fatal(err)
	}
	if err = <-r; err == nil {
		t.Fatal("false success receipt")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".snapshot-") {
			t.Fatal("temporary file leaked")
		}
	}
}

func TestRejectZeroRetentionAndWhitespaceNull(t *testing.T) {
	if s, err := Open(Options{Directory: t.TempDir()}); !errors.Is(err, ErrInvalid) {
		if s != nil {
			s.Close(context.Background())
		}
		t.Fatalf("zero retention: %v", err)
	}
	s := openTest(t, t.TempDir())
	v := sample()
	v.Data = json.RawMessage(` null `)
	if _, err := s.Save("a", v); !errors.Is(err, ErrInvalid) {
		t.Fatalf("null: %v", err)
	}
}

func TestQueueBoundCancellationAndCloseTimeout(t *testing.T) {
	s := openTest(t, t.TempDir())
	started := make(chan struct{})
	release := make(chan struct{})
	if err := s.enqueue(func() { close(started); <-release }); err != nil {
		t.Fatal(err)
	}
	<-started
	// Holding the worker makes queue saturation deterministic without slow disk.
	receipts := make([]<-chan error, 0, 32)
	for i := 0; i < 32; i++ {
		r, err := s.Save("a", sample())
		if err != nil {
			t.Fatal(err)
		}
		receipts = append(receipts, r)
	}
	if _, err := s.Save("a", sample()); !errors.Is(err, ErrBusy) {
		t.Fatalf("full Save: %v", err)
	}
	if _, err := s.Load(context.Background(), "a"); !errors.Is(err, ErrBusy) {
		t.Fatalf("full Load: %v", err)
	}
	if err := s.Clear(context.Background(), "a"); !errors.Is(err, ErrBusy) {
		t.Fatalf("full Clear: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Load(ctx, "a"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := s.Clear(ctx, "a"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := s.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := s.Save("a", sample()); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	close(release)
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, r := range receipts {
		if err := <-r; err != nil {
			t.Fatal(err)
		}
	}
}

func TestSaveOwnsDataAndCloseDrains(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir)
	started := make(chan struct{})
	release := make(chan struct{})
	if err := s.enqueue(func() { close(started); <-release }); err != nil {
		t.Fatal(err)
	}
	<-started
	snap := sample()
	want := string(snap.Data)
	r, err := s.Save("a", snap)
	if err != nil {
		t.Fatal(err)
	}
	for i := range snap.Data {
		snap.Data[i] = 'x'
	}
	close(release)
	if err = s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = <-r; err != nil {
		t.Fatal(err)
	}
	s = openTest(t, dir)
	got, err := s.Load(context.Background(), "a")
	if err != nil || string(got.Data) != want {
		t.Fatalf("got %q %v", got.Data, err)
	}
}

func TestQueuedCanceledClearDoesNotDelete(t *testing.T) {
	s := openTest(t, t.TempDir())
	saveTest(t, s, "a", sample())
	started := make(chan struct{})
	release := make(chan struct{})
	if err := s.enqueue(func() { close(started); <-release }); err != nil {
		t.Fatal(err)
	}
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { finished <- s.Clear(ctx, "a") }()
	deadline := time.After(time.Second)
	for len(s.jobs) == 0 {
		select {
		case <-deadline:
			t.Fatal("Clear not queued")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(release)
	if _, err := s.Load(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
}

func TestOpenSecuresExistingFilesAndRejectsSymlinks(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir)
	saveTest(t, s, "a", sample())
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(devicePath(dir, "a"), 0644); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, dir)
	info, err := os.Stat(devicePath(dir, "a"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("existing permissions: %v %v", info, err)
	}
	if err = s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "snapshot")
	b, _ := json.Marshal(sample())
	if err = os.WriteFile(target, b, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(devicePath(dir, "a")); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(target, devicePath(dir, "a")); err != nil {
		t.Fatal(err)
	}
	if bad, err := Open(Options{Directory: dir, Retention: time.Hour}); err == nil {
		bad.Close(context.Background())
		t.Fatal("accepted symlink")
	}
}

func TestConfiguredRetentionAndExpiredCapacity(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(Options{Directory: dir, Retention: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	old := sample()
	old.UpdatedAt = time.Now().Add(-2 * time.Hour)
	saveTest(t, s, "old", old)
	if _, err = s.Load(context.Background(), "old"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		saveTest(t, s, fmt.Sprint(i), sample())
	}
	// Reopening with a shorter retention removes old files before counting devices.
	if err = s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		b, _ := json.Marshal(old)
		if err = os.WriteFile(devicePath(dir, fmt.Sprint(i)), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	s, err = Open(Options{Directory: dir, Retention: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	saveTest(t, s, "new", sample())
}

func TestFutureTimestampAndEnvelopeBound(t *testing.T) {
	s := openTest(t, t.TempDir())
	v := sample()
	v.UpdatedAt = time.Now().Add(time.Minute)
	if _, err := s.Save("a", v); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	v = sample()
	v.Data = json.RawMessage(`"` + strings.Repeat("x", (1<<20)-3) + `"`)
	if _, err := s.Save("a", v); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("envelope exceeded limit: %v", err)
	}
}
