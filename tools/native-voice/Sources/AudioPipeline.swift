import Foundation
import AVFoundation

func metadata(_ event: String, _ fields: [String: Any] = [:]) {
    // A whitelist at call sites, never serialize received provider events or Error descriptions.
    var payload = fields
    payload["event"] = event
    payload["monotonic_ms"] = Int(ProcessInfo.processInfo.systemUptime * 1000)
    if let data = try? JSONSerialization.data(withJSONObject: payload, options: [.sortedKeys]),
       let line = String(data: data, encoding: .utf8) { print(line); fflush(stdout) }
}

final class AudioPipeline {
    struct Segment {
        let start: AVAudioFramePosition, logical: Int, count: Int
        var done = false
    }
    let engine = AVAudioEngine()
    let player = AVAudioPlayerNode()
    let queue: DispatchQueue
    let format = AVAudioFormat(standardFormatWithSampleRate: 24000, channels: 1)!
    var onCompleted: ((Int) -> Void)?
    var onPCM: (([Int16]) -> Void)?
    var onFailure: ((String) -> Void)?
    private var segments: [Segment] = []
    private var decoder: OpusDecoder?
    private var nextFrame: AVAudioFramePosition = 0
    private var accepted = 0, completed = 0, generation = 0
    private var tapping = false
    private let captureLock = NSLock()
    private var capturePending = 0
    private var converter: AVAudioConverter?
    private(set) var aecEnabled = false
    private var inputVoiceProcessing = false, outputVoiceProcessing = false
    private(set) var presentationLatency = 0.0

