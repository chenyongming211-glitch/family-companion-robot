import Foundation

@main struct LedgerSpec {
    static func main() throws {
        precondition(nativeEndpoint("ws://127.0.0.1:18021/xiaozhi/v1/") != nil, "Foundation normalized trailing slash must accept native endpoint")
        precondition(nativeEndpoint("ws://example.com/xiaozhi/v1/") == nil)
        precondition(nativeEndpoint("ws://127.0.0.1:18020/xiaozhi/v1/?ticket=secret") == nil)
        let ledger = PlaybackLedger()
        try ledger.handoff(id: 1, epoch: 0, inputEpoch: 0, after: [])
        try ledger.accept(id: 1, epoch: 0, samples: 1440)
        try ledger.seal(id: 1, epoch: 0, sent: 1440, valid: 1000, lookahead: 156)
        try ledger.begin(1)
        try ledger.handoff(id: 2, epoch: 1, inputEpoch: 1, after: [1])
        try ledger.accept(id: 2, epoch: 1, samples: 1440)
        try ledger.seal(id: 2, epoch: 1, sent: 1440, valid: 1000, lookahead: 156)
        precondition(!ledger.canBegin(2), "staged output must wait for real old completion")
        try ledger.observe(id: 1, played: 720)
        precondition(!ledger.canBegin(2), "partial playback must not release dependency")
        try ledger.observe(id: 1, played: 1440)
        try ledger.finish(1, discarded: false)
        precondition(ledger.canBegin(2), "local actual completion releases staged output without ACK")
        try ledger.begin(2)
        try ledger.revoke([2], inputEpoch: 2)
        precondition(!ledger.blocks[2]!.finished, "already begun output must drain after revoke")
        try ledger.observe(id: 2, played: 1440)
        try ledger.finish(2, discarded: false)
        try ledger.handoff(id: 3, epoch: 2, inputEpoch: 2, after: [2])
        try ledger.accept(id: 3, epoch: 2, samples: 1440)
        try ledger.seal(id: 3, epoch: 2, sent: 1440, valid: 1000, lookahead: 156)
        try ledger.revoke([3], inputEpoch: 3)
        try ledger.finish(3, discarded: true)
        precondition(ledger.blocks[3]!.played == 0 && ledger.blocks[3]!.finished)
        do { try ledger.handoff(id: 4, epoch: 3, inputEpoch: 2, after: []); fatalError("stale authorization accepted") } catch {}
        do { try ledger.observe(id: 2, played: 100); fatalError("backward coordinate accepted") } catch {}
        let bounded = PlaybackLedger()
        try bounded.handoff(id: 1, epoch: 0, inputEpoch: 0, after: [])
        for _ in 0..<10 { try bounded.accept(id: 1, epoch: 0, samples: 1440) }
        do { try bounded.accept(id: 1, epoch: 0, samples: 1440); fatalError("600ms aggregate bound exceeded") } catch {}
        print("PASS ledger: dependency gate, partial progress, ACK-independent release, revoke, stale identity, monotonicity, aggregate bound")
    }
}
