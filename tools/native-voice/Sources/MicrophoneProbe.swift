import AVFoundation
import Foundation

// Actual capture regression using the same engine as the online client.
// Counts and aggregate levels only; no network, speech recognition or audio files.
final class MicrophoneProbe {
    private let queue = DispatchQueue(label: "family.native.microphone-probe")
    private var audio: AudioPipeline?
    private var callbacks = 0, samples = 0, peak = 0
    private var energy = 0.0
    private let voiceProcessing: Bool

    init(voiceProcessing: Bool = true) { self.voiceProcessing = voiceProcessing }

    func start() {
        AVCaptureDevice.requestAccess(for: .audio) { granted in
            guard granted else { metadata("microphone.probe_failure", ["code": "permission_denied"]); exit(1) }
            self.queue.async { self.capture() }
        }
    }
    private func capture() {
        let audio = AudioPipeline(queue: queue)
        self.audio = audio
        audio.onPCM = { pcm in
            self.callbacks += 1; self.samples += pcm.count
            for sample in pcm {
                self.peak = max(self.peak, abs(Int(sample)))
                self.energy += Double(sample) * Double(sample)
            }
        }
        audio.onFailure = { code in
            metadata("microphone.probe_failure", ["code": code]); audio.stop(); exit(1)
        }
        do { try audio.start(microphone: true, voiceProcessing: voiceProcessing) }
        catch { metadata("microphone.probe_failure", ["code": "engine_start"]); exit(1) }
        queue.asyncAfter(deadline: .now() + 4) {
            let raw = audio.captureStatistics
            metadata("microphone.probe", ["tap_callbacks": raw.callbacks, "tap_samples": raw.samples,
                                          "tap_peak": raw.peak,
                                          "pcm_callbacks": self.callbacks, "pcm_samples": self.samples,
                                          "peak": self.peak, "rms": self.samples > 0 ? sqrt(self.energy / Double(self.samples)) : 0,
                                          "voice_processing": audio.aecEnabled, "audio_saved": false])
            audio.stop()
            exit(self.samples > 0 ? 0 : 1)
        }
    }
}
