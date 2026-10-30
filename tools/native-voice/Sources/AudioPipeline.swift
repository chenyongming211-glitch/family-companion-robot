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
    private var captureCallbacks = 0, captureSamples = 0
    private var capturePeak = 0.0
    private var converter: AVAudioConverter?
    private(set) var aecEnabled = false
    private var inputVoiceProcessing = false, outputVoiceProcessing = false
    private(set) var presentationLatency = 0.0
    private let diagnostics: AudioDiagnostics?
    private let correlation: AudioCorrelationProbe?
    private var captureEvidence: AudioCaptureEvidence?
    private var diagnosticPlaybackID = 0

    var captureStatistics: (callbacks: Int, samples: Int, peak: Double) {
        captureLock.lock(); defer { captureLock.unlock() }
        return (captureCallbacks, captureSamples, capturePeak)
    }

    init(queue: DispatchQueue, diagnosticSessionID: String? = nil) {
        self.queue = queue
        let trace = AudioDiagnostics.make(sessionID: diagnosticSessionID)
        diagnostics = trace
        correlation = AudioCorrelationProbe.make(diagnostics: trace)
        engine.attach(player)
        engine.connect(player, to: engine.mainMixerNode, format: format)
        NotificationCenter.default.addObserver(forName: .AVAudioEngineConfigurationChange, object: engine, queue: nil) { [weak self] _ in
            guard let self else { return }
            self.queue.async { if self.engine.isRunning { self.onFailure?("audio_device_configuration_changed") } }
        }
    }
    func start(microphone: Bool, voiceProcessing: Bool = true) throws {
        if microphone {
            do {
                try engine.inputNode.setVoiceProcessingEnabled(voiceProcessing)
            } catch { metadata("audio.voice_processing_request", ["accepted": false]) }
            inputVoiceProcessing = engine.inputNode.isVoiceProcessingEnabled
            outputVoiceProcessing = engine.outputNode.isVoiceProcessingEnabled
            aecEnabled = inputVoiceProcessing && outputVoiceProcessing
            let input = engine.inputNode
            let source = input.outputFormat(forBus: 0)
            metadata("microphone.format", ["sample_rate": source.sampleRate, "channels": source.channelCount,
                                           "interleaved": source.isInterleaved, "voice_processing_muted": input.isVoiceProcessingInputMuted])
            if diagnostics != nil {
                captureEvidence = AudioCaptureEvidence(channels: Int(source.channelCount), rate: source.sampleRate)
                captureEvidence?.begin(playbackID: 0, generation: 0)
                traceState("voice_processing_configured")
            }
            let converter = try microphoneConverter(from: source)
            let target = converter.outputFormat
            self.converter = converter
            input.installTap(onBus: 0, bufferSize: 1024, format: source) { [weak self] buffer, time in
                guard let self else { return }
                let sourceHost = self.correlation != nil && time.isHostTimeValid ? AVAudioTime.seconds(forHostTime: time.hostTime) : nil
                let sourceFrames = Int(buffer.frameLength)
                self.captureLock.lock()
                self.captureCallbacks += 1
                self.captureSamples += Int(buffer.frameLength)
                self.captureEvidence?.add(buffer, at: time)
                if let channels = buffer.floatChannelData {
                    let count = Int(buffer.frameLength)
                    let channelCount = Int(buffer.format.channelCount)
                    let stride = buffer.format.isInterleaved ? channelCount : 1
                    for i in 0..<count {
                        self.capturePeak = max(self.capturePeak, Double(abs(channels[0][i * stride])))
                    }
                }
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
                    self.correlation?.capture(pcm, sourceHostSeconds: sourceHost, sourceFrames: sourceFrames, sourceRate: source.sampleRate)
                    self.traceCapture()
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
                                      "aec_requested": microphone && voiceProcessing, "input_voice_processing": inputVoiceProcessing,
                                      "output_voice_processing": outputVoiceProcessing,
                                      "presentation_latency_ms": presentationLatency * 1000,
                                      "output_sample_rate": engine.outputNode.outputFormat(forBus: 0).sampleRate])
        traceState("engine_ready")
    }
    func begin(diagnosticID: Int? = nil) throws {
        guard decoder == nil else { throw ClientFailure("decoder_overlap") }
        traceCapture(force: true)
        generation += 1; segments.removeAll(); nextFrame = 0; accepted = 0; completed = 0
        diagnosticPlaybackID = diagnosticID ?? 0 // 0 means no wire identity; generation remains local.
        if diagnostics?.active == true {
            captureLock.lock()
            captureEvidence?.begin(playbackID: diagnosticPlaybackID, generation: generation)
            captureLock.unlock()
        }
        player.stop()
        decoder = try OpusDecoder(rate: 24000)
        correlation?.begin(playbackID: diagnosticPlaybackID, generation: generation)
        traceState("playback_begin")
    }
    func traceFirstPacket(bytes: Int) {
        diagnostics?.note("first_packet_received", ["playback_id": diagnosticPlaybackID,
            "generation": generation, "opus_bytes": bytes])
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
        if index == 0 { traceState("first_buffer_will_schedule") }
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
                // The external callback can synchronously end this playback or
                // begin another. Preserve the completing buffer's state first.
                let firstPlayed = index == 0 ? self.traceStateSnapshot("first_buffer_data_played") : nil
                let referenceOrigin = index <= 48 && index % 16 == 0 ? self.correlationClockOrigin() : nil
                self.onCompleted?(contiguous)
                if let referenceOrigin { self.correlation?.anchor(generation: token, originHostSeconds: referenceOrigin) }
                if let firstPlayed { self.diagnostics?.note("audio_state", firstPlayed) }
            }
        }
        if !player.isPlaying {
            player.play()
            traceState("player_play_invoked")
        }
        correlation?.reference(pcm, frame: Int64(when.sampleTime), generation: token)
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
        traceCapture(force: true)
        traceState("playback_end")
        correlation?.end(generation: generation)
        generation += 1; player.stop(); decoder = nil; segments.removeAll()
    }
    func stop() {
        traceCapture(force: true)
        traceState("audio_will_stop")
        generation += 1
        if tapping { engine.inputNode.removeTap(onBus: 0); tapping = false }
        player.stop(); engine.stop(); decoder = nil; converter = nil; segments.removeAll()
        onPCM = nil
        correlation?.stop()
        if let diagnostics {
            let captured = captureStatistics
            diagnostics.note("session_stopped", ["capture_callbacks": captured.callbacks, "capture_frames": captured.samples], terminal: true)
        }
    }
    private func correlationClockOrigin() -> Double? {
        guard correlation?.active == true, let render = player.lastRenderTime, render.isHostTimeValid,
              let clock = player.playerTime(forNodeTime: render), clock.isSampleTimeValid,
              clock.sampleRate == 24000, clock.sampleTime >= 0, clock.sampleTime <= nextFrame + 2048 else { return nil }
        return AVAudioTime.seconds(forHostTime: render.hostTime) - Double(clock.sampleTime) / 24000
    }
    private func traceCapture(force: Bool = false) {
        guard let diagnostics, diagnostics.active else { return }
        captureLock.lock()
        let snapshot = captureEvidence?.snapshot(force: force)
        captureLock.unlock()
        if let snapshot { diagnostics.note("input_energy_window", snapshot) }
    }
    private func traceState(_ phase: String) {
        if let state = traceStateSnapshot(phase) { diagnostics?.note("audio_state", state) }
    }
    private func traceStateSnapshot(_ phase: String) -> [String: Any]? {
        guard let diagnostics, diagnostics.active else { return nil }
        let queryStarted = ProcessInfo.processInfo.systemUptime
        var fields: [String: Any] = ["phase": phase, "playback_id": diagnosticPlaybackID,
            "generation": generation, "engine_running": engine.isRunning,
            "observed_monotonic_ms": Int(queryStarted * 1000),
            "player_playing": player.isPlaying, "accepted": accepted, "completed": completed,
            "player_render": AudioDiagnostics.clock(player.lastRenderTime),
            "output_render": AudioDiagnostics.clock(engine.outputNode.lastRenderTime),
            "player_format": AudioDiagnostics.format(player.outputFormat(forBus: 0)),
            "output_input_format": AudioDiagnostics.format(engine.outputNode.inputFormat(forBus: 0)),
            "output_hardware_format": AudioDiagnostics.format(engine.outputNode.outputFormat(forBus: 0))]
        let captured = captureStatistics
        fields["capture_callbacks"] = captured.callbacks
        fields["capture_frames"] = captured.samples
        if tapping || captureEvidence != nil {
            let input = engine.inputNode
            fields["vp_input_enabled"] = input.isVoiceProcessingEnabled
            fields["vp_output_enabled"] = engine.outputNode.isVoiceProcessingEnabled
            fields["vp_bypassed"] = input.isVoiceProcessingBypassed
            fields["vp_agc_enabled"] = input.isVoiceProcessingAGCEnabled
            fields["vp_input_muted"] = input.isVoiceProcessingInputMuted
            fields["input_hardware_format"] = AudioDiagnostics.format(input.inputFormat(forBus: 0))
            fields["input_processed_format"] = AudioDiagnostics.format(input.outputFormat(forBus: 0))
            fields["input_render"] = AudioDiagnostics.clock(input.lastRenderTime)
        }
        fields["state_query_ms"] = (ProcessInfo.processInfo.systemUptime - queryStarted) * 1000
        return fields
    }
}
