import Foundation
import AVFoundation

final class LiveClient: @unchecked Sendable {
    let queue = DispatchQueue(label: "family.output.live")
    let url: URL, plan: String?
    let ownsIdentity: Bool, deviceProfile: String
    private var sessionID: String?
    var status: ((String) -> Void)?, ended: ((Bool) -> Void)?
    private var session: URLSession?, socket: URLSessionWebSocketTask?, audio: AudioPipeline?, encoder: OpusEncoder?
    private var closed = false, ready = false, sending = false
    private var active = false, sealed = false, identity = 0, accepted = 0, completed = 0
    private var draining = false, drainStarted = 0.0
    private var outgoing: [URLSessionWebSocketTask.Message] = [], input: [Int16] = []
    private var fixture: RealtimeFixture?, outputIndex = -1, transcript = ""
    init(url: URL, plan: String? = nil, ownsIdentity: Bool = true, deviceProfile: String = "default") {
        self.url = url; self.plan = plan; self.ownsIdentity = ownsIdentity; self.deviceProfile = deviceProfile
    }
    func start() { queue.async { self.pair() } }
    func stop() { queue.async { self.finish(true) } }
    private func pair() {
        let cfg = URLSessionConfiguration.ephemeral
        cfg.connectionProxyDictionary = [:]; cfg.httpCookieStorage = nil; cfg.urlCache = nil
        let session = URLSession(configuration: cfg); self.session = session
        var parts = URLComponents(url: url, resolvingAgainstBaseURL: false)!
        parts.scheme = "http"; parts.path = "/pair"
        var request = URLRequest(url: parts.url!); request.httpMethod = "POST"; request.timeoutInterval = 8
        status?("正在连接…")
        let owned = ownsIdentity, profile = deviceProfile, kind = plan == nil ? "mac_native" : "mac_fixture"
        Task { [weak self] in
            do {
                var pairRequest = request
                var device: DeviceIdentity?
                if owned {
                    device = try await DeviceIdentity.prepare(session: session, endpoint: request.url!, profile: profile, kind: kind)
                    pairRequest.setValue("Bearer " + device!.credential, forHTTPHeaderField: "Authorization")
                    pairRequest.setValue("application/json", forHTTPHeaderField: "Content-Type")
                    pairRequest.httpBody = try JSONSerialization.data(withJSONObject: ["device_id": device!.deviceID, "client_version": "ownership-v1"])
                }
                let (data, response) = try await session.data(for: pairRequest)
                guard (response as? HTTPURLResponse)?.statusCode == 200, data.count <= 1024,
                      let value = try JSONSerialization.jsonObject(with: data) as? [String: Any],
                      let ticket = value["ticket"] as? String,
                      ticket.range(of: "^[A-Za-z0-9_-]{43}$", options: .regularExpression) != nil else { throw ClientFailure("pair_unavailable") }
                if let device, value["device_id"] as? String != device.deviceID { throw ClientFailure("pair_identity_mismatch") }
                if let device { metadata("live.terminal_authenticated", ["device_id": device.deviceID, "client_type": device.clientType]) }
                self?.queue.async { [weak self] in self?.connect(ticket) }
            } catch { self?.queue.async { [weak self] in self?.fail("连接未成功，请稍后重新开始。") } }
        }
    }
    private func connect(_ ticket: String) {
        guard !closed, let session else { return }
        var request = URLRequest(url: url); request.setValue("Bearer " + ticket, forHTTPHeaderField: "Authorization")
        let socket = session.webSocketTask(with: request); socket.maximumMessageSize = 16384
        self.socket = socket; socket.resume(); receive()
        queue.asyncAfter(deadline: .now() + 15) { [weak self] in
            guard let self, !self.closed, !self.ready else { return }; self.fail("连接超时，可重新开始。")
        }
    }
    private func send(_ event: [String: Any]) {
        var scoped = event
        if let sessionID { scoped["session_id"] = sessionID }
        guard let data = try? JSONSerialization.data(withJSONObject: scoped), let text = String(data: data, encoding: .utf8) else { return }
        enqueue(.string(text))
    }
    private func enqueue(_ value: URLSessionWebSocketTask.Message) {
        guard !closed else { return }
        guard outgoing.count < 64 else { fail("网络发送拥堵，可重新开始。"); return }
        outgoing.append(value); pump()
    }
    private func pump() {
        guard !closed, !sending, !outgoing.isEmpty, let socket else { return }
        sending = true; let value = outgoing.removeFirst()
        Task { [weak self] in
            do { try await socket.send(value); self?.queue.async { [weak self] in self?.sending = false; self?.pump() } }
            catch { self?.queue.async { [weak self] in self?.fail("连接已结束，可重新开始。") } }
        }
    }
    private func receive() {
        guard !closed, let socket else { return }
        Task { [weak self] in
            do {
                let value = try await socket.receive()
                self?.queue.async { [weak self] in
                    guard let self, !self.closed else { return }
                    do { try self.message(value); self.receive() }
                    catch { metadata("live.client_error", ["code": (error as? ClientFailure)?.description ?? "invalid_message"]); self.fail("本轮连接异常，可重新开始。") }
                }
            } catch { self?.queue.async { [weak self] in self?.fail("连接已结束，可重新开始。") } }
        }
    }
    private func message(_ message: URLSessionWebSocketTask.Message) throws {
        switch message {
        case .data(let data):
            guard data.count > 4, data.count <= 4004 else { throw ClientFailure("packet_size") }
            let id = data.prefix(4).reduce(0) { ($0 << 8) | Int($1) }
            guard id == identity, active else { metadata("live.retired_packet", ["id": id]); return }
            guard !sealed, accepted - completed < 24000 else { throw ClientFailure("packet_window") }
            if accepted == 0 { audio?.traceFirstPacket(bytes: data.count - 4) }
            try audio?.enqueue(Data(data.dropFirst(4))); accepted += 1440
        case .string(let text):
            guard let data = text.data(using: .utf8), data.count <= 16384,
                  let event = try JSONSerialization.jsonObject(with: data) as? [String: Any] else { throw ClientFailure("control") }
            if ownsIdentity {
                guard let scope = event["session_id"] as? String,
                      scope.range(of: "^[a-f0-9]{32}$", options: .regularExpression) != nil else { throw ClientFailure("session_scope_missing") }
                if event["type"] as? String == "ready", sessionID == nil {
                    sessionID = scope; metadata("live.session_bound", ["session_id": scope])
                } else if scope != sessionID { throw ClientFailure("session_scope_mismatch") }
            }
            switch event["type"] as? String {
            case "ready":
                guard !ready else { throw ClientFailure("duplicate_ready") }
                let audio = AudioPipeline(queue: queue, diagnosticSessionID: sessionID); self.audio = audio; encoder = try OpusEncoder(rate: 16000)
                audio.onPCM = { [weak self] in self?.inputPCM($0) }
                audio.onFailure = { [weak self] _ in self?.fail("音频设备发生变化，请重新开始。") }
                try audio.start(microphone: plan == nil)
                ready = true; status?("对话中 · 插话后播完已收到的尾音，再接新回复")
                metadata("live.client_ready", ["microphone": plan == nil, "basis": "CoreAudio_dataPlayedBack"])
                if let plan {
                    let fixture = try RealtimeFixture(path: plan, queue: queue); self.fixture = fixture
                    fixture.sendPCM = { [weak self] in self?.inputPCM($0) }
                    fixture.done = { [weak self] success in self?.queue.asyncAfter(deadline: .now() + 0.3) { [weak self] in self?.finish(success) } }
                    fixture.start()
                }
            case "begin":
                guard ready, !active, let id = event["id"] as? Int, id > identity else { throw ClientFailure("begin_identity") }
                identity = id; accepted = 0; completed = 0; active = true; sealed = false; draining = false; transcript = ""
                outputIndex = fixture?.index ?? -1
                try audio?.begin(diagnosticID: id); let token = id
                audio?.onCompleted = { [weak self] samples in
                    guard let self, !self.closed, self.active, self.identity == token else { return }
                    guard samples >= self.completed, samples <= self.accepted else { self.fail("播放回报异常，可重新开始。"); return }
                    if self.completed == 0 && samples > 0 {
                        metadata("live.output_first_complete", ["id": token, "samples": samples])
                    }
                    self.completed = samples; self.receipt()
                    if self.sealed && self.completed == self.accepted { self.played() }
                    else { self.fixture?.progress(samples: samples, outputIndex: self.outputIndex) }
                }
                metadata("live.output_begin", ["id": id])
            case "drain":
                guard let id = event["id"] as? Int, let target = event["sent"] as? Int,
                      id == identity, target == accepted else { throw ClientFailure("drain_boundary") }
                // WebSocket ordering guarantees all sealed packets arrived first.
                // Do not stop/reset the player until dataPlayedBack covers them.
                sealed = true
                if !draining { drainStarted = ProcessInfo.processInfo.systemUptime; draining = true }
                metadata("live.output_drain", ["id": id, "completed": completed, "accepted": accepted,
                    "remaining_ms": Double(accepted - completed) / 24])
                if completed == accepted { played() }
                else { status?("正在播完刚才的尾音 · 你可以继续说话") }
            case "end":
                guard active, event["id"] as? Int == identity else { throw ClientFailure("end_identity") }
                sealed = true; transcript = event["generated_text"] as? String ?? ""
                guard transcript.count <= 4096 else { throw ClientFailure("text_bound") }
                if completed == accepted { played() }
            case "status":
                switch event["code"] as? String {
                case "listening": status?("正在听…")
                case "thinking": status?("正在回应…")
                case "turn_failed": status?("这次回复未完成，可以继续说话。")
                case "drain_failed": status?("未收到播放完成确认，请结束后重新开始。")
                case "input_too_long": status?("这句话较长，请分几句说。")
                default: break
                }
            default: throw ClientFailure("unknown_control")
            }
        @unknown default: throw ClientFailure("unknown_message")
        }
    }
    private func receipt(terminal: String = "progress") {
        send(["type": "receipt", "id": identity, "completed": completed, "accepted": accepted,
              "basis": "CoreAudio_dataPlayedBack", "terminal": terminal])
    }
    private func played() {
        receipt(terminal: draining ? "drained" : "played"); active = false; audio?.end()
        metadata(draining ? "live.output_drained" : "live.output_played", ["id": identity, "samples": completed,
            "drain_ms": draining ? Int((ProcessInfo.processInfo.systemUptime - drainStarted) * 1000) : 0])
        status?("对话中 · 可以自由说话和插话")
        if draining { fixture?.interrupted() }
        else { fixture?.outputCompleted(index: outputIndex, generatedText: transcript) }
    }
    private func inputPCM(_ pcm: [Int16]) {
        guard ready, !closed, let encoder else { return }
        guard input.count + pcm.count <= 8000 else { fail("麦克风处理拥堵，可重新开始。"); return }
        input.append(contentsOf: pcm)
        do {
            while input.count >= 320 {
                let frame = Array(input.prefix(320)); input.removeFirst(320)
                enqueue(.data(try encoder.encode(frame)))
            }
        } catch { fail("麦克风编码失败，可重新开始。") }
    }
    private func fail(_ message: String) { guard !closed else { return }; status?(message); finish(false) }
    private func finish(_ success: Bool) {
        guard !closed else { return }; closed = true; ready = false
        fixture?.stop(); audio?.stop(); outgoing.removeAll(); input.removeAll()
        socket?.cancel(with: .normalClosure, reason: nil); session?.invalidateAndCancel()
        metadata("live.client_closed", ["success": success]); ended?(success)
    }
}
