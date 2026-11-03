import Foundation
import CoreFoundation

// Validated session-only protocol value; construction always validates the wire event.
struct SpeakerObservation {
    enum State: String { case pending, temporary, uncertain, unknown, unavailable }
    let inputID: Int
    let state: State
    let label: String
    let reason: String
    let audioMS: Int
    let display: String
    private init(inputID: Int, state: State, label: String, reason: String, audioMS: Int, display: String) {
        self.inputID = inputID; self.state = state; self.label = label
        self.reason = reason; self.audioMS = audioMS; self.display = display
    }
    enum InvalidEvent: Error { case malformed, incompatibleReady }
    private static let uncertainDescriptions = [
        "": "证据不足", "incomplete_span": "片段边界未确认",
        "too_short": "声音片段不足1.5秒", "too_long": "片段超过8秒",
        "not_queued": "本段未送入识别", "match_uncertain": "声音特征匹配证据不足",
        "low_energy": "收到的声音较弱"
    ]

    static func validateReady(_ event: [String: Any], enabled: Bool) throws {
        guard strictBoolean(event["speaker_test"]) == true,
              strictBoolean(event["speaker_enabled"]) == enabled else {
            throw InvalidEvent.incompatibleReady
        }
    }

    static func validate(_ event: [String: Any]) throws -> SpeakerObservation {
        guard event["type"] as? String == "speaker_observation",
              event["evidence"] as? String == "turn_taking_test",
              let id = Self.nonnegativeInteger(event["input_id"]),
              let start = Self.nonnegativeInteger(event["start_sample"]),
              let end = Self.nonnegativeInteger(event["end_sample"]),
              Self.nonnegativeInteger(event["elapsed_ms"]) != nil,
              let state = event["state"] as? String,
              let label = event["label"] as? String else { throw InvalidEvent.malformed }
        // Older servers omit these fields. Explicit null or wrong types are invalid.
        var reason = "", audioMS = 0
        if let value = event["reason"] {
            guard let code = value as? String else { throw InvalidEvent.malformed }
            reason = code
        }
        if let value = event["audio_ms"] {
            guard let duration = Self.nonnegativeInteger(value), duration <= 35000 else { throw InvalidEvent.malformed }
            audioMS = duration
        }
        let display: String
        switch state {
        case "pending":
            guard label.isEmpty, reason.isEmpty else { throw InvalidEvent.malformed }
            display = "正在识别本次发言…"
        case "temporary":
            guard reason.isEmpty || reason == "candidate",
                  let index = (1...8).first(where: { label == "guest_\($0)" }),
                  end > start, (24000...128000).contains(end - start) else { throw InvalidEvent.malformed }
            let letter = String(UnicodeScalar(64 + index)!)
            display = "本次发言：访客 \(letter)（候选）"
        case "uncertain":
            guard label.isEmpty, let explanation = Self.uncertainDescriptions[reason] else { throw InvalidEvent.malformed }
            let duration = audioMS > 0 ? "，片段" + String(format: "%.2f", locale: Locale(identifier: "en_US_POSIX"), Double(audioMS) / 1000) + "秒" : ""
            display = "本次发言：不确定（\(explanation)\(duration)）"
        case "unknown":
            guard label.isEmpty, reason.isEmpty || reason == "capacity" else { throw InvalidEvent.malformed }
            display = "本次发言：未知说话人"
        case "unavailable":
            guard label.isEmpty, reason.isEmpty || reason == "observer_unavailable" else { throw InvalidEvent.malformed }
            display = "声纹观测暂不可用，可以继续聊天"
        default: throw InvalidEvent.malformed
        }
        return SpeakerObservation(inputID: id, state: State(rawValue: state)!, label: label,
                                  reason: reason, audioMS: audioMS, display: display)
    }

    private static func nonnegativeInteger(_ value: Any?) -> Int? {
        guard let number = value as? NSNumber, CFGetTypeID(number) != CFBooleanGetTypeID(),
              let result = Int(number.stringValue), result >= 0 else { return nil }
        return result
    }
    private static func strictBoolean(_ value: Any?) -> Bool? {
        guard let number = value as? NSNumber, CFGetTypeID(number) == CFBooleanGetTypeID() else { return nil }
        return number.boolValue
    }
}

// Compatibility presentation API; validation occurs before retiring an input ID.
struct SpeakerObservationState {
    typealias InvalidEvent = SpeakerObservation.InvalidEvent
    private var latestInputID = -1
    static func validateReady(_ event: [String: Any], enabled: Bool) throws {
        try SpeakerObservation.validateReady(event, enabled: enabled)
    }
    mutating func apply(_ observation: SpeakerObservation) -> String? {
        guard observation.inputID >= latestInputID else { return nil }
        latestInputID = observation.inputID
        return observation.display
    }
    mutating func apply(_ event: [String: Any]) throws -> String? {
        apply(try SpeakerObservation.validate(event))
    }
}
