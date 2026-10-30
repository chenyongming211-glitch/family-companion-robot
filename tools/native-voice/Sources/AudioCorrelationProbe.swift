import Foundation

// Raw samples live only in bounded RAM on this utility worker. Only scalar
// summaries reach AudioDiagnostics; never serialize pcm or pointer contents.
final class AudioCorrelationProbe: @unchecked Sendable {
    private let worker = DispatchQueue(label: "family.audio.correlation", qos: .utility)
    private let lock = NSLock()
    private let diagnostics: AudioDiagnostics
    private var core: OpaquePointer?
    private var pending = 0, dropped = 0, closed = false
    var active: Bool {
        lock.lock(); let available = !closed; lock.unlock()
        return available && diagnostics.active
    }

    static func make(diagnostics: AudioDiagnostics?) -> AudioCorrelationProbe? {
        guard let diagnostics, ProcessInfo.processInfo.environment["NATIVE_AUDIO_CORRELATION"] == "1" else { return nil }
        return AudioCorrelationProbe(diagnostics: diagnostics)
    }
    private init?(diagnostics: AudioDiagnostics) {
        guard let core = fc_echo_create() else { return nil }
        self.core = core; self.diagnostics = diagnostics
        diagnostics.note("echo_probe_started", ["analysis_rate": 4000, "window_ms": 250,
            "lag_min_ms": -200, "lag_max_ms": 800, "time_quantum_ms": 0.25,
            "input_ring_seconds": 6, "reference_seconds": 3, "playback_capacity": 2,
            "pending_limit": 8, "filter": "box_mean_4k",
            "reference_source": "decoded_pcm_scheduled",
            "capture_source": "post_voice_processing_pre_opus_16k_pcm",
            "delay_basis": "player_render_to_source_tap_before_src",
            "capture_continuity_tolerance_ms": 0.25, "second_peak_exclusion_ms": 20,
            "converter_latency_calibrated": false])
        worker.asyncAfter(deadline: .now() + 120) { [weak self] in self?.stop(reason: "duration_limit") }
    }
    deinit { if let core { fc_echo_destroy(core) } }

    private func submit(_ job: @escaping (OpaquePointer) -> Void) {
        guard diagnostics.active else { stop(reason: "duration_limit"); return }
        lock.lock()
        guard !closed, pending < 8 else { if !closed { dropped += 1 }; lock.unlock(); return }
        pending += 1
        // Admission and dispatch have the same order, including the terminal job.
        worker.async { [self] in
            if let core { job(core); drain(core) }
            lock.lock(); pending -= 1; lock.unlock()
        }
        lock.unlock()
    }
    func begin(playbackID: Int, generation: Int) {
        guard playbackID > 0 else {
            diagnostics.note("echo_probe_playback_unsupported", ["generation": generation, "reason": "missing_wire_playback_id"])
            return
        }
        submit { fc_echo_begin($0, Int64(playbackID), Int64(generation)) }
    }
    func reference(_ pcm: [Int16], frame: Int64, generation: Int) {
        guard frame >= 0, frame < 72000, !pcm.isEmpty, pcm.count <= 4096 else { return }
        submit { core in pcm.withUnsafeBufferPointer { fc_echo_reference(core, Int64(generation), $0.baseAddress, $0.count, frame) } }
    }
    func capture(_ pcm: [Int16], sourceHostSeconds: Double?) {
        guard let host = sourceHostSeconds, host.isFinite, !pcm.isEmpty, pcm.count <= 4096 else { return }
        submit { core in pcm.withUnsafeBufferPointer { fc_echo_capture(core, $0.baseAddress, $0.count, host) } }
    }
    func anchor(generation: Int, originHostSeconds: Double) {
        submit { fc_echo_anchor($0, Int64(generation), originHostSeconds) }
    }
    func end(generation: Int) { submit { fc_echo_end($0, Int64(generation)) } }
    func stop(reason: String = "audio_stopped") {
        lock.lock()
        guard !closed else { lock.unlock(); return }
        closed = true
        worker.async { [self] in
            if let core {
                fc_echo_close(core); drain(core)
                lock.lock(); let lost = dropped; lock.unlock()
                diagnostics.note("echo_probe_stopped", ["reason": reason,
                    "probe_dropped_jobs": lost, "probe_dropped_playbacks": fc_echo_dropped_playbacks(core)], terminal: true)
                fc_echo_destroy(core); self.core = nil
            }
        }
        lock.unlock()
    }
    private func drain(_ core: OpaquePointer) {
        let statuses = ["measured", "low_reference_energy", "low_capture_energy", "reference_gap", "capture_gap",
                        "reference_clock_missing", "clock_unstable", "incomplete_capture", "short_window"]
        for _ in 0..<24 {
            var result = FCEchoResult()
            let began = ProcessInfo.processInfo.systemUptime
            guard fc_echo_poll(core, &result) != 0 else { break }
            lock.lock(); let lost = dropped; lock.unlock()
            var row: [String: Any] = ["playback_id": result.playback_id, "generation": result.generation,
                "status": statuses.indices.contains(Int(result.status)) ? statuses[Int(result.status)] : "unknown",
                "window_start_ms": result.window_start_ms, "samples": result.samples,
                "reference_origin_host_s": result.reference_origin_host_s, "window_host_s": result.window_host_s,
                "anchor_spread_ms": result.anchor_spread_ms, "anchors": result.anchors,
                "reference_rms": result.reference_rms, "analysis_rate": 4000,
                "analysis_ms": (ProcessInfo.processInfo.systemUptime - began) * 1000,
                "probe_dropped_jobs": lost, "probe_dropped_playbacks": fc_echo_dropped_playbacks(core)]
            if result.status == FC_ECHO_MEASURED {
                row["correlation"] = result.correlation; row["second_peak_abs"] = result.second_peak
                row["apparent_lag_ms"] = result.lag_ms; row["search_boundary"] = result.boundary != 0
                row["tested_lags"] = result.tested_lags; row["capture_rms"] = result.capture_rms
                row["projection_gain"] = result.projection_gain; row["projected_capture_rms"] = result.projected_capture_rms
            }
            diagnostics.note("echo_correlation_window", row, terminal: true)
        }
    }
}
