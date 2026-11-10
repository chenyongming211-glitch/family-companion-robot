package realtimelive

import (
	"context"
	"encoding/json"
	"errors"
	"familyrobot/internal/recordstore"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func clearClaim() MemoryClearClaim {
	return MemoryClearClaim{DeviceID: memoryDeviceA, ActorID: "alice", FamilyID: "family-a", BindingID: "binding-a", BindingVersion: 1, Deadline: time.Now().Add(time.Minute)}
}
func allowClear(ctx context.Context, c MemoryClearClaim, fn func() error) error { return fn() }

func TestMemoryClearBusyGateAndShutdown(t *testing.T) {
	s := memoryServer(t, t.TempDir())
	claim := clearClaim()
	s.mu.Lock()
	lease := s.reserveLocked(memoryDeviceA)
	s.mu.Unlock()
	called := false
	guard := func(ctx context.Context, c MemoryClearClaim, fn func() error) error { called = true; return fn() }
	if e := s.ClearDeviceMemory(context.Background(), claim, guard); !errors.Is(e, ErrMemoryBusy) || called {
		t.Fatalf("lease clear %v guard=%v", e, called)
	}
	s.releaseLease(lease)
	admitted, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- s.ClearDeviceMemory(context.Background(), claim, func(ctx context.Context, c MemoryClearClaim, fn func() error) error {
			close(admitted)
			<-release
			return fn()
		})
	}()
	<-admitted
	if e := s.ClearDeviceMemory(context.Background(), claim, allowClear); !errors.Is(e, ErrMemoryBusy) {
		t.Fatal("double clear", e)
	}
	s.mu.Lock()
	blocked := s.reserveLocked(memoryDeviceA)
	other := s.reserveLocked(memoryDeviceB)
	s.mu.Unlock()
	if blocked != nil || other == nil {
		t.Fatal("incorrect per-device gate")
	}
	s.releaseLease(other)
	shutdown := make(chan error, 1)
	go func() { shutdown <- s.CloseActive(context.Background()) }()
	select {
	case e := <-shutdown:
		t.Fatal("shutdown before clear", e)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if e := <-shutdown; e != nil {
		t.Fatal(e)
	}
	if e := s.ClearDeviceMemory(context.Background(), claim, allowClear); e == nil {
		t.Fatal("clear after shutdown")
	}
}

type settlingStore struct {
	receipt          chan error
	admitted         chan struct{}
	loads            int
	saveErr, loadErr error
	data             json.RawMessage
}

func (s *settlingStore) Save(_ string, v recordstore.Snapshot) (<-chan error, error) {
	s.data = v.Data
	close(s.admitted)
	return s.receipt, s.saveErr
}
func (s *settlingStore) Load(ctx context.Context, _ string) (recordstore.Snapshot, error) {
	s.loads++
	if ctx.Err() != nil {
		return recordstore.Snapshot{}, ctx.Err()
	}
	return recordstore.Snapshot{Version: 1, Data: s.data}, s.loadErr
}
func TestMemoryClearWaitsForActualReceiptAfterCancellation(t *testing.T) {
	s := &settlingStore{receipt: make(chan error, 1), admitted: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- clearStoredMemory(ctx, s, memoryDeviceA) }()
	<-s.admitted
	cancel()
	select {
	case e := <-done:
		t.Fatal("returned before receipt", e)
	case <-time.After(2100 * time.Millisecond):
	}
	s.receipt <- nil
	if e := <-done; e != nil || s.loads != 1 || string(s.data) != `{"turns":[]}` {
		t.Fatalf("receipt=%v loads=%d data=%s", e, s.loads, s.data)
	}
}
func TestMemoryClearFailureReceiptsNeverConfirm(t *testing.T) {
	for _, kind := range []string{"admission", "receipt", "load"} {
		t.Run(kind, func(t *testing.T) {
			fail := errors.New("synthetic failure")
			s := &settlingStore{receipt: make(chan error, 1), admitted: make(chan struct{})}
			switch kind {
			case "admission":
				s.saveErr = fail
			case "receipt":
				s.receipt <- fail
			case "load":
				s.receipt <- nil
				s.loadErr = fail
			}
			if e := clearStoredMemory(context.Background(), s, memoryDeviceA); e == nil {
				t.Fatal("failure confirmed")
			}
		})
	}
}
func TestMemoryControlIsPrivateAndStrict(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("private socket requires root")
	}
	s := memoryServer(t, t.TempDir())
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/memory/clear", nil))
	if w.Code != 404 {
		t.Fatal("public mutation route", w.Code)
	}
	parent := t.TempDir()
	os.Chmod(parent, 0700)
	path := filepath.Join(parent, "control.sock")
	ln, e := ListenMemoryControl(path)
	if e != nil {
		t.Fatal(e)
	}
	defer ln.Close()
	if _, e = ListenMemoryControl(path); e == nil {
		t.Fatal("replaced live listener")
	}
	st, _ := os.Lstat(path)
	if st.Mode().Perm() != 0600 {
		t.Fatal(st.Mode())
	}
	h := s.MemoryControlHandler(allowClear)
	raw, _ := json.Marshal(clearClaim())
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/memory/clear", strings.NewReader(string(raw))))
	if w.Code != 200 {
		t.Fatalf("clear %d %s", w.Code, w.Body)
	}
	for _, raw := range []string{`{}`, `{"deviceId":"x","deviceId":"y"}`, `{"unknown":true}`, `null`, strings.Repeat("x", 4097)} {
		w = httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/memory/clear", strings.NewReader(raw)))
		if w.Code != 400 {
			t.Fatalf("invalid %q: %d", raw, w.Code)
		}
	}
}

