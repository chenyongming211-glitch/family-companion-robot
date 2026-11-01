# C3 Bounded PCM Implementation Plan

> Use subagent-driven-development task by task with spec review followed by quality review. All workers use main; no branches/worktrees. User has authorized this design direction and physical validation after the build/resource gates.

**Goal:** Replace C3 Opus media with bounded PCM and streamed cues, enable full residual echo processing, and validate the actual device without losing its working fallback.

**Architecture:** Common Go Session negotiates PCM only at the Realtime entry. C3 owns fixed input/output slots and a separately verified Flash cue trailer; its original DMA/reference path remains authoritative. No VAD/drain-policy change.

**Tech stack:** Go1.27.3/libopus for cloud tests and cue conversion; locked IDF6.1/ESP-SR2.6.1; host C++ O2/ASan/UBSan; GitHub exact-target builds; USB on Mac only after verification.

## Task 1: Promote native PCM server transport

Files: internal/protocol/protocol.go; internal/realtimelive/{xiaozhi_entry.go,xiaozhi_wire.go,session.go,pcm_test.go}; tools/c3-pcm-assessment remains a frozen historical tool.

- [x] Copy/adapt the approved trial assertions to production tests, including old parser rejection, authenticated PCM admission, strict1920/2880B frames, raw PCM preservation, wake isolation,7200-sample window, ASR/drain and output tail clocks.
- [x] Run cloud baseline test to observe missing-format RED; implement the minimal trial adaptation with production names/comments. Public `ParseHello` stays Opus-only; new `ParseRealtimeHello` explicitly enables PCM. PCM zero-output RED→GREEN preserves Session/nextreply and Opus zero tail.
- [x] Run cloud `go test ./...` and focused `-race`; spec review then quality review.5d90ef7:376primary/869incl pass,2optional skips; PCM/race10primary/30incl. Both independent reviews PASS, pushed with designc680239; no deployment.

## Task 2: Fixed PCM storage and cue pack primitives

Files: firmware/overlays/main/audio/{c3_pcm_buffer.h,c3_pcm_cues.h}; firmware/tools/generate_c3_pcm_cues.py; firmware/tests/{c3_pcm_buffer_host.cc,test_c3_pcm_buffer.py,c3_pcm_cues_host.cc,test_c3_pcm_cues.py}.

- [x] Add RED host contracts for exact slots, 512→960 assembly/tails, pool exhaustion, stale epochs, in-flight Reset retention, duplicate/old release, fixed metadata limits and no heap allocation in the pure pool.
- [x] Implement checked C3PcmInputPool Feed/Pop/Reset and C3PcmOutputPool Push/Acquire/Release/Reset with fixed arrays, external locking and nonrewinding serial.
- [x] Add pack parser/converter RED for all21 source cues and packet-clock duration, malformed Ogg pages/lacing/truncated packets, stereo/rate/frame mismatch, invalid header/table/ranges/duplicates/hash, unaligned reads and bounded streaming.
- [x] Implement deterministic source-SHA lookup,64-byte header/48-byte entries, real fullpack/body SHA callbacks. Canonical converter is overlays/scripts/family_c3_pcm_cues.py with thin tools CLI; generated pack/header/manifest stay outsideGit. All21 cues:517frames,1490032Bpack, SHA7457718e68e984c3ebced58164e7e967a1304b52003b02d23d1baffa9ca779da.
- [x] f0ef155 cloud16tests O2/ASan/UBSan/real21converter+reader pass; independent spec and quality reviews PASS.

## Task 2B: Bound the actual C3 WebSocket/TCP build inputs

Files: firmware/overlays/scripts/family_c3_network.py; pure bounded receive/deadline helper headers and host tests. Managed dependency files/native locks remain untouched; CMake wiring follows in Task4.

