import Foundation

@main struct SpeakerGuideTests {
    static var checks = 0
    static func check(_ value: Bool, _ message: String) {
        checks += 1; precondition(value, message)
    }
    static func observation(_ id: Int, _ state: String, label: String = "", reason: String = "", audioMS: Int = 2000) throws -> SpeakerObservation {
        try SpeakerObservation.validate(["type": "speaker_observation", "evidence": "turn_taking_test", "input_id": id,
            "start_sample": 0, "end_sample": state == "temporary" ? 32000 : 0, "elapsed_ms": 1,
            "state": state, "label": label, "reason": reason, "audio_ms": audioMS])
    }
    static func main() throws {
        var guide = SpeakerGuide()
        check(!guide.canPrepare, "not connected")
        guide.connect(generation: 1, enabled: true)
        check(!guide.prepare(generation: 1), "not ready")
        guide.ready(generation: 99)
        check(!guide.canPrepare, "old ready")
        guide.ready(generation: 1)
        guide.playback(busy: true, generation: 1)
        check(!guide.prepare(generation: 1), "playback blocks prepare")
        guide.playback(busy: false, generation: 99)
        check(!guide.canPrepare, "old playback callback")
        guide.playback(busy: false, generation: 1)
        check(guide.canPrepare && guide.step == 1, "ready first step")
        // Prior input and final-only events cannot become the next guided utterance.
        guide.receive(try observation(2, "pending"), generation: 1)
        check(guide.prepare(generation: 1), "arm first step")
        guide.receive(try observation(2, "pending"), generation: 1)
        guide.receive(try observation(2, "temporary", label: "guest_1"), generation: 1)
        guide.playback(busy: false, generation: 1) // simulated reply completion
        guide.receive(try observation(3, "temporary", label: "guest_1"), generation: 1)
        guide.playback(busy: false, generation: 1) // simulated reply completion
        check(guide.step == 1 && !guide.canPrepare, "old and final-only do not bind")
        guide.receive(try observation(4, "pending"), generation: 1)
        check(guide.step == 1, "pending never passes")
        guide.receive(try observation(5, "pending"), generation: 1)
        guide.receive(try observation(5, "temporary", label: "guest_2"), generation: 1)
        guide.playback(busy: false, generation: 1) // simulated reply completion
        check(guide.step == 1 && guide.canPrepare && guide.message.contains("本步未通过"), "newer utterance retires bound input without rebinding")
        guide.receive(try observation(4, "temporary", label: "guest_1"), generation: 99)
        guide.playback(busy: false, generation: 1) // simulated reply completion
        check(guide.step == 1, "old generation final ignored")
        guide.receive(try observation(4, "temporary", label: "guest_1"), generation: 1)
        guide.playback(busy: false, generation: 1) // simulated reply completion
        check(guide.step == 1 && guide.canPrepare, "late bound final cannot advance")
        check(guide.prepare(generation: 1), "rearm after superseded input")
        guide.receive(try observation(6, "pending"), generation: 1)
        guide.receive(try observation(6, "temporary", label: "guest_1"), generation: 1)
        guide.playback(busy: false, generation: 1) // simulated reply completion
        check(guide.step == 2 && guide.canPrepare, "fresh final establishes baseline")
        guide.receive(try observation(4, "temporary", label: "guest_1"), generation: 1)
        guide.playback(busy: false, generation: 1) // simulated reply completion
        check(guide.step == 2, "duplicate final cannot advance")
        check(guide.prepare(generation: 1), "arm second")
        guide.receive(try observation(4, "pending"), generation: 1)
        guide.receive(try observation(4, "temporary", label: "guest_1"), generation: 1)
        guide.playback(busy: false, generation: 1) // simulated reply completion
        check(guide.step == 2 && !guide.canPrepare, "repeat input cannot bind")
        guide.receive(try observation(7, "pending"), generation: 1)
        guide.receive(try observation(7, "temporary", label: "guest_2"), generation: 1)
        guide.playback(busy: false, generation: 1) // simulated reply completion
        check(guide.step == 2 && guide.canPrepare && guide.message.contains("本步未通过"), "different label fails without person claim")
        let advice = ["too_short": "3～5秒", "too_long": "一句", "low_energy": "靠近", "incomplete_span": "完整", "match_uncertain": "正常语速", "not_queued": "重试", "": "重试"]
        var id = 8
        for (reason, expected) in advice.sorted(by: { $0.key < $1.key }) {
            check(guide.prepare(generation: 1), "arm retry")
            guide.receive(try observation(6, "temporary", label: "guest_1"), generation: 1)
            guide.playback(busy: false, generation: 1) // simulated reply completion
            check(guide.step == 2, "failed input replay cannot pass")
            guide.receive(try observation(id, "pending"), generation: 1)
            guide.receive(try observation(id, "uncertain", reason: reason, audioMS: 1234), generation: 1)
            guide.playback(busy: false, generation: 1) // simulated reply completion
            check(guide.step == 2 && guide.canPrepare && guide.message.contains(expected), "specific advice: \(reason)")
            check(guide.message.contains("1.23秒"), "validated duration displayed")
            id += 1
        }
        for state in ["unknown"] {
            check(guide.prepare(generation: 1), "arm noncandidate")
            guide.receive(try observation(id, "pending"), generation: 1)
            guide.receive(try observation(id, state), generation: 1)
            guide.playback(busy: false, generation: 1) // simulated reply completion
            check(guide.step == 2 && guide.message.contains("本步未通过"), "noncandidate cannot pass")
            id += 1
        }
        for nextStep in [3, 4] {
            check(guide.prepare(generation: 1), "arm successful retry")
            guide.receive(try observation(id, "pending"), generation: 1)
            guide.receive(try observation(id, "temporary", label: "guest_1"), generation: 1)
            guide.playback(busy: false, generation: 1) // simulated reply completion
            check(guide.step == nextStep, "same candidate advances")
            id += 1
        }
        check(!guide.canPrepare && guide.message.contains("本次单人三句标签一致性通过"), "limited final conclusion")
        check(!guide.message.contains("声纹通过"), "not general speaker acceptance")
        guide.disconnect(generation: 99)
        check(guide.message.contains("一致性通过"), "old disconnect ignored")
        guide.disconnect(generation: 1)
        check(!guide.canPrepare && guide.message.contains("一致性通过"), "disconnect preserves concise conclusion")
        guide.connect(generation: 2, enabled: true); guide.ready(generation: 2)
        check(guide.step == 1 && guide.message.contains("未完成"), "new connection resets result")
        check(guide.prepare(generation: 2), "arm new connection")
        guide.receive(try observation(1, "pending"), generation: 2)
        guide.receive(try observation(1, "temporary", label: "guest_8"), generation: 2)
        guide.playback(busy: false, generation: 1) // simulated reply completion
        check(guide.step == 2, "new connection clears prior label and IDs")
        guide.disconnect(generation: 2)
        check(guide.message.contains("未完成") && !guide.canPrepare, "unfinished disconnect")
        guide.connect(generation: 3, enabled: false); guide.ready(generation: 3)
        check(!guide.prepare(generation: 3), "off control cannot prepare")
        guide.receive(try observation(1, "pending"), generation: 3)
        check(guide.step == 1, "off ignores results")
        // Exercise the exact typed relay used by the UI; ready/result/playback
        // callbacks and prepare remain scoped even across delayed delivery.
        var relayed = SpeakerGuide()
        relayed.connect(generation: 10, enabled: true)
        relayed.handle(.ready, generation: 9)
        check(!relayed.canPrepare, "relay rejects stale ready")
        relayed.handle(.ready, generation: 10)
        relayed.handle(.playback(true), generation: 10)
        relayed.handle(.prepare, generation: 10)
        check(!relayed.canPrepare && relayed.instruction.contains("等待"), "relay playback blocks prepare")
        relayed.handle(.playback(false), generation: 10)
        relayed.handle(.prepare, generation: 10)
        relayed.handle(.observation(try observation(1, "pending")), generation: 10)
        relayed.handle(.observation(try observation(1, "temporary", label: "guest_1")), generation: 9)
        check(relayed.step == 1, "relay rejects stale result")
        relayed.handle(.responseWaiting(true), generation: 10)
        relayed.handle(.observation(try observation(1, "uncertain", reason: "too_short")), generation: 10)
        check(!relayed.canPrepare, "final before begin must wait for response")
        relayed.handle(.playback(true), generation: 10)
        check(!relayed.canPrepare, "begin remains blocked")
        relayed.handle(.playback(false), generation: 10)
        check(relayed.step == 1 && relayed.canPrepare, "relay final failure stays on step")
        relayed.handle(.responseWaiting(true), generation: 10)
        check(!relayed.canPrepare, "thinking blocks before begin")
        relayed.handle(.responseWaiting(false), generation: 10)
        check(relayed.canPrepare, "turn_failed releases response wait")
        relayed.handle(.disconnected, generation: 10)
        check(relayed.message.contains("本步未通过") && !relayed.canPrepare, "failed conclusion survives disconnect")
        let closedMessage = relayed.message
        relayed.handle(.ready, generation: 10)
        relayed.handle(.playback(false), generation: 10)
        relayed.handle(.prepare, generation: 10)
        relayed.handle(.observation(try observation(2, "temporary", label: "guest_1")), generation: 10)
        check(relayed.message == closedMessage && !relayed.canPrepare, "closed connection rejects all late callbacks")
        // A permanently failed observer may omit pending on its next input.
        for priorSuccess in [false, true] {
            var unavailable = SpeakerGuide()
            unavailable.connect(generation: 20, enabled: true)
            unavailable.handle(.ready, generation: 20)
            if priorSuccess {
                unavailable.handle(.prepare, generation: 20)
                unavailable.handle(.observation(try observation(1, "pending")), generation: 20)
                unavailable.handle(.observation(try observation(1, "temporary", label: "guest_1")), generation: 20)
                unavailable.handle(.playback(false), generation: 20)
            }
            let currentStep = unavailable.step
            unavailable.handle(.prepare, generation: 20)
            unavailable.handle(.observation(try observation(2, "unavailable", reason: "observer_unavailable")), generation: 19)
            check(!unavailable.message.contains("重新开始三步测试"), "stale unavailable ignored")
            unavailable.handle(.observation(try observation(2, "unavailable", reason: "observer_unavailable")), generation: 20)
            check(unavailable.step == currentStep && unavailable.message.contains("本步未通过") && unavailable.message.contains("结束后重新开始三步测试"), "direct unavailable terminates round with actionable restart")
            check(unavailable.instruction.contains("重新开始三步测试"), "terminal instruction replaces record-next prompt")
            unavailable.handle(.playback(false), generation: 20)
            unavailable.handle(.responseWaiting(false), generation: 20)
            check(!unavailable.canPrepare && !unavailable.prepare(generation: 20), "unavailable cannot be released by reply completion")
            unavailable.handle(.observation(try observation(3, "pending")), generation: 20)
            unavailable.handle(.observation(try observation(3, "temporary", label: "guest_1")), generation: 20)
            check(unavailable.step == currentStep, "terminal round ignores further results")
            unavailable.connect(generation: 21, enabled: true)
            unavailable.handle(.ready, generation: 21)
            check(unavailable.canPrepare && unavailable.step == 1, "new connection resets unavailable terminal state")
            unavailable.handle(.prepare, generation: 21)
            unavailable.handle(.observation(try observation(1, "temporary", label: "guest_1")), generation: 21)
            check(unavailable.step == 1, "recovery still rejects final-only candidate")
        }
        var noReply = SpeakerGuide()
        noReply.connect(generation: 30, enabled: true)
        noReply.handle(.ready, generation: 30)
        noReply.handle(.prepare, generation: 30)
        noReply.handle(.observation(try observation(1, "pending")), generation: 30)
        noReply.handle(.observation(try observation(1, "uncertain", reason: "too_long", audioMS: 26000)), generation: 30)
        noReply.handle(.inputTooLong, generation: 29)
        check(!noReply.message.contains("重新开始三步测试"), "stale no-reply rejection ignored")
        noReply.handle(.inputTooLong, generation: 30)
        check(noReply.step == 1 && noReply.message.contains("本步未通过") && noReply.message.contains("结束后重新开始三步测试"), "no-reply rejection terminates with restart guidance")
        check(noReply.instruction.contains("重新开始三步测试"), "no-reply terminal instruction")
        noReply.handle(.playback(false), generation: 30)
        noReply.handle(.responseWaiting(false), generation: 30)
        check(!noReply.canPrepare && !noReply.prepare(generation: 30), "no-reply round cannot retry in same connection")
        noReply.handle(.observation(try observation(1, "pending")), generation: 30)
        noReply.handle(.observation(try observation(1, "temporary", label: "guest_1")), generation: 30)
        check(noReply.step == 1, "producer reused ID cannot pass terminal round")
        noReply.disconnect(generation: 30)
        check(noReply.message.contains("本步未通过"), "no-reply short conclusion survives close")
        noReply.connect(generation: 31, enabled: true)
        noReply.handle(.ready, generation: 31)
        check(noReply.canPrepare && noReply.step == 1, "new connection resets no-reply terminal state")
        noReply.handle(.prepare, generation: 31)
        noReply.handle(.observation(try observation(1, "pending")), generation: 31)
        noReply.handle(.observation(try observation(1, "temporary", label: "guest_8")), generation: 31)
        check(noReply.step == 2, "new pending after reconnect establishes fresh baseline")
        var malformed = ["type":"speaker_observation", "input_id":99] as [String: Any]
        malformed["reason"] = "untrusted"
        do { _ = try SpeakerObservation.validate(malformed); preconditionFailure("invalid structured event") } catch {}
        let value = try observation(1, "temporary", label: "guest_1", reason: "candidate")
        check(value.inputID == 1 && value.state == .temporary && value.label == "guest_1" && value.reason == "candidate" && value.audioMS == 2000, "structured fields retained")
        check(SpeakerGuide.prompts == ["小满，给我推荐一种在家可以做的简单运动。", "如果我每天只运动十分钟，应该先从什么动作开始？", "小满，请告诉我运动之前可以怎样热身，记得说得简单一点。"], "exact prompts")
        print("Speaker guide: PASS (\(checks) checks)")
    }
}
