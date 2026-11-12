# Online Music Implementation Plan

> **For agentic workers:** Use subagent-driven-development for scoped implementation and fresh review. Main only, root alone stages/commits. Existing user authorization covers the four-step flow and online source choice; do not request it again.

**Goal:** Voice request singer/title, resolve a real complete public online song, stream over existing Wi-Fi playback, stop or select next.
**Architecture:** internal/music owns bounded fixed-provider HTTPS+FFmpeg; realtimelive owns semantic routing/Session queue; existing output path owns generation,600ms receipts and cancellation. No new board player/voice provider or persistent service.
**Tech Stack:** Existing Go/std library, Native function calls, installed FFmpeg/libopus; no new Go dependency.

## Global Constraints

- Develop/commit/push main, no branch/worktree. Mac code/Git only; runtime deps/tests/build/deploy cloud. No music files, credentials, raw audio or firmware in Git.
- Supported hardware Waveshare ESP32-S3 2.16 only. Shared logic single implementation; preserve current cbad clock/diagnostics/icons and actual preferences. No M5/C3 release gates.
- Real source only, no product mock/seed/demo/fallback. Tests may use isolated synthetic input.
- Follow exact network/resource/media/permission limits and behavior in ../specs/2026-11-12-online-music-design.md; changes to these constants/interfaces must be coordinated before overlapping edits.
- Only task-owned source/staging. Cloud root /home/ubuntu/music-playback-20261112; control SSH /tmp/family-management-preview-chen/control to ubuntu@82.156.48.111, sudo-n available. Never print/copy private config/env values. Current Voice1608608/261a87d and manager2273712/3f071303; preserve runtime until root release.
- Root is sole production/deploy operator and Git committer. Agents never deploy/restart/auth production/flash/USB, or clear shared caches/data. Use one small Go source staging; no SDK copies.

### Task 1: Actual online source and streaming

**Files:** Create internal/music/{source.go,stream.go,source_test.go,stream_test.go} only; no other product files.
**Interfaces:** `Query{Title,Artist string}`; `Track{ID int64,Title,Artist string,Duration time.Duration}`; `Resolved` opaque external resource with exported `Track Track` and private validated source details. `New() *Catalog`; `(*Catalog).Search(context.Context,Query)([]Track,error)`; `Resolve(context.Context,Track)(Resolved,error)`; `Stream(context.Context,Resolved,func([]byte)error)(int64,error)` requires caller context deadline <=now+Track.Duration+30s and callback honors that same context; returns decoded PCM sample count, never device playback proof. Catalog immutable/concurrency-safe. Errors fixed tags via exported `ErrUnavailable`, `ErrInvalid`, `ErrSource` (errors.Is-compatible); no URL/body in error. `Resolve` must reject invalid duration/size/preview/type/ID and use API no login. Tests can inject unexported transport/dial/command seams.

- [x] Add meaningful fixtures for query/matching artist/title, proper metadata, unavailable/preview, ID mismatch, limits, URL allowlist/private DNS and redirect rejection. New package tests fail before implementation.
- [x] Implement fixed search GET /api/search/get and resolve /api/song/enhance/player/url with ids/br128000; normalize to Track, retain at most20 matched candidates. No cookies/keys or third-party unlock service.
- [x] Add real FFmpeg synthetic MP3 fixture tests for24k mono chunks<=2880, truncated bytes, excessive/deceptive duration, consumer backpressure/cancel and process reap. RED then implement input via secured HTTP body->pipe and fixed args, bounded queues/stderr, timeout/cancellation.
- [x] Cloud go test -race ./internal/music, vet; real song5255640 probe from spec. Root may run actual complete344s source decode separately. Report commands/logs/hash/filelist, self-review, no Git or prod action.

### Task 2: Reuse output stream for media safely

**Files:** Modify internal/realtimelive/session.go,memory.go,session_expression.go if needed; add media_playback_test.go. Do not edit root's new music*.go/config.go/main.go or task1 files.
**Consumes:** Root `func(s *Session) routeMusic(rt *liveTurn,history string)(musicRoute,error)`, `type musicRoute struct{Handled bool;Duration time.Duration;Guidance string;Stream func(context.Context,func(string,[]byte)error)error}` defined in music.go. Zero route preserves ordinary behavior. Root `type sessionMusic` used by `Session.music *sessionMusic`; root creates it when cfg.Music!=nil. Add `liveTurn.media bool` under Session.mu. Root sets media=true before Stream is returned. No firmware/wire format changes.
**Produces:** Existing Session.reply routes once after settle: call routeMusic; if Handled skip unrelated memory/control/expression routing and append Guidance to normal TTS prompt if Stream=nil. If Stream!=nil use it instead of s.generate. Media producerCtx MUST use context.WithTimeout(rt.ctx,route.Duration+30s), shared by Stream and put/emit; ordinary voice remains WithCancel. Reject missing/invalid media duration before starting. Existing packet encoding, physical CanSend/interrupt/seal intact; media queue10frames instead of1002. Media does not create/feed OutputASR or send expression; memory snapshots exclude media turns. Music stream emits ready/audio only, Generated remains empty. `release` must handle nil asr. Ordinary reply tests preserve behavior.

- [x] RED tests actual Session reply with injected route/stream and synthetic wire/source: media physical completion/backpressure, cancel producer and release/refeed, no ASR/lyrics/Generated/context/expressions, sourceerror, normal nonmedia still works. Coordinate test seam minimal with root.
- [x] Minimal factoring of existing producer/source path, skip ASR/expression/media memory; do not copy whole reply or hardware player.
- [x] Cloud focused race and affected ordinary PCM/receipt/memory/expression tests; vet/build after root interfaces exist. Report exact tests/source/hash, self-review/no Git or prod actions.

