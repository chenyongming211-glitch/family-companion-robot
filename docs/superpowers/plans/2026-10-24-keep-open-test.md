# Keep-open native voice test implementation

> For agentic workers: use subagent-driven-development for the isolated Go bridge; root integrates Python and native client. TDD, spec/quality review, main-only, no worktree/PR. User explicitly requests a window/session that remains open until manually closed; no repeat approval needed.

**Goal:** 原生测试窗口长期保留，开始后不因空闲、8轮或15分钟自动结束；关闭窗口停止收音及连接。出错保留窗口，可手动重连。

**Architecture:** `VOICE_TEST_KEEP_OPEN=1`仅启用独立loopback设备测试服务。Python drain取消用户闲置/轮次/总时长预算，Go取消会话deadline，native遵守ready.manual_close并移除本地15分钟timer。短期票据保持120秒单次使用，改在点击开始时通过现有SSH转发取得；每次重连创建新会话，不自动恢复旧音频。浏览器兼容模式、原8000、单会话、身份校验、写入/真实回执超时及单句/PCM限额保留。长发言超过25秒只丢弃本段，等待停顿后继续收音，不退出。

**Tech:** existing Python/LiveKit/aiohttp, Go/Gorilla, Swift/AppKit/AVAudioEngine. Backend tests/services on cloud; Mac only edits/Git/native tests.

- [x] Go: Config.KeepOpen + env; context has no session deadline only in keep-open mode; retain frontend wire-idle timeout and safe Close joins. Test shortened legacy timeout vs keep-open connection, manual disconnect releases lease. No edits outside Go bridge/cmd.
- [x] Python: Settings flag, drain-only unlimited ready/timeout; max_turns/idle disabled in this mode. SDK tests >8 turns and tiny idle window, defaults unchanged. Long speech discard until next onset; no overlong ASR; preserve session. Rolling model context and confirmed history bounded, active original/main+side unaffected.
- [x] Native: pair-on-start over derived loopback HTTP via already-authorized SSH tunnel; no ticket argv/files; fixtures retain stdin. GUI remains open after stop/error; single button can start/reconnect. Keep-open resource lifetime only if server advertises manual_close; closing still disposes mic/player/WS. Build + ledger/fixture/mic/launcher/selftest.
- [x] Deploy: explicit keep-open env on both isolated services; all tests on server; no production8000 change. Verify native window and real session past old idle/turn limits, manual exit releases lease. Commit/push main, source sync, leave window foreground and don't close it after handing off.
