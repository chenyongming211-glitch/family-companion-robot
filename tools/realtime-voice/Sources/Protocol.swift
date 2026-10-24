import Foundation

func realtimeEndpoint(_ address: String) -> URL? {
 guard let u=URL(string:address),u.scheme=="ws",u.host=="127.0.0.1",u.port != nil,
       u.user==nil,u.password==nil,u.query==nil,u.fragment==nil,
       URLComponents(url:u,resolvingAgainstBaseURL:false)?.percentEncodedPath=="/xiaozhi/v1/" else{return nil}
 return u
}

struct RealtimePair {
 let ticket:String, model:String, voice:String
 init(_ data:Data) throws {
  guard data.count<=4096,let e=try JSONSerialization.jsonObject(with:data) as? [String:Any],
    e["device_id"] as? String=="native-realtime",e["backend"] as? String=="qwen-native-existing",
    e["transport"] as? String=="xiaozhi-opus-v1",
    let ticket=e["ticket"] as? String,ticket.count==43,
    ticket.range(of:"^[A-Za-z0-9_-]{43}$",options:.regularExpression) != nil,
    let model=e["model"] as? String,!model.isEmpty,model.count<=128,
    let voice=e["voice"] as? String,!voice.isEmpty,voice.count<=32 else{throw ClientFailure("invalid_pair")}
  self.ticket=ticket;self.model=model;self.voice=voice
 }
}

// Counts are local CoreAudio evidence only. The old gateway requires I2S DMA
// receipts, so this test client deliberately does not negotiate that feature.
final class RealtimePlaybackState {
 private(set) var generation=0,accepted=0,completed=0
 private(set) var active=false,sealed=false
 func start() throws -> Int {
  guard !active else{throw ClientFailure("overlapping_tts")}
  generation += 1;accepted=0;completed=0;active=true;sealed=false;return generation
 }
 func packet() throws {
  guard active && !sealed && accepted-completed+1440<=24000 else{throw ClientFailure("invalid_or_excess_audio")}
  accepted += 1440
 }
 var finished:Bool{active && sealed && completed==accepted}
 func seal()->Bool{sealed=true;return finished}
 func observe(samples:Int,generation:Int)throws->Bool {
  guard generation==self.generation && active else{return false}
  guard samples>=completed && samples<=accepted else{throw ClientFailure("invalid_playback_count")}
  completed=samples;return finished
 }
 func finish(){active=false;sealed=false}
 func interrupt(){generation += 1;active=false;sealed=false;accepted=0;completed=0}
}