func TestMemoryControlRejectsUnsafeSocketPaths(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root")
	}
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	path := filepath.Join(dir, "control.sock")
	if e := os.WriteFile(path, []byte("synthetic"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := ListenMemoryControl(path); e == nil {
		t.Fatal("accepted regular file")
	}
	os.Remove(path)
	target := filepath.Join(dir, "target")
	os.WriteFile(target, []byte("synthetic"), 0600)
	os.Symlink(target, path)
	if _, e := ListenMemoryControl(path); e == nil {
		t.Fatal("accepted symlink")
	}
	os.Remove(path)
	os.Chmod(dir, 0755)
	if _, e := ListenMemoryControl(path); e == nil {
		t.Fatal("accepted shared parent")
	}
	os.Chmod(dir, 0700)
	linked := filepath.Join(t.TempDir(), "parent")
	os.Symlink(dir, linked)
	if _, e := ListenMemoryControl(filepath.Join(linked, "control.sock")); e == nil {
		t.Fatal("accepted symlink parent")
	}
	ln, e := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if e != nil {
		t.Fatal(e)
	}
	ln.SetUnlinkOnClose(false)
	os.Chmod(path, 0600)
	ln.Close()
	fresh, e := ListenMemoryControl(path)
	if e != nil {
		t.Fatal("stale private socket", e)
	}
	fresh.Close()
	if _, e := ListenMemoryControl("relative/control.sock"); e == nil {
		t.Fatal("accepted relative socket")
	}
}

func TestMemoryClearFIFOAndOtherDeviceIsolation(t *testing.T) {
	s := memoryServer(t, t.TempDir())
	snapshot := recordstore.Snapshot{Version: 1, UpdatedAt: time.Now().UTC(), Data: json.RawMessage(`{"turns":[{"synthetic":"old"}]}`)}
	first, e := s.memory.Save(memoryDeviceA, snapshot)
	if e != nil {
		t.Fatal(e)
	}
	other, e := s.memory.Save(memoryDeviceB, snapshot)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ClearDeviceMemory(context.Background(), clearClaim(), allowClear); e != nil {
		t.Fatal(e)
	}
	if e = <-first; e != nil {
		t.Fatal(e)
	}
	if e = <-other; e != nil {
		t.Fatal(e)
	}
	cleared, e := s.memory.Load(context.Background(), memoryDeviceA)
	if e != nil || string(cleared.Data) != `{"turns":[]}` {
		t.Fatal("prior save resurrected", e, string(cleared.Data))
	}
	kept, e := s.memory.Load(context.Background(), memoryDeviceB)
	if e != nil || string(kept.Data) != string(snapshot.Data) {
		t.Fatal("other device changed", e)
	}
}
