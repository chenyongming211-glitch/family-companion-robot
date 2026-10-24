### Spec Compliance

- ❌ Issues found: cancellation does not close an in-progress HTTP WebSocket Upgrade; the close-on-cancel hook starts only after `DialContext` returns (`internal/qwen/classifier.go:23`, `internal/qwen/classifier.go:31`). This misses the requirement that all classifier connections terminate/close on abort, reconnect, disconnect or replacement input.
- ✅ The reviewed code uses a separate text-only connection with the proven response instructions, strict completed enum, bounded output/events/time and safe errors (`internal/qwen/classifier.go:15`, `internal/qwen/classifier.go:36`, `internal/qwen/classifier.go:74`, `internal/qwen/classifier.go:84`, `internal/qwen/classifier.go:205`).
- ✅ Immutable worker inputs return to the event loop with token/generation/input/task/revision checks; stale revisions and failures share three attempts (`internal/gateway/task_routing.go:71`, `internal/gateway/task_routing.go:95`, `internal/gateway/task_routing.go:106`, `internal/gateway/task_routing.go:129`). Main generation waits for accepted ownership and the current snapshot ACK (`internal/gateway/task_routing.go:142`, `internal/gateway/generation_sync.go:78`, `internal/gateway/session.go:462`).
- ⚠️ Cannot verify from this diff: exact-head Linux checks, native five-second delayed-final ownership, normal/manual-abort native regressions and physical acceptance. These remain controller-owned; the implementation report correctly separates them (`.superpowers/resume-state-alignment/task1c-report.md:40`). No deployment approval is implied.

### Strengths

- The worker never writes task/session state, and the event-loop timeout independently prevents an expired worker from leaving generation waiting (`internal/gateway/task_routing.go:52`, `internal/gateway/task_routing.go:96`, `internal/gateway/task_routing_test.go:455`).
- Main voice updates contain only normal instructions/snapshot; classifier text and synthetic tool output are absent from that context (`internal/gateway/generation_sync.go:24`, `internal/gateway/generation_sync.go:76`, `internal/gateway/task_routing.go:157`).
- Focused tests cover unrelated STT, identity changes, mixed retry failures, late finals, pre-ACK main output and original-goal retention through three resume/device-tool iterations (`internal/gateway/task_routing_test.go:68`, `internal/gateway/task_routing_test.go:196`, `internal/gateway/task_routing_test.go:257`, `internal/gateway/task_routing_test.go:322`, `internal/gateway/task_routing_test.go:483`, `internal/gateway/task_routing_test.go:510`).

### Issues

#### Critical (Must Fix)

- None found in this task-scoped review.

#### Important (Should Fix)

- **P2 — Cancel the socket during HTTP Upgrade, not only after it.** `internal/qwen/classifier.go:23` invokes Gorilla v1.5.3 `DialContext` before registering the cancellation hook at line 31. That dependency applies a deadline to the connected socket but performs HTTP Upgrade I/O directly without a cancellation watcher. If the server accepts TCP/TLS and stalls its Upgrade response, an abort/disconnect cancels the context but leaves the connection and worker alive until the original eight-second deadline. Rapid replacement inputs can accumulate canceled in-flight handshakes. Existing tests do not exercise this phase: their `handshake` stall occurs after `websocket.Upgrader.Upgrade` and after reading `session.update` (`internal/qwen/classifier_test.go:19`, `internal/qwen/classifier_test.go:34`, `internal/qwen/classifier_test.go:130`). Add a focused test that withholds HTTP 101 after accepting the request, explicitly cancel a still-long-lived context, and require prompt return/socket close. Arrange close-on-cancel ownership for the underlying network connection while dialing/upgrading, keeping the existing bounded deadline and safe errors.

#### Minor (Nice to Have)

- Reported verification output is not pristine: macOS race builds emitted C/Opus `LC_DYSYMTAB` linker warnings (`.superpowers/resume-state-alignment/task1c-report.md:43`). The report identifies these as existing toolchain noise; they are not a new functional regression or a substitute for Linux verification.

### Focused Checks

