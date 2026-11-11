package realtimelive

import (
	"bytes"
	"errors"
	"familyrobot/internal/protocol"
	"fmt"
	"github.com/gorilla/websocket"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// A subprocess observes the production logger without mutating global log state.
func TestConnectionTraceDefaultLoggerAuthRejection(t *testing.T) {
	if os.Getenv("ROBOT_CONNECTION_TRACE_TEST_CHILD") == "1" {
		s := NewServer(Config{}, nil, t.TempDir(), "trace-test")
		r := httptest.NewRequest("GET", "/xiaozhi/v1/", nil)
		r.Header.Set("Device-Id", testDeviceMAC)
		r.Header.Set("Authorization", "Bearer rejected-synthetic-token")
		r.Header.Set("X-Robot-Connection-Id", "c1-0123456789abcdef-00000001")
		w := httptest.NewRecorder()
		s.DeviceHandler(DeviceConfig{Token: testDeviceToken, AllowedDevice: testDeviceMAC}).ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("status=%d", w.Code)
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestConnectionTraceDefaultLoggerAuthRejection$")
	cmd.Env = append(os.Environ(), "ROBOT_CONNECTION_TRACE_TEST_CHILD=1")
	raw, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child: %v", err)
	}
	out := string(raw)
	if !strings.Contains(out, "connection_id=c1-0123456789abcdef-00000001 stage=auth status=401") {
		t.Fatal("missing bounded auth rejection trace")
	}
	if strings.Contains(out, "rejected-synthetic-token") {
		t.Fatal("credential leaked")
	}
}

func TestConnectionTraceIDsAndHTTPBoundaries(t *testing.T) {
	ids := []struct {
		name   string
		values []string
		logged bool
	}{
		{"valid", []string{"c1-0123456789abcdef-00000001"}, true},
		{"missing", nil, false}, {"uppercase", []string{"c1-0123456789ABCDEF-00000001"}, false},
		{"short", []string{"c1-0123456789abcde-00000001"}, false},
		{"long", []string{"c1-0123456789abcdef-000000010"}, false},
		{"whitespace", []string{" c1-0123456789abcdef-00000001"}, false},
		{"injection", []string{"c1-0123456789abcdef-00000001\nsecret-header"}, false},
		{"duplicate", []string{"c1-0123456789abcdef-00000001", "c1-0123456789abcdef-00000002"}, false},
	}
	for _, id := range ids {
		t.Run(id.name, func(t *testing.T) {
			for _, tc := range []struct {
				name, token, mac, version, origin, stage string
				status                                   int
			}{
				{"auth_rejected", "private-token", testDeviceMAC, "1", "", "auth", 401},
				{"device_rejected", testDeviceToken, "invalid-device", "1", "", "auth", 403},
				{"origin_rejected", testDeviceToken, testDeviceMAC, "1", "https://private-origin", "auth", 403},
				{"version_rejected", testDeviceToken, testDeviceMAC, "injected-version", "", "version", 400},
				{"upgrade_rejected", testDeviceToken, testDeviceMAC, "1", "", "upgrade", 400},
			} {
				t.Run(tc.name, func(t *testing.T) {
					s := NewServer(Config{}, nil, t.TempDir(), "trace")
					logs := &traceBuffer{}
					s.connectionLog = log.New(logs, "", 0)
					r := httptest.NewRequest("GET", "/xiaozhi/v1/?secret-query", strings.NewReader("private-body"))
					r.Header["X-Robot-Connection-Id"] = id.values
					r.Header.Set("Device-Id", tc.mac)
					r.Header.Set("Authorization", "Bearer "+tc.token)
					r.Header.Set("Protocol-Version", tc.version)
					r.Header.Set("Origin", tc.origin)
					r.Header.Set("Client-Id", "private-client")
					w := httptest.NewRecorder()
					s.DeviceHandler(DeviceConfig{Token: testDeviceToken, AllowedDevice: testDeviceMAC}).ServeHTTP(w, r)
					if w.Code != tc.status {
						t.Fatalf("status=%d want=%d", w.Code, tc.status)
					}
					waitActive(t, s, 0)
					out := logs.String()
					if id.logged {
						assertTraceLines(t, out)
						if !strings.Contains(out, fmt.Sprintf("stage=%s status=%d", tc.stage, tc.status)) {
							t.Fatalf("missing terminal stage: %s", out)
						}
					} else if out != "" {
						t.Fatal("invalid/missing ID logged")
					}
					for _, secret := range []string{testDeviceToken, "private-token", "private-body", "secret-query", "private-origin", "injected-version", "private-client", "secret-header"} {
						if strings.Contains(out, secret) {
							t.Fatal("client input leaked")
						}
					}
					if tc.stage == "upgrade" && (w.Header().Get("Sec-Websocket-Version") != "13" || w.Body.String() != "Bad Request\n") {
						t.Fatal("upgrade rejection changed")
					}
				})
			}
		})
	}
}

func TestConnectionTraceWebsocketReconnectAndAdmission(t *testing.T) {
	s := NewServer(Config{}, nil, t.TempDir(), "trace")
	logs := &traceBuffer{}
	s.connectionLog = log.New(logs, "", 0)
	entered := make(chan *Session, 4)
	s.runDevice = func(session *Session) { defer session.cancel(); entered <- session; session.ws.ReadMessage() }
	h := httptest.NewServer(s.DeviceHandler(DeviceConfig{Token: testDeviceToken, AllowedDevice: testDeviceMAC}))
	defer h.Close()
	var sessions []string
	for i, id := range []string{"c1-0123456789abcdef-00000001", "c1-0123456789abcdef-00000002", ""} {
		c, r, err := traceDial(h.URL, id)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		if r.StatusCode != 101 {
			t.Fatal("upgrade status")
		}
		deviceHello(t, c)
		session := nextSession(t, entered)
		sessions = append(sessions, session.rec.owner.SessionID)
		if i == 0 {
			busy, resp, err := traceDial(h.URL, "c1-0123456789abcdef-00000003")
			if busy != nil {
				busy.Close()
			}
			if err == nil || resp == nil || resp.StatusCode != 409 {
				t.Fatal("admission changed")
			}
			resp.Body.Close()
		}
		c.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(1000, "private-close-reason"), time.Now().Add(time.Second))
		c.Close()
		waitActive(t, s, 0)
	}
	if sessions[0] == sessions[1] || sessions[1] == sessions[2] {
		t.Fatal("session ID reused")
	}
	out := logs.String()
	assertTraceLines(t, out)
	for i, id := range []string{"c1-0123456789abcdef-00000001", "c1-0123456789abcdef-00000002"} {
		for _, stage := range []string{"entry status=0", "auth status=200", "version status=200", "admission status=200", "upgrade status=101", "hello status=200", "session status=200", "disconnect status=0"} {
			if !strings.Contains(out, "connection_id="+id+" stage="+stage) {
				t.Fatalf("missing %s for attempt %d", stage, i)
			}
		}
		if !strings.Contains(out, "session_id="+sessions[i]) {
			t.Fatal("server session ID missing")
		}
	}
	if strings.Contains(out, sessions[2]) || strings.Contains(out, "private-close-reason") {
		t.Fatal("old client trace or close text logged")
	}
	if !strings.Contains(out, "connection_id=c1-0123456789abcdef-00000003 stage=admission status=409") {
		t.Fatal("busy log missing")
	}
}

