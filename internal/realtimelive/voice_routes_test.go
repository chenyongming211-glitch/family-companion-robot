package realtimelive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"familyrobot/internal/realtimestate"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestVoiceRouteProductFactsPromptAndReady(t *testing.T) {
	product, err := realtimestate.LoadProduct("../../config/product-identity.json")
	if err != nil {
		t.Fatal(err)
	}
	// Only the common listener opens this isolated provider; no generation occurs.
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		if err := conn.WriteJSON(map[string]any{"type": "session.created"}); err != nil {
			return
		}
		for {
			var event map[string]any
			if err := conn.ReadJSON(&event); err != nil {
				return
			}
			if event["type"] == "session.update" {
				if err := conn.WriteJSON(map[string]any{"type": "session.updated", "session": map[string]any{"enable_search": false}}); err != nil {
					return
				}
			}
		}
	}))
	defer provider.Close()
	for _, route := range []string{"A", "B", "C"} {
		t.Run(route, func(t *testing.T) {
			search := true
			cfg := Config{Model: "qwen3-omni-flash-realtime", Voice: "Tina", Search: &search, URL: "ws" + strings.TrimPrefix(provider.URL, "http"), Synthetic: true, VoiceRouteForDevice: func(string) (string, error) { return route, nil }}
			server := NewServer(cfg, product, t.TempDir(), "route-facts")
			dir, err := server.sessionDirectory(Ownership{DeviceID: memoryDeviceA, ClientType: "fixture"})
			if err != nil {
				t.Fatal(err)
			}
			ws, client := testSocket(t)
			session := server.newSession(ws, dir)
			defer session.cancel()
			originalFacts := cfg.facts()
			listenerFacts := session.listener.cfg.facts()
			if listenerFacts["model"] != cfg.Model || listenerFacts["QWEN_VOICE"] != cfg.Voice || listenerFacts["enable_search"] != false {
				t.Error("common listener lost its native configuration")
			}
			facts := session.sessionFacts()
			model, voice, enabled := cfg.Model, cfg.Voice, true
			if route != "C" {
				voice, enabled = "longanyang", false
			}
			if route == "B" {
				model = "qwen-plus"
			}
			if facts["model"] != model || facts["QWEN_VOICE"] != voice || facts["enable_search"] != enabled {
				t.Errorf("route presentation mismatch: %v", facts)
			}
			prompt, err := product.InstructionsFor(facts, `{"recent_turns":[]}`, true, time.Unix(0, 0))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(prompt, "底层语音模型："+model+"；") || (route == "B" && strings.Contains(prompt, cfg.Model)) {
				t.Error("product prompt has wrong route model identity")
			}
			if strings.Contains(prompt, "当前可使用内置联网搜索") != enabled {
				t.Error("product prompt has wrong search instruction")
			}
			snapshot, err := product.Snapshot(facts, `{"recent_turns":[]}`, true)
			if err != nil {
				t.Fatal(err)
			}
			status := "not_connected"
			if enabled {
				status = "available"
			}
			if object(object(snapshot["capabilities"])["live_information"])["status"] != status {
				t.Error("product snapshot advertises wrong search capability")
			}
			if route == "C" && !reflect.DeepEqual(facts, originalFacts) {
				t.Error("C product facts changed from native baseline")
			}
			done := make(chan error, 1)
			go func() { done <- session.Run() }()
			t.Cleanup(func() {
				client.Close()
				if err := <-done; err != nil {
					t.Error("session ended unexpectedly", err)
				}
			})
			client.SetReadDeadline(time.Now().Add(3 * time.Second))
			var ready map[string]any
			if err := client.ReadJSON(&ready); err != nil {
				t.Fatal(err)
			}
			if ready["type"] != "ready" || ready["model"] != model || ready["voice"] != voice {
				t.Errorf("ready misrepresents route: %v", ready)
			}
			if !reflect.DeepEqual(server.cfg.facts(), originalFacts) || !reflect.DeepEqual(session.cfg.facts(), originalFacts) || !reflect.DeepEqual(session.listener.cfg.facts(), listenerFacts) || !search {
				t.Error("route presentation mutated native server/session/listener configuration")
			}
		})
	}
}

