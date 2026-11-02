import Foundation

@main struct SpeakerObservationTests {
 static func main() throws {
  func check(_ value: Bool) { precondition(value) }
  var view = SpeakerObservationState()
  func event(_ id: Int, _ state: String, _ label: String = "", _ start: Int = 0, _ end: Int = 0) -> [String: Any] {
   ["type":"speaker_observation","input_id":id,"state":state,"label":label,"start_sample":start,"end_sample":end,"elapsed_ms":10,"evidence":"turn_taking_test"]
  }
  check(try view.apply(event(1,"pending")) == "正在识别本次发言…")
  check(try view.apply(event(1,"temporary","guest_1",16000,48000)) == "本次发言：访客 A（候选）")
  check(try view.apply(event(2,"pending")) != nil)
  check(try view.apply(event(1,"temporary","guest_2",16000,48000)) == nil)
  for invalid in [event(2,"temporary","张三",0,32000), event(2,"temporary","guest_9",0,32000),event(2,"temporary","guest_1",0,100),event(2,"bogus"),event(-1,"pending")] {
   do { _ = try view.apply(invalid); fatalError("accepted invalid candidate") } catch {}
  }
  var wrongEvidence=event(3,"pending"); wrongEvidence["evidence"]="verified_identity"
  do { _ = try view.apply(wrongEvidence);fatalError("accepted false evidence") } catch {}
  func reject(_ event: [String: Any]) {
   var fresh = SpeakerObservationState()
   do { _ = try fresh.apply(event); fatalError("accepted malformed observation") } catch {}
  }
  for (field, value) in [("input_id", true as Any), ("input_id", 1.5 as Any), ("start_sample", -1 as Any), ("end_sample", false as Any), ("elapsed_ms", -1 as Any), ("elapsed_ms", "10" as Any), ("type", "status" as Any)] {
   var malformed = event(4,"pending"); malformed[field] = value; reject(malformed)
  }
  for field in ["input_id", "state", "label", "start_sample", "end_sample", "elapsed_ms", "evidence"] {
   var missing = event(4,"pending"); missing.removeValue(forKey: field); reject(missing)
  }
  reject(event(4,"pending","guest_1"))
  reject(event(4,"temporary","guest_1",32000,16000))
  reject(event(4,"temporary","guest_1",0,128001))
  reject(event(4,"temporary","guest_1",0,23999))
  var bounds = SpeakerObservationState()
  check(try bounds.apply(event(0,"temporary","guest_8",0,24000)) == "本次发言：访客 H（候选）")
  check(try bounds.apply(event(1,"temporary","guest_1",16000,144000)) != nil)
  check(try bounds.apply(event(2,"uncertain")) == "本次发言：不确定（请轮流说话）")
  check(try bounds.apply(event(3,"unknown")) == "本次发言：未知说话人")
  check(try bounds.apply(event(4,"unavailable")) == "声纹观测暂不可用，可以继续聊天")
  // A rejected higher ID must not retire the current valid input.
  do { _ = try bounds.apply(event(99,"temporary","guest_9",0,32000)); fatalError("accepted invalid label") } catch {}
  check(try bounds.apply(event(4,"unknown")) != nil)
  let wire = try JSONSerialization.data(withJSONObject: event(5,"pending"))
  check(try bounds.apply(JSONSerialization.jsonObject(with: wire) as! [String: Any]) != nil)
  var largeID = SpeakerObservationState()
  check(try largeID.apply(event(9007199254740993,"pending")) != nil)
  check(try largeID.apply(event(9007199254740992,"pending")) == nil)
  for enabled in [false, true] {
   try SpeakerObservationState.validateReady(["speaker_test":true,"speaker_enabled":enabled], enabled: enabled)
   for wrong: [String: Any] in [["speaker_test":false,"speaker_enabled":enabled], ["speaker_test":true,"speaker_enabled":!enabled], ["speaker_test":1,"speaker_enabled":enabled], ["speaker_test":true,"speaker_enabled":enabled ? 1 : 0], [:]] {
    do { try SpeakerObservationState.validateReady(wrong, enabled: enabled); fatalError("accepted incompatible ready") } catch {}
   }
  }
  print("Speaker observation parser: PASS")
 }
}
