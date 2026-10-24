import Foundation
import AVFoundation

// Mutable transport/audio state is confined to queue. UI callbacks are set
// before start; asynchronous URLSession results always re-enter that queue.
final class RealtimeClient: @unchecked Sendable {
 let queue=DispatchQueue(label:"family.realtime.test")
 let url:URL
 var status:((String)->Void)?,ended:((Bool)->Void)?
 private var session:URLSession?,socket:URLSessionWebSocketTask?,audio:AudioPipeline?,encoder:OpusEncoder?
 private let playback=RealtimePlaybackState()
 private let pending=RealtimePendingOutput()
 private var closed=false,ready=false,sending=false,sessionID=""
 private var outgoing:[URLSessionWebSocketTask.Message]=[],input:[Int16]=[]
 private var fixture:RealtimeFixture?
 private let fixturePlan:String?
 private var outputFixtureIndex = -1
 private var transcript=""
 init(url:URL,fixturePlan:String?=nil){self.url=url;self.fixturePlan=fixturePlan}
 func start(){queue.async{self.pair()}}
 func stop(){queue.async{self.shutdown(true)}}
 private func transport()->URLSession {
  if let session{return session}
  let cfg=URLSessionConfiguration.ephemeral;cfg.httpShouldSetCookies=false;cfg.httpCookieStorage=nil
  cfg.urlCache=nil;cfg.connectionProxyDictionary=[:];cfg.timeoutIntervalForRequest=10
  let created=URLSession(configuration:cfg);session=created;return created
 }
 private func pair(){
  guard !closed else{return}
  var parts=URLComponents(url:url,resolvingAgainstBaseURL:false)!;parts.scheme="http";parts.path="/v1/pair"
  var request=URLRequest(url:parts.url!);request.httpMethod="POST"
  request.setValue("application/json",forHTTPHeaderField:"Content-Type");request.httpBody=Data("{\"device_id\":\"native-realtime\"}".utf8)
  let session=transport();status?("正在连接 Realtime 测试入口…")
  Task{[weak self] in
   do{let(data,response)=try await session.data(for:request)
    guard (response as? HTTPURLResponse)?.statusCode==200 else{throw ClientFailure("pairing_busy_or_unavailable")}
    let pair=try RealtimePair(data)
    self?.queue.async{[weak self] in self?.connect(pair)}
   }catch{self?.queue.async{[weak self] in self?.fail("pairing_unavailable")}}
  }
 }
 private func connect(_ pair:RealtimePair){
  guard !closed else{return}
  var request=URLRequest(url:url);request.setValue("Bearer "+pair.ticket,forHTTPHeaderField:"Authorization")
  request.setValue("native-realtime",forHTTPHeaderField:"Device-Id");request.setValue("1",forHTTPHeaderField:"Protocol-Version")
  let socket=transport().webSocketTask(with:request);socket.maximumMessageSize=16384;self.socket=socket;socket.resume()
  send(["type":"hello","version":1,"transport":"websocket","audio_params":["format":"opus","sample_rate":16000,"channels":1,"frame_duration":20],"features":["mcp":false]])
  status?("Realtime · \(pair.model) · \(pair.voice) · 正在连接")
  metadata("realtime.identity",["backend":"qwen-native-existing","model":pair.model,"voice":pair.voice,"recording":false])
  receive()
  queue.asyncAfter(deadline:.now()+12){[weak self] in guard let self,!self.closed,!self.ready else{return};self.fail("hello_timeout")}
 }
 private func send(_ event:[String:Any]){
  guard let data=try? JSONSerialization.data(withJSONObject:event),let value=String(data:data,encoding:.utf8) else{fail("invalid_outgoing_control");return}
  enqueue(.string(value))
 }
 private func enqueue(_ message:URLSessionWebSocketTask.Message){
  guard !closed else{return}
  guard outgoing.count<32 else{fail("network_send_backpressure");return}
  outgoing.append(message);pump()
 }
 private func pump(){
  guard !closed,!sending,!outgoing.isEmpty,let socket else{return}
  sending=true;let message=outgoing.removeFirst()
  Task{[weak self] in
   do{try await socket.send(message);self?.queue.async{[weak self] in self?.sending=false;self?.pump()}}
   catch{self?.queue.async{[weak self] in self?.fail("websocket_send")}}
  }
 }
 private func receive(){
  guard !closed,let socket else{return}
  Task{[weak self] in
   do{let message=try await socket.receive();self?.queue.async{[weak self] in
    guard let self,!self.closed else{return}
    do{try self.message(message);self.receive()}catch{self.fail((error as? ClientFailure)?.description ?? "invalid_server_message")}
   }}catch{self?.queue.async{[weak self] in self?.fail("websocket_receive")}}
  }
 }
 private func message(_ message:URLSessionWebSocketTask.Message)throws{
  switch message {
  case .data(let packet):
   if pending.hasPending{try pending.packet(packet);return}
   guard ready,let audio,packet.count<=4000 else{throw ClientFailure("audio_before_hello")}
   try playback.packet();try audio.enqueue(packet)
   if playback.accepted==1440{metadata("realtime.first_playback_packet",["generation":playback.generation])}
  case .string(let text):
   guard let data=text.data(using:.utf8),data.count<=16384,let e=try JSONSerialization.jsonObject(with:data) as? [String:Any],let type=e["type"] as? String else{throw ClientFailure("invalid_control")}
   if type=="hello" {
    guard !ready,let id=e["session_id"] as? String,!id.isEmpty,
      (e["version"] as? Int)==1,e["transport"] as? String=="websocket",
      let params=e["audio_params"] as? [String:Any],params["format"] as? String=="opus",
      params["sample_rate"] as? Int==24000,params["channels"] as? Int==1,params["frame_duration"] as? Int==60 else{throw ClientFailure("unsupported_hello")}
    sessionID=id
    let audio=AudioPipeline(queue:queue);self.audio=audio;encoder=try OpusEncoder(rate:16000)
    audio.onFailure={[weak self] in self?.fail($0)}
    audio.onPCM={[weak self] in self?.inputPCM($0)}
    try audio.start(microphone:fixturePlan==nil)
    ready=true
    send(["type":"listen","state":"start","mode":"realtime","session_id":id])
    status?("Realtime 对话中 · 自由说话，插话后可继续提问")
    metadata("realtime.ready",["microphone":fixturePlan==nil,"playback_receipts":false,"transport":"opus"])
    if let fixturePlan {
     let fixture=try RealtimeFixture(path:fixturePlan,queue:queue);self.fixture=fixture
     fixture.sendPCM={[weak self] in self?.inputPCM($0)}
     fixture.done={[weak self] success in self?.shutdown(success)}
     fixture.start()
    }
   } else {
    guard ready,e["session_id"] as? String==sessionID else{throw ClientFailure("session_mismatch")}
    if type=="tts" {try tts(e)}
    else if type=="stt"{metadata("realtime.stt",["characters":(e["text"] as? String)?.count ?? 0])}
    else if type=="alert"{throw ClientFailure("gateway_alert")}
   }
  @unknown default:throw ClientFailure("unsupported_websocket_message")
  }
 }
 private func tts(_ e:[String:Any])throws{
  if pending.hasPending {
   if e["state"] as? String=="stop" && e["reason"] as? String=="interrupted" {
    // The interrupted reply is still waiting behind a normally sealed tail.
    // Revoke it before it reaches CoreAudio; the completed reply still drains.
    pending.clear();fixture?.interrupted();metadata("realtime.pending_interrupted");return
   }
   try pending.control(e);return
  }
  if e["state"] as? String=="start" && playback.active && playback.sealed {
   try pending.control(e);metadata("realtime.normal_tail_wait");return
  }
  switch e["state"] as? String {
     case "start":
      guard let audio else{throw ClientFailure("audio_missing")}
      let token=try playback.start();try audio.begin();transcript="";outputFixtureIndex=fixture?.index ?? -1
      audio.onCompleted={[weak self] samples in
       guard let self,!self.closed else{return}
       do {
        if try self.playback.observe(samples:samples,generation:token){self.finishOutput()}
        else if token==self.playback.generation{self.fixture?.progress(samples:samples,outputIndex:self.outputFixtureIndex)}
       }catch{self.fail("invalid_audio_completion")}
      }
      metadata("realtime.output_start",["generation":token])
     case "sentence_start":
      // This is the provider's generated text, never claimed as heard text.
      if let text=e["text"] as? String,text.count<=4096{transcript=text}
     case "stop":
      if e["reason"] as? String=="interrupted" {
       metadata("realtime.interrupted",["accepted_samples":playback.accepted,"completed_samples":playback.completed])
       fixture?.interrupted();playback.interrupt();audio?.end();transcript=""
      } else if playback.active && playback.seal(){finishOutput()}
     default:throw ClientFailure("invalid_tts_state")
  }
 }
 private func finishOutput(){
  guard playback.finished else{return}
  metadata("realtime.output_completed",["samples":playback.completed,"basis":"CoreAudio_dataPlayedBack"])
  playback.finish();audio?.end()
  fixture?.outputCompleted(index:outputFixtureIndex,generatedText:transcript)
  transcript=""
  guard !closed else{return}
  do {
   for event in pending.take() {
    switch event {
    case .control(let e):try tts(e)
    case .packet(let data):try message(.data(data))
    }
   }
  }catch{fail((error as? ClientFailure)?.description ?? "pending_output")}
 }
 private func inputPCM(_ pcm:[Int16]){
  guard ready,!closed,let encoder else{return}
  guard input.count+pcm.count<=8000 else{fail("microphone_backpressure");return}
  input.append(contentsOf:pcm)
  do{while input.count>=320{let frame=Array(input.prefix(320));input.removeFirst(320);enqueue(.data(try encoder.encode(frame)))}}
  catch{fail("opus_encode")}
 }
 private func fail(_ code:String){guard !closed else{return};metadata("realtime.failure",["code":code]);status?("连接已结束（\(code)），可重新开始");shutdown(false)}
 private func shutdown(_ success:Bool){
  guard !closed else{return};closed=true;ready=false
  fixture?.stop();fixture=nil;audio?.stop();audio=nil;encoder=nil;playback.interrupt()
  outgoing.removeAll();input.removeAll();socket?.cancel(with:.normalClosure,reason:nil);socket=nil
  pending.clear()
  session?.invalidateAndCancel();session=nil;metadata("realtime.closed",["success":success]);ended?(success)
 }
}
