import Foundation

@main struct SpeakerObservationTests {
 static func main() throws {
  func check(_ value: Bool) { precondition(value) }
  var view = SpeakerObservationState()
  func event(_ id: Int, _ state: String, _ label: String = "", _ start: Int = 0, _ end: Int = 0) -> [String: Any] {
   ["type":"speaker_observation","input_id":id,"state":state,"label":label,"start_sample":start,"end_sample":end,"elapsed_ms":10,"evidence":"turn_taking_test"]
  }
  // Use only synthetic protocol values; never print an upstream reason or result.
  var reasonChecks = 0, reasonFailures = 0, failuresByName: [String: Int] = [:]
  func reasonCheck(_ value: Bool, _ name: String) {
   reasonChecks += 1
   if !value { reasonFailures += 1; failuresByName[name,default:0] += 1 }
  }
  func diagnosed(_ state: String, _ reason: Any?, _ audioMS: Any? = nil, id: Int = 1) -> [String: Any] {
   var value = state == "temporary" ? event(id,state,"guest_1",0,32000) : event(id,state)
   if let reason { value["reason"] = reason }
   if let audioMS { value["audio_ms"] = audioMS }
   return value
  }
  func reasonReject(_ value: [String: Any], _ name: String) {
   var fresh = SpeakerObservationState()
   do { _ = try fresh.apply(value); reasonCheck(false,name) }
   catch { reasonCheck(true,name) }
  }
  let descriptions = [
   "": "证据不足", "incomplete_span": "片段边界未确认", "too_short": "声音片段不足1.5秒",
   "too_long": "片段超过8秒", "not_queued": "本段未送入识别",
   "match_uncertain": "声音特征匹配证据不足", "low_energy": "收到的声音较弱"
  ]
  for (reason, description) in descriptions {
   var fresh = SpeakerObservationState()
   reasonCheck(try fresh.apply(diagnosed("uncertain",reason)) == "本次发言：不确定（\(description)）", "static uncertain explanation")
   reasonCheck(try fresh.apply(diagnosed("uncertain",reason,2340)) == "本次发言：不确定（\(description)，片段2.34秒）", "uncertain audio duration")
  }
  for (state, allowed) in ["pending":[""], "temporary":["","candidate"], "uncertain":Array(descriptions.keys), "unknown":["","capacity"], "unavailable":["","observer_unavailable"]] {
   let all = ["", "candidate", "capacity", "observer_unavailable", "incomplete_span", "too_short", "too_long", "not_queued", "match_uncertain", "low_energy", "untrusted_upstream_text"]
   for reason in all {
    let value = diagnosed(state,reason,2000)
    if allowed.contains(reason) {
     var fresh = SpeakerObservationState()
     reasonCheck(try fresh.apply(value) != nil, "accepted known state/reason pair")
    } else { reasonReject(value,"rejected incompatible or unknown reason") }
   }
   for invalid: Any in [NSNull(), 1, true, ["code":"too_short"], ["too_short"]] {
    reasonReject(diagnosed(state,invalid),"rejected non-string reason")
   }
   for audioMS: Any in [NSNull(), -1, 35001, true, false, 1.5, "2000", [2000], ["value":2000]] {
    reasonReject(diagnosed(state,"",audioMS),"rejected malformed audio duration")
   }
  }
  for audioMS in [0, 1, 35000] {
   var fresh = SpeakerObservationState()
   let suffix = audioMS == 0 ? "" : audioMS == 1 ? "，片段0.00秒" : "，片段35.00秒"
   reasonCheck(try fresh.apply(diagnosed("uncertain",nil,audioMS)) == "本次发言：不确定（证据不足\(suffix)）", "duration boundaries and missing reason")
  }
  var compatibility = SpeakerObservationState()
  reasonCheck(try compatibility.apply(diagnosed("uncertain",nil)) == "本次发言：不确定（证据不足）", "old event remains valid")
  reasonCheck(try compatibility.apply(diagnosed("temporary","candidate",2000)) == "本次发言：访客 A（候选）", "temporary text remains exact")
  // Neither invalid optional field may retire a valid lower ID.
  for invalid in [diagnosed("uncertain","bogus",2000,id:99), diagnosed("uncertain","too_short",true,id:99)] {
   var fresh = SpeakerObservationState()
   _ = try fresh.apply(diagnosed("pending","",id:2))
   do { _ = try fresh.apply(invalid); reasonCheck(false,"invalid optional field rejected before ID retirement") }
   catch { reasonCheck(true,"invalid optional field rejected before ID retirement") }
   reasonCheck(try fresh.apply(diagnosed("uncertain","match_uncertain",2100,id:2)) == "本次发言：不确定（声音特征匹配证据不足，片段2.10秒）", "invalid event does not retire valid input")
  }
  let diagnosedWire = try JSONSerialization.data(withJSONObject: diagnosed("uncertain","low_energy",1780))
  var jsonState = SpeakerObservationState()
  reasonCheck(try jsonState.apply(JSONSerialization.jsonObject(with: diagnosedWire) as! [String: Any]) == "本次发言：不确定（收到的声音较弱，片段1.78秒）", "JSON round trip")
  for name in failuresByName.keys.sorted() {
   FileHandle.standardError.write(Data("FAIL \(name): \(failuresByName[name]!)\n".utf8))
  }
  precondition(reasonFailures == 0, "reason/audio_ms checks failed: \(reasonFailures) of \(reasonChecks)")
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
  check(try bounds.apply(event(2,"uncertain")) == "本次发言：不确定（证据不足）")
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
  print("Speaker observation parser: PASS (\(reasonChecks) reason/audio_ms checks plus existing schema/integer cases)")
 }
}