func TestConnectionTraceHelloFailuresReleaseLease(t *testing.T) {
	for _, tc := range []struct {
		name    string
		kind    int
		payload string
		status  int
		close   bool
	}{
		{"malformed", websocket.TextMessage, `{"type":"hello","private":"secret-hello"}`, 1008, false},
		{"binary", websocket.BinaryMessage, "secret-binary", 1003, false},
		{"missing_drain", websocket.TextMessage, `{"type":"hello","version":1,"transport":"websocket","audio_params":{"format":"opus","sample_rate":16000,"channels":1,"frame_duration":60},"features":{"playback_progress_v1":true}}`, 1008, false},
		{"closed", 0, "", 1000, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewServer(Config{}, nil, t.TempDir(), "trace")
			logs := &traceBuffer{}
			s.connectionLog = log.New(logs, "", 0)
			s.runDevice = func(*Session) { t.Error("invalid hello reached session") }
			h := httptest.NewServer(s.DeviceHandler(DeviceConfig{Token: testDeviceToken, AllowedDevice: testDeviceMAC}))
			defer h.Close()
			c, _, err := traceDial(h.URL, "c1-0123456789abcdef-00000001")
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if tc.close {
				c.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(1000, "secret-close"), time.Now().Add(time.Second))
			} else {
				if err := c.WriteMessage(tc.kind, []byte(tc.payload)); err != nil {
					t.Fatal(err)
				}
			}
			c.SetReadDeadline(time.Now().Add(time.Second))
			c.ReadMessage()
			c.Close()
			waitActive(t, s, 0)
			out := logs.String()
			assertTraceLines(t, out)
			if !strings.Contains(out, fmt.Sprintf("stage=hello status=%d", tc.status)) {
				t.Fatalf("wrong hello code: %s", out)
			}
			if strings.Contains(out, "secret") || strings.Contains(out, "session_id=") {
				t.Fatal("hello leaked or session started")
			}
		})
	}
}

