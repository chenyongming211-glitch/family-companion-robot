package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"familyrobot/internal/audio"
	"familyrobot/internal/qwen"
	"familyrobot/internal/recordstore"
	"github.com/gorilla/websocket"
)

func beginNativeClear(t *testing.T, h *toolHarness, old *toolPeer) {
	prepareNativeClear(t, h, old, 0)
	toolCall(t, old, "clear-parent", "clear-call", "conversation_clear_records", "{}")
}

func prepareNativeClear(t *testing.T, h *toolHarness, old *toolPeer, frames int) {
	t.Helper()
	h.device.send(t, map[string]any{"type": "listen", "state": "start", "mode": "auto"})
	old.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "secret", "transcript": "private-before-clear"})
	h.device.want(t, "stt")
	old.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "clear", "transcript": "请清空本设备对话记录"})
	h.device.want(t, "stt")
	toolParent(t, old, "clear-parent", frames)
	old.send(t, map[string]any{"type": "response.audio_transcript.done", "response_id": "clear-parent", "transcript": "private-before-clear"})
}

func TestClearUsesFreshNativeConversation(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			h := newToolHarness(t, false, func(c *Config) {
				if enabled {
					c.RecordDirectory = t.TempDir()
				}
			})
			old, _ := h.model(t)
			beginNativeClear(t, h, old)
			fresh, cfg := h.model(t)
			if strings.Contains(fmt.Sprint(cfg), "private-before-clear") {
				t.Fatal("old history in new native configuration")
			}
			user := fresh.want(t, "conversation.item.create")["item"].(map[string]any)
			if user["role"] != "user" || !strings.Contains(fmt.Sprint(user), "请清空本设备对话记录") {
				t.Fatal(user)
			}
			update := fresh.read(t).body
			if update["type"] != "session.update" || strings.Contains(fmt.Sprint(update), "private-before-clear") || !strings.Contains(fmt.Sprint(update), `"clear_records_result":{"ok":true,"cleared":true}`) {
				t.Fatal(update)
			}
			if tools, ok := update["session"].(map[string]any)["tools"].([]any); !ok || len(tools) != 0 {
				t.Fatal("confirmation did not explicitly disable native tools", update)
			}
			fresh.quiet(t, 30*time.Millisecond)
			fresh.send(t, map[string]any{"type": "session.updated"})
			fresh.want(t, "response.create")
			toolParent(t, fresh, "confirmation", 0)
			toolDone(t, fresh, "confirmation")
			fresh.send(t, map[string]any{"type": "input_audio_buffer.committed", "item_id": "later"})
			fresh.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "later", "transcript": "你还记得吗"})
			h.device.want(t, "stt")
			restoredTools := false
			for {
				next := fresh.read(t).body
				if strings.Contains(fmt.Sprint(next), "private-before-clear") {
					t.Fatal("old secret in subsequent response path")
				}
				if next["type"] == "session.update" {
					if tools, ok := next["session"].(map[string]any)["tools"].([]any); ok {
						if len(tools) < 2 {
							t.Fatal("normal generation did not restore tools", next)
						}
						restoredTools = true
					}
					fresh.send(t, map[string]any{"type": "session.updated"})
					continue
				}
				if next["type"] == "response.create" {
					if !restoredTools {
						t.Fatal("normal generation bypassed tools restoration ACK")
					}
					break
				}
				t.Fatal(next)
			}
		})
	}
}

func (b *controlledClearStore) Close(context.Context) error { return nil }

func TestClearFailureUsesFreshNativeReceipt(t *testing.T) {
	b := &controlledClearStore{brokenRecordStore: brokenRecordStore{err: recordstore.ErrNotFound}, release: make(chan error, 1)}
	h := newToolHarnessStore(t, nil, b)
	old, _ := h.model(t)
	beginNativeClear(t, h, old)
	fresh, cfg := h.model(t)
	if strings.Contains(fmt.Sprint(cfg), "private-before-clear") {
		t.Fatal(cfg)
	}
	fresh.want(t, "conversation.item.create")
	fresh.quiet(t, 30*time.Millisecond)
	b.release <- errors.New("synthetic failure")
	update := fresh.read(t).body
	if update["type"] != "session.update" || !strings.Contains(fmt.Sprint(update), `"clear_records_result":{"ok":false,"error":"clear_failed"}`) {
		t.Fatal(update)
	}
	fresh.quiet(t, 30*time.Millisecond)
	fresh.send(t, map[string]any{"type": "session.updated"})
	fresh.want(t, "response.create")
}

