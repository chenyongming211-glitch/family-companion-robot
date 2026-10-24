### Task 1: Optional interrupted stop reason

**Files:**
- Modify: `internal/protocol/protocol.go`
- Modify: `internal/gateway/session.go`
- Modify: `internal/gateway/server_test.go`
- Modify: `internal/gateway/playback_test.go`
- Modify: `internal/gateway/session_tools_test.go`
- Modify: `README.md`

**Interfaces:**
- `protocol.Control` gains `Reason string` with JSON tag `reason,omitempty`.
- Existing `session.tts(state, text string)` stays unchanged for normal controls.
- Existing `session.interrupt(ctx context.Context, replay, force bool)` keeps its signature and guard; its immediate stop is emitted directly with the optional reason before cancellation/reconnection.

- [ ] **Step 1: Extend existing wire-level behavioral regressions before implementation.** In `testInterrupt`, when the first stop is observed, assert its decoded JSON map has `reason == "interrupted"`. When the replacement reply finishes, assert the normal stop has no `reason` key. Cover existing device/model/pending/capturing cases through the existing table. In `TestDeviceToolsAbortDropsLateResult`, retain the returned stop map and assert both state and reason for all existing triggers. In the playback harness, decode raw control data into a JSON map when a stop is received and fail if a reason key exists; this makes every existing normal-drain scenario validate omission on the actual wire.

```go
if stopped && v["reason"] != "interrupted" {
    t.Fatalf("interruption stop reason = %v, want interrupted", v["reason"])
}
// At the replacement reply's normal stop:
if _, exists := v["reason"]; exists {
    t.Fatal("normal playback stop unexpectedly carried a reason")
}
// Tool interruption:
stop := h.device.want(t, "tts")
if stop["state"] != "stop" || stop["reason"] != "interrupted" {
    t.Fatalf("missing explicit interrupted stop: %v", stop)
}
```

Run `go test ./internal/gateway -run 'TestInterruptDropsOldConnectionAudioAndStartsNewResponse|TestDeviceToolsAbortDropsLateResult' -count=1` and record the missing reason failure before changing production code. Do not add tests that only compare source text or mirror JSON struct declarations.

- [ ] **Step 2: Add the minimal production field and interrupt emission.**

```go
// Within protocol.Control:
Reason string `json:"reason,omitempty"`

// In session.interrupt, replace only s.tts("stop", ""):
if err := s.json(protocol.Control{
    Type: "tts", State: "stop", Reason: "interrupted", SessionID: s.id,
}); err != nil {
    return err
}
```

Do not move the existing no-op guard, change cancellation order, or set the reason in normal playback's `tts` helper. Do not change production pacing, idle behavior or model APIs.

- [ ] **Step 3: Run focused green regressions, then the full meaningful verification once.**

```sh
gofmt -w internal/protocol/protocol.go internal/gateway/session.go internal/gateway/server_test.go internal/gateway/playback_test.go internal/gateway/session_tools_test.go
go test ./internal/gateway -run 'TestInterruptDropsOldConnectionAudioAndStartsNewResponse|TestDeviceToolsAbortDropsLateResult|TestPlayback' -count=1
go test -race ./...
go vet ./...
go build ./cmd/gateway
git diff --check
```

Expected: focused regressions pass, all packages pass race/vet/build, no tracked generated binary or private material.

- [ ] **Step 4: Document the wire contract and commit.** Add a short README paragraph near the existing interrupt explanation, with the exact interruption example below and a statement that normal stop omits reason. State that the optional field alone does not make the current stock firmware clear its queues or support natural barge-in; those need the custom firmware and field acceptance.

```json
{"type":"tts","state":"stop","reason":"interrupted","session_id":"..."}
```

Stage only the six task files, commit on `main`, write the detailed report including RED/GREEN commands and outputs to the assigned ignored report path, and do not push or deploy. The controller handles review and remote synchronization.

## Self-review

This task implements only the approved design's optional stop reason. Software reference, AEC alignment, firmware queue handling, experimental variant build, flash/rollback and acoustic acceptance remain active dependencies and are not completed by passing these regressions.
