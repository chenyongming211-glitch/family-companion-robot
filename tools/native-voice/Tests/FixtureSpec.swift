import Foundation

@main struct FixtureSpec {
    static func state(_ name: String, _ state: String, _ next: Int, revision: Int = 1, total: Int = 3) -> [String: Any] {
        ["type": "content.state", "task_id": name + ":" + String(revision), "revision": revision,
         "total": total, "confirmed_count": next, "next": next, "state": state]
    }
    static func handoff(_ unit: Int) -> [String: Any] {
        ["type": "output.handoff", "block_id": 3, "content_id": "main:1", "content_revision": 1, "unit_index": unit]
    }
    static func paused() throws -> FixtureInput.Proof {
        let proof = FixtureInput.Proof()
        try proof.observe(state("main", "playing", 0))
        try proof.observe(handoff(0))
        try proof.interrupt()
        try proof.observe(state("main", "draining", 0))
        try proof.observe(state("main", "paused", 1)) // late real drain confirms old unit0
        try proof.observe(state("side", "playing", 0, total: 1))
        try proof.observe(state("side", "completed", 1, total: 1))
        return proof
    }
    static func resume(_ proof: FixtureInput.Proof) throws {
        try proof.observe(["type": "content.action", "action": "resume", "target": "main"])
        try proof.observe(state("main", "playing", 1))
    }
    static func rejects(_ body: () throws -> Void) {
        do { try body(); fatalError("invalid fixture history accepted") } catch {}
    }
    static func main() throws {
        let good = try paused()
        try resume(good)
        try good.observe(handoff(1))
        try good.observe(state("main", "completed", 3))
        precondition(good.verified && good.resumedFrom == 1, "fixture success must prove original revision resumes at latest confirmed checkpoint")
        rejects {
            let proof = try paused()
            try proof.observe(state("main", "playing", 0, revision: 2))
        }
        rejects {
            let proof = try paused()
            try proof.observe(["type": "content.action", "action": "resume", "target": "main"])
            try proof.observe(state("main", "playing", 0))
        }
        rejects {
            let proof = try paused()
            try resume(proof)
            try proof.observe(handoff(0))
        }
        rejects {
            let proof = try paused()
            try proof.observe(state("main", "completed", 3))
        }
        rejects {
            let proof = FixtureInput.Proof()
            try proof.observe(state("main", "playing", 0))
            try proof.observe(handoff(0))
            try proof.interrupt()
            try proof.observe(state("side", "completed", 1, total: 1))
        }
        print("PASS fixture proof: immutable original, late drain checkpoint, main resume action, exact resumed unit; replacement/wrong checkpoint/missing pause/premature completion rejected")
    }
}
