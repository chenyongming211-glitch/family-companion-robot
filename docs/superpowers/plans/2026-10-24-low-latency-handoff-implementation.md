# Low Latency Voice Handoff Implementation Plan

> **For agentic workers:** Use superpowers:subagent-driven-development for isolated modules and two-stage review; root performs SDK/runtime integration. Follow TDD and verification-before-completion. User approved implementation, main-only, no worktree/branch/PR. Do not ask for the already-authorized execution choice again.

**Goal:** 故事/回答固定原稿，插话停止旧下发但排空已收音频，新回答可预置并本地交接，真实播放确认驱动恢复；原生Mac走同手表的小智/Opus设备桥，自行测试。

**Architecture:** Python独立LiveKit服务新增drain模式与ContentController、DrainOutput；Go隔离设备桥连接该模式，原生Swift终端处理CoreAudio、Opus和本地交接。权威原稿/短句进度首版会话内存保存，旧8000保持不变；不声称跨唤醒、Go持久化或手表新固件已验收。

**Tech Stack:** Existing LiveKit Agents1.8.8/RTC、Silero、aiohttp、Go/Gorilla/libopus、Swift/AVAudioEngine/CoreAudio/URLSessionWebSocket。后端依赖/测试/服务在服务器；Mac只构建/运行原生测试客户端，不搭建后端。Swift使用xcrun指定Apple编译器，现有Mac libopus1.6.1可用；服务器Go路径/opt/family-robot/toolchains/go1.27.3/bin/go，libopus1.4。

## Task 1 — deterministic content controller

**Files:** Create `services/livekit-agent/family_voice/content.py`, `services/livekit-agent/tests/test_content.py`.

- [x] Server RED: immutable sentence split, receipt-driven prefix, drain settling, resume waiting for final receipt, stale content callback, bounded prefetch and cleanup.
- [x] Implement `split_sentences(text)->list[str]`, maximum1200 chars/40 units/80 chars per unit; no silent text loss. `ContentController(prepare, play, emit, name='main', prefetch=3)` with synchronous `start(text, original_input='', kind='reply')`, `pause()`, `resume()`, `discard()`, async `close()`, snapshot property.
- [x] Callback contracts: `await prepare(text)->bytes` complete mono24k s16 PCM <=12seconds; `await play(revision,index,text,pcm,settle)` where `settle(complete:bool)` is the authoritative final observation callback. It may occur after play coroutine cancellation. Pause cancels dispatch worker, keeps original/prepared audio, accepts current-content late drain confirmation, and waits for that callback before dispatching resume; newer user pause retires queued resume. Replacement/discard/close invalidate old content callback. No unknown synthesis auto retries.
- [x] Snapshot includes task_id/name, revision, original_input, kind, units, confirmed_count, next (zero-based), state. Event `content.state` includes metadata only (no text/original_input). At most3 prepared/inflight PCM units including current, each12sec; all workers/prepares joined on close.

```python
# Test contract: full unit0, partial unit1, branch answer, resume unit1.
settle0(True)
controller.pause()
controller.resume()  # cannot dispatch until prior final callback
settle1(False)
assert controller.snapshot['next'] == 1
# play list eventually [0,1,1,2], unchanged original units.
```

Run on server:
```sh
PYTHONPATH=services/livekit-agent .superpowers/livekit-chain/venv/bin/python -m unittest discover -s services/livekit-agent/tests -p test_content.py -v
```
- [x] Spec review then quality review; fix material issues before integrating.

## Task 2 — drain output and real SDK contract

**Files:** Create `family_voice/drain_output.py`, `tests/test_drain_output.py` under services/livekit-agent.

