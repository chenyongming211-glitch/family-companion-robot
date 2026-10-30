import Foundation
import AVFoundation
import Darwin

// Optional evidence only. Never put PCM, provider objects, text, or credentials here.
private final class AudioTraceWriter: @unchecked Sendable {
    static let shared: AudioTraceWriter? = {
        guard let path = ProcessInfo.processInfo.environment["NATIVE_AUDIO_TRACE"], !path.isEmpty else { return nil }
        return AudioTraceWriter(path: path)
    }()
    private let worker = DispatchQueue(label: "family.audio.evidence.writer", qos: .utility)
    private let lock = NSLock()
    private let file: FileHandle
    private var pending = 0, dropped = 0, bytes = 0
    private var failed = false
    private let limit = 1_048_576

    private init?(path: String) {
        let parent = URL(fileURLWithPath: path).deletingLastPathComponent().path
        guard let attrs = try? FileManager.default.attributesOfItem(atPath: parent),
              attrs[.type] as? FileAttributeType == .typeDirectory,
              (attrs[.posixPermissions] as? NSNumber)?.intValue == 0o700,
              (attrs[.ownerAccountID] as? NSNumber)?.uint32Value == getuid() else { return nil }
        let fd = Darwin.open(path, O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW, 0o600)
        guard fd >= 0 else { return nil }
        file = FileHandle(fileDescriptor: fd, closeOnDealloc: true)
    }
    func write(_ value: [String: Any]) {
        lock.lock()
        guard !failed, pending < 64 else { dropped += 1; lock.unlock(); return }
        pending += 1
        lock.unlock()
        worker.async { [self] in
            defer { lock.lock(); pending -= 1; lock.unlock() }
            lock.lock(); let lost = dropped; let unavailable = failed; lock.unlock()
            guard !unavailable else { return }
            var row = value
            row["writer_dropped_events"] = lost
            guard var data = try? JSONSerialization.data(withJSONObject: row, options: [.sortedKeys]), data.count <= 8192 else {
                lock.lock(); dropped += 1; lock.unlock(); return
            }
            data.append(10)
            guard bytes + data.count <= limit else {
                lock.lock(); failed = true; dropped += 1; lock.unlock(); return
            }
            do { try file.write(contentsOf: data); bytes += data.count }
            catch { lock.lock(); failed = true; lock.unlock() }
        }
    }
}

final class AudioDiagnostics {
    private let writer: AudioTraceWriter
    private let started = ProcessInfo.processInfo.systemUptime
    private let nonce = UUID().uuidString
    private let sessionID: String?
    var active: Bool { ProcessInfo.processInfo.systemUptime - started <= 120 }
    static func make(sessionID: String?) -> AudioDiagnostics? {
        guard let writer = AudioTraceWriter.shared else { return nil }
        return AudioDiagnostics(writer: writer, sessionID: sessionID)
    }
    private init(writer: AudioTraceWriter, sessionID: String?) {
        self.writer = writer; self.sessionID = sessionID
        note("session_started", ["duration_limit_s": 120, "file_limit_bytes": 1_048_576, "pending_limit": 64])
    }
    func note(_ event: String, _ fields: [String: Any] = [:], terminal: Bool = false) {
        guard terminal || active else { return }
        var row = fields
        row["event"] = event; row["schema_version"] = 1; row["audio_session"] = nonce
        let queuedAt = Int(ProcessInfo.processInfo.systemUptime * 1000)
        row["monotonic_ms"] = fields["observed_monotonic_ms"] as? Int ?? queuedAt
        row["queued_monotonic_ms"] = queuedAt
        if let sessionID { row["session_id"] = sessionID }
        writer.write(row)
    }
    static func format(_ format: AVAudioFormat) -> [String: Any] {
        ["rate": format.sampleRate, "channels": format.channelCount,
         "interleaved": format.isInterleaved, "common_format": format.commonFormat.rawValue]
    }
    static func clock(_ time: AVAudioTime?) -> [String: Any] {
        guard let time else { return ["available": false] }
        var result: [String: Any] = ["available": true, "host_valid": time.isHostTimeValid,
                                     "sample_valid": time.isSampleTimeValid]
        if time.isHostTimeValid {
            result["host_ticks"] = time.hostTime
            result["host_seconds"] = AVAudioTime.seconds(forHostTime: time.hostTime)
        }
        if time.isSampleTimeValid { result["sample_time"] = time.sampleTime; result["rate"] = time.sampleRate }
        return result
    }
}

// All calls are protected by AudioPipeline's existing captureLock. Arrays are
// allocated before capture; snapshots allocate only on the client control queue.
final class AudioCaptureEvidence {
    private let channels: Int, rate: Double
    private var squares = [Double](repeating: 0, count: 8)
    private var peaks = [Double](repeating: 0, count: 8)
    private var remaining = 0, frames = 0, sampled = 0, nonfinite = 0
    private var firstHost: UInt64 = 0, lastHost: UInt64 = 0
    private var playbackID = 0, generation = 0
    init?(channels: Int, rate: Double) {
        guard channels > 0, rate.isFinite, rate > 0, rate <= 384000 else { return nil }
        self.channels = min(8, channels); self.rate = rate
    }
    func begin(playbackID: Int, generation: Int) {
        self.playbackID = playbackID; self.generation = generation
        remaining = Int(rate * 3); clear()
    }
    private func clear() {
        frames = 0; sampled = 0; nonfinite = 0; firstHost = 0; lastHost = 0
        for c in 0..<8 { squares[c] = 0; peaks[c] = 0 }
    }
    func add(_ buffer: AVAudioPCMBuffer, at time: AVAudioTime) {
        guard remaining > 0, buffer.format.commonFormat == .pcmFormatFloat32,
              let pointers = buffer.floatChannelData else { return }
        let count = min(Int(buffer.frameLength), remaining)
        guard count > 0 else { return }
        let actualChannels = Int(buffer.format.channelCount)
        let available = min(channels, actualChannels)
        if frames == 0, time.isHostTimeValid { firstHost = time.hostTime }
        if time.isHostTimeValid { lastHost = time.hostTime }
        for i in stride(from: 0, to: count, by: 8) {
            sampled += 1
            for c in 0..<available {
                let value = Double(buffer.format.isInterleaved ? pointers[0][i * actualChannels + c] : pointers[c][i])
                if value.isFinite { squares[c] += value * value; peaks[c] = max(peaks[c], abs(value)) }
                else { nonfinite += 1 }
            }
        }
        frames += count; remaining -= count
    }
    func snapshot(force: Bool = false) -> [String: Any]? {
        guard frames > 0, force || frames >= Int(rate / 10) || remaining == 0 else { return nil }
        let result: [String: Any] = ["playback_id": playbackID, "generation": generation,
            "frames": frames, "rate": rate, "channels": channels, "sample_stride": 8,
            "sampled_values_per_channel": sampled, "nonfinite_values": nonfinite,
            "sampled_rms": (0..<channels).map { sqrt(squares[$0] / Double(max(1, sampled))) },
            "sampled_peak": (0..<channels).map { peaks[$0] },
            "first_buffer_host_ticks": firstHost, "last_buffer_host_ticks": lastHost]
        clear()
        return result
    }
}
