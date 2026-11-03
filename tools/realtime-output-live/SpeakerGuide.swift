import Foundation

// In-memory single-participant consistency guide. No identity claim or persistence.
struct SpeakerGuide {
    enum Event {
        case ready, playback(Bool), responseWaiting(Bool), prepare, observation(SpeakerObservation), disconnected
    }
    mutating func handle(_ event: Event, generation: Int) {
        switch event {
        case .ready: ready(generation: generation)
        case .playback(let busy): playback(busy: busy, generation: generation)
        case .responseWaiting(let waiting):
            guard self.generation == generation else { return }; awaitingResponse = waiting
        case .prepare: prepare(generation: generation)
        case .observation(let value): receive(value, generation: generation)
        case .disconnected: disconnect(generation: generation)
        }
    }
    static let prompts = [
        "小满，给我推荐一种在家可以做的简单运动。",
        "如果我每天只运动十分钟，应该先从什么动作开始？",
        "小满，请告诉我运动之前可以怎样热身，记得说得简单一点。"
    ]
    private var generation: Int?
    private var enabled = false, isReady = false, busy = false, armed = false
    private var awaitingResponse = false
    private var latestInputID = -1
    private var boundInputID: Int?
    private var baseline: String?
    private var conclusion = "未完成"
    private(set) var step = 1
    private(set) var message = "未完成 · 点击开始对话后准备第一步。"
    var isConnected: Bool { generation != nil }
    var prompt: String { step <= 3 ? Self.prompts[step - 1] : "" }
    var canPrepare: Bool {
        generation != nil && enabled && isReady && !busy && !awaitingResponse && !armed && boundInputID == nil && step <= 3
    }
    var instruction: String {
        if !enabled && generation != nil { return "声纹已关闭：本连接仅作聊天对照，不执行三步测试。" }
        if !isReady { return "等待连接就绪；三步须由同一位参与者在同一连接完成。" }
        if step > 3 { return "仅证明本次三句标签一致，不代表人物身份、多人与重叠能力通过。" }
        if busy || awaitingResponse { return "请等待小满播完回答和本句原因结果，再准备下一步。" }
        if boundInputID != nil { return "已记录本句，等待最终原因结果；请先不要继续说下一句。" }
        if armed { return "已准备：现在只说下方一句，等待回答和最终原因结果。" }
        return "点击“准备本步／记录下一句”，再按正常语速说下方一句（建议约3～5秒）。"
    }
    mutating func connect(generation: Int, enabled: Bool) {
        self = SpeakerGuide(); self.generation = generation; self.enabled = enabled
        message = enabled ? "未完成 · 等待连接就绪。" : "未完成 · 声纹观测已关闭。"
    }
    mutating func ready(generation: Int) {
        guard self.generation == generation else { return }; isReady = true
    }
    mutating func playback(busy: Bool, generation: Int) {
        guard self.generation == generation else { return }; self.busy = busy
        if !busy { awaitingResponse = false }
    }
    @discardableResult mutating func prepare(generation: Int) -> Bool {
        guard self.generation == generation, canPrepare else { return false }
        armed = true; message = "未完成 · 已准备第\(step)步，记录下一句新发言。"
        return true
    }
    mutating func receive(_ observation: SpeakerObservation, generation: Int) {
        guard self.generation == generation, isReady, enabled else { return }
        let isNew = observation.inputID > latestInputID
        latestInputID = max(latestInputID, observation.inputID)
        if let boundInputID, observation.inputID > boundInputID {
            self.boundInputID = nil; armed = false; conclusion = "本步未通过"
            message = "本步未通过 · 本句结果前收到更新的发言，旧结果已退役。请等回答和结果后准备本步，用一句新发言重试。"
            return
        }
        if observation.state == .pending {
            if armed && isNew && boundInputID == nil {
                boundInputID = observation.inputID; armed = false; awaitingResponse = true
                message = "未完成 · 第\(step)步正在识别，pending 不算结果。"
            }
            return
        }
        guard observation.inputID == boundInputID else { return }
        boundInputID = nil
        if observation.state == .temporary && (step == 1 || observation.label == baseline) {
            if step == 1 { baseline = observation.label }
            step += 1
            if step == 4 {
                conclusion = "本次单人三句标签一致性通过"
                message = conclusion + "。仅限本次一致性，不是人物识别准确率或整体验收。"
            } else {
                conclusion = "未完成"
                message = step == 2 ? "未完成 · 第1步已建立临时候选，不计人物识别准确率。" : "未完成 · 第2步与第一句标签一致。"
            }
        } else {
            conclusion = "本步未通过"
            message = "本步未通过 · " + advice(observation) + " 请准备本步后说一句新发言重试。"
        }
    }
    mutating func disconnect(generation: Int) {
        guard self.generation == generation else { return }
        let lastConclusion = conclusion
        self = SpeakerGuide()
        message = lastConclusion + " · 连接已关闭，临时候选与事件状态已清除。"
    }
    private func advice(_ value: SpeakerObservation) -> String {
        let duration = value.audioMS > 0 ? "（片段" + String(format: "%.2f", locale: Locale(identifier: "en_US_POSIX"), Double(value.audioMS) / 1000) + "秒）" : ""
        let text: String
        switch value.state {
        case .temporary: text = "本句标签与第一句不同；不能据此认定换了一个真人。"
        case .unknown: text = "未得到可比较的临时候选；请保持单人环境重试。"
        case .unavailable: text = "声纹观测暂不可用；可以结束后重新开始整轮测试，聊天仍可继续。"
        default:
            switch value.reason {
            case "too_short": text = "声音片段不足1.5秒，请用正常语速说约3～5秒。"
            case "too_long": text = "片段超过8秒，请一次只说一句，控制约3～5秒。"
            case "low_energy": text = "收到的声音较弱，请靠近麦克风并正常音量说话。"
            case "incomplete_span": text = "片段边界未确认，请等回答播完，再完整说一句。"
            case "match_uncertain": text = "声音特征匹配证据不足，请保持同一人、正常语速和安静环境。"
            case "not_queued": text = "本段未送入识别，请等待回答和结果后重试。"
            default: text = "证据不足，请保持单人、安静环境后重试。"
            }
        }
        return text + duration
    }
}