- Read the supplied diff once. No Git commands, broad suite reruns, remote/provider/device/API actions, branch/worktree/index/source mutations were performed. This review report is the only repository write.
- Named risk: task binding across side replies, tools and interruptions. Focused unchanged-call-site inspection found side playback leaves the original task retained, explicit resume binds it, and device continuation preserves that binding (`internal/gateway/playback_progress.go:106`, `internal/gateway/playback_progress.go:323`, `internal/gateway/session_tools.go:193`).
- Named risk: stale main-snapshot ACK. Focused unchanged-call-site inspection confirms FIFO update tokens and recomputation of current instructions before `response.create`; a changed snapshot triggers another update (`internal/gateway/generation_sync.go:79`, `internal/gateway/generation_sync.go:89`).
- Named risk: canceling the connection before WebSocket creation. Inspected the installed `github.com/gorilla/websocket@v1.5.3/client.go` implementation at lines 281–300 and 362–372, then ran only the local probe below. `go run "$task_probe"` printed `FAIL: DialContext still blocked 300ms after cancellation during HTTP Upgrade` and `returned only at handshake deadline after 1.001346667s`. The probe used a one-second deadline to keep the check short; production config uses eight seconds.

```go
package main
import (
    "context"
    "fmt"
    "net/http"
    "net/http/httptest"
    "strings"
    "time"
    "github.com/gorilla/websocket"
)
func main() {
    entered, release := make(chan struct{}), make(chan struct{})
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        close(entered)
        <-release // Accept the request but withhold the HTTP Upgrade response.
    }))
    defer srv.Close()
    defer close(release)
    ctx, cancel := context.WithTimeout(context.Background(), time.Second)
    defer cancel()
    done := make(chan error, 1)
    d := websocket.Dialer{HandshakeTimeout: time.Second}
    go func() {
        c, _, err := d.DialContext(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
        if c != nil { c.Close() }
        done <- err
    }()
    <-entered
    cancel()
    start := time.Now()
    select {
    case err := <-done:
        fmt.Printf("returned after cancellation in %v: %v\n", time.Since(start), err)
    case <-time.After(300*time.Millisecond):
        fmt.Println("FAIL: DialContext still blocked 300ms after cancellation during HTTP Upgrade")
        <-done
        fmt.Printf("returned only at handshake deadline after %v\n", time.Since(start))
    }
}
```

### Assessment

**Task quality:** Needs fixes.

**Reasoning:** The architecture, state validation and response gate satisfy the inspected Task 1c design. The reproducible HTTP Upgrade cancellation gap violates the explicit cancellation requirement and needs a source fix plus a focused regression before promotion.

### Fix Re-review — Final Task Verdict at 5ee8d4b312bdeb8eb1fd0a0f0a6a5c2c4fef79ca

- ✅ **Spec compliance: compliant for the reviewed implementation scope.** The Important P2 is resolved: a custom `NetDialContext` installs the close watcher immediately after TCP creation, before TLS/HTTP Upgrade, and watches the outer classifier context so successful Upgrade does not prematurely stop the connection (`internal/qwen/classifier.go:32`, `internal/qwen/classifier.go:39`). Standard dial cancellation still covers the pre-connection phase; deferred watcher cleanup and existing connection-close paths retain bounded lifetime (`internal/qwen/classifier.go:23`, `internal/qwen/classifier.go:52`).
- ✅ **Task quality: Approved.** The fix adds only connection-lifetime ownership and its focused regression; callback state is immutable, and `stopClose` is assigned on the synchronous dial path. No new material findings in the supplied fix diff (`internal/qwen/classifier.go:23`, `internal/qwen/classifier.go:32`).
- ✅ **Regression quality:** the new test withholds HTTP 101 after receiving and hijacking the real local request, explicitly cancels without an early parent deadline, and requires both prompt classifier return and peer EOF (`internal/qwen/classifier_test.go:150`, `internal/qwen/classifier_test.go:155`, `internal/qwen/classifier_test.go:183`, `internal/qwen/classifier_test.go:194`).
- ✅ **Evidence checked without rerunning suites:** read the actual focused RED/GREEN logs and qwen full/race/vet logs. RED failed after 1.00s with `classifier blocked during HTTP Upgrade after cancellation`; GREEN returned and closed in `42.417µs`; qwen full/race passed and vet was empty (`.superpowers/resume-state-alignment/task1c-report.md:54`, `.superpowers/resume-state-alignment/task1c-report.md:55`, `.superpowers/resume-state-alignment/task1c-report.md:56`, `.superpowers/resume-state-alignment/task1c-report.md:57`, `.superpowers/resume-state-alignment/task1c-report.md:58`).
- ⚠️ **Remaining external verification boundary:** controller reports ownership PASS for the prior 44357b0 native candidate, while exact-5ee8d4b Linux verification and final native verification are controller-owned. This source review does not establish final native acceptance, physical hearing, word alignment or deployment readiness (`.superpowers/resume-state-alignment/task1c-report.md:61`). The earlier macOS toolchain warning remains historical; the fix's qwen race log contains no warning (`.superpowers/resume-state-alignment/task1c-report.md:43`, `.superpowers/resume-state-alignment/task1c-report.md:57`).
