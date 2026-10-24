import Foundation

struct ClientFailure: Error, CustomStringConvertible {
    let description: String
    init(_ code: String) { description = code }
}

final class PlaybackBlock {
    let id: Int, epoch: Int, inputEpoch: Int, after: [Int]
    var packets: [Data] = []
    var accepted = 0, played = 0, completed = 0
    var sealed = false, started = false, finished = false, revoked = false
    var discarded = false, validSamples = 0, lookahead = 0
    init(id: Int, epoch: Int, inputEpoch: Int, after: [Int]) {
        self.id = id; self.epoch = epoch; self.inputEpoch = inputEpoch; self.after = after
    }
}

// Access only from the client's serial queue. ACKs never authorize local playback.
final class PlaybackLedger {
    private(set) var blocks: [Int: PlaybackBlock] = [:]
    private(set) var order: [Int] = []
    private var lastID = 0, latestInputEpoch = 0
    var remaining: Int { blocks.values.filter { !$0.finished }.reduce(0) { $0 + $1.accepted - $1.played } }

    func handoff(id: Int, epoch: Int, inputEpoch: Int, after: [Int]) throws {
        guard id > lastID, epoch >= 0, inputEpoch >= latestInputEpoch, after.count <= 16,
              Set(after).count == after.count, blocks.values.filter({ !$0.finished }).count < 16 else {
            throw ClientFailure("invalid_handoff")
        }
        for predecessor in after {
            guard let b = blocks[predecessor], b.epoch < epoch else { throw ClientFailure("unknown_dependency") }
        }
        latestInputEpoch = inputEpoch
        blocks[id] = PlaybackBlock(id: id, epoch: epoch, inputEpoch: inputEpoch, after: after)
        order.append(id); lastID = id
        while order.count > 64, let index = order.firstIndex(where: { blocks[$0]?.finished == true }) {
            blocks.removeValue(forKey: order.remove(at: index))
        }
    }
    func block(_ id: Int, epoch: Int) throws -> PlaybackBlock {
        guard let b = blocks[id], b.epoch == epoch else { throw ClientFailure("wrong_block_identity") }
        return b
    }
    func accept(id: Int, epoch: Int, samples: Int) throws {
        let b = try block(id, epoch: epoch)
        guard !b.sealed, !b.finished, samples == 1440,
              b.accepted + samples <= 24_000 * 12 + 2880, remaining + samples <= 14_400 else {
            throw ClientFailure("audio_allowance_or_duration")
        }
        b.accepted += samples
    }
    func seal(id: Int, epoch: Int, sent: Int, valid: Int, lookahead: Int) throws {
        let b = try block(id, epoch: epoch)
        guard !b.sealed, sent == b.accepted, valid >= 0, valid <= 24_000 * 12,
              lookahead >= 0, lookahead < 1440, valid + lookahead <= sent,
              sent - valid - lookahead < 1440 else { throw ClientFailure("invalid_audio_end") }
        b.sealed = true; b.validSamples = valid; b.lookahead = lookahead
    }
    func canBegin(_ id: Int) -> Bool {
        guard let b = blocks[id], !b.started, !b.finished, !b.revoked,
              b.inputEpoch == latestInputEpoch,
              b.after.allSatisfy({ blocks[$0]?.finished == true }) else { return false }
        return !blocks.values.contains { $0.started && !$0.finished }
    }
    func begin(_ id: Int) throws {
        guard canBegin(id), let b = blocks[id] else { throw ClientFailure("handoff_gate_closed") }
        b.started = true
    }
    func observe(id: Int, played: Int) throws {
        guard let b = blocks[id], played >= b.played, played <= b.accepted,
              b.started || played == 0 else { throw ClientFailure("invalid_playback_coordinate") }
        b.played = played
    }
    func finish(_ id: Int, discarded: Bool) throws {
        guard let b = blocks[id], b.sealed, !b.finished else { throw ClientFailure("unsealed_final") }
        if discarded {
            guard b.revoked, !b.started, !b.after.isEmpty, b.played == 0 else { throw ClientFailure("invalid_discard") }
        } else {
            guard b.started, b.played == b.accepted else { throw ClientFailure("incomplete_final") }
        }
        b.finished = true; b.discarded = discarded; b.packets.removeAll()
    }
    func revoke(_ ids: [Int], inputEpoch: Int) throws {
        guard inputEpoch >= latestInputEpoch, ids.count <= 16, Set(ids).count == ids.count else { throw ClientFailure("stale_revoke") }
        latestInputEpoch = inputEpoch
        for id in ids {
            guard let b = blocks[id], b.sealed else { throw ClientFailure("invalid_revoke") }
            if !b.finished { b.revoked = true }
        }
    }
}