- [x] Verify original78/esp-ml3073.7.6 package/source hashes before creating build-directory-only web_socket.cc/esp_tcp.cc replacements; retain original/patched hashes and recipe identity. No source update or managed-source mutation.
- [x] RED→GREEN actual parser tests for handshake/text/binary/fragment/control bounds, TCP coalescing, direct complete-frame delivery, partial headers and repeated failure. Cap handshake2048/text4096/binary2880/rawbuffer6144; no callback-side Disconnect/selfwait. Fragment4096 is allocated only on the first fragmented message and retained.
- [x] Serialize before frame allocation, require exactSend byte count; add post-DNS connect wait and250ms total send budget using nonblocking writes/select. Cover partial/zero/error/wouldblock/deadline behavior from actual helper code. PCM profile later rejectswss before connect; no claims about TLS deadlines.
- [x] d9b4cac: destructor TCP-stop/join ordering, source anchors/hashes and managed immutability verified; cloud7Python tests PASS with actualWS39/TCP12/helpers14 checks each O2+ASan/UBSan. OriginalWS30/39 andTCP10/12 RED. Real blocked POSIX sends250281/250295us. Independent spec then quality reviews PASS; actual FreeRTOS scheduling/IDF wiring still later gates.

## Task 3: C3 media, cue scheduling, FD and statistics integration

Files: firmware/patches/0015-c3-bounded-pcm.patch and new overlays c3_pcm_audio.{h,inc}, c3_echo_stats.h; c3_lite_uplink.inc/c3_lite_aec.h; FamilyC3Codec playback output; firmware host integration tests.

- [ ] Prepare the exact locked upstream plus existing patches in a fresh cloud directory. Add guarded CONFIG_XMINI_C3_PCM to C3 only.
- [ ] Skip C3 PCM encoder/decoder creation and Opus worker. Feed AEC spans directly to2 fixed input slots; Pop creates at most one network1920B packet and retains ButtonUplink token. Overflow reports reference/media fault and closes the session.
- [ ] Copy validated downlink into5 fixed1440-sample slots; use the codec pointer/span entry against the same DMA implementation. Keep in-flight slots until output returns, preserve truthful admit/release/seal accounting and generation cancellation.
- [ ] Stream cue descriptors through a fixed2880B aligned scratch; validate trailer before OTA-valid, cancel entire cues on reset, preserve activation/digit order, prevent cues from entering active server output. Include descriptors/producer/in-flight work in idle/drained checks.
- [ ] Gate unsupported C3 remoteOgg/audio-testing paths before state entry; preserve Wi-Fi configuration and main button behavior. Protect PCM trailer from generic assets downloads before any erase.
- [ ] Enable FD mode5 only under new flag; accumulate bounded numeric echo/processing/pool statistics and report from main. Preserve8KiB input, increase the existing C3 output stack from2KiB to4KiB, remove unneeded Opus worker, and guard TCB lifetimes.
- [ ] RED→GREEN actual production-method tests and all legacy/enabled combinations; full firmware/upstream tests, spec review and quality review.

### Integration contracts confirmed during preparation

Pure APIs are `C3PcmInputPool::Feed(samples,1..512,epoch)/Pop(bytes,1920,&epoch)/Reset(epoch=0)` and `C3PcmOutputPool::Push(bytes,2880,metadata)/Acquire(Lease&)/Release(Lease)/Reset()`. AudioService rechecks `ButtonUplink::Allows` under its queue lock before allowing first-epoch adoption after Reset0. Lease data stays owned until the worker returns; metadata maps connection/playback/first/output_generation without relabeling.

`C3PcmCuePack::Open` takes a real SHA256 callback; `Find` takes original embeddedOgg SHA256; `CopyFrame` emits1440 aligned destination samples. Generated files are `family_c3_pcm_cues.pack`, `.json`, and `_generated.h`; header constants `kFamilyC3PcmPackBytes` and `kFamilyC3PcmPackSha256[32]` bind app to trailer. Put the build directory containing the generated header on C3's include path only. Currentpack size1490032B/source clocks verified, no outputbinary is tracked.

Add the pointer/span codec entry by factoring the existing `FamilyC3Codec::OutputTagged` implementation into one shared submit loop; keep vector callers as wrappers. TTSstart cancels all old cue work before starting the new server identity, and explicit receiving/queue/hardware state prevents cues being inserted while server playback is open. Normal TTSstop/drain must only seal; it must not reset or discard the admitted tail. Cue descriptors keep the entire original output generation and are included in idle/drained checks.