func TestClearNewCommittedInputSupersedesPendingReceipt(t *testing.T) {
	b := &controlledClearStore{brokenRecordStore: brokenRecordStore{err: recordstore.ErrNotFound}, release: make(chan error, 1)}
	// Always release on failure so cleanup cannot wait for a deliberately held receipt.
	defer func() {
		select {
		case b.release <- nil:
		default:
		}
	}()
	h := newToolHarnessStore(t, nil, b)
	old, _ := h.model(t)
	beginNativeClear(t, h, old)
	fresh, _ := h.model(t)
	fresh.want(t, "conversation.item.create")
	fresh.send(t, map[string]any{"type": "input_audio_buffer.committed", "item_id": "new-input"})
	fresh.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "new-input", "transcript": "new input after clear"})
	h.device.want(t, "stt")
	// No VAD or device listen.start is needed for a committed identity to supersede
	// a pending clear confirmation. Storage must not gate this new generation.
	update := fresh.read(t).body
	if update["type"] != "session.update" || strings.Contains(fmt.Sprint(update), "private-before-clear") {
		t.Fatal(update)
	}
	fresh.send(t, map[string]any{"type": "session.updated"})
	fresh.want(t, "response.create")
	toolParent(t, fresh, "new-response", 0)
	b.release <- nil
	toolDone(t, fresh, "new-response")
	fresh.quiet(t, 100*time.Millisecond)
}

func TestClearHandshakeBuffersInputAndAbortIsResponsive(t *testing.T) {
	for _, abort := range []bool{false, true} {
		t.Run(fmt.Sprint(abort), func(t *testing.T) {
			h := newToolHarness(t, false)
			old, _ := h.model(t)
			beginNativeClear(t, h, old)
			var fresh *toolPeer
			select {
			case fresh = <-h.models:
			case <-time.After(time.Second):
				t.Fatal("no new native connection")
			}
			defer fresh.c.Close()
			// Keep session.created pending while new device input and abort arrive.
			if abort {
				h.device.send(t, map[string]any{"type": "abort"})
				select {
				case ev := <-h.device.in:
					if ev.body["type"] != "tts" || ev.body["state"] != "stop" {
						t.Fatal(ev)
					}
				case <-time.After(300 * time.Millisecond):
					t.Fatal("abort blocked on native handshake")
				}
			}
			h.device.send(t, map[string]any{"type": "listen", "state": "start", "mode": "manual"})
			enc, e := audio.NewEncoderAtRate(16000)
			if e != nil {
				t.Fatal(e)
			}
			defer enc.Close()
			dec, e := audio.NewDecoder()
			if e != nil {
				t.Fatal(e)
			}
			defer dec.Close()
			var expected []byte
			for i := 0; i < 4; i++ {
				pcm := make([]byte, 1920)
				for j := range pcm {
					pcm[j] = byte(j + i)
				}
				packet, e := enc.Encode(pcm)
				if e != nil {
					t.Fatal(e)
				}
				decoded, e := dec.Decode(packet)
				if e != nil {
					t.Fatal(e)
				}
				expected = append(expected, decoded...)
				if e := h.device.c.WriteMessage(websocket.BinaryMessage, packet); e != nil {
					t.Fatal(e)
				}
			}
			h.device.send(t, map[string]any{"type": "listen", "state": "stop"})
			fresh.send(t, map[string]any{"type": "session.created"})
			fresh.want(t, "session.update")
			fresh.want(t, "session.update") // manual mode, distinct ACK FIFO entry
			var got []byte
			for i := 0; i < 4; i++ {
				ev := fresh.want(t, "input_audio_buffer.append")
				pcm, e := base64.StdEncoding.DecodeString(ev["audio"].(string))
				if e != nil {
					t.Fatal(e)
				}
				got = append(got, pcm...)
			}
			if !bytes.Equal(got, expected) {
				t.Fatal("clear reconnect lost or changed new input")
			}
			fresh.want(t, "input_audio_buffer.commit")
			fresh.send(t, map[string]any{"type": "input_audio_buffer.committed", "item_id": "new-manual"})
			fresh.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "new-manual", "transcript": "new captured request"})
			h.device.want(t, "stt")
			fresh.want(t, "response.create")
			fresh.quiet(t, 30*time.Millisecond)
		})
	}
}

func TestClearConfirmationCannotRepeatDeletion(t *testing.T) {
	b := &controlledClearStore{brokenRecordStore: brokenRecordStore{err: recordstore.ErrNotFound}, release: make(chan error, 1)}
	defer func() { b.release <- nil }()
	s := recordSession(t, b, "one")
	s.generation = 2
	s.responseID = "confirmation"
	s.currentInput = requestOrigin{generation: 2, itemID: "clear", known: true, text: "请清空本设备对话记录"}
	s.clearResult = []byte(`{"ok":true,"cleared":true}`)
	if err := s.callTool(qwen.Event{ResponseID: "confirmation", CallID: "repeated", Name: "conversation_clear_records", Arguments: "{}"}); err != nil {
		t.Fatal(err)
	}
	if s.records.clear != nil || s.toolCalls["2:repeated"].output != string(s.clearResult) {
		t.Fatal("confirmation reexecuted deletion instead of reusing the actual receipt")
	}
}