### Task 3: Root semantic route, configuration and integration

**Files:** Create internal/realtimelive/music.go,music_planner.go,music_test.go,music_planner_test.go; modify config.go/server.go/cmd/realtime-output-live/main.go only as needed. Coordinate root changes before task2 needs fields; root alone commit all owned tasks serially.
**Interfaces:** cfg.Music *music.Catalog (nil off); CLI --music-online defaultfalse invokes music.New; Session.music *sessionMusic perSession owned by Session.mu, no global device queue. musicRoute contract Task2. Native.PlanMusic fixed action enum none/play/stop/next/clarify with title/artist strings, duplicate/extra/shape reject,3s budget, current request+played context only. Root uses existing strict JSON/native tool event validation.

- [x] Plan tests for quoted/negated/discussed music=>none, explicit artist/title/singer-only=>play, next/stop only currentSession, ambiguity=>clarify, malformed planner zero action. Use isolated native provider fixtures, not network model as deterministic unit test.
- [x] routeMusic waits existing transcript, calls bounded planner, Search/Resolve up to5 candidates within15s; perSession candidate order/current index; next excludes prior selected IDs; noresult/failure fixed structured Guidance for ordinary TTS. Source Stream closure calls emit ready/audio and catalog.Stream with same rt.ctx. Set media under lock only when actual resolved source selected, record music_selected metadata without URL.
- [x] Stop inspect prior media turn after existing settle/DrainDone; trustworthy final receipt needed for verified stop; no current item or unconfirmed drain gets accurate Guidance. Existing speech_started cancels stream; no old data admitted after next generation.
- [x] Verify config copy/facts and CLI opt-in. Cloud isolated assertions cover snapshots/facts; CLI default false is statically checked, and candidate --help is a separate smoke check. Full related package functional+focused race on cloud, not giant repeated race under2CPU load. Fresh task reviews + broad final architecture review before release.

### Task 4: Actual-source build, acceptance and delivery

**Files:** docs verification/handoff/current plan; task-private operators and cloud fixtures.

- [x] Freeze exact selected public inputs and commit proof; derive candidate from deployed Voice source, overlay only reviewed changed/new paths, preserve all unrelated actual differences. Source package imports internal/music, installedffmpeg only; no fresh SDK/dependency copies.
- [x] Actual complete-song decode verifies344.24s/audio byte count, not device output. Isolated real protocol exercise song stream/receipts/stop/next/ordinary reply restoration; synthetic receipts never field PASS.
- [x] Independent final review and guarded single Voice-only release under original task authorization, strict idle and before/after config/records/profile/authority/otherprocesses/Caddy/firewall protection. Preserve originals; no automatic retry/restore/data rollback. Current role of public API versus official licensed service made explicit.
- [ ] Real song playback requires user present/ready, no autoaudible playback during absence. If user unavailable, finish source/tests/build/deployment and mark fullsong acoustic/stop/next pending; no firmware flash unless actual gap establishes need and concrete reviewed artifact ready.
- [x] Update source/test/deployed/field boundaries, directmain push, exactremote check, clear only owned temporary resources/artifactduplicates, preserve source/evidence and constrained disk.

## Current execution state

产品代码冻结62c9f3f；Task1、Task2、Task3规格/质量审查均通过，分类前记忆窗口另经RED→修复→race→复审关闭。源解码、实际模型13例、隔离真实音源到Session整曲和普通语音恢复均有云端证据。实际部署来源按14路径差异构建、审查并发布（Voice2766139/SHA d550ee58），保护现有未部署差异；发布及真人实播结果以[验证记录](../../verification/2026-11-12-online-music.md)为准。未把合成设备回执当作现场通过。

root单次发布保护检查通过，管理/Caddy/防火墙/配置/资料不变，当前固件未刷写。真人整曲/停止/切歌及对话/钟表恢复仍待现场验收；已发起可用性询问，不因未答复而自动播放或标记通过。原审计事务仍未关闭。

现场首轮用户确认“歌曲正常，停止也正常”；两次i2s_dma停止终态63.18s/11.76s。播放和语音停止通过，整曲自然结束/切歌/普通对话/钟表仍待验，继续核对现场日志。

语音“下一首”两次资源ID转换及实际I2S均已核对，用户确认切到另一首，切歌通过。后续缺口：普通对话尚无当前/上次选曲元数据，问谁唱时无法回答；与歌词转写分开补播放器事实上下文。自然整曲/恢复仍观察，不因停止终态提前判通过。

最终筛选：无自然music_source_end complete=true，最长本轮后段周深曲目125.1s后被新有效语音打断；原voice策略会因“嗯”等短语先停歌，未做背景保留/自动续播，作为音乐交互后续边界。普通回复I2S已观察，听感与钟表/自然结束完整恢复未确认。观察器已关，未操作设备/服务。

当前曲目查询缺口已修复d0c03da并发布r2 Voice2804087/f5dfaf86，实际来源最终功能506PASS/2旧opt-in skip、相关race216PASS，模型查询4问法/3状态通过，保护发布/健康通过。前文缺元数据为首次现场失败记录，当前已提供事实上下文；本连接选曲之外不虚构记录。现场查询待用户反馈，自动续播和语义保留音乐未改。