PCM Stop/Start needs an explicit input/output task exit barrier before reuse: the old Stop only posts a stop flag, so an immediate failed-OTA restart could otherwise revive an old worker alongside the new one. Set stopped, wake workers, reset queues without holding the queue lock during exit wait, and fail closed on timeout. Worker completion bits precede self-deletion; Start clears old handles before publishing running and never reuses state while old workers can access it. Resource snapshots query TCBs only in the existing safe stopped/scheduler window; DSP/queue locks and heap/log calls stay outside scheduler suspension.

Unsupported remoteOgg notification is rejected before parsing/scheduling and before power/state/queue changes in StartNotification. Gate only WifiConfiguring branches of the two button handlers for the unavailable10s audio test, plus a defensive AudioService gate; normal buttons/Wi-Fi remain. Asset guards must check announced length/header before unmap/erase and EACH read/write/rounded erase before touching Flash, because the old downloader can accept bytes beyond Content-Length until EOF. This protects the independent trailer; it does not make legacy prefix updates transactional.

Keep the callback/input path nonblocking on a network failure: a capture-token-specific revoke request may stop new publication, but the main owner completes dispatch-linearized Close/flush after any bounded in-flight Send. Never wait for the dispatch lock from the high-priority input task. Tests must cover close during processing, old token requests after reopen, and queue full while send is in progress.

## Task 4: Build and inspect exact candidate

Files: .github/workflows/firmware-experiment.yml, firmware-c3-diagnostic.yml; build preparation/CMake/converter hooks and verify_experiment.py; tracked provenance tests.

- [ ] Build C3 cue trailer/header before app compilation using a recorded libopus version; retain trailer, manifest, source/SDK/native lock, app/ELF/map and hashes in artifacts. Verify original generated_assets.bin stays within0x50000 and unchanged.
- [ ] Commit/push main once source is reviewed. Require exact C3 symbols, five variants and stock success; inspect downloaded actual app/trailer/source/ELF and partition compatibility.
- [ ] Update private installer/rollback guards for the additional exact trailer range; cloud failure-injection tests cover partial writes, boot/mapping/hash failures, resource-gate failure and fault-tail recovery.

Installer preparation audit: old candidate_install.py is tied to2210797 and must not be used unchanged. Its startup capture currently raises immediately on a fault, while only the later monitor keeps an8s tail; a monitor fault after `installed=true` also does not restore stable automatically. The PCM helper must retain the same serial handle for≥8s after the first fault in BOTH startup and loaded monitoring, then select/verify stable app1 and restore authorized trailer bytes as needed. Independently test partial app/trailer/selector writes and both pre/post-install failures. Allow only app0 sectors, selector0 sector and the exact sector-rounded trailer extent; fresh backups and complement checks include the assets prefix, stable selector1/app1 and upper8MiB. Final exact source/pack/ELF hashes bind the installer only after artifact inspection; no broad merged-image flashing.

## Task 5: Controlled server rollout and physical verification

- [ ] With all previous gates passed, build/deploy the verified Go media adapter on cloud, preserve protected config/credentials/identity/memory and backup binary; require active_sessions0, readiness, unchanged policies and old Opus clients. No runtime service on Mac.
- [ ] Inventory exact C3 USB identity and readbacks; back up authorized changed ranges, install app0/trailer/selector0 only, verify all complements/stable fallback/security. Observe two post-ready boot gates with complete logs.
- [ ] Measure actual loaded CPU/heap/largest/stack/pool/reference statistics; fail/rollback below budget or on fault. Do not infer success from startup alone.
- [ ] Only then request the shortest physical acoustic acceptance: no speech during reply, then intentional speak-over-playback, then full new answer/remembered context and button end. Preserve failed results honestly.
- [ ] Update handoff/verification and commit/push main; leave devices/services in the verified state, close serial when finished. Natural voice interruption remains incomplete until actual acoustic acceptance.
