import Foundation

final class PlaybackSelfTest {
    let queue = DispatchQueue(label: "family.native.selftest")
    private var audio: AudioPipeline?
    private var player: DrainPlayer?
    private var oldFinal = false, newStarted = false, discarded = false
    private var sends: [Int: Int] = [:]
    private var done = false
    private var sequence = 0
    private func control(_ event: [String: Any]) throws {
        sequence += 1
        var event = event; event["control_seq"] = sequence
        try player?.event(event)
    }
    func start() {
        queue.async {
            do {
                let audio = AudioPipeline(queue: self.queue)
                self.audio = audio
                let player = DrainPlayer(session: "controlled-tone-self-test", audio: audio, queue: self.queue)
                self.player = player
                player.failure = { [weak self] _ in self?.finish(false) }
                player.send = { [weak self] event in
                    guard let self, event["type"] as? String == "playback", event["finished"] as? Bool == true,
                          let id = event["block_id"] as? Int, let epoch = event["epoch"] as? Int else { return }
                    self.sends[id, default: 0] += 1
                    // Drop the first final ACK, then ACK the repeated final through the real parser.
                    if self.sends[id] == 2 {
                        self.queue.asyncAfter(deadline: .now() + .milliseconds(150)) {
                            do {
                                try self.control(["type": "playback.ack", "session_id": player.session, "block_id": id, "epoch": epoch])
                                self.check()
                            } catch { self.finish(false) }
                        }
                    }
                }
                player.started = { [weak self] id in
                    guard let self else { return }
                    if id == 2 {
                        guard self.oldFinal, self.sends[1, default: 0] >= 1 else { self.finish(false); return }
                        self.newStarted = true
                        metadata("selftest.handoff", ["old_locally_complete": true, "old_ack_pending": player.finals[1] != nil,
                                                      "new_block_id": 2, "overlap": false])
                    }
                    if id == 3 { self.finish(false) }
                }
                player.finalized = { [weak self] id, discarded in
                    guard let self else { return }
                    if id == 1 { self.oldFinal = true }
                    if id == 3 { self.discarded = discarded }
                    self.check()
                }
                player.acknowledged = { [weak self] _ in self?.check() }
                try audio.start(microphone: false)
                try self.feed(id: 1, epoch: 0, after: [], validSamples: 7200, frequency: 440)
                try self.feed(id: 2, epoch: 1, after: [1], validSamples: 2400, frequency: 660)
                try self.feed(id: 3, epoch: 1, after: [1], validSamples: 500, frequency: 880)
                try self.control(["type": "output.revoke", "session_id": player.session, "blocks": [3], "input_epoch": 1])
                self.queue.asyncAfter(deadline: .now() + 8) { [weak self] in self?.finish(false) }
            } catch {
                metadata("selftest.failure", ["code": (error as? ClientFailure)?.description ?? "audio_setup"])
                self.finish(false)
            }
        }
    }
    private func feed(id: Int, epoch: Int, after: [Int], validSamples: Int, frequency: Double) throws {
        guard let player else { throw ClientFailure("selftest_missing_player") }
        let encoder = try OpusEncoder(rate: 24000)
        let total = ((validSamples + encoder.lookahead + 1439) / 1440) * 1440
        try self.control(["type": "output.handoff", "session_id": player.session, "block_id": id,
                          "epoch": epoch, "input_epoch": epoch, "after": after])
        for start in stride(from: 0, to: total, by: 1440) {
            var pcm = [Int16](repeating: 0, count: 1440)
            for index in 0..<1440 where start + index < validSamples {
                pcm[index] = Int16(sin(Double(start + index) * frequency * 2 * .pi / 24000) * 4000)
            }
            try self.control(["type": "audio.packet", "session_id": player.session, "block_id": id,
                              "epoch": epoch, "playback_id": id, "samples": 1440])
            try player.binary(encoder.encode(pcm))
        }
        try self.control(["type": "audio.end", "session_id": player.session, "block_id": id, "epoch": epoch,
                          "device_sent_samples": total, "valid_samples": validSamples, "lookahead_samples": encoder.lookahead])
    }
    private func check() {
        guard !done, let player, oldFinal, newStarted, discarded, player.settled,
              player.ledger.blocks[2]?.finished == true,
              sends.values.allSatisfy({ $0 >= 2 }) else { return }
        metadata("selftest.success", ["old_drained": true, "staged_handoff_no_overlap": true,
                                     "unstarted_revoked": true, "final_retry_acked": true,
                                     "evidence": "actual_CoreAudio_dataPlayedBack", "microphone": false])
        finish(true)
    }
    private func finish(_ success: Bool) {
        guard !done else { return }; done = true
        player?.stop(); audio?.stop()
        if !success { metadata("selftest.failure", ["code": "coreaudio_handoff_or_ack_failed"]) }
        exit(success ? 0 : 1)
    }
}
