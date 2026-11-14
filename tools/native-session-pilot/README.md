# Native device context repair assets

These are cloud-only, test-package assets for the isolated Waveshare runner,
health build `native-device-pilot-20261114-v9`. They add no product service entry
point. The files ending in `.go.txt` are deliberately excluded from normal Go
builds. The separate `probe/` assets have their own explicit admission gates.

The patch applies only to the fixed v8 runner and guards already preserved in
the private 2026-11-14 source snapshot. Its exact inputs are:

| File under `internal/realtimelive/` | SHA-256 |
| --- | --- |
| `native_device_pilot_test.go` | `f9d64ab738c1374254cc7c81d1e89bc42dba0201a20d35bd1ff292c0fb153981` |
| `native_device_pilot_guard_test.go` | `85f18e558ab1a2346f0627d1c80b386f44db7677fb3773661d83612989062d68` |

Keep that source, its original support inventory, and v8 evidence unchanged.
The patch has zero context so its unchanged Go indentation is not interpreted as patch-file whitespace. Apply with `--unidiff-zero`, only in a separately derived source directory, after checking both input
hashes. The derived source also requires the reviewed
`internal/realtimestate/playback_segments.go` implementation and its tests from
this change. Do not copy server credentials, raw audio, or generated binaries
into this repository.

## Apply on the cloud host

Use the existing server toolchain and the preserved source/support inventory.
The following commands assume a fresh derived copy still containing the exact
v8 inputs, and a separately copied directory of these reviewed assets:

```sh
cd "$derived_source"
sha256sum internal/realtimelive/native_device_pilot{,_guard}_test.go
git apply --unidiff-zero --check "$repair_assets/native-device-v8-context-repair.patch"
git apply --unidiff-zero "$repair_assets/native-device-v8-context-repair.patch"
install -m 0600 "$repair_assets/native_context_repair_test.go.txt" \
  internal/realtimelive/native_context_repair_test.go
install -m 0600 "$repair_assets/native_context_repair_guard_test.go.txt" \
  internal/realtimelive/native_context_repair_guard_test.go
sha256sum -c "$repair_assets/runner-v9.sha256"
```

The manifest binds the four derived runner files, including unchanged portions
of the private fixed v8 inputs. It does not publish those full private inputs.
The two added sources contain synthetic fixtures and protocol handling only.

Run these checks **on the cloud host**, with real-provider and serving opt-in
environment variables unset. None of these checks needs provider credentials:

```sh
/opt/family-robot/toolchains/go1.27.3/bin/go test \
  ./internal/realtimelive ./internal/realtimestate -count=1
/opt/family-robot/toolchains/go1.27.3/bin/go test -race \
  ./internal/realtimelive ./internal/realtimestate -count=1
/opt/family-robot/toolchains/go1.27.3/bin/go vet \
  ./internal/realtimelive ./internal/realtimestate
/opt/family-robot/toolchains/go1.27.3/bin/go test -c \
  -o "$private_output/native-device-v9.test" ./internal/realtimelive
```

Preserve each check's first output in a new, private, write-once log. Starting
the opt-in server, admitting a real input, or routing the device requires the
separate reviewed source/binary proof and route workflow. These asset checks
do not start a voice service, consume the real-answer ledger, or prove provider
history mutation support or physical playback quality.

## Safety contract

`turnMu` keeps a single reader. All normal writes and the cancellation watcher
share `writeMu`. The watcher sends at most one schema-confirmed
`response.cancel` after the response identity is known, then allows at most
three seconds for the reader to verify the same response's terminal and exact
ordered output items. A cancelled producer remains owned until that work and
the audit finish. An interrupting next question may wait, with its own context,
at most four seconds for that cancelled owner; overlapping live inputs still
fail before reservation.

After `Session.settle`, repair independently checks final device playback for
the exact Cut turn. The core's existing ASR wait remains capped at 1200ms.
Before any context mutation or rebuild, a Cut turn receives one immutable
evidence snapshot under `Session.mu`. If `OutputASR.Done` is still open, already
validated clauses remain available, `unknown=true` is forced, and the system
marker records `output_asr_unfinished_at_settle`. A finished but failed ASR
records `output_asr_failed_at_settle` with the same conservative unknown flag.
The marker explains that playback completeness describes receipts rather than
ASR completeness. The snapshot survives native reconnects and is pruned with
the existing recent-history limit; late ASR remains diagnostic and cannot
silently upgrade either the repaired context or a later typed rebuild. This
adds no further ASR wait and does not stop the shared ASR worker.

Repair deletes every recorded assistant item and validates
each deleted ACK. Confirmed short clauses are inserted after the original user
item, followed by a separate system item carrying that turn's interruption,
canonical playback samples/milliseconds, unknown flag, and current-request
priority. Created ACKs must match ID, message type, role, exact text/content
type, and top-level previous-item ordering. New input PCM and `response.create`
wait until this finishes. Already repaired turns are skipped; other native
audio history remains on the connection. Audio-window accounting stays
conservative after deletion.

The decoder supports one audio content part per assistant output item. Audio,
audio-transcript and content-part events must carry integer `content_index=0`;
missing/nonzero indexes, repeated content-part additions or terminal transcript
events, and item/terminal content arrays with multiple parts fail as unknown.
This prevents a later part's transcript from replacing text for earlier PCM.
The old connection closes and the next admitted input uses the safe rebuild;
the original response is never retried. Multiple assistant **output items**
remain supported. The fake frames include the same required content-index field
as the provider schema.

Any unknown cancellation terminal, final playback, deletion, creation, identity,
or ordering drops the old connection. The next admitted input uses the existing
typed rebuild with the **same turn-specific system marker**, including when no
assistant clause can be confirmed. Original unknown responses are never
retried, and the shared 48-primary/8-protocol ledger is unchanged. A generation
deadline or the 60-second audio cap retains the existing failure behavior. The
600ms device credit, playback/drain timeouts, voice, and firmware are unchanged.

## Evidence

Cloud fake-server RED records captured the v8 completed-Cut rebuild, missing
interruption state, generic marker without turn/position/unknown fields, and
the rejected interrupting question while cancellation was settling. Ordinary,
race, schedule-stress and package results are retained privately under the
2026-11-14 `ops/repair-v1/driver-*.log` records. The fake tests distinguish exact
mutation ACKs, missing/foreign/reordered ACKs, all assistant items, cancellation
uncertainty, retained interruption state, delayed input ASR identity isolation,
initial shutdown and live-overlap rejection. Real protocol and device outcomes
must be reported separately.

The ASR-freeze regression uses the real `Session.settle` wait with an unfinished
output-ASR channel beyond 1200ms. It checks that mutation and the next response
wait for settlement, then delivers another valid ASR clause and completes ASR.
Both same-connection repair and an initial mutation-failure rebuild must keep
the exact original frozen text/state during a later window rebuild. Its RED
record captured the original silent context upgrade.

The multipart regression's RED record captured a two-part assistant item being
reported complete with only the second part's transcript. Its wire cases cover
audio/transcript indexes, repeated content events, and multipart added, done,
created and response-terminal item shapes; each must close and safely rebuild
for the next input while preserving the ordinary multiple-output-item tests.
