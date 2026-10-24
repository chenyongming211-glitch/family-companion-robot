import Foundation

func integer(_ object: [String: Any], _ key: String) throws -> Int {
    guard let number = object[key] as? NSNumber, String(cString: number.objCType) != "c",
          number.doubleValue.isFinite, number.doubleValue.rounded() == number.doubleValue,
          number.doubleValue >= 0, number.doubleValue <= Double(Int.max) else { throw ClientFailure("invalid_integer_" + key) }
    return number.intValue
}

final class DrainPlayer {
    struct Final {
        let payload: [String: Any]
        let epoch: Int
        let began: TimeInterval
        var sent: TimeInterval
    }
    let ledger = PlaybackLedger()
    let audio: AudioPipeline
    let queue: DispatchQueue
    let session: String
    var send: (([String: Any]) -> Void)?
    var failure: ((String) -> Void)?
    var progress: ((Int, Int) -> Void)?
    var started: ((Int) -> Void)?
    var finalized: ((Int, Bool) -> Void)?
    var acknowledged: ((Int) -> Void)?
    private(set) var current: Int?
    private(set) var finals: [Int: Final] = [:]
    private var timer: DispatchSourceTimer?
    private var stopped = false
    private(set) var appliedControlSequence = 0
    private var pendingPacket: (id: Int, epoch: Int, samples: Int)?
    private var lastProgress: [Int: Int] = [:]
    var settled: Bool { current == nil && finals.isEmpty && ledger.blocks.values.allSatisfy { $0.finished } }

