import Foundation
import AVFoundation

// A no-microphone fixture client. State belongs to queue; URLSession tasks only
// deliver messages back to that queue. No provider key or human audio is used.
final class OutputProbe: @unchecked Sendable {
    let queue = DispatchQueue(label: "family.output-asr.probe")
    let url: URL
    private var transport: URLSession?, socket: URLSessionWebSocketTask?, audio: AudioPipeline?
    private var closed = false, active = false, sealed = false, discardingRetired = false
    private var retiredPackets = 0
    private var identity = "", accepted = 0, completed = 0, target = 0, generation = 0
    init(url: URL) { self.url = url }
    func start() { queue.async {
        do {
            let audio = AudioPipeline(queue: self.queue); self.audio = audio
            audio.onFailure = { [weak self] _ in self?.finish(false) }
            try audio.start(microphone: false)
            let cfg = URLSessionConfiguration.ephemeral
            cfg.connectionProxyDictionary = [:]; cfg.httpCookieStorage = nil; cfg.urlCache = nil
            let session = URLSession(configuration: cfg); self.transport = session
            let socket = session.webSocketTask(with: self.url); self.socket = socket
            socket.maximumMessageSize = 4096; socket.resume()
            self.send(["type": "ready", "microphone": false]); self.receive()
            self.queue.asyncAfter(deadline: .now() + 175) { [weak self] in self?.finish(false) }
        } catch { self.finish(false) }
    } }
    private func send(_ event: [String: Any]) {
        guard !closed, let socket, let data = try? JSONSerialization.data(withJSONObject: event),
              let value = String(data: data, encoding: .utf8) else { finish(false); return }
        Task { [weak self] in
            do { try await socket.send(.string(value)) }
            catch { self?.queue.async { [weak self] in self?.finish(false) } }
        }
    }
    private func receive() {
        guard !closed, let socket else { return }
        Task { [weak self] in
            do {
                let message = try await socket.receive()
                self?.queue.async { [weak self] in
                    guard let self, !self.closed else { return }
                    do { try self.message(message); self.receive() }
                    catch {
                        metadata("output_probe.failure", ["code": (error as? ClientFailure)?.description ?? "invalid_message"])
                        self.finish(false)
                    }
                }
            } catch { self?.queue.async { [weak self] in self?.finish(false) } }
        }
    }
    private func message(_ message: URLSessionWebSocketTask.Message) throws {
        guard let audio else { throw ClientFailure("probe_audio_missing") }
        switch message {
        case .data(let packet):
            if discardingRetired && !active {
                guard packet.count <= 4000, retiredPackets < 16 else { throw ClientFailure("probe_retired_packet_bound") }
                retiredPackets += 1
                metadata("output_probe.retired_packet", ["count": retiredPackets]); return
            }
            guard active, !sealed, accepted - completed + 1440 <= 24000 else { throw ClientFailure("probe_audio_bound") }
            try audio.enqueue(packet); accepted += 1440
        case .string(let text):
            guard let data = text.data(using: .utf8), let event = try JSONSerialization.jsonObject(with: data) as? [String: Any] else {
                throw ClientFailure("probe_invalid_control")
            }
            switch event["type"] as? String {
            case "begin":
                guard !active, let id = event["id"] as? String, ["original", "math", "resume"].contains(id),
                      let target = event["cut_samples"] as? Int, target >= 0, target <= 240000 else {
                    throw ClientFailure("probe_invalid_begin")
                }
                generation += 1; let token = generation
                identity = id; self.target = target; accepted = 0; completed = 0; active = true; sealed = false
                discardingRetired = false; retiredPackets = 0
                try audio.begin()
                audio.onCompleted = { [weak self] samples in
                    guard let self, !self.closed, self.active, self.generation == token,
                          samples >= self.completed, samples <= self.accepted else { return }
                    self.completed = samples
                    if self.target > 0 && samples >= self.target { self.receipt(cut: true) }
                    else if self.sealed && samples == self.accepted { self.receipt(cut: false) }
                }
                metadata("output_probe.begin", ["id": id, "cut_samples": target])
            case "end":
                guard active, event["id"] as? String == identity else { throw ClientFailure("probe_invalid_end") }
                sealed = true
                if completed == accepted { receipt(cut: false) }
            case "waiting": metadata("output_probe.waiting")
            case "result":
                metadata("output_probe.result", ["success": event["success"] as? Bool == true,
                                                  "code": event["code"] as? String ?? "unknown"])
                finish(event["success"] as? Bool == true)
            default: throw ClientFailure("probe_unknown_control")
            }
        @unknown default: throw ClientFailure("probe_unknown_message")
        }
    }
    private func receipt(cut: Bool) {
        guard active else { return }
        let value: [String: Any] = ["type": cut ? "cut" : "played", "id": identity,
            "completed_samples": completed, "accepted_samples": accepted,
            "basis": "CoreAudio_dataPlayedBack", "microphone": false]
        active = false; discardingRetired = cut; generation += 1; audio?.end()
        metadata("output_probe.receipt", value); send(value)
    }
    private func finish(_ success: Bool) {
        guard !closed else { return }; closed = true
        audio?.stop(); socket?.cancel(with: .normalClosure, reason: nil); transport?.invalidateAndCancel()
        metadata("output_probe.closed", ["success": success, "microphone": false]); exit(success ? 0 : 1)
    }
}

@main struct Main {
    static func main() {
        let args = Array(CommandLine.arguments.dropFirst())
        guard args.count == 1, let url = URL(string: args[0]), url.scheme == "ws", url.host == "127.0.0.1",
              url.port == 18031, url.path == "/probe", url.query == nil, url.fragment == nil,
              url.user == nil, url.password == nil else { exit(2) }
        let client = OutputProbe(url: url); client.start(); withExtendedLifetime(client) { dispatchMain() }
    }
}
