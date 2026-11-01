# C3 PCM Isolated Assessment Plan

> Execute inline using the executing-plans workflow. Work on main as required; only the cloud validation directory receives the experimental internal source patch. No deployment or hardware write.

**Goal:** Determine whether native PCM transport can preserve the common voice session and evidence semantics while freeing the C3 codec workload.

**Architecture:** A tracked assessment test and explicit prototype patch are copied into a frozen cloud source snapshot. Test-only loopback servers exercise the actual Session and Xiaozhi adapter, without a model call or production configuration.

**Tech stack:** Go, gorilla/websocket, existing cloud libopus/resampler; retained C3 resource measurements.

## Task 1: Protocol and lifecycle RED

- [x] Create `tools/c3-pcm-assessment/pcm_trial_test.go.txt` with authenticated PCM negotiation, invalid formats/frames, bit-exact input/output, wake isolation, shared window/drain and ASR assertions. Copy it with the `.go` extension only in the isolated tree.
- [x] Archive current tracked source to a fresh cloud validation directory and copy the trial test into `internal/realtimelive/` there.
- [x] Run `/opt/family-robot/toolchains/go1.27.3/bin/go test ./internal/realtimelive -run '^TestPCMTrial' -count=1`; retain expected baseline rejection as RED. Final negotiation fixture also reproduced RED against a separate unchanged baseline.

## Task 2: Explicit isolated prototype GREEN

- [x] Create `tools/c3-pcm-assessment/apply_trial.py` with guarded exact replacements for protocol negotiation, wire adaptation and PCM packetization/ASR. Each replacement must match exactly once; invoke only against the cloud validation copy.
- [x] Run `python3 tools/c3-pcm-assessment/apply_trial.py /absolute/cloud/source`; format only that copy's changed Go files. Old shared parser stays Opus-only; isolated Realtime explicitly opts in.
- [x] Run the focused test again. Require exact1920/2880 byte clocks and common7200-sample credit/drain checks to pass. Final7 top-level/19 including subtests passed.

## Task 3: Compatibility and resource assessment

- [x] Run the cloud package regression for protocol, realtimeaudio, realtimestate and realtimelive, followed by all Go packages and a race run of the focused trial. Final373 top-level tests passed,2 optional skipped;858 pass including subtests. Race7/19 passed.
- [x] Inspect the pinned product queue capacities and retained actual chip measurements. Calculate CPU/heap deltas with exclusions; no host timing is reported as C3 performance. Found134400B naive PCM queues and persistent local-cue Opus state; both require firmware work.
- [x] Write the reproducible cloud commands, source hashes, results, queue budget and firmware readiness decision into `docs/verification/2026-11-01-c3-pcm-assessment.md`.

## Task 4: Handoff

- [x] Add `tools/c3-pcm-assessment/README.md` explaining that the prototype is evaluation-only and not loaded by any normal service/build.
- [x] Check diffs/links and commit/push main. No generated firmware, raw logs, credentials or PCM files enter Git. Commit covers only the isolated assessment tools and handoff documents.
- [x] Report protocol findings and remaining physical/resource gates; keep C3 and production at their verified versions. Findings and limits are documented and communicated; final handoff accompanies the commit.
