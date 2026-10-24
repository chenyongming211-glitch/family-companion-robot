import Foundation
import AVFoundation

// Mutable transport state is confined to queue; callback installation precedes start.
final class VoiceClient: @unchecked Sendable {
    let queue = DispatchQueue(label: "family.native.voice")
    let url: URL
    private var ticket: String
    let microphone: Bool
    let fixturePaths: [String]
    let pairOnStart: Bool, keepOpen: Bool
    var status: ((String) -> Void)?
    var ended: ((Bool) -> Void)?
    private var session: URLSession?
    private var socket: URLSessionWebSocketTask?
    private var audio: AudioPipeline?
    private var player: DrainPlayer?
    private var inputEncoder: OpusEncoder?
    private var inputPCM: [Int16] = []
    private var outbound: [URLSessionWebSocketTask.Message] = []
    private var writing = false, writeSequence = 0, closed = false, ready = false
    private var fixture: FixtureInput?
    private var inputFrames = 0, inputSamples = 0, inputWindowSamples = 0, inputPeak = 0
    private var inputEnergy = 0.0, inputLoggedAt = ProcessInfo.processInfo.systemUptime

    init(url: URL, ticket: String, fixturePaths: [String] = [], pairOnStart: Bool = false, keepOpen: Bool = false) {
        self.url = url; self.ticket = ticket; self.fixturePaths = fixturePaths
        self.pairOnStart = pairOnStart; self.keepOpen = keepOpen
        self.microphone = fixturePaths.isEmpty
    }
    func start() { queue.async { if self.pairOnStart { self.pair() } else { self.connect() } } }
    private func transport() -> URLSession {
        if let session { return session }
        let configuration = URLSessionConfiguration.ephemeral
        configuration.timeoutIntervalForRequest = 5
        configuration.timeoutIntervalForResource = keepOpen ? 0 : 15 * 60
        let session = URLSession(configuration: configuration)
        self.session = session
        return session
    }
    private func pair() {
        guard !closed, let endpoint = nativePairingEndpoint(url.absoluteString) else { fail("invalid_pair_endpoint"); return }
        status?("正在连接测试服务…")
        var request = URLRequest(url: endpoint)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = Data("{\"device_id\":\"native-mac\"}".utf8)
        let session = transport()
        Task { [weak self] in
            do {
                let (data, response) = try await session.data(for: request)
                guard (response as? HTTPURLResponse)?.statusCode == 200 else { throw ClientFailure("pairing_unavailable") }
                let token = try pairedNativeTicket(data)
                self?.queue.async { [weak self] in
                    guard let self, !self.closed else { return }
                    self.ticket = token; self.connect()
                }
            } catch { self?.queue.async { self?.fail("pairing_unavailable") } }
        }
    }
    private func connect() {
        guard !closed else { return }
        status?("正在连接云端设备桥…")
        let session = transport()
        var request = URLRequest(url: url)
        request.setValue("Bearer " + ticket, forHTTPHeaderField: "Authorization")
        request.setValue("native-mac", forHTTPHeaderField: "Device-Id")
        request.setValue("1", forHTTPHeaderField: "Protocol-Version")
        ticket.removeAll(keepingCapacity: false)
        let socket = session.webSocketTask(with: request)
        socket.maximumMessageSize = 16_384
        self.socket = socket
        socket.resume()
        send(["type": "hello", "version": 1, "transport": "websocket",
              "audio_params": ["format": "opus", "sample_rate": 16000, "channels": 1, "frame_duration": 20],
              "features": ["drain": true, "staging": true, "coreaudio_playback": true]])
        receive()
        queue.asyncAfter(deadline: .now() + 5) { [weak self] in
            guard let self, !self.closed, !self.ready else { return }
            self.fail("ready_timeout")
        }
        if !keepOpen { queue.asyncAfter(deadline: .now() + 15 * 60) { [weak self] in self?.stopInternal(success: true) } }
    }
    private func receive() {
        guard !closed, let socket else { return }
        Task { [weak self] in
            do {
                let message = try await socket.receive()
                self?.queue.async { [weak self] in
                    guard let self, !self.closed else { return }
                    do { try self.message(message); self.receive() }
                    catch { self.fail((error as? ClientFailure)?.description ?? "invalid_server_message") }
                }
            } catch { self?.queue.async { self?.fail("websocket_receive") } }
        }
    }
    private func message(_ message: URLSessionWebSocketTask.Message) throws {
        switch message {
        case .data(let packet):
            guard ready, let player else { throw ClientFailure("binary_before_ready") }
            try player.binary(packet)
        case .string(let text):
            guard text.utf8.count <= 16_384, let data = text.data(using: .utf8),
                  let e = try JSONSerialization.jsonObject(with: data) as? [String: Any],
                  let type = e["type"] as? String else { throw ClientFailure("invalid_json") }
            if type == "hello" {
                guard player == nil, let id = e["session_id"] as? String, !id.isEmpty,
                      e["playback_basis"] as? String == "coreaudio_playback",
                      let params = e["audio_params"] as? [String: Any], params["format"] as? String == "opus",
                      try integer(params, "sample_rate") == 24000, try integer(params, "channels") == 1,
                      try integer(params, "frame_duration") == 60,
                      let features = e["features"] as? [String: Bool], features["drain"] == true,
                      features["staging"] == true, features["coreaudio_playback"] == true else { throw ClientFailure("unsupported_server_hello") }
                let audio = AudioPipeline(queue: queue)
                let player = DrainPlayer(session: id, audio: audio, queue: queue)
                self.audio = audio; self.player = player
                try player.event(e)
                player.send = { [weak self] in self?.send($0) }
                player.failure = { [weak self] in self?.fail($0) }
                player.progress = { [weak self] id, samples in self?.fixture?.progress(id: id, samples: samples) }
                player.acknowledged = { [weak self] _ in self?.fixture?.checkCompleted() }
                inputEncoder = try OpusEncoder(rate: 16000)
                metadata("session.hello", ["input_rate": 16000, "output_rate": 24000, "frame_ms": 20])
            } else if type == "ready" {
                guard !ready, let audio, let player, e["session_id"] as? String == player.session,
                      e["mode"] as? String == "drain", try integer(e, "input_rate") == 16000,
                      try integer(e, "output_rate") == 24000 else { throw ClientFailure("invalid_ready") }
                try player.event(e)
                if keepOpen && e["manual_close"] as? Bool != true { throw ClientFailure("keep_open_unavailable") }
                try audio.start(microphone: microphone)
                ready = true
                audio.onPCM = { [weak self] in self?.input($0) }
                if microphone {
                    status?(keepOpen ? "对话中 · 可持续测试，结束时关闭窗口" : "对话中 · 连续收音")
                } else {
                    let fixture = try FixtureInput(paths: fixturePaths, queue: queue, player: player)
                    self.fixture = fixture
                    fixture.sendPCM = { [weak self] in self?.input($0) }
                    fixture.done = { [weak self] success in self?.stopInternal(success: success) }
                    fixture.start()
                }
                metadata("session.ready", ["microphone": microphone])
            } else {
                guard ready, let player else { throw ClientFailure("event_before_ready") }
                if type == "error" { throw ClientFailure("server_error") }
                try player.event(e)
                try fixture?.event(e)
                if type == "output.stop" || type == "sleep" {
                    metadata(type == "sleep" ? "session.standby" : "session.server_stop", ["fixture_incomplete": !fixturePaths.isEmpty])
                    stopInternal(success: fixturePaths.isEmpty)
                    return
                }
                if type == "content.state" {
                    let safe = ["task_id", "revision", "kind", "total", "confirmed_count", "next", "state"]
                    metadata("content.state", Dictionary(uniqueKeysWithValues: safe.compactMap { key in e[key].map { (key, $0) } }))
                } else if type == "content.action" {
                    metadata("content.action", ["action": e["action"] as? String ?? "", "target": e["target"] as? String ?? ""])
                } else if type == "input.limit" {
                    status?("这句话较长，请稍停一下再继续；连接仍保持。")
                }
                // ASR, replies and content.snapshot payloads are deliberately never logged.
            }
        @unknown default: throw ClientFailure("unknown_websocket_message")
        }
    }
    private func input(_ samples: [Int16]) {
        guard ready, !closed, let inputEncoder else { return }
        if microphone {
            inputSamples += samples.count; inputWindowSamples += samples.count
            for sample in samples {
                inputPeak = max(inputPeak, abs(Int(sample)))
                inputEnergy += Double(sample) * Double(sample)
            }
        }
        inputPCM.append(contentsOf: samples)
        guard inputPCM.count <= 6400 else { fail("input_pcm_overflow"); return }
        do {
            while inputPCM.count >= 320 {
                let frame = Array(inputPCM.prefix(320)); inputPCM.removeFirst(320)
                enqueue(.data(try inputEncoder.encode(frame)))
                inputFrames += 1
            }
        } catch { fail("input_opus_encode") }
        let now = ProcessInfo.processInfo.systemUptime
        if microphone && now - inputLoggedAt >= 1 {
            metadata("microphone.input", ["pcm_samples": inputSamples, "opus_frames": inputFrames,
                                           "peak": inputPeak, "rms": inputWindowSamples > 0 ? sqrt(inputEnergy / Double(inputWindowSamples)) : 0,
                                           "pending_writes": outbound.count])
            inputLoggedAt = now; inputPeak = 0; inputEnergy = 0; inputWindowSamples = 0
        }
    }
    private func send(_ event: [String: Any]) {
        guard let data = try? JSONSerialization.data(withJSONObject: event), data.count <= 16_384,
              let text = String(data: data, encoding: .utf8) else { fail("outgoing_json"); return }
        enqueue(.string(text))
    }
    private func enqueue(_ message: URLSessionWebSocketTask.Message) {
        guard !closed else { return }
        guard outbound.count < 8 else { fail("websocket_write_queue_overflow"); return }
        outbound.append(message); writeNext()
    }
    private func writeNext() {
        guard !closed, !writing, !outbound.isEmpty, let socket else { return }
        writing = true; writeSequence += 1
        let sequence = writeSequence, message = outbound.removeFirst()
        Task { [weak self] in
            do {
                try await socket.send(message)
                self?.queue.async { [weak self] in
                    guard let self, !self.closed, self.writeSequence == sequence else { return }
                    self.writing = false; self.writeNext()
                }
            } catch { self?.queue.async { self?.fail("websocket_send") } }
        }
        queue.asyncAfter(deadline: .now() + .milliseconds(250)) { [weak self] in
            guard let self, !self.closed, self.writing, self.writeSequence == sequence else { return }
            self.fail("websocket_write_timeout")
        }
    }
    func stop() { queue.async { self.stopInternal(success: true) } }
    private func fail(_ code: String) {
        guard !closed else { return }
        metadata("session.failure", ["code": code]); status?("连接结束：" + code)
        stopInternal(success: false)
    }
    private func stopInternal(success: Bool) {
        guard !closed else { return }
        // Explicit user stop may end playback; ordinary speech never calls this path.
        if ready { send(["type": "listen", "state": "stop"]) }
        closed = true; ready = false
        fixture?.stop(); fixture = nil
        player?.stop(); audio?.stop()
        inputPCM.removeAll(keepingCapacity: false); inputEncoder = nil
        outbound.removeAll(); ticket.removeAll(keepingCapacity: false)
        socket?.cancel(with: .normalClosure, reason: nil)
        session?.invalidateAndCancel(); socket = nil; session = nil
        metadata("session.closed", ["success": success])
        ended?(success)
    }
}

