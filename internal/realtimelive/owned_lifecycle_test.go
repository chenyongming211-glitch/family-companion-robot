package realtimelive

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestOwnedStartupRecordFailureStillEndsManifest(t *testing.T) {
	server, _ := testSocket(t)
	dir := t.TempDir()
	info := SessionInfo{Ownership: Ownership{SessionID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ClientType: "legacy_unattributed"}, SchemaVersion: 1, StartedAt: time.Now().UTC()}
	if e := privateJSON(filepath.Join(dir, "session.json"), info); e != nil {
		t.Fatal(e)
	}
	if e := os.Mkdir(filepath.Join(dir, "events.jsonl"), 0700); e != nil {
		t.Fatal(e)
	}
	session := NewSession(server, Config{}, dir, nil)
	if e := session.Run(); e == nil {
		t.Fatal("record failure ignored")
	}
	final, e := ReadSessionInfo(dir)
	if e != nil || final.EndedAt == nil || final.EndReason != "record_write_failed" {
		t.Fatal("startup failure lifecycle lost", e, final.EndReason)
	}
}

func TestOwnedWireScopeAndLifecycle(t *testing.T) {
	for _, mode := range []string{"stop", "wrong_receipt", "missing_receipt", "server_shutdown", "normal_close", "provider_failure"} {
		t.Run(mode, func(t *testing.T) {
			providerConnection := make(chan *websocket.Conn, 1)
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, e := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if e != nil {
					return
				}
				defer c.Close()
				c.WriteJSON(map[string]any{"type": "session.created"})
				for i := 0; i < 2; i++ {
					var request map[string]any
					if c.ReadJSON(&request) != nil {
						return
					}
					session := map[string]any{}
					if i == 0 {
						session["enable_search"] = false
					}
					c.WriteJSON(map[string]any{"type": "session.updated", "session": session})
				}
				providerConnection <- c
				for {
					if _, _, e := c.ReadMessage(); e != nil {
						return
					}
				}
			}))
			defer provider.Close()
			server, client := testSocket(t)
			dir := t.TempDir()
			scope := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			info := SessionInfo{Ownership: Ownership{DeviceID: "dev_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", SessionID: scope, ClientType: "mac_fixture"}, SchemaVersion: 1, StartedAt: time.Now().UTC()}
			if e := privateJSON(filepath.Join(dir, "session.json"), info); e != nil {
				t.Fatal(e)
			}
			s := NewSession(server, Config{URL: "ws" + strings.TrimPrefix(provider.URL, "http")}, dir, nil)
			rt := s.addTurn()
			rt.turn.Sent = 1440
			if mode == "server_shutdown" {
				s.serverStopping = func() bool { return true }
			}
			done := make(chan error, 1)
			go func() { done <- s.Run() }()
			client.SetReadDeadline(time.Now().Add(2 * time.Second))
			var ready map[string]any
			if e := client.ReadJSON(&ready); e != nil || ready["session_id"] != scope {
				t.Fatal("ready scope missing", e)
			}
			switch mode {
			case "wrong_receipt", "missing_receipt":
				event := map[string]any{"type": "receipt", "id": 1, "basis": "CoreAudio_dataPlayedBack", "completed": 1440, "accepted": 1440, "terminal": "progress"}
				if mode == "wrong_receipt" {
					event["session_id"] = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
				}
				client.WriteJSON(event)
			case "provider_failure":
				(<-providerConnection).Close()
			case "normal_close":
				client.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(1000, ""), time.Now().Add(time.Second))
			default:
				client.WriteJSON(map[string]any{"type": "stop", "session_id": scope})
			}
			select {
			case e := <-done:
				if strings.Contains(mode, "receipt") {
					if e == nil || e.Error() != "wrong_session_scope" {
						t.Fatal("scope violation accepted", e)
					}
				} else if mode == "provider_failure" {
					if e == nil {
						t.Fatal("provider failure mislabeled as normal transport close")
					}
				} else if e != nil {
					t.Fatal(e)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("scope/cleanup stalled")
			}
			if rt.turn.Completed != 0 {
				t.Fatal("old receipt changed new playback")
			}
			final, e := ReadSessionInfo(dir)
			if e != nil || final.EndedAt == nil || final.EndReason == "" {
				t.Fatal("lifecycle missing", e)
			}
			if mode == "provider_failure" && final.EndReason != "input_provider_failed" {
				t.Fatal("provider failure mislabeled", final.EndReason)
			}
			if mode == "stop" && final.EndReason != "client_stop" {
				t.Fatal(final.EndReason)
			}
			if mode == "server_shutdown" && final.EndReason != "server_shutdown" {
				t.Fatal(final.EndReason)
			}
			if mode == "normal_close" && final.EndReason != "client_closed" {
				t.Fatal(final.EndReason)
			}
		})
	}
}
