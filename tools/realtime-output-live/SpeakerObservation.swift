import Foundation
import CoreFoundation

// Session-only presentation state. No audio, vectors, names or result logging.
struct SpeakerObservationState {
    private var latestInputID = -1
    enum InvalidEvent: Error { case malformed, incompatibleReady }

    static func validateReady(_ event: [String: Any], enabled: Bool) throws {
        guard strictBoolean(event["speaker_test"]) == true,
              strictBoolean(event["speaker_enabled"]) == enabled else {
            throw InvalidEvent.incompatibleReady
        }
    }

    mutating func apply(_ event: [String: Any]) throws -> String? {
        guard event["type"] as? String == "speaker_observation",
              event["evidence"] as? String == "turn_taking_test",
              let id = Self.nonnegativeInteger(event["input_id"]),
              let start = Self.nonnegativeInteger(event["start_sample"]),
              let end = Self.nonnegativeInteger(event["end_sample"]),
              Self.nonnegativeInteger(event["elapsed_ms"]) != nil,
              let state = event["state"] as? String,
              let label = event["label"] as? String else { throw InvalidEvent.malformed }
        let display: String
        switch state {
        case "pending":
            guard label.isEmpty else { throw InvalidEvent.malformed }
            display = "正在识别本次发言…"
        case "temporary":
            guard let index = (1...8).first(where: { label == "guest_\($0)" }),
                  end > start, (24000...128000).contains(end - start) else { throw InvalidEvent.malformed }
            let letter = String(UnicodeScalar(64 + index)!)
            display = "本次发言：访客 \(letter)（候选）"
        case "uncertain":
            guard label.isEmpty else { throw InvalidEvent.malformed }
            display = "本次发言：不确定（请轮流说话）"
        case "unknown":
            guard label.isEmpty else { throw InvalidEvent.malformed }
            display = "本次发言：未知说话人"
        case "unavailable":
            guard label.isEmpty else { throw InvalidEvent.malformed }
            display = "声纹观测暂不可用，可以继续聊天"
        default: throw InvalidEvent.malformed
        }
        guard id >= latestInputID else { return nil }
        latestInputID = id
        return display
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