- [x] RED: credit blocks fast output, valid progress releases only matched credit, clear_buffer seals/drains instead of audio.cancel, SDK cancellation settles independently of real receipt, real late valid full receipt still confirms content, old/wrong/invalid/fractional receipt rejected, hard-close releases all tasks.
- [x] Implement AudioOutput adapter using only public SDK APIs. Defaults: normal content window300ms, staged new audio120ms, raw global420ms plus reserved180ms codec/render allowance; no claim physical600ms until calibration. Per-block identity, expected content samples, real observation callback, SDK-finished flag tracked separately. Send serialized audio/audio.end/output.drain and handoff metadata; capture sender epoch fence prevents old frames after sealing.
- [x] `bind_unit(revision,index,expected_samples,settle)` before each say; callback only true for matching complete real unit, regardless SDK generation cancellation. `clear_buffer()` releases SDK lifecycle as interrupted/unknown but preserves real pending ledger and drain control. Final real receipt does not double-finish SDK. Real progress comes from device, never timer-generated; deadline unknown settles false and reports failure to client, does not authorize mixing unknown outputs.
- [x] New output may stage bounded audio behind old drain; both tracked. Events carry block_id/epoch; handoff authorization binds to current input epoch. Native client and bridge must negotiate drain/staging and respect cancellation of an unplayed staged answer.

```python
# Existing received audio remains after SDK interrupt.
sink.clear_buffer()
assert 'audio.cancel' not in [e['type'] for e in events]
assert any(e['type'] == 'output.drain' for e in events)
assert sink.receipt(old_id, old_epoch, old_sent, valid=True, finished=True)
assert settled == [True]  # cancelled generation did not erase genuine playback
```
- [x] Real AgentSession.say fixed PCM/add_to_chat_ctx=False, interrupt/new speech while old receipt held, next output begins preparation before old actual final, late observation remains correct.

## Task 3 — content runtime and protected drain endpoint

Runtime allocation: main prefetch1 + side prefetch1 + one serialized notice, at most3 resident units session-wide. Dispatch anchors confirmed history to the original user turn. Handoff exports content_id/revision/unit_index; causal control_seq handles crossed start/revoke and retains the first revoke fence.

**Files:** Create `family_voice/content_runtime.py`, `tests/test_content_runtime.py`; narrowly extend `runtime.py` agent/hook factories and `server.py` drain mode; preserve existing manual/continuous paths and tests.

- [x] RED real SDK/Silero controlled providers: story text freezes, partial old output drains, side question answers, direct/natural tool resume fixed original, query uses actual prefix, repeated onset retires stale tools/staged answer.
- [x] `DrainVoiceSession` uses existing STT/VAD/no-retry provider configuration, ContentAgent, main+recent side ContentController and DrainOutput. Ordinary model text is fully collected/bounded before fixed playback, not auto-forwarded as already spoken history. Tool-only chunks remain SDK tool calls; their textual preamble is not played. Reject overlong/truncated/empty drafts explicitly, no silent truncation.
- [x] Tools resume/restart/discard/query/replace operate current controller, no model-supplied checkpoint/authority ID. Guard SpeechHandle/input epoch as in prior fixes. Expose only confirmed prefix as assistant history and original/unplayed draft as readonly task data. Side answer cannot overwrite paused main.
- [x] `/v1/audio?mode=drain`, bearer/one-session/15min/turn and speech limits remain. Browser endpoint cannot select drain. Input listen.audio, playback, snapshot request, explicit hard stop; output ready capabilities, content.state, audio, drain/handoff. No permanent user recordings/text logs.

## Task 4 — device adapter and Opus coordinates

**Files:** Create `internal/devicebridge/`, `cmd/livekit-device-gateway/`; extend `internal/audio/opus.go` with rate-specific decoder/lookahead helpers and tests. Add standalone service template; do not change default cmd/gateway/8000.