func TestVoiceRouteSelectionFixedAndReadFailure(t *testing.T) {
	selected := "A"
	s := NewServer(Config{VoiceRouteForDevice: func(string) (string, error) { return selected, nil }}, nil, t.TempDir(), "routes")
	dir, err := s.sessionDirectory(Ownership{DeviceID: memoryDeviceA, ClientType: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	a := s.newSession(nil, dir)
	defer a.cancel()
	selected = "B"
	b := s.newSession(nil, dir)
	defer b.cancel()
	if a.voiceRoute != "A" || b.voiceRoute != "B" {
		t.Fatalf("routes %s %s", a.voiceRoute, b.voiceRoute)
	}
	selected = "invalid"
	bad := s.newSession(nil, dir)
	defer bad.cancel()
	if bad.ownerErr == nil {
		t.Fatal("invalid route accepted")
	}
	s.cfg.VoiceRouteForDevice = func(string) (string, error) { return "", errors.New("broken") }
	bad = s.newSession(nil, dir)
	defer bad.cancel()
	if bad.ownerErr == nil {
		t.Fatal("read failure fell back")
	}
	for _, route := range []string{"A", "B", "C", ""} {
		if _, err := parseVoiceRoute(route); err != nil {
			t.Fatal(route, err)
		}
	}
	for _, route := range []string{"a", " C", "D"} {
		if _, err := parseVoiceRoute(route); err == nil {
			t.Fatal(route)
		}
	}
}
func routeClaim() VoiceRouteClaim {
	return VoiceRouteClaim{DeviceID: memoryDeviceA, ActorID: "admin", FamilyID: "family", BindingID: "binding", BindingVersion: 1, Route: "B", Version: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Deadline: time.Now().Add(time.Minute)}
}
func routeGuard(ctx context.Context, c VoiceRouteClaim, write bool, fn func() error) error {
	return fn()
}
func TestVoiceRouteConnectingDisconnectAndGuard(t *testing.T) {
	s := NewServer(Config{MaxSessions: 2}, nil, t.TempDir(), "routes")
	s.mu.Lock()
	old := s.reserveLocked(memoryDeviceA)
	other := s.reserveLocked(memoryDeviceB)
	s.mu.Unlock()
	claim := routeClaim()
	state, err := s.voiceRouteStatus(context.Background(), claim, routeGuard)
	if err != nil || state.Connection != "connecting" || state.ActiveRoute != nil {
		t.Fatal(state, err)
	}
	denied := func(context.Context, VoiceRouteClaim, bool, func() error) error { return ErrVoiceRouteForbidden }
	if _, err = s.disconnectVoiceRoute(context.Background(), claim, denied); !errors.Is(err, ErrVoiceRouteForbidden) {
		t.Fatal(err)
	}
	s.mu.Lock()
	revoked := old.revoked
	s.mu.Unlock()
	if revoked {
		t.Fatal("guard reject changed lease")
	}
	started := make(chan struct{})
	guard := func(ctx context.Context, c VoiceRouteClaim, b bool, fn func() error) error {
		err := fn()
		close(started)
		return err
	}
	done := make(chan error, 1)
	go func() { _, err := s.disconnectVoiceRoute(context.Background(), claim, guard); done <- err }()
	<-started
	s.mu.Lock()
	next := s.reserveLocked(memoryDeviceA)
	stillOther := s.leases[memoryDeviceB] == other
	s.mu.Unlock()
	if next != nil || !stillOther || s.attachLease(old, nil) {
		t.Fatal("disconnect admission failed")
	}
	s.releaseLease(old)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	next = s.reserveLocked(memoryDeviceA)
	s.mu.Unlock()
	if next == nil {
		t.Fatal("reconnect blocked")
	}
	s.releaseLease(old)
	s.mu.Lock()
	same := s.leases[memoryDeviceA] == next
	s.mu.Unlock()
	if !same {
		t.Fatal("late cleanup removed new lease")
	}
	s.releaseLease(next)
	s.releaseLease(other)
}
func TestVoiceRouteDisconnectCancellation(t *testing.T) {
	s := NewServer(Config{}, nil, t.TempDir(), "routes")
	s.mu.Lock()
	old := s.reserveLocked(memoryDeviceA)
	s.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.disconnectVoiceRoute(ctx, routeClaim(), routeGuard); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	s.mu.Lock()
	revoked := old.revoked
	s.mu.Unlock()
	if revoked {
		t.Fatal("cancelled request mutated")
	}
	s.releaseLease(old)
}
func TestVoiceRouteControlPrivateAndStrict(t *testing.T) {
	s := NewServer(Config{}, nil, t.TempDir(), "routes")
	claim := routeClaim()
	body, _ := json.Marshal(claim)
	calls := 0
	guard := func(ctx context.Context, c VoiceRouteClaim, b bool, fn func() error) error { calls++; return fn() }
	h := s.VoiceRouteControlHandler(guard)
	for _, path := range []string{"/voice-route/status", "/voice-route/disconnect"} {
		r := httptest.NewRequest("POST", path, bytes.NewReader(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		var state VoiceRouteState
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &state) != nil || state.DeviceID != claim.DeviceID || state.Connection != "idle" {
			t.Fatal(w.Code, w.Body.String())
		}
		public := httptest.NewRecorder()
		s.Handler().ServeHTTP(public, httptest.NewRequest("POST", path, bytes.NewReader(body)))
		if public.Code != 404 {
			t.Fatal(public.Code)
		}
	}
	for _, bad := range []string{string(body[:len(body)-1]) + `,"route":"A"}`, strings.Replace(string(body), `"route"`, `"Route"`, 1), strings.Replace(string(body), `"route":"B"`, `"route":"b"`, 1)} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/voice-route/disconnect", strings.NewReader(bad)))
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if calls != 2 {
		t.Fatal("invalid input invoked guard", calls)
	}
}

func TestVoiceRouteDisconnectRealSocketAndReconnect(t *testing.T) {
	var selected atomic.Value
	selected.Store("A")
	s := NewServer(Config{MaxSessions: 2, VoiceRouteForDevice: func(string) (string, error) { return selected.Load().(string), nil }}, nil, t.TempDir(), "routes")
	entered := make(chan *Session, 2)
	s.runDevice = func(session *Session) { entered <- session; _, _, _ = session.ws.ReadMessage(); session.cancel() }
	h := httptest.NewServer(s.DeviceHandler(registryConfig(t)))
	defer h.Close()
	a, _, err := deviceDial(h.URL, testDeviceMAC, testDeviceToken)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	deviceHello(t, a)
	first := nextSession(t, entered)
	b, _, err := deviceDial(h.URL, secondDeviceMAC, secondDeviceToken)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	deviceHello(t, b)
	other := nextSession(t, entered)
	claim := routeClaim()
	claim.DeviceID = first.rec.owner.DeviceID
	state, err := s.voiceRouteStatus(context.Background(), claim, routeGuard)
	if err != nil || state.ActiveRoute == nil || *state.ActiveRoute != "A" || state.Connection != "active" {
		t.Fatal(state, err)
	}
	selected.Store("B")
	if first.voiceRoute != "A" {
		t.Fatal("active route changed")
	}
	state, err = s.disconnectVoiceRoute(context.Background(), claim, routeGuard)
	if err != nil || state.Connection != "idle" {
		t.Fatal(state, err)
	}
	_ = a.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err = a.ReadMessage(); err == nil {
		t.Fatal("target socket alive")
	}
	s.mu.Lock()
	otherLive := s.leases[other.rec.owner.DeviceID] != nil
	s.mu.Unlock()
	if !otherLive {
		t.Fatal("other device disconnected")
	}
	next, _, err := deviceDial(h.URL, testDeviceMAC, testDeviceToken)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	deviceHello(t, next)
	second := nextSession(t, entered)
	if second.voiceRoute != "B" {
		t.Fatal("new connection reused old route", second.voiceRoute)
	}
	_ = next.Close()
	_ = b.Close()
	waitActive(t, s, 0)
}
func TestVoiceRouteBlockedResolverDisconnectNoLockDeadlock(t *testing.T) {
	reached := make(chan struct{})
	release := make(chan struct{})
	s := NewServer(Config{VoiceRouteForDevice: func(string) (string, error) { close(reached); <-release; return "A", nil }}, nil, t.TempDir(), "routes")
	s.runDevice = func(session *Session) {
		if session.ownerErr == nil {
			t.Error("revoked initialization ran")
		}
		session.cancel()
	}
	h := httptest.NewServer(s.DeviceHandler(DeviceConfig{Token: testDeviceToken, AllowedDevice: testDeviceMAC}))
	defer h.Close()
	conn, _, err := deviceDial(h.URL, testDeviceMAC, testDeviceToken)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	deviceHello(t, conn)
	<-reached
	device, err := s.devices.BindXiaozhi(testDeviceMAC, testDeviceToken)
	if err != nil {
		t.Fatal(err)
	}
	claim := routeClaim()
	claim.DeviceID = device.ID
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	guard := func(ctx context.Context, c VoiceRouteClaim, b bool, fn func() error) error {
		err := fn()
		close(release)
		return err
	}
	go func() { _, err := s.disconnectVoiceRoute(ctx, claim, guard); done <- err }()
	if err := <-done; err != nil {
		t.Fatal("preparing lease failed to finish", err)
	}
	waitActive(t, s, 0)
}

func TestProductRouteGenerationAdapterIntentAndNoRawCapture(t *testing.T) {
	const generated = "😋你好……你好。✨"
	const spoken = "你好……你好。"
	const providerWait = 15 * time.Millisecond
	for _, tc := range []struct {
		name, route, firstSource     string
		crossResponse, failAfterText bool
	}{
		{name: "A_done", route: "A", firstSource: "response.text.done"},
		{name: "A_delta", route: "A", firstSource: "response.text.delta"},
		{name: "A_delta_then_failure", route: "A", firstSource: "response.text.delta", failAfterText: true},
		{name: "A_no_matching_text", route: "A", crossResponse: true},
		{name: "B", route: "B"},
	} {
		route := tc.route
		t.Run(tc.name, func(t *testing.T) {
			var firstAudio atomic.Bool
			usage := func(stage string) map[string]any {
				switch stage {
				case "tts":
					return map[string]any{"characters": 3, "private_text": "must-not-persist"}
				case "asr":
					return map[string]any{"duration": .1}
				case "llm":
					return map[string]any{"prompt_tokens": 2, "completion_tokens": 3, "total_tokens": 5}
				default:
					return map[string]any{"input_tokens": 3, "output_tokens": 3, "total_tokens": 6, "input_tokens_details": map[string]any{"text_tokens": 1, "audio_tokens": 2}}
				}
			}
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/compatible-mode/v1/chat/completions" {
					_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": generated}}}, "usage": usage("llm")})
					return
				}
				c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer c.Close()
				read := func() map[string]any {
					var v map[string]any
					if err := c.ReadJSON(&v); err != nil {
						t.Error(err)
					}
					return v
				}
				if r.URL.Path == "/realtime" {
					_ = c.WriteJSON(map[string]any{"type": "session.created"})
					update := read()["session"]
					_ = c.WriteJSON(map[string]any{"type": "session.updated", "session": update})
					_ = read()
					_ = read()
					_ = c.WriteJSON(map[string]any{"type": "input_audio_buffer.committed"})
					create := read()
					instructions := update.(map[string]any)["instructions"].(string)
					if !strings.Contains(instructions, "existing common business context") || create["type"] != "response.create" {
						t.Error("session instructions or response request missing")
					}
					if _, duplicated := create["response"].(map[string]any)["instructions"]; duplicated {
						t.Error("turn instructions sent twice")
					}
					_ = c.WriteJSON(map[string]any{"type": "response.created", "response": map[string]any{"id": "r"}})
					_ = c.WriteJSON(map[string]any{"type": "response.text.delta", "response_id": "r", "delta": ""})
					if tc.crossResponse {
						_ = c.WriteJSON(map[string]any{"type": "response.text.delta", "response_id": "r", "delta": "  "})
						_ = c.WriteJSON(map[string]any{"type": "response.text.delta", "response_id": "old", "delta": "foreign"})
						return
					}
					time.Sleep(providerWait)
					if tc.firstSource == "response.text.delta" {
						_ = c.WriteJSON(map[string]any{"type": "response.text.delta", "response_id": "r", "delta": generated})
						if tc.failAfterText {
							return
						}
					}
					_ = c.WriteJSON(map[string]any{"type": "response.text.done", "response_id": "r", "text": generated})
					_ = c.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"id": "r", "status": "completed", "usage": usage("native"), "output": []any{map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "text", "text": generated}}}}}})
					return
				}
				run := read()
				task := run["payload"].(map[string]any)["task"].(string)
				id := run["header"].(map[string]any)["task_id"].(string)
				event := func(name string, output any) {
					_ = c.WriteJSON(map[string]any{"header": map[string]any{"task_id": id, "event": name}, "payload": map[string]any{"output": output, "usage": usage(task)}})
				}
				event("task-started", nil)
				if task == "asr" {
					for {
						kind, _, err := c.ReadMessage()
						if err != nil {
							return
						}
						if kind == websocket.TextMessage {
							break
						}
					}
					event("result-generated", map[string]any{"sentence": map[string]any{"text": "你好", "sentence_end": true}})
					event("task-finished", nil)
					return
				}
				cont := read()
				text := cont["payload"].(map[string]any)["input"].(map[string]any)["text"].(string)
				_ = read()
				if text == "你好。" && !firstAudio.Load() {
					t.Error("second synthesis began before the validated first audio was delivered")
				}
				out := func(kind string, words any) any {
					return map[string]any{"type": kind, "original_text": text, "sentence": map[string]any{"index": 0, "words": words}}
				}
				event("result-generated", out("sentence-begin", nil))
				event("result-generated", out("sentence-synthesis", nil))
				_ = c.WriteMessage(websocket.BinaryMessage, make([]byte, 9600))
				event("result-generated", out("sentence-end", []any{map[string]any{"text": "你好", "begin_index": 0, "end_index": 2, "begin_time": 0, "end_time": 200}}))
				event("task-finished", nil)
			}))
			defer provider.Close()
			base := "ws" + strings.TrimPrefix(provider.URL, "http")
			s := NewServer(Config{Key: "fixture", URL: base + "/realtime", ASRURL: base + "/api-ws/v1/inference", VoiceRouteForDevice: func(string) (string, error) { return route, nil }}, nil, t.TempDir(), "routes")
			dir, err := s.sessionDirectory(Ownership{DeviceID: memoryDeviceA, ClientType: "fixture"})
			if err != nil {
				t.Fatal(err)
			}
			session := s.newSession(nil, dir)
			defer session.cancel()
			var text, reference string
			audio := 0
			err = session.generate(context.Background(), 1, "existing common business context", make([]byte, 3200), func(kind string, b []byte) error {
				switch kind {
				case "text":
					if text != "" {
						return errors.New("display transcript emitted more than once")
					}
					text = string(b)
				case "spoken_reference":
					reference = string(b)
				case "audio":
					wantReference := spoken
					if audio < 9600 {
						wantReference = "你好……"
					}
					if text != generated || reference != wantReference {
						return errors.New("spoken reference did not follow validated audio")
					}
					audio += len(b)
					firstAudio.Store(true)
				}
				return nil
			})
			failed := tc.crossResponse || tc.failAfterText
			if failed && (err == nil || audio != 0) {
				t.Fatal("invalid response reached playback")
			}
			if !failed && (err != nil || audio != 2*9600) {
				t.Fatal("adapter generation", err, text, reference, audio)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			for _, entry := range entries {
				if entry.Name() == "session.json" {
					continue
				}
				if !strings.HasPrefix(entry.Name(), "voice-route-") || !strings.HasSuffix(entry.Name(), ".json") {
					t.Fatal("extra capture", entry.Name())
				}
				raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(raw, []byte("must-not-persist")) || bytes.Contains(raw, []byte(generated)) {
					t.Fatal("raw usage persisted")
				}
				var intent map[string]any
				_ = json.Unmarshal(raw, &intent)
				state := "completed"
				if failed {
					state = "failed_or_unknown"
				}
				ms, measured := intent["response_create_to_first_text_ms"].(float64)
				source, sourced := intent["first_text_source"]
				if intent["stage"] == "voice-route-native" && tc.firstSource != "" {
					if !measured || ms <= 0 || source != tc.firstSource || intent["id"] != float64(1) {
						t.Errorf("first text missing or misattributed: ms=%v source=%v", ms, source)
					}
				} else if measured || sourced {
					t.Error("first text attributed to unrelated stage or invalid response")
				}
				if intent["route"] != route || intent["state"] != state || intent["usage"] == nil {
					t.Fatal("missing product intent", string(raw))
				}
				calls++
			}
			want := 3
			if failed {
				want = 1
			}
			if route == "B" {
				want = 4
			}
			if calls != want {
				t.Fatal("API intent count", calls, want)
			}
		})
	}
}

func TestVoiceRouteDisconnectCancelsSessionBeforeSocketRead(t *testing.T) {
	s := NewServer(Config{}, nil, t.TempDir(), "routes")
	entered := make(chan *Session, 1)
	// A provider Open can precede the device read loop. Teardown must cancel its
	// context directly rather than rely on ReadMessage noticing the closed socket.
	s.runDevice = func(session *Session) { entered <- session; <-session.ctx.Done() }
	h := httptest.NewServer(s.DeviceHandler(DeviceConfig{Token: testDeviceToken, AllowedDevice: testDeviceMAC}))
	defer h.Close()
	conn, _, err := deviceDial(h.URL, testDeviceMAC, testDeviceToken)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	deviceHello(t, conn)
	session := nextSession(t, entered)
	defer session.cancel()
	claim := routeClaim()
	claim.DeviceID = session.rec.owner.DeviceID
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = s.disconnectVoiceRoute(ctx, claim, routeGuard)
	if err != nil {
		session.cancel()
		waitActive(t, s, 0)
		t.Fatal("provider startup not canceled", err)
	}
	if session.ctx.Err() == nil {
		t.Fatal("session still active")
	}
}
