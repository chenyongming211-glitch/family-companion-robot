import Foundation

var failures = 0
func check(_ ok: Bool, _ label: String) {
    if !ok { fputs("FAIL \(label)\n", stderr); failures += 1 }
}
func rejects(_ label: String, _ body: () throws -> Void) {
    do { try body(); check(false, label) } catch { }
}

let clip = String(repeating: "a", count: 32)
var state = CaptureState()
check(state.phase == .idle, "initial idle; no automatic permission or connection")
check(state.acceptReady(clip: clip, generation: 0) == false, "ready without manual action rejected")
let generation = state.beginManual(clip: clip)!
check(state.beginManual(clip: clip) == nil, "one busy capture")
check(!state.acceptReady(clip: String(repeating: "b", count: 32), generation: generation), "ready wrong clip")
check(!state.permissionGranted(generation: generation), "permission before ready rejected")
check(state.acceptReady(clip: clip, generation: generation), "ready allows permission request")
check(state.permissionGranted(generation: generation), "manual ready permission gate")
check(state.captureCompleted(generation: generation), "capture must finish before send")
check(!state.stored(clip: "wrong", generation: generation), "stored wrong clip ignored")
check(state.stored(clip: clip, generation: generation), "matching stored confirmation")
check(state.saved == 1 && state.phase == .idle, "saved count only after confirmation")
let old = state.beginManual(clip: clip)!
state.cancel()
check(!state.acceptReady(clip: clip, generation: old), "cancel invalidates late ready")
let current = state.beginManual(clip: clip)!
check(!state.fail(generation: old), "old failure cannot cancel new attempt")
check(state.fail(generation: current) && state.phase == .idle, "current failure frees next manual attempt")
var capped = CaptureState(maxClips: 1)
let cappedGeneration = capped.beginManual(clip: clip)!
_ = capped.acceptReady(clip: clip, generation: cappedGeneration)
_ = capped.permissionGranted(generation: cappedGeneration)
_ = capped.captureCompleted(generation: cappedGeneration)
_ = capped.stored(clip: clip, generation: cappedGeneration)
check(capped.beginManual(clip: clip) == nil, "saved prompt limit prevents automatic cycling or further recording")

do {
    let collector = try NativePCMCollector(sampleRate: 8000, channels: 2)
    try collector.appendPlanar([[0.25, -0.5], [0.75, 1]], frameCount: 2)
    check(collector.bytes == Data([0,0,128,62, 0,0,64,63, 0,0,0,191, 0,0,128,63]), "planar all channels exact f32 little endian interleave")
    check(collector.frames == 2, "frames distinct from samples")
    rejects("insufficient frames") { _ = try collector.finish() }
    let interleaved = try NativePCMCollector(sampleRate: 8000, channels: 2)
    try interleaved.appendInterleaved([0.25,0.75,-0.5,1], frameCount: 2)
    check(interleaved.bytes == collector.bytes, "interleaved stride preserved")
    let full = try NativePCMCollector(sampleRate: 8000, channels: 1)
    try full.appendInterleaved(Array(repeating: 0, count: 32005), frameCount: 32005)
    let record = try full.finish()
    check(record.pcm.count == 128000 && record.header.frames == 32000, "exact four seconds truncates last buffer only")
    let framed = try record.framed()
    check(framed.count > 128004 && framed.count < 130052, "bounded frame header")
    for (rate, channels) in [(7999.0,1),(96001.0,1),(8000.5,1),(8000.0,0),(8000.0,9)] {
        rejects("invalid native format") { _ = try NativePCMCollector(sampleRate: rate, channels: channels) }
    }
    rejects("nonfinite value") { try interleaved.appendInterleaved([Float.nan,0], frameCount: 1) }
    rejects("overscale value") { try interleaved.appendInterleaved([17,0], frameCount: 1) }
} catch { check(false, "PCM unexpected error") }

do {
    let config = try CaptureConfig(environment: ["COMPUTER_CAPTURE_SSH_TARGET":"root@example.com", "COMPUTER_CAPTURE_SSH_KEY":"/tmp/key", "COMPUTER_CAPTURE_RECEIVER":"/tmp/receiver.py", "COMPUTER_CAPTURE_ROOT":"/tmp/private", "COMPUTER_CAPTURE_PARTICIPANT":"M01"])
    check(config.arguments(clip: clip).contains("StrictHostKeyChecking=yes"), "strict SSH host checking")
    check(config.arguments(clip: clip).contains("ControlMaster=no"), "owned SSH does not multiplex")
    rejects("missing config") { _ = try CaptureConfig(environment: [:]) }
    rejects("shell injection target") { _ = try CaptureConfig(environment: ["COMPUTER_CAPTURE_SSH_TARGET":"root@bad;echo x"]) }
    let ready = Data("{\"schema_version\":1,\"event\":\"ready\",\"clip\":\"\(clip)\"}\n".utf8)
    var lines = ReplyParser(clip: clip)
    check(try lines.append(ready.prefix(7)).isEmpty, "split ready waits for newline")
    check(try lines.append(ready.dropFirst(7)) == [.ready], "split ready reassembled")
    check(try lines.append(Data("{\"schema_version\":1,\"event\":\"stored\",\"clip\":\"\(clip)\",\"samples\":64000,\"duration_seconds\":4,\"rms\":0.1}\n".utf8)) == [.stored], "fixed stored response")
    rejects("duplicate ready") { _ = try lines.append(ready) }
    var wrong = ReplyParser(clip: clip)
    rejects("wrong ready id") { _ = try wrong.append(Data("{\"schema_version\":1,\"event\":\"ready\",\"clip\":\"bad\"}\n".utf8)) }
    var oversized = ReplyParser(clip: clip)
    rejects("bounded server output") { _ = try oversized.append(Data(repeating: 65, count: 4097)) }
    let missing = ReplyParser(clip: clip)
    rejects("EOF before stored") { try missing.finish() }
} catch { check(false, "transport unexpected error") }

