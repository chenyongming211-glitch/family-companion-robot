import Foundation
import AVFoundation

// Pure in-memory encoder. No device, engine, resampler, VAD or file access.
final class NativePCMCollector {
    let sampleRate: Int
    let channels: Int
    let targetFrames: Int
    private(set) var frames = 0
    private(set) var bytes = Data()
    init(sampleRate: Double, channels: Int) throws {
        guard sampleRate.isFinite, sampleRate.rounded() == sampleRate, (8000...96000).contains(sampleRate), (1...8).contains(channels) else { throw ClientFailure("format_invalid") }
        self.sampleRate = Int(sampleRate); self.channels = channels
        targetFrames = Int(sampleRate) * CaptureProtocol.seconds
        guard targetFrames * channels * 4 <= CaptureProtocol.pcmLimit else { throw ClientFailure("format_invalid") }
        bytes.reserveCapacity(targetFrames * channels * 4)
    }
    private func append(frameCount: Int, sample: (Int, Int) -> Float) throws {
        guard frameCount >= 0 else { throw ClientFailure("format_invalid") }
        let count = min(frameCount, targetFrames - frames)
        for frame in 0..<count {
            for channel in 0..<channels {
                let value = sample(frame, channel)
                guard value.isFinite, abs(value) <= 16 else { throw ClientFailure("sample_invalid") }
                var bits = value.bitPattern.littleEndian
                withUnsafeBytes(of: &bits) { bytes.append(contentsOf: $0) }
            }
        }
        frames += count
    }
    func appendPlanar(_ values: [[Float]], frameCount: Int) throws {
        guard values.count == channels, frameCount >= 0, values.allSatisfy({ $0.count >= frameCount }) else { throw ClientFailure("format_invalid") }
        try append(frameCount: frameCount) { values[$1][$0] }
    }
    func appendInterleaved(_ values: [Float], frameCount: Int) throws {
        guard frameCount >= 0, frameCount <= values.count / channels else { throw ClientFailure("format_invalid") }
        try append(frameCount: frameCount) { values[$0 * channels + $1] }
    }
    func appendBuffer(_ buffer: AVAudioPCMBuffer) throws {
        guard buffer.format.commonFormat == .pcmFormatFloat32, buffer.format.sampleRate == Double(sampleRate), Int(buffer.format.channelCount) == channels, let pointers = buffer.floatChannelData else { throw ClientFailure("format_invalid") }
        let stride = buffer.stride
        guard stride > 0, buffer.format.isInterleaved ? stride == channels : stride == 1 else { throw ClientFailure("format_invalid") }
        try append(frameCount: Int(buffer.frameLength)) { frame, channel in
            buffer.format.isInterleaved ? pointers[0][frame * stride + channel] : pointers[channel][frame * stride]
        }
    }
    func finish() throws -> NativeRecord {
        guard frames == targetFrames, bytes.count == targetFrames * channels * 4 else { throw ClientFailure("capture_short") }
        return NativeRecord(header: NativeHeader(sample_rate: sampleRate, channels: channels, frames: frames, pcm_bytes: bytes.count), pcm: bytes)
    }
    func clear() {
        bytes.resetBytes(in: 0..<bytes.count); bytes.removeAll(keepingCapacity: false); frames = 0
    }
}

// Engine creation exists only in start(), after the UI's manual/ready/permission gate.
final class ClipCapture {
    private var engine: AVAudioEngine?
    private var collector: NativePCMCollector?
    private let lock = NSLock()
    private var generation = 0
    private var active = false
    private var finished = false
    private var completion: ((Result<NativeRecord, ClientFailure>) -> Void)?
    private var timeout: DispatchWorkItem?

    func start(completion: @escaping (Result<NativeRecord, ClientFailure>) -> Void) throws {
        precondition(Thread.isMainThread)
        stop()
        let audio = AVAudioEngine()
        let input = audio.inputNode
        let format = input.outputFormat(forBus: 0)
        guard format.commonFormat == .pcmFormatFloat32 else { throw ClientFailure("format_invalid") }
        let collector = try NativePCMCollector(sampleRate: format.sampleRate, channels: Int(format.channelCount))
        lock.lock(); self.collector = collector; self.completion = completion; active = true; finished = false; lock.unlock()
        input.installTap(onBus: 0, bufferSize: 1024, format: format) { [weak self] buffer, _ in
            guard let self else { return }
            self.lock.lock(); defer { self.lock.unlock() }
            guard self.active, !self.finished, let collector = self.collector else { return }
            do {
                try collector.appendBuffer(buffer)
                if collector.frames == collector.targetFrames { self.finishLocked(.success(try collector.finish())) }
            } catch let error as ClientFailure { self.finishLocked(.failure(error)) }
            catch { self.finishLocked(.failure(ClientFailure("capture_failed"))) }
        }
        engine = audio
        let deadline = DispatchWorkItem { [weak self] in
            guard let self else { return }
            self.lock.lock(); defer { self.lock.unlock() }
            if self.active, !self.finished { self.finishLocked(.failure(ClientFailure("capture_timeout"))) }
        }
        timeout = deadline
        DispatchQueue.main.asyncAfter(deadline: .now() + 7, execute: deadline)
        do { audio.prepare(); try audio.start() } catch { stop(); throw ClientFailure("capture_failed") }
    }
    private func finishLocked(_ result: Result<NativeRecord, ClientFailure>) {
        finished = true
        let token = generation
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            self.lock.lock(); let callback = self.active && self.generation == token ? self.completion : nil; self.lock.unlock()
            guard let callback else { return }
            self.stop(); callback(result)
        }
    }
    func stop() {
        precondition(Thread.isMainThread)
        timeout?.cancel(); timeout = nil
        lock.lock(); generation += 1; active = false; completion = nil; collector?.clear(); collector = nil; lock.unlock()
        if let audio = engine { audio.inputNode.removeTap(onBus: 0); audio.stop(); audio.reset() }
        engine = nil
    }
    deinit { engine?.stop() }
}
