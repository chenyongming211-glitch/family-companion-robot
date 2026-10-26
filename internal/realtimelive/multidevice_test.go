package realtimelive

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"familyrobot/internal/protocol"
	"familyrobot/internal/realtimestate"
	"github.com/gorilla/websocket"
)

const secondDeviceMAC = "aa:bb:cc:dd:ee:02"

var secondDeviceToken = strings.Repeat("z", 64)

func registryConfig(t *testing.T) DeviceConfig {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.json")
	raw, _ := json.Marshal(map[string]any{"schema_version": 1, "devices": []any{map[string]any{"mac": testDeviceMAC, "token": testDeviceToken}, map[string]any{"mac": secondDeviceMAC, "token": secondDeviceToken}}})
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	env := filepath.Join(dir, "server.env")
	if err := os.WriteFile(env, []byte("ROBOT_ALLOWED_DEVICE="+testDeviceMAC+"\nROBOT_DEVICE_TOKEN="+testDeviceToken+"\nROBOT_DEVICE_REGISTRY="+path+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadDeviceConfig(env)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
func deviceDial(endpoint, mac, token string) (*websocket.Conn, *http.Response, error) {
	return websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(endpoint, "http")+"/xiaozhi/v1/", http.Header{"Authorization": []string{"Bearer " + token}, "Device-Id": []string{mac}, "Client-Id": []string{"forged-owner"}})
}
func deviceHello(t *testing.T, c *websocket.Conn) {
	t.Helper()
	if err := c.WriteJSON(protocol.Hello{Type: "hello", Version: 1, Transport: "websocket", AudioParams: protocol.AudioParams{Format: "opus", SampleRate: 16000, Channels: 1, FrameDuration: 60}, Features: map[string]bool{"playback_progress_v1": true, "playback_drain_v1": true}}); err != nil {
		t.Fatal(err)
	}
}
func nextSession(t *testing.T, entered <-chan *Session) *Session {
	t.Helper()
	select {
	case s := <-entered:
		return s
	case <-time.After(2 * time.Second):
		t.Fatal("session not admitted")
		return nil
	}
}
func waitActive(t *testing.T, s *Server, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
		var health map[string]any
		json.Unmarshal(w.Body.Bytes(), &health)
		if health["active_sessions"] == float64(want) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("active sessions did not become %d", want)
}
func TestRegistrySecondDeviceAuthenticatesWithoutChangingPrimary(t *testing.T) {
	s := NewServer(Config{}, nil, t.TempDir(), "multi")
	entered := make(chan *Session, 1)
	s.runDevice = func(session *Session) { entered <- session; session.ws.ReadMessage(); session.cancel() }
	h := httptest.NewServer(s.DeviceHandler(registryConfig(t)))
	defer h.Close()
	c, r, err := deviceDial(h.URL, secondDeviceMAC, secondDeviceToken)
	if err != nil {
		if r != nil {
			t.Fatalf("registered second device rejected: status=%d", r.StatusCode)
		}
		t.Fatal(err)
	}
	defer c.Close()
	deviceHello(t, c)
	got := nextSession(t, entered)
	if got.rec.owner.DeviceID == "" || got.rec.owner.ClientType != "xiaozhi" {
		t.Fatal("missing authenticated ownership")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.CloseActive(ctx); err != nil {
		t.Fatal(err)
	}
}
func TestDifferentOwnersPendingTicketsCoexist(t *testing.T) {
	s := NewServer(Config{}, nil, t.TempDir(), "multi")
	entered := make(chan string, 2)
	s.run = func(c *websocket.Conn, dir string) { entered <- dir; c.ReadMessage() }
	h := httptest.NewServer(s.Handler())
	defer h.Close()
	a := ownedRequest(t, h.URL, "/devices", `{"client_type":"mac_fixture"}`, "", 201)
	b := ownedRequest(t, h.URL, "/devices", `{"client_type":"mac_fixture"}`, "", 201)
	ticket := func(d map[string]any) string {
		raw, _ := json.Marshal(map[string]any{"device_id": d["device_id"]})
		return ownedRequest(t, h.URL, "/pair", string(raw), d["credential"].(string), 200)["ticket"].(string)
	}
	ta, tb := ticket(a), ticket(b)
	for _, token := range []string{ta, tb} {
		c, r, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.URL, "http")+"/live", http.Header{"Authorization": []string{"Bearer " + token}})
		if err != nil {
			if r != nil {
				t.Fatalf("owner ticket lost: status=%d", r.StatusCode)
			}
			t.Fatal(err)
		}
		<-entered
		c.Close()
		waitActive(t, s, 0)
	}
}

func TestMultiDeviceLeasesCapacityReconnectAndShutdown(t *testing.T) {
	s := NewServer(Config{MaxSessions: 2}, nil, t.TempDir(), "multi")
	entered := make(chan *Session, 8)
	s.runDevice = func(session *Session) {
		defer session.cancel()
		entered <- session
		for {
			_, raw, err := session.ws.ReadMessage()
			if err != nil {
				return
			}
			if err = session.ws.WriteMessage(websocket.TextMessage, raw); err != nil {
				return
			}
		}
	}
	device := httptest.NewServer(s.DeviceHandler(registryConfig(t)))
	defer device.Close()
	native := httptest.NewServer(s.Handler())
	defer native.Close()
	connect := func(mac, token string) (*websocket.Conn, *Session) {
		t.Helper()
		c, r, err := deviceDial(device.URL, mac, token)
		if err != nil {
			if r != nil {
				t.Fatalf("dial status=%d", r.StatusCode)
			}
			t.Fatal(err)
		}
		deviceHello(t, c)
		return c, nextSession(t, entered)
	}
	a, sa := connect(testDeviceMAC, testDeviceToken)
	defer a.Close()
	b, sb := connect(secondDeviceMAC, secondDeviceToken)
	defer b.Close()
	waitActive(t, s, 2)
	if sa.rec.owner.DeviceID == sb.rec.owner.DeviceID || sa.rec.owner.SessionID == sb.rec.owner.SessionID {
		t.Fatal("sessions share authenticated identity")
	}
	for _, v := range []struct {
		mac, token string
		status     int
	}{{testDeviceMAC, testDeviceToken, 409}, {testDeviceMAC, secondDeviceToken, 401}, {secondDeviceMAC, testDeviceToken, 401}, {"aa:bb:cc:dd:ee:03", testDeviceToken, 403}} {
		c, r, err := deviceDial(device.URL, v.mac, v.token)
		if c != nil {
			c.Close()
		}
		if err == nil || r == nil || r.StatusCode != v.status {
			t.Fatalf("admission: want %d got %v %v", v.status, r, err)
		}
	}
	ownedRequest(t, native.URL, "/pair", "", "", 409)
	a.Close()
	waitActive(t, s, 1)
	select {
	case <-sa.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("A did not cancel")
	}
	select {
	case <-sb.ctx.Done():
		t.Fatal("A close canceled B")
	default:
	}
	if err := b.WriteJSON(map[string]any{"marker": "B-survives"}); err != nil {
		t.Fatal(err)
	}
	b.SetReadDeadline(time.Now().Add(time.Second))
	var echo map[string]any
	if err := b.ReadJSON(&echo); err != nil || echo["marker"] != "B-survives" {
		t.Fatal("B lost on A cleanup", err)
	}
	a2, sa2 := connect(testDeviceMAC, testDeviceToken)
	defer a2.Close()
	if sa2.rec.owner.DeviceID != sa.rec.owner.DeviceID || sa2.rec.owner.SessionID == sa.rec.owner.SessionID {
		t.Fatal("reconnect identity failed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.CloseActive(ctx); err != nil {
		t.Fatal(err)
	}
	waitActive(t, s, 0)
	for _, session := range []*Session{sb, sa2} {
		select {
		case <-session.ctx.Done():
		default:
			t.Fatal("shutdown missed session")
		}
	}
	ownedRequest(t, native.URL, "/pair", "", "", 409)
	c, r, err := deviceDial(device.URL, testDeviceMAC, testDeviceToken)
	if c != nil {
		c.Close()
	}
	if err == nil || r == nil || r.StatusCode != 409 {
		t.Fatal("shutdown admitted device")
	}
}
func TestNativeAndDeviceShareBoundedCapacityWithoutOwnerConfusion(t *testing.T) {
	s := NewServer(Config{MaxSessions: 2}, nil, t.TempDir(), "multi")
	entered := make(chan *Session, 4)
	s.runDevice = func(session *Session) { defer session.cancel(); entered <- session; session.ws.ReadMessage() }
	s.run = func(c *websocket.Conn, dir string) {
		session := NewSession(c, Config{}, dir, nil)
		defer session.cancel()
		entered <- session
		c.ReadMessage()
	}
	native := httptest.NewServer(s.Handler())
	defer native.Close()
	dev := httptest.NewServer(s.DeviceHandler(registryConfig(t)))
	defer dev.Close()
	a := ownedRequest(t, native.URL, "/devices", `{"client_type":"mac_fixture"}`, "", 201)
	b := ownedRequest(t, native.URL, "/devices", `{"client_type":"mac_fixture"}`, "", 201)
	ticket := func(d map[string]any) string {
		raw, _ := json.Marshal(map[string]any{"device_id": d["device_id"]})
		return ownedRequest(t, native.URL, "/pair", string(raw), d["credential"].(string), 200)["ticket"].(string)
	}
	ta, tb := ticket(a), ticket(b)
	dial := func(token string) (*websocket.Conn, *http.Response, error) {
		return websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(native.URL, "http")+"/live", http.Header{"Authorization": []string{"Bearer " + token}, "Device-Id": []string{b["device_id"].(string)}})
	}
	m, _, err := dial(ta)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	sm := nextSession(t, entered)
	if sm.rec.owner.DeviceID != a["device_id"] {
		t.Fatal("client forged owner")
	}
	// The same authenticated owner cannot hold a second slot even below capacity.
	raw, _ := json.Marshal(map[string]any{"device_id": a["device_id"]})
	ownedRequest(t, native.URL, "/pair", string(raw), a["credential"].(string), 409)
	x, _, err := deviceDial(dev.URL, testDeviceMAC, testDeviceToken)
	if err != nil {
		t.Fatal(err)
	}
	defer x.Close()
	deviceHello(t, x)
	sx := nextSession(t, entered)
	waitActive(t, s, 2)
	extra, r, err := dial(tb)
	if extra != nil {
		extra.Close()
	}
	if err == nil || r == nil || r.StatusCode != 403 {
		t.Fatal("oversubscription accepted")
	}
	x.Close()
	waitActive(t, s, 1)
	// Capacity rejection must not consume another owner's ticket.
	other, _, err := dial(tb)
	if err != nil {
		t.Fatal("ticket consumed on capacity rejection", err)
	}
	defer other.Close()
	so := nextSession(t, entered)
	if so.rec.owner.DeviceID != b["device_id"] || so.rec.owner.DeviceID == sx.rec.owner.DeviceID {
		t.Fatal("mixed owner confusion")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.CloseActive(ctx); err != nil {
		t.Fatal(err)
	}
}
func TestSessionCapacityValidationAndHealth(t *testing.T) {
	for _, n := range []int{-1, 9} {
		s := NewServer(Config{MaxSessions: n}, nil, t.TempDir(), "invalid")
		if s.InitializationError() == nil {
			t.Fatal("invalid capacity accepted")
		}
	}
	for _, v := range []struct{ configured, want int }{{0, 1}, {1, 1}, {2, 2}, {8, 8}} {
		s := NewServer(Config{MaxSessions: v.configured}, nil, t.TempDir(), "health")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
		var value map[string]any
		json.Unmarshal(w.Body.Bytes(), &value)
		if value["max_sessions"] != float64(v.want) || value["active_sessions"] != float64(0) || value["capacity_full"] != false || value["busy"] != false {
			t.Fatal("health capacity missing", value)
		}
	}
}

func singleActiveDone(s *Server) <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, lease := range s.leases {
		return lease.done
	}
	return nil
}

func TestPendingTicketLimitExpiryAndPerOwnerReplacement(t *testing.T) {
	s := NewServer(Config{MaxSessions: 2}, nil, t.TempDir(), "tickets")
	h := httptest.NewServer(s.Handler())
	defer h.Close()
	var owners []map[string]any
	pair := func(d map[string]any, status int) string {
		raw, _ := json.Marshal(map[string]any{"device_id": d["device_id"]})
		value := ownedRequest(t, h.URL, "/pair", string(raw), d["credential"].(string), status)
		token, _ := value["ticket"].(string)
		return token
	}
	var first string
	for i := 0; i < 64; i++ {
		d := ownedRequest(t, h.URL, "/devices", `{"client_type":"mac_fixture"}`, "", 201)
		owners = append(owners, d)
		token := pair(d, 200)
		if i == 0 {
			first = token
		}
	}
	extra := ownedRequest(t, h.URL, "/devices", `{"client_type":"mac_fixture"}`, "", 201)
	pair(extra, 409)
	replacement := pair(owners[0], 200)
	s.mu.Lock()
	if len(s.tickets) != 64 {
		t.Error("pending cap changed")
	}
	if _, ok := s.tickets[first]; ok {
		t.Error("same owner old ticket remains")
	}
	for ticket, binding := range s.tickets {
		if ticket != replacement {
			binding.expiry = time.Now().Add(-time.Second)
			s.tickets[ticket] = binding
		}
	}
	s.mu.Unlock()
	pair(extra, 200)
	s.mu.Lock()
	if len(s.tickets) != 2 {
		t.Error("expired tickets were not pruned")
	}
	s.mu.Unlock()
	// Anonymous legacy has a separate pending-ticket scope and counts toward cap.
	ownedRequest(t, h.URL, "/pair", "", "", 200)
	ownedRequest(t, h.URL, "/pair", "", "", 200)
	s.mu.Lock()
	if len(s.tickets) != 3 {
		t.Error("legacy replacement touched owned tickets")
	}
	s.mu.Unlock()
}
func TestLeaseCleanupCannotReleaseReplacementOrOtherDevice(t *testing.T) {
	s := NewServer(Config{MaxSessions: 2}, nil, t.TempDir(), "lease")
	s.mu.Lock()
	a := s.reserveLocked("device-a")
	b := s.reserveLocked("device-b")
	s.mu.Unlock()
	if a == nil || b == nil {
		t.Fatal("reservation failed")
	}
	s.releaseLease(a)
	s.mu.Lock()
	next := s.reserveLocked("device-a")
	s.mu.Unlock()
	s.releaseLease(a) // A late cleanup for the retired generation has no authority.
	s.mu.Lock()
	valid := s.leases["device-a"] == next && s.leases["device-b"] == b && len(s.leases) == 2
	s.mu.Unlock()
	if !valid {
		t.Fatal("old cleanup released another lease")
	}
	s.releaseLease(b)
	s.releaseLease(next)
}
func TestFailedDeviceUpgradeReleasesOnlyItsSlot(t *testing.T) {
	s := NewServer(Config{MaxSessions: 2}, nil, t.TempDir(), "upgrade")
	entered := make(chan *Session, 2)
	s.runDevice = func(session *Session) { defer session.cancel(); entered <- session; session.ws.ReadMessage() }
	h := httptest.NewServer(s.DeviceHandler(registryConfig(t)))
	defer h.Close()
	b, _, err := deviceDial(h.URL, secondDeviceMAC, secondDeviceToken)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	deviceHello(t, b)
	nextSession(t, entered)
	req, _ := http.NewRequest("GET", h.URL+"/xiaozhi/v1/", nil)
	req.Header.Set("Device-Id", testDeviceMAC)
	req.Header.Set("Authorization", "Bearer "+testDeviceToken)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 400 {
		t.Fatal("invalid upgrade status", r.StatusCode)
	}
	waitActive(t, s, 1)
	a, _, err := deviceDial(h.URL, testDeviceMAC, testDeviceToken)
	if err != nil {
		t.Fatal("failed upgrade leaked capacity", err)
	}
	defer a.Close()
	deviceHello(t, a)
	nextSession(t, entered)
	waitActive(t, s, 2)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.CloseActive(ctx); err != nil {
		t.Fatal(err)
	}
}
func TestShutdownClosesEveryConnectionBeforeBoundedCleanupWait(t *testing.T) {
	s := NewServer(Config{MaxSessions: 2}, nil, t.TempDir(), "shutdown")
	entered := make(chan *Session, 2)
	disconnected := make(chan struct{}, 2)
	cleanup := make(chan struct{})
	s.runDevice = func(session *Session) {
		defer session.cancel()
		entered <- session
		session.ws.ReadMessage()
		disconnected <- struct{}{}
		<-cleanup
	}
	h := httptest.NewServer(s.DeviceHandler(registryConfig(t)))
	defer h.Close()
	for _, v := range []struct{ mac, token string }{{testDeviceMAC, testDeviceToken}, {secondDeviceMAC, secondDeviceToken}} {
		c, _, err := deviceDial(h.URL, v.mac, v.token)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		deviceHello(t, c)
		nextSession(t, entered)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.CloseActive(ctx) }()
	for i := 0; i < 2; i++ {
		select {
		case <-disconnected:
		case <-time.After(time.Second):
			close(cleanup)
			t.Fatal("shutdown blocked before closing all connections")
		}
	}
	if err := <-done; err != context.DeadlineExceeded {
		close(cleanup)
		t.Fatal("cleanup wait was not bounded", err)
	}
	close(cleanup)
	waitActive(t, s, 0)
}
func TestServerAndSessionOwnSearchConfiguration(t *testing.T) {
	search := true
	cfg := Config{Search: &search, Tools: []any{"initial"}}
	s := NewServer(cfg, nil, t.TempDir(), "snapshot")
	search = false
	cfg.Tools[0] = "changed"
	if s.cfg.Search == nil || !*s.cfg.Search || s.cfg.Tools[0] != "initial" {
		t.Fatal("server config aliases caller")
	}
	session := NewSession(nil, s.cfg, t.TempDir(), nil)
	defer session.cancel()
	*session.cfg.Search = false
	session.cfg.Tools[0] = "session changed"
	if !*s.cfg.Search || s.cfg.Tools[0] != "initial" {
		t.Fatal("session changed shared server config")
	}
}

// This fixture uses real WebSockets and the production receipt, interrupt,
// context and recorder code. It does not call a model or claim acoustic proof.
func TestConcurrentDevicePlaybackContextCancelAndRecordsStayOwned(t *testing.T) {
	s := NewServer(Config{MaxSessions: 2}, nil, t.TempDir(), "isolation")
	type state struct {
		session *Session
		turn    *liveTurn
	}
	entered := make(chan state, 2)
	changed := make(chan *Session, 4)
	errors := make(chan error, 4)
	s.runDevice = func(session *Session) {
		defer session.cancel()
		rt := session.addTurn()
		session.mu.Lock()
		rt.turn.User = "question-" + session.rec.owner.DeviceID
		rt.turn.Generated = "answer-" + session.rec.owner.DeviceID
		rt.turn.Sent = 1440
		rt.turn.Ended = true
		wire := session.wire.(*xiaozhiWire)
		wire.blocks[rt.turn.ID] = &devicePlayback{sent: 1440, sealed: true}
		session.mu.Unlock()
		entered <- state{session, rt}
		for {
			_, raw, err := session.ws.ReadMessage()
			if err != nil {
				return
			}
			event, err := session.wire.control(raw)
			if err == nil && event != nil {
				err = session.validateScope(event)
			}
			if err == nil && event != nil {
				switch event["type"] {
				case "receipt":
					err = session.receiveReceipt(event)
				case "interrupt":
					err = session.interrupt()
				}
			}
			if err != nil {
				errors <- err
				return
			}
			changed <- session
		}
	}
	h := httptest.NewServer(s.DeviceHandler(registryConfig(t)))
	defer h.Close()
	connect := func(mac, token string) (*websocket.Conn, state) {
		t.Helper()
		c, _, err := deviceDial(h.URL, mac, token)
		if err != nil {
			t.Fatal(err)
		}
		deviceHello(t, c)
		select {
		case st := <-entered:
			return c, st
		case <-time.After(time.Second):
			t.Fatal("fixture not admitted")
			return nil, state{}
		}
	}
	a, sa := connect(testDeviceMAC, testDeviceToken)
	defer a.Close()
	b, sb := connect(secondDeviceMAC, secondDeviceToken)
	defer b.Close()
	receipt := map[string]any{"type": "playback", "session_id": sa.session.rec.owner.SessionID, "playback_id": 1, "sample_rate": 24000, "played_samples": 1440, "basis": "i2s_dma", "valid": true, "interrupted": false, "state": "finished"}
	if err := a.WriteJSON(receipt); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-changed:
		if got != sa.session {
			t.Fatal("receipt crossed sessions")
		}
	case err := <-errors:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("receipt not processed")
	}
	sa.session.mu.Lock()
	playedA := sa.turn.turn.Completed
	sa.session.mu.Unlock()
	sb.session.mu.Lock()
	playedB := sb.turn.turn.Completed
	sb.session.mu.Unlock()
	if playedA != 1440 || playedB != 0 {
		t.Fatal("playback ledgers overlap", playedA, playedB)
	}
	if err := b.WriteJSON(map[string]any{"type": "abort", "session_id": sb.session.rec.owner.SessionID}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-changed:
		if got != sb.session {
			t.Fatal("interrupt crossed sessions")
		}
	case err := <-errors:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("interrupt not processed")
	}
	select {
	case <-sb.turn.ctx.Done():
	default:
		t.Fatal("B generation not canceled")
	}
	select {
	case <-sa.turn.ctx.Done():
		t.Fatal("B interrupt canceled A generation")
	default:
	}
	for _, pair := range []struct{ own, other state }{{sa, sb}, {sb, sa}} {
		pair.own.session.mu.Lock()
		contextText := realtimestate.ContextFor([]*realtimestate.Turn{pair.own.turn.turn})
		pair.own.session.mu.Unlock()
		if !strings.Contains(contextText, pair.own.session.rec.owner.DeviceID) || strings.Contains(contextText, pair.other.session.rec.owner.DeviceID) {
			t.Fatal("context mixed terminals")
		}
		raw, err := os.ReadFile(filepath.Join(pair.own.session.directory, "events.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), pair.own.session.rec.owner.SessionID) || strings.Contains(string(raw), pair.other.session.rec.owner.SessionID) || strings.Contains(string(raw), pair.other.session.rec.owner.DeviceID) {
			t.Fatal("records mixed terminals")
		}
	}
	// A valid socket cannot submit B's playback ownership by claiming its scope.
	receipt["session_id"] = sb.session.rec.owner.SessionID
	if err := a.WriteJSON(receipt); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errors:
		if err.Error() != "wrong_session_scope" {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("forged scope accepted")
	}
	waitActive(t, s, 1)
	sb.session.mu.Lock()
	playedB = sb.turn.turn.Completed
	sb.session.mu.Unlock()
	if playedB != 0 {
		t.Fatal("forged receipt changed B")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.CloseActive(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSimultaneousMixedAdmissionNeverOversubscribes(t *testing.T) {
	for iteration := 0; iteration < 6; iteration++ {
		t.Run(fmt.Sprint(iteration), func(t *testing.T) {
			s := NewServer(Config{MaxSessions: 2}, nil, t.TempDir(), "parallel-admission")
			entered := make(chan *Session, 3)
			run := func(session *Session) { defer session.cancel(); entered <- session; session.ws.ReadMessage() }
			s.runDevice = run
			s.run = func(c *websocket.Conn, dir string) { run(NewSession(c, Config{}, dir, nil)) }
			native := httptest.NewServer(s.Handler())
			defer native.Close()
			device := httptest.NewServer(s.DeviceHandler(registryConfig(t)))
			defer device.Close()
			owner := ownedRequest(t, native.URL, "/devices", `{"client_type":"mac_fixture"}`, "", 201)
			raw, _ := json.Marshal(map[string]any{"device_id": owner["device_id"]})
			ticket := ownedRequest(t, native.URL, "/pair", string(raw), owner["credential"].(string), 200)["ticket"].(string)
			type outcome struct {
				conn   *websocket.Conn
				status int
				err    error
			}
			results := make(chan outcome, 3)
			start := make(chan struct{})
			for _, v := range []struct{ mac, token string }{{testDeviceMAC, testDeviceToken}, {secondDeviceMAC, secondDeviceToken}, {"", ticket}} {
				go func(mac, token string) {
					<-start
					var c *websocket.Conn
					var response *http.Response
					var err error
					if mac == "" {
						c, response, err = websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(native.URL, "http")+"/live", http.Header{"Authorization": []string{"Bearer " + token}})
					} else {
						c, response, err = deviceDial(device.URL, mac, token)
						if err == nil {
							err = c.WriteJSON(protocol.Hello{Type: "hello", Version: 1, Transport: "websocket", AudioParams: protocol.AudioParams{Format: "opus", SampleRate: 16000, Channels: 1, FrameDuration: 60}, Features: map[string]bool{"playback_progress_v1": true, "playback_drain_v1": true}})
						}
					}
					status := 0
					if response != nil {
						status = response.StatusCode
					}
					results <- outcome{c, status, err}
				}(v.mac, v.token)
			}
			close(start)
			accepted, rejected := 0, 0
			var clients []*websocket.Conn
			for i := 0; i < 3; i++ {
				select {
				case result := <-results:
					if result.err == nil {
						accepted++
						clients = append(clients, result.conn)
					} else {
						if result.conn != nil {
							result.conn.Close()
						}
						if result.status != 403 && result.status != 409 {
							t.Fatal("unexpected admission failure", result.status, result.err)
						}
						rejected++
					}
				case <-time.After(2 * time.Second):
					t.Fatal("parallel admission stalled")
				}
			}
			defer func() {
				for _, client := range clients {
					client.Close()
				}
			}()
			if accepted != 2 || rejected != 1 {
				t.Fatal("capacity exceeded", accepted, rejected)
			}
			waitActive(t, s, 2)
			for i := 0; i < 2; i++ {
				nextSession(t, entered)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := s.CloseActive(ctx); err != nil {
				t.Fatal(err)
			}
			waitActive(t, s, 0)
		})
	}
}