// Real owned subprocesses with fixed synthetic protocol replies; no audio devices.
func pump(until done: () -> Bool, timeout: Double = 4) {
    let end = Date().addingTimeInterval(timeout)
    while !done() && Date() < end { RunLoop.current.run(until: Date().addingTimeInterval(0.01)) }
}
do {
    let config = try CaptureConfig(environment: ["COMPUTER_CAPTURE_SSH_TARGET":"root@example.com", "COMPUTER_CAPTURE_SSH_KEY":"/tmp/key", "COMPUTER_CAPTURE_RECEIVER":"/tmp/receiver.py", "COMPUTER_CAPTURE_ROOT":"/tmp/private"])
    let ready = "{\"schema_version\":1,\"event\":\"ready\",\"clip\":\"\(clip)\"}"
    let stored = "{\"schema_version\":1,\"event\":\"stored\",\"clip\":\"\(clip)\",\"samples\":64000,\"duration_seconds\":4,\"rms\":0}"
    let collector = try NativePCMCollector(sampleRate: 8000, channels: 1)
    try collector.appendInterleaved(Array(repeating: 0, count: 32000), frameCount: 32000)
    let record = try collector.finish()
    let normal = CaptureTransport(clip: clip)
    var normalReady = false, normalStored = false, normalFailed = false
    let split = ready.index(ready.startIndex, offsetBy: 7)
    let script = "printf '%s' '\(ready[..<split])'; /bin/sleep 0.03; printf '%s\\n' '\(ready[split...])'; /bin/cat >/dev/null; printf '%s\\n' '\(stored)'"
    normal.start(config: config, executable: "/bin/sh", arguments: ["-c", script]) { event in
        switch event {
        case .ready: normalReady = true; normal.send(record)
        case .stored: normalStored = true
        case .failed: normalFailed = true
        }
    }
    pump(until: { normalStored || normalFailed })
    check(normalReady && normalStored && !normalFailed, "owned process split ready -> send -> stored -> reaped")
    for (label, fixture, timeout) in [("auth error", "exit 255", 1.0), ("EOF missing stored", "printf '%s\\n' '\(ready)'", 1.0), ("ready timeout", "exec /bin/sleep 30", 0.05)] {
        let transport = CaptureTransport(clip: clip)
        var failed = false
        transport.start(config: config, executable: "/bin/sh", arguments: ["-c", fixture], timeout: timeout) { event in
            if case .failed = event { failed = true }
        }
        pump(until: { failed })
        check(failed, "\(label) reports failure after child cleanup")
    }
    let pidURL = FileManager.default.temporaryDirectory.appendingPathComponent("computer-capture-sdk-\(UUID().uuidString).pid")
    defer { try? FileManager.default.removeItem(at: pidURL) }
    let cancel = CaptureTransport(clip: clip)
    var readyToCancel = false, firstCancelled = false, secondCancelled = false
    cancel.start(config: config, executable: "/bin/sh", arguments: ["-c", "printf '%s' $$ > '\(pidURL.path)'; printf '%s\\n' '\(ready)'; trap '' TERM; exec /bin/sleep 30"]) { event in
        if case .ready = event { readyToCancel = true }
    }
    pump(until: { readyToCancel })
    check(readyToCancel, "cancel fixture reaches ready")
    cancel.cancel { firstCancelled = true }
    cancel.cancel { secondCancelled = true }
    pump(until: { firstCancelled && secondCancelled })
    check(firstCancelled && secondCancelled, "concurrent cancel and close both complete")
    if let pidString = try? String(contentsOf: pidURL, encoding: .utf8), let pid = Int32(pidString) {
        check(kill(pid, 0) == -1 && errno == ESRCH, "cancel killed and reaped only owned SSH child")
    } else { check(false, "child PID available") }
} catch { check(false, "process lifecycle unexpected error") }

if failures == 0 { print("PASS computer capture SDK checks (no microphone)") }
exit(failures == 0 ? 0 : 1)