    init(queue: DispatchQueue) {
        self.queue = queue
        engine.attach(player)
        engine.connect(player, to: engine.mainMixerNode, format: format)
        NotificationCenter.default.addObserver(forName: .AVAudioEngineConfigurationChange, object: engine, queue: nil) { [weak self] _ in
            guard let self else { return }
            self.queue.async { if self.engine.isRunning { self.onFailure?("audio_device_configuration_changed") } }
        }
    }
    func start(microphone: Bool) throws {
        if microphone {
            do {
                try engine.inputNode.setVoiceProcessingEnabled(true)
            } catch { metadata("audio.voice_processing_request", ["accepted": false]) }
            inputVoiceProcessing = engine.inputNode.isVoiceProcessingEnabled
            outputVoiceProcessing = engine.outputNode.isVoiceProcessingEnabled
            aecEnabled = inputVoiceProcessing && outputVoiceProcessing
            let input = engine.inputNode
            let source = input.outputFormat(forBus: 0)
            guard source.sampleRate > 0, source.channelCount > 0,
                  let target = AVAudioFormat(commonFormat: .pcmFormatInt16, sampleRate: 16000, channels: 1, interleaved: true),
                  let converter = AVAudioConverter(from: source, to: target) else { throw ClientFailure("microphone_format") }
            self.converter = converter
            input.installTap(onBus: 0, bufferSize: 1024, format: source) { [weak self] buffer, _ in
                guard let self else { return }
                self.captureLock.lock()
                if self.capturePending >= 4 {
                    self.captureLock.unlock()
                    self.queue.async { self.onFailure?("microphone_queue_overflow") }
                    return
                }
                self.capturePending += 1
                self.captureLock.unlock()
                let capacity = AVAudioFrameCount(ceil(Double(buffer.frameLength) * 16000 / source.sampleRate) + 128)
                guard let converted = AVAudioPCMBuffer(pcmFormat: target, frameCapacity: capacity) else {
                    self.captureLock.lock(); self.capturePending -= 1; self.captureLock.unlock()
                    self.queue.async { self.onFailure?("microphone_buffer_allocation") }; return
                }
                var provided = false
                var conversionError: NSError?
                let status = converter.convert(to: converted, error: &conversionError) { _, state in
                    if provided { state.pointee = .noDataNow; return nil }
                    provided = true; state.pointee = .haveData; return buffer
                }
                let pcm: [Int16]
                if status != .error, conversionError == nil, let pointer = converted.int16ChannelData?[0] {
                    pcm = Array(UnsafeBufferPointer(start: pointer, count: Int(converted.frameLength)))
                } else { pcm = [] }
                self.queue.async {
                    self.captureLock.lock(); self.capturePending -= 1; self.captureLock.unlock()
                    if pcm.isEmpty { self.onFailure?("microphone_conversion") } else { self.onPCM?(pcm) }
                }
            }
            tapping = true
        }
        engine.prepare()
        try engine.start()
        presentationLatency = max(engine.outputNode.presentationLatency, player.outputPresentationLatency)
        guard presentationLatency.isFinite, presentationLatency >= 0, presentationLatency < 1 else {
            throw ClientFailure("unknown_output_presentation_latency")
        }
        metadata("audio.capability", ["microphone": microphone, "voice_processing": aecEnabled,
                                      "aec_requested": microphone, "input_voice_processing": inputVoiceProcessing,
                                      "output_voice_processing": outputVoiceProcessing,
                                      "presentation_latency_ms": presentationLatency * 1000,
                                      "output_sample_rate": engine.outputNode.outputFormat(forBus: 0).sampleRate])
    }
    func begin() throws {
        guard decoder == nil else { throw ClientFailure("decoder_overlap") }
        generation += 1; segments.removeAll(); nextFrame = 0; accepted = 0; completed = 0
        player.stop()
        decoder = try OpusDecoder(rate: 24000)
    }
    func enqueue(_ packet: Data) throws {
        guard let decoder else { throw ClientFailure("missing_decoder") }
        let pcm = try decoder.decode(packet)
        guard let buffer = AVAudioPCMBuffer(pcmFormat: format, frameCapacity: AVAudioFrameCount(pcm.count)),
              let pointer = buffer.floatChannelData?[0] else { throw ClientFailure("playback_buffer") }
        buffer.frameLength = AVAudioFrameCount(pcm.count)
        for i in pcm.indices { pointer[i] = Float(pcm[i]) / 32768 }
        // Give an underrun a new explicit clock segment; elapsed silence is never played audio.
        if player.isPlaying, let render = player.lastRenderTime, let clock = player.playerTime(forNodeTime: render),
           clock.sampleTime + 256 > nextFrame {
            nextFrame = clock.sampleTime + 512
            metadata("audio.underrun", ["completed_samples": completed, "accepted_samples": accepted])
        }
        let index = segments.count, token = generation
        segments.append(Segment(start: nextFrame, logical: accepted, count: pcm.count))
        let when = AVAudioTime(sampleTime: nextFrame, atRate: 24000)
        nextFrame += AVAudioFramePosition(pcm.count); accepted += pcm.count
        player.scheduleBuffer(buffer, at: when, options: [], completionCallbackType: .dataPlayedBack) { [weak self] type in
            guard let self else { return }
            self.queue.async {
                guard token == self.generation, type == .dataPlayedBack, index < self.segments.count else { return }
                self.segments[index].done = true
                var contiguous = 0
                for segment in self.segments {
                    if !segment.done { break }
                    contiguous += segment.count
                }
                self.completed = contiguous
                self.onCompleted?(contiguous)
            }
        }
        if !player.isPlaying { player.play() }
    }
    func played() throws -> Int {
        guard decoder != nil else { return 0 }
        guard let render = player.lastRenderTime, render.isSampleTimeValid,
              let clock = player.playerTime(forNodeTime: render), clock.isSampleTimeValid else {
            // Before the first render, zero is conservative and trustworthy.
            return completed
        }
        guard clock.sampleRate == 24000 else {
            metadata("audio.clock_invalid", ["sample_rate": clock.sampleRate, "sample_time": clock.sampleTime])
            throw ClientFailure("unknown_player_clock")
        }
        // CoreAudio exposes a short negative pre-roll after each player.play().
        if clock.sampleTime < 0 { return completed }
        // The render clock leads presentation. An extra 512-frame render guard is conservative.
        let presented = clock.sampleTime - AVAudioFramePosition(ceil(presentationLatency * 24000)) - 512
        var position = completed
        for segment in segments {
            let count = max(0, min(segment.count, Int(presented - segment.start)))
            if count > 0 { position = max(position, segment.logical + count) }
            if count < segment.count { break }
        }
        return min(position, accepted)
    }
    func end() {
        // Called only after all accepted samples have dataPlayedBack evidence.
        generation += 1; player.stop(); decoder = nil; segments.removeAll()
    }
    func stop() {
        generation += 1
        if tapping { engine.inputNode.removeTap(onBus: 0); tapping = false }
        player.stop(); engine.stop(); decoder = nil; converter = nil; segments.removeAll()
        onPCM = nil
    }
}
