import Foundation
import AVFoundation

struct ClientFailure: Error { let code: String; init(_ code: String) { self.code = code } }

// No engine or microphone is touched by initialization. start() is called only
// after a manual action, remote ready and granted microphone permission.
final class ClipCapture {
    private var engine: AVAudioEngine?
    private let lock = NSLock()
    private var samples: [Int16] = []
    private var generation = 0
    private var active = false
    private var finished = false
    private var completion: ((Result<[Int16], ClientFailure>) -> Void)?
    private var timeout: DispatchWorkItem?

    func start(completion: @escaping (Result<[Int16], ClientFailure>) -> Void) throws {
        precondition(Thread.isMainThread)
        stop()
        let audio = AVAudioEngine()
        let input = audio.inputNode
        let source = input.outputFormat(forBus: 0)
        let converter = try microphoneConverter(from: source)
        lock.lock(); active = true; finished = false; samples.reserveCapacity(64000); self.completion = completion; lock.unlock()
        input.installTap(onBus: 0, bufferSize: 1024, format: source) { [weak self] buffer, _ in
            guard let self else { return }
            self.lock.lock(); defer { self.lock.unlock() }
            guard self.active, !self.finished else { return }
            let capacity = AVAudioFrameCount(ceil(Double(buffer.frameLength) * 16000 / source.sampleRate) + 128)
            guard let output = AVAudioPCMBuffer(pcmFormat: converter.outputFormat, frameCapacity: capacity) else { self.finishLocked(.failure(ClientFailure("capture_failed"))); return }
            var supplied = false
            var error: NSError?
            let status = converter.convert(to: output, error: &error) { _, state in
                if supplied { state.pointee = .noDataNow; return nil }
                supplied = true; state.pointee = .haveData; return buffer
            }
            guard status != .error, error == nil, let pointer = output.int16ChannelData?[0] else { self.finishLocked(.failure(ClientFailure("capture_failed"))); return }
            let n = min(Int(output.frameLength), 64000 - self.samples.count)
            self.samples.append(contentsOf: UnsafeBufferPointer(start: pointer, count: n))
            if self.samples.count == 64000 {
                let clip = self.samples
                self.samples.removeAll(keepingCapacity: false)
                self.finishLocked(.success(clip))
            }
        }
        engine = audio
        let deadline = DispatchWorkItem { [weak self] in
            guard let self else { return }
            self.lock.lock(); defer { self.lock.unlock() }
            if self.active && !self.finished { self.finishLocked(.failure(ClientFailure("capture_timeout"))) }
        }
        timeout = deadline
        DispatchQueue.main.asyncAfter(deadline: .now() + 6, execute: deadline)
        do { audio.prepare(); try audio.start() } catch { stop(); throw ClientFailure("capture_failed") }
    }
    private func finishLocked(_ result: Result<[Int16], ClientFailure>) {
        finished = true
        let token = generation
        // At most one queued completion, guarded again after stop on main.
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            self.lock.lock()
            let callback = self.active && self.generation == token ? self.completion : nil
            self.lock.unlock()
            guard let callback else { return }
            self.stop()
            callback(result)
        }
    }
    func stop() {
        precondition(Thread.isMainThread)
        timeout?.cancel(); timeout = nil
        lock.lock(); generation += 1; active = false; completion = nil; samples.withUnsafeMutableBufferPointer { $0.initialize(repeating: 0) }; samples.removeAll(keepingCapacity: false); lock.unlock()
        if let audio = engine { audio.inputNode.removeTap(onBus: 0); audio.stop(); audio.reset() }
        engine = nil
    }
    deinit { engine?.stop() }
}