final class FixtureInput {
    // Metadata-only evidence. A completed replacement cannot satisfy this proof.
    final class Proof {
        private(set) var originalID: String?
        private(set) var originalRevision: Int?
        private(set) var originalTotal: Int?
        private(set) var firstMainBlock: Int?
        private(set) var resumedFrom: Int?
        private(set) var interrupted = false, pausedObserved = false, sideCompleted = false
        private var checkpoint: Int?, sideID: String?
        private var lastConfirmed = 0
        private var resumeAction = false, resumedPlaying = false, resumedHandoff = false, completed = false
        var verified: Bool { interrupted && pausedObserved && sideCompleted && resumeAction && resumedPlaying && resumedHandoff && completed }

        func interrupt() throws {
            guard !interrupted, originalID != nil, firstMainBlock != nil else { throw ClientFailure("fixture_missing_original_identity") }
            interrupted = true
        }
        func observe(_ e: [String: Any]) throws {
            switch e["type"] as? String {
            case "content.state":
                guard let task = e["task_id"] as? String, let state = e["state"] as? String else { throw ClientFailure("fixture_content_identity") }
                if task.hasPrefix("main:") {
                    let revision = try integer(e, "revision"), total = try integer(e, "total")
                    let next = try integer(e, "next"), confirmed = try integer(e, "confirmed_count")
                    guard total > 0, total <= 40, next == confirmed, next <= total, next >= lastConfirmed else {
                        throw ClientFailure("fixture_invalid_checkpoint")
                    }
                    if originalID == nil {
                        guard state == "playing", !interrupted else { throw ClientFailure("fixture_missing_original_start") }
                        originalID = task; originalRevision = revision; originalTotal = total
                    }
                    guard task == originalID, revision == originalRevision, total == originalTotal else {
                        throw ClientFailure("fixture_original_replaced")
                    }
                    lastConfirmed = confirmed
                    if ["failed", "discarded", "closed"].contains(state) { throw ClientFailure("fixture_original_not_resumable") }
                    if interrupted && ["draining", "paused"].contains(state) {
                        guard !resumedPlaying else { throw ClientFailure("fixture_unexpected_second_pause") }
                        pausedObserved = true; checkpoint = confirmed
                    }
                    if resumeAction && state == "playing" && !resumedPlaying {
                        guard pausedObserved, sideCompleted, let checkpoint, next == checkpoint else {
                            throw ClientFailure("fixture_resume_checkpoint_mismatch")
                        }
                        resumedFrom = checkpoint; resumedPlaying = true
                    }
                    if state == "completed" {
                        guard resumedPlaying, resumedHandoff, resumeAction, pausedObserved, sideCompleted,
                              confirmed == total else { throw ClientFailure("fixture_premature_main_completion") }
                        completed = true
                    }
                } else if task.hasPrefix("side:"), interrupted {
                    if sideID == nil { sideID = task }
                    guard task == sideID else { throw ClientFailure("fixture_side_replaced") }
                    if state == "completed" {
                        guard pausedObserved else { throw ClientFailure("fixture_missing_main_pause") }
                        sideCompleted = true
                    }
                }
            case "content.action":
                if e["action"] as? String == "resume", e["target"] as? String == "main" {
                    guard interrupted, pausedObserved, sideCompleted else { throw ClientFailure("fixture_resume_without_branch") }
                    resumeAction = true
                }
            case "output.handoff":
                guard let identity = e["content_id"] as? String else {
                    if resumeAction && !resumedHandoff { throw ClientFailure("fixture_resumed_handoff_missing_identity") }
                    return
                }
                if identity.hasPrefix("main:") {
                    let revision = try integer(e, "content_revision"), index = try integer(e, "unit_index")
                    guard identity == originalID, revision == originalRevision, let total = originalTotal, index < total else {
                        throw ClientFailure("fixture_handoff_original_mismatch")
                    }
                    if firstMainBlock == nil { firstMainBlock = try integer(e, "block_id") }
                    if resumeAction && !resumedHandoff {
                        guard resumedPlaying, index == resumedFrom else { throw ClientFailure("fixture_resumed_unit_mismatch") }
                        resumedHandoff = true
                    }
                } else if resumeAction && !resumedHandoff {
                    throw ClientFailure("fixture_expected_original_handoff")
                }
            default: break
            }
        }
    }
    let queue: DispatchQueue
    weak var player: DrainPlayer?
    let clips: [[Int16]]
    var sendPCM: (([Int16]) -> Void)?
    var done: ((Bool) -> Void)?
    private var timer: DispatchSourceTimer?
    private var clip = 0, offset = 0, phase = 0
    private let proof = Proof()
    private var stopped = false
    init(paths: [String], queue: DispatchQueue, player: DrainPlayer) throws {
        guard paths.count == 3 else { throw ClientFailure("three_synthetic_fixtures_required") }
        self.queue = queue; self.player = player
        clips = try paths.map { path in
            let file = try AVAudioFile(forReading: URL(fileURLWithPath: path), commonFormat: .pcmFormatInt16, interleaved: true)
            guard file.processingFormat.sampleRate == 16000, file.processingFormat.channelCount == 1,
                  file.length > 0, file.length <= 16000 * 20,
                  let buffer = AVAudioPCMBuffer(pcmFormat: file.processingFormat, frameCapacity: AVAudioFrameCount(file.length)) else {
                throw ClientFailure("fixture_must_be_mono_16k_max20s")
            }
            try file.read(into: buffer)
            guard let samples = buffer.int16ChannelData?[0] else { throw ClientFailure("fixture_pcm") }
            return Array(UnsafeBufferPointer(start: samples, count: Int(buffer.frameLength)))
        }
    }
    func start() {
        metadata("fixture.inject", ["index": 1])
        let timer = DispatchSource.makeTimerSource(queue: queue)
        timer.schedule(deadline: .now(), repeating: .milliseconds(20), leeway: .milliseconds(1))
        timer.setEventHandler { [weak self] in self?.tick() }
        self.timer = timer; timer.resume()
        queue.asyncAfter(deadline: .now() + 180) { [weak self] in
            guard let self, !self.stopped else { return }
            metadata("fixture.failure", ["code": "180s_timeout"]); self.stop(); self.done?(false)
        }
    }
    private func tick() {
        guard !stopped else { return }
        var frame = [Int16](repeating: 0, count: 320)
        if clip < clips.count, offset < clips[clip].count {
            let length = min(320, clips[clip].count - offset)
            frame.replaceSubrange(0..<length, with: clips[clip][offset..<(offset + length)])
            offset += length
        }
        sendPCM?(frame)
        checkCompleted()
    }
    func progress(id: Int, samples: Int) {
        guard !stopped else { return }
        if phase == 0, id == proof.firstMainBlock, samples >= 2880 {
            do { try proof.interrupt() }
            catch { metadata("fixture.failure", ["code": "missing_original_identity"]); stop(); done?(false); return }
            phase = 1; clip = 1; offset = 0
            metadata("fixture.inject", ["index": 2, "during_block_id": id, "played_samples": samples])
        }
    }
    func event(_ e: [String: Any]) throws {
        try proof.observe(e)
        if phase == 1 && proof.sideCompleted {
            phase = 2; clip = 2; offset = 0
            metadata("fixture.inject", ["index": 3, "after_side_completed": true])
        }
        checkCompleted()
    }
    func checkCompleted() {
        guard !stopped, phase == 2, proof.verified, player?.settled == true,
              let id = proof.originalID, let revision = proof.originalRevision,
              let total = proof.originalTotal, let resumed = proof.resumedFrom else { return }
        metadata("fixture.success", ["story_interrupted": true, "side_completed": true, "main_completed": true,
                                     "main_task_id": id, "main_revision": revision, "main_total": total,
                                     "resumed_from": resumed, "original_identity_verified": true,
                                     "all_finals_acked": true, "evidence": "native_coreaudio_synthetic_input"])
        stop(); done?(true)
    }
    func stop() { stopped = true; timer?.cancel(); timer = nil }
}