    init(session: String, audio: AudioPipeline, queue: DispatchQueue) {
        self.session = session; self.audio = audio; self.queue = queue
        audio.onCompleted = { [weak self] samples in self?.didPlay(samples) }
        audio.onFailure = { [weak self] code in self?.unknown(code) }
        let timer = DispatchSource.makeTimerSource(queue: queue)
        timer.schedule(deadline: .now() + .milliseconds(50), repeating: .milliseconds(50))
        timer.setEventHandler { [weak self] in self?.tick() }
        self.timer = timer; timer.resume()
    }
    func event(_ e: [String: Any]) throws {
        guard !stopped, e["session_id"] as? String == session,
              let type = e["type"] as? String else { throw ClientFailure("wrong_session") }
        guard pendingPacket == nil else { throw ClientFailure("missing_adjacent_binary") }
        let sequence = try integer(e, "control_seq")
        guard sequence > appliedControlSequence else { throw ClientFailure("stale_control_sequence") }
        appliedControlSequence = sequence
        switch type {
        case "output.handoff":
            let id = try integer(e, "block_id"), epoch = try integer(e, "epoch")
            guard let raw = e["after"] as? [Any] else { throw ClientFailure("missing_after") }
            let after = try raw.map { try integer(["value": $0], "value") }
            try ledger.handoff(id: id, epoch: epoch, inputEpoch: integer(e, "input_epoch"), after: after)
            metadata("output.handoff", ["block_id": id, "epoch": epoch, "after": after, "input_epoch": try integer(e, "input_epoch")])
        case "audio.packet":
            let id = try integer(e, "block_id"), epoch = try integer(e, "epoch")
            if let playback = e["playback_id"] { guard try integer(["id": playback], "id") == id else { throw ClientFailure("wrong_playback_id") } }
            let samples = try integer(e, "samples")
            _ = try ledger.block(id, epoch: epoch)
            guard samples == 1440 else { throw ClientFailure("wrong_packet_duration") }
            pendingPacket = (id, epoch, samples)
        case "audio.end":
            let id = try integer(e, "block_id"), epoch = try integer(e, "epoch")
            try ledger.seal(id: id, epoch: epoch, sent: integer(e, "device_sent_samples"),
                            valid: integer(e, "valid_samples"), lookahead: integer(e, "lookahead_samples"))
            metadata("audio.end", ["block_id": id, "epoch": epoch, "device_sent_samples": try integer(e, "device_sent_samples"),
                                   "valid_samples": try integer(e, "valid_samples"), "lookahead_samples": try integer(e, "lookahead_samples")])
            if let b = ledger.blocks[id], b.revoked && !b.started { try finish(id, discarded: true) }
            else { try completeIfReady() }
        case "output.revoke":
            guard let raw = e["blocks"] as? [Any] else { throw ClientFailure("missing_revoke_blocks") }
            let ids = try raw.map { try integer(["value": $0], "value") }
            try ledger.revoke(ids, inputEpoch: integer(e, "input_epoch"))
            for id in ids {
                if let b = ledger.blocks[id], !b.started && !b.finished { try finish(id, discarded: true) }
            }
            metadata("output.revoke", ["blocks": ids, "input_epoch": try integer(e, "input_epoch")])
        case "output.drain":
            metadata("output.drain", ["blocks": e["blocks"] as? [Int] ?? []])
        case "playback.ack":
            let id = try integer(e, "block_id"), epoch = try integer(e, "epoch")
            _ = try ledger.block(id, epoch: epoch)
            if let final = finals[id], final.epoch == epoch {
                finals.removeValue(forKey: id)
                metadata("playback.ack", ["block_id": id, "epoch": epoch])
                acknowledged?(id)
            }
        case "audio.cancel": throw ClientFailure("unsupported_clear_audio")
        default: break
        }
        try pump()
    }
    func binary(_ packet: Data) throws {
        guard let meta = pendingPacket, !stopped else { throw ClientFailure("binary_without_metadata") }
        pendingPacket = nil
        guard packet.count > 0, packet.count <= 4000 else { throw ClientFailure("packet_limit") }
        try ledger.accept(id: meta.id, epoch: meta.epoch, samples: meta.samples)
        let block = try ledger.block(meta.id, epoch: meta.epoch)
        block.packets.append(packet)
        try pump()
    }
    private func pump() throws {
        if current == nil, let id = ledger.order.first(where: { ledger.canBegin($0) && !(ledger.blocks[$0]?.packets.isEmpty ?? true) }) {
            try ledger.begin(id)
            try audio.begin()
            current = id
            guard let b = ledger.blocks[id] else { throw ClientFailure("missing_current") }
            var event = identity(b); event["type"] = "playback.started"
            event["observed_control_seq"] = appliedControlSequence
            send?(event)
            metadata("playback.started", ["block_id": id, "epoch": b.epoch, "input_epoch": b.inputEpoch, "observed_control_seq": appliedControlSequence])
            started?(id)
        }
        if let id = current, let b = ledger.blocks[id] {
            let packets = b.packets; b.packets.removeAll()
            for packet in packets { try audio.enqueue(packet) }
        }
    }
    private func identity(_ b: PlaybackBlock) -> [String: Any] {
        ["session_id": session, "block_id": b.id, "playback_id": b.id, "epoch": b.epoch,
         "basis": "coreaudio_playback", "sample_rate": 24000]
    }
    private func receipt(_ b: PlaybackBlock, valid: Bool = true, finished: Bool, discarded: Bool = false) -> [String: Any] {
        var e = identity(b)
        e["type"] = "playback"; e["played_samples"] = b.played
        e["valid"] = valid; e["finished"] = finished; e["discarded"] = discarded
        return e
    }
    private func didPlay(_ samples: Int) {
        guard !stopped, let id = current, let b = ledger.blocks[id] else { return }
        do {
            b.completed = samples
            try ledger.observe(id: id, played: max(b.played, samples))
            metadata("coreaudio.data_played_back", ["block_id": id, "samples": samples])
            progress?(id, b.played)
            try completeIfReady()
        } catch { unknown("data_played_back_coordinate") }
    }
    private func completeIfReady() throws {
        if let id = current, let b = ledger.blocks[id], b.sealed, b.completed == b.accepted {
            try ledger.observe(id: id, played: b.accepted)
            try finish(id, discarded: false)
        }
    }
    private func finish(_ id: Int, discarded: Bool) throws {
        try ledger.finish(id, discarded: discarded)
        guard let b = ledger.blocks[id] else { throw ClientFailure("missing_final") }
        if current == id { audio.end(); current = nil }
        lastProgress.removeValue(forKey: id)
        let e = receipt(b, finished: true, discarded: discarded)
        let now = ProcessInfo.processInfo.systemUptime
        finals[id] = Final(payload: e, epoch: b.epoch, began: now, sent: now)
        send?(e)
        metadata("playback.final", ["block_id": id, "epoch": b.epoch, "played_samples": b.played,
                                    "device_sent_samples": b.accepted, "discarded": discarded,
                                    "evidence": discarded ? "unstarted_revocation" : "coreaudio_dataPlayedBack"])
        finalized?(id, discarded)
        try pump() // Local gate releases immediately; no network ACK is needed here.
    }
    private func tick() {
        guard !stopped else { return }
        do {
            if let id = current, let b = ledger.blocks[id] {
                let played = try audio.played()
                try ledger.observe(id: id, played: max(b.played, played))
                if b.played > (lastProgress[id] ?? -1) {
                    lastProgress[id] = b.played
                    send?(receipt(b, finished: false))
                    metadata("playback.progress", ["block_id": id, "epoch": b.epoch, "played_samples": b.played,
                                                   "remaining_samples": ledger.remaining])
                    progress?(id, b.played)
                }
            }
            let now = ProcessInfo.processInfo.systemUptime
            for id in Array(finals.keys) {
                guard var final = finals[id] else { continue }
                if now - final.began > 2.5 { unknown("final_ack_timeout"); return }
                if now - final.sent >= 0.095 {
                    final.sent = now; finals[id] = final; send?(final.payload)
                    metadata("playback.final_retry", ["block_id": id, "epoch": final.epoch])
                }
            }
        } catch { unknown("player_clock_unknown") }
    }
    func unknown(_ code: String) {
        guard !stopped else { return }
        if let id = current, let b = ledger.blocks[id] { send?(receipt(b, valid: false, finished: false)) }
        metadata("playback.unknown", ["code": code])
        stop(); failure?(code)
    }
    func stop() {
        guard !stopped else { return }
        stopped = true; timer?.cancel(); timer = nil; audio.stop(); pendingPacket = nil
        finals.removeAll()
        lastProgress.removeAll()
        for b in ledger.blocks.values { b.packets.removeAll() }
    }
}