- [x] RED real WebSocket protocol with fake protected PCM backend: hello/capability validation, binary Opus input, tts identity mapping, audio/end/drain/handoff, wrong session and stale receipts, staged cancellation and closure.
- [x] Go bridge listens loopback18020, upstream loopback18010 drain mode. One session, bounded queues/writes/read sizes. Ephemeral120s single-use native tickets via SSH-authorized local pair endpoint; backend token/model secrets remain server-only.
- [x] Input16k mono negotiated20/40/60ms Opus→16k PCM<=20ms backend chunks; output24k PCM→60ms Opus. At every seal flush already admitted PCM plus required zero tail, track encoder lookahead/padding. Device played position maps conservatively to valid backend samples `clamp(device_played-lookahead,0,valid_samples)`. Respect raw+codec allowances; original decoder16k defaults unchanged.
- [x] Mac basis coreaudio_playback only for negotiated native identity, future hand watch i2s_dma only for corresponding capability; do not fake I2S from Mac. Require drain+staging feature for this dev route, legacy firmware rejected explicitly rather than silently clearing audio.
- [x] Run server Go race/vet/build; no runtime changes to original production gateway.

## Task 5 — Mac native client

**Files:** Create `tools/native-voice/` Swift source, C Opus bridge/header, build/open scripts, Info.plist, README.

- [x] Client uses Apple AVAudioEngine/CoreAudio and existing libopus; no browser, backend, or model keys. Single wake/start then continuous full-duplex microphone; apply native voice processing when supported and report actual capability. Build client on Mac only as required for client platform.
- [x] WebSocket speaks device protocol: opus16k up,24k down, metadata identity per receive unit. Decode/play old current separately from compressed staged new queue; do not reset old audio for tts start of staged new output.
- [x] Native playback completion/progress from CoreAudio/player clock and completion callbacks, not queue enqueue or sleep. Account output presentation latency; report unknown if no trustworthy coordinate. Current output bounded by server credits, staged queue bounded and authenticated handoff release after old completed, no extra server roundtrip. Raw real audio not saved.
- [x] Command-line controlled-tone/playback self-test mode uses actual same playback code and no microphone, produces metadata evidence only. Synthetic input WAV mode exercises real codec/server path distinctly from human microphone. Privacy permission/capture disposal verified; text/token not printed in metadata logs.

## Task 6 — integration, latency, release

- [x] All server Python/Node/SDK regressions + Go race/vet/build, pinned dependency check. Native client compile/link and actual CoreAudio playback tests. No browser test for main acceptance.
- [x] Server fixed3-sentence drain/handoff/resume probe with Opus coordinate boundaries, drop/stale receipt and wrong generation cases. Mac native actual output fixed-fixture self-test: admitted old samples all play, staged new waits, first new output after old final, no overlap, unit resumed from final prefix.
- [ ] Real ASR/LLM/TTS via native synthetic input, story→new question→resume at original text, repeated interruption. Reuse existing private synthetic fixtures; new paid calls bounded and intent-audited, no unknown auto retry.
- [ ] Record speech onset, server seal, native drain, ASR final, first substantive response ready/start, underrun and outstanding samples on aligned clocks. Targets reported separately from measurements; report latency sample size and cloud/provider failures. Do not call synthetic speech human microphone or claim M5/I2S acoustic acceptance.
- [ ] Spec and quality final review, docs/handoff update, main commit/push, server tested-source hash equality, isolated new services health. Keep old8000 unchanged and SSH tunnel alive. Hardware firmware/physical M5 acceptance requires available device and is not fabricated.

## Execution status — 2026-10-24

Tasks1–5 implemented and separately spec/quality reviewed. Server109 Python +25 Node +3 SDK tests and full Go race/vet/build passed. Native build/ledger/fixture proof/SSH cleanup and actual CoreAudio self-test passed. Real short3-sentence cloud native flow completed with same main:1/revision1 resumed_from0 and all finals ACKed. Long15-unit cloud test preserved original/resume but failed its90s fixture budget after8 confirmations; retained as failure, not auto-retried. See [verification](../../verification/2026-10-24-native-drain-handoff.md).

Task6 remaining acceptance: real microphone/speaker acoustic AEC, at least10 varied interruption rounds including two consecutive interruptions, imposed RTT/jitter matrix, calibrated timing/P50/P95, M5/I2S. These are not claimed complete by a single short synthetic flow. Cross-wake Go persistence remains a later phase. Source release/deployment identity is recorded with final main synchronization.