func TestClearConfirmationInterruptionKeepsOnlyNewInput(t *testing.T) {
	h := newToolHarness(t, false)
	old, _ := h.model(t)
	beginNativeClear(t, h, old)
	fresh, _ := h.model(t)
	fresh.want(t, "conversation.item.create")
	fresh.want(t, "response.create")
	toolParent(t, fresh, "confirmation", 20)
	if h.device.want(t, "tts")["state"] != "start" {
		t.Fatal("no confirmation playback")
	}
	enc, e := audio.NewEncoderAtRate(16000)
	if e != nil {
		t.Fatal(e)
	}
	defer enc.Close()
	packet, e := enc.Encode(make([]byte, 1920))
	if e != nil {
		t.Fatal(e)
	}
	if e = h.device.c.WriteMessage(websocket.BinaryMessage, packet); e != nil {
		t.Fatal(e)
	}
	prefix := fresh.want(t, "input_audio_buffer.append")["audio"]
	fresh.send(t, map[string]any{"type": "input_audio_buffer.speech_started"})
	stop := readStop(t, h.device)
	if stop["reason"] != "interrupted" {
		t.Fatal(stop)
	}
	next, cfg := h.model(t)
	if strings.Contains(fmt.Sprint(cfg), "private-before-clear") {
		t.Fatal("confirmation interrupt revived old context")
	}
	if got := next.want(t, "input_audio_buffer.append")["audio"]; got != prefix {
		t.Fatal("confirmation interrupt lost new speech prefix")
	}
	next.send(t, map[string]any{"type": "input_audio_buffer.committed", "item_id": "after-confirmation"})
	next.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "after-confirmation", "transcript": "新的问题"})
	h.device.want(t, "stt")
	next.want(t, "response.create")
	next.quiet(t, 30*time.Millisecond)
}

func TestClearStopsParentPlaybackBeforeHandshake(t *testing.T) {
	h := newToolHarnessFeatures(t, map[string]bool{"playback_progress_v1": true})
	old, _ := h.model(t)
	prepareNativeClear(t, h, old, 50)
	start := readStart(t, h.device)
	toolCall(t, old, "clear-parent", "clear-call", "conversation_clear_records", "{}")
	stop := readStop(t, h.device)
	if stop["reason"] != "interrupted" || stop["playback_id"] != start["playback_id"] {
		t.Fatal("clear did not interrupt old device queue with its playback identity", stop)
	}
	fresh, cfg := h.model(t)
	if strings.Contains(fmt.Sprint(cfg), "private-before-clear") {
		t.Fatal(cfg)
	}
	fresh.want(t, "conversation.item.create")
	fresh.want(t, "response.create")
	h.device.quiet(t, 30*time.Millisecond)
}

func TestClearOldSnapshotACKCannotReleaseNewInput(t *testing.T) {
	h := newToolHarness(t, false)
	old, _ := h.model(t)
	beginNativeClear(t, h, old)
	fresh, _ := h.model(t)
	fresh.want(t, "conversation.item.create")
	first := fresh.read(t).body
	if first["type"] != "session.update" {
		t.Fatal(first)
	}
	fresh.send(t, map[string]any{"type": "input_audio_buffer.committed", "item_id": "superseding"})
	fresh.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "superseding", "transcript": "只回答这个新问题"})
	h.device.want(t, "stt")
	second := fresh.read(t).body
	if second["type"] != "session.update" {
		t.Fatal(second)
	}
	cfg := second["session"].(map[string]any)
	if tools, ok := cfg["tools"].([]any); !ok || len(tools) < 2 {
		t.Fatal("replacement input did not restore tools")
	}
	if strings.Contains(fmt.Sprint(cfg), `"clear_records_result":{"ok":true`) {
		t.Fatal("old receipt attached to replacement input")
	}
	fresh.send(t, map[string]any{"type": "session.updated"}) // retired clear snapshot
	fresh.quiet(t, 30*time.Millisecond)
	fresh.send(t, map[string]any{"type": "session.updated"}) // replacement snapshot
	fresh.want(t, "response.create")
	fresh.quiet(t, 30*time.Millisecond)
}

func TestClearNewNativeGenerationDoesNotInheritRetiredResponseIDs(t *testing.T) {
	h := newToolHarness(t, false)
	old, _ := h.model(t)
	h.device.send(t, map[string]any{"type": "listen", "state": "start", "mode": "auto"})
	old.send(t, map[string]any{"type": "input_audio_buffer.committed", "item_id": "secret"})
	old.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "secret", "transcript": "private-before-clear"})
	h.device.want(t, "stt")
	old.want(t, "response.create")
	toolParent(t, old, "reused-id", 0)
	toolDone(t, old, "reused-id")
	old.send(t, map[string]any{"type": "input_audio_buffer.committed", "item_id": "clear"})
	old.send(t, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "clear", "transcript": "请清空本设备对话记录"})
	h.device.want(t, "stt")
	old.want(t, "response.create")
	toolParent(t, old, "clear-parent", 0)
	toolCall(t, old, "clear-parent", "clear-call", "conversation_clear_records", "{}")
	fresh, _ := h.model(t)
	fresh.want(t, "conversation.item.create")
	fresh.want(t, "response.create")
	toolParent(t, fresh, "reused-id", 1)
	toolDone(t, fresh, "reused-id")
	readStart(t, h.device)
	readStop(t, h.device)
}
