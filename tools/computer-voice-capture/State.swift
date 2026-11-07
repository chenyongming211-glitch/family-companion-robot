import Foundation

// Mirrored by tools/robot-audio-capture/receive_computer.py; protocol version 1.
enum CaptureProtocol {
    static let headerLimit = 2048
    static let pcmLimit = 16 * 1024 * 1024
    static let seconds = 4
}
struct ClientFailure: Error { let code: String; init(_ code: String) { self.code = code } }
enum CapturePhase { case idle, connecting, permission, recording, sending }
struct CaptureState {
    private(set) var phase: CapturePhase = .idle
    private(set) var generation = 0
    private(set) var saved = 0
    private let maxClips: Int
    private var clip: String?
    init(maxClips: Int = 12) { self.maxClips = maxClips }
    mutating func beginManual(clip: String) -> Int? {
        guard phase == .idle, saved < maxClips else { return nil }
        generation += 1; self.clip = clip; phase = .connecting
        return generation
    }
    mutating func acceptReady(clip: String, generation: Int) -> Bool {
        guard self.generation == generation, self.clip == clip, phase == .connecting else { return false }
        phase = .permission; return true
    }
    mutating func permissionGranted(generation: Int) -> Bool {
        guard self.generation == generation, phase == .permission else { return false }
        phase = .recording; return true
    }
    mutating func captureCompleted(generation: Int) -> Bool {
        guard self.generation == generation, phase == .recording else { return false }
        phase = .sending; return true
    }
    mutating func stored(clip: String, generation: Int) -> Bool {
        guard self.generation == generation, self.clip == clip, phase == .sending else { return false }
        saved += 1; cancel(); return true
    }
    mutating func fail(generation: Int) -> Bool {
        guard self.generation == generation, phase != .idle else { return false }
        cancel(); return true
    }
    mutating func cancel() { generation += 1; phase = .idle; clip = nil }
}

struct NativeHeader: Encodable {
    let schema_version = 1
    let source_kind = "private_human"
    let source_device = "mac_microphone"
    let sample_format = "f32le_interleaved"
    let sample_rate: Int
    let channels: Int
    let frames: Int
    let pcm_bytes: Int
}
struct NativeRecord {
    let header: NativeHeader
    let pcm: Data
    func framed() throws -> Data {
        let encoder = JSONEncoder(); encoder.outputFormatting = .sortedKeys
        let raw = try encoder.encode(header)
        guard raw.count <= CaptureProtocol.headerLimit, pcm.count == header.pcm_bytes, pcm.count <= CaptureProtocol.pcmLimit else { throw ClientFailure("format_invalid") }
        var size = UInt32(raw.count).bigEndian
        var data = withUnsafeBytes(of: &size) { Data($0) }
        data.append(raw); data.append(pcm); return data
    }
}