func TestConnectionTraceReadStatus(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want int
	}{
		{&websocket.CloseError{Code: 1000, Text: "secret"}, 1000},
		{&websocket.CloseError{Code: 1006, Text: "secret"}, 1006},
		{os.ErrDeadlineExceeded, 408}, {errors.New("secret"), 1011},
	} {
		if got := connectionReadStatus(tc.err); got != tc.want {
			t.Fatalf("status=%d want=%d", got, tc.want)
		}
	}
}

type traceBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *traceBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(p)
}
func (b *traceBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.Buffer.String() }
func traceDial(url, id string) (*websocket.Conn, *http.Response, error) {
	headers := http.Header{"Authorization": []string{"Bearer " + testDeviceToken}, "Device-Id": []string{testDeviceMAC}, "Client-Id": []string{"private-client"}}
	if id != "" {
		headers.Set("X-Robot-Connection-Id", id)
	}
	return websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(url, "http")+"/xiaozhi/v1/", headers)
}
func assertTraceLines(t *testing.T, out string) {
	t.Helper()
	re := regexp.MustCompile(`^robot_connection connection_id=c1-[0-9a-f]{16}-[0-9a-f]{8} stage=(entry|auth|version|admission|upgrade|hello|session|disconnect) status=[0-9]+ elapsed_ms=[0-9]+( session_id=[0-9a-f]{32})?$`)
	last := map[string]int64{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if !re.MatchString(line) {
			t.Fatalf("unexpected trace shape: %s", line)
		}
		fields := strings.Fields(line)
		elapsed, err := strconv.ParseInt(strings.TrimPrefix(fields[4], "elapsed_ms="), 10, 64)
		if err != nil || elapsed < last[fields[1]] {
			t.Fatal("invalid/non-monotonic elapsed time")
		}
		last[fields[1]] = elapsed
	}
}

// Exercise the production Session.Run hello reply using a loopback protocol
// fixture. It accepts setup only: no external provider, input audio or model call.
func TestConnectionTraceSessionRunHello(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		c.WriteJSON(map[string]any{"type": "session.created"})
		for i := 0; i < 2; i++ {
			var event map[string]any
			if c.ReadJSON(&event) != nil {
				return
			}
			if event["type"] != "session.update" {
				t.Error("unexpected provider operation")
				return
			}
			c.WriteJSON(map[string]any{"type": "session.updated", "session": map[string]any{"enable_search": false}})
		}
		for {
			var event map[string]any
			if c.ReadJSON(&event) != nil {
				return
			}
			t.Error("unexpected provider operation")
		}
	}))
	defer provider.Close()
	s := NewServer(Config{URL: "ws" + strings.TrimPrefix(provider.URL, "http"), Synthetic: true}, nil, t.TempDir(), "trace")
	logs := &traceBuffer{}
	s.connectionLog = log.New(logs, "", 0)
	h := httptest.NewServer(s.DeviceHandler(DeviceConfig{Token: testDeviceToken, AllowedDevice: testDeviceMAC}))
	defer h.Close()
	c, _, err := traceDial(h.URL, "c1-0123456789abcdef-00000001")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	deviceHello(t, c)
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	var hello protocol.Hello
	if err := c.ReadJSON(&hello); err != nil {
		t.Fatal(err)
	}
	if hello.Type != "hello" || len(hello.SessionID) != 32 || !hello.Features["playback_drain_v1"] {
		t.Fatal("invalid server hello")
	}
	c.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(1000, "private-close"), time.Now().Add(time.Second))
	c.Close()
	waitActive(t, s, 0)
	out := logs.String()
	assertTraceLines(t, out)
	if !strings.Contains(out, "stage=disconnect status=0") || !strings.Contains(out, "session_id="+hello.SessionID) {
		t.Fatal("actual session lifecycle missing")
	}
}

func TestConnectionTraceHelloTimeout(t *testing.T) {
	s := NewServer(Config{}, nil, t.TempDir(), "trace")
	logs := &traceBuffer{}
	s.connectionLog = log.New(logs, "", 0)
	h := httptest.NewServer(s.DeviceHandler(DeviceConfig{Token: testDeviceToken, AllowedDevice: testDeviceMAC}))
	defer h.Close()
	c, _, err := traceDial(h.URL, "c1-0123456789abcdef-00000001")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(10 * time.Second))
	c.ReadMessage()
	c.Close()
	waitActive(t, s, 0)
	out := logs.String()
	assertTraceLines(t, out)
	if !strings.Contains(out, "stage=hello status=408") {
		t.Fatalf("timeout not mapped: %s", out)
	}
	fields := strings.Fields(strings.TrimSpace(out[strings.LastIndex(out, "robot_connection"):]))
	ms, err := strconv.ParseInt(strings.TrimPrefix(fields[4], "elapsed_ms="), 10, 64)
	if err != nil || ms < 7900 || ms > 10000 {
		t.Fatalf("unexpected timeout elapsed=%d", ms)
	}
}
