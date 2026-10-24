import Foundation
import AVFoundation

final class RealtimeFixture {
 struct Step:Decodable {let wav:String;let expected_pattern:String?;let interrupt_after_samples:Int?}
 let steps:[Step],queue:DispatchQueue
 private(set) var index=0
 var sendPCM:(([Int16])->Void)?,done:((Bool)->Void)?
 private var timer:DispatchSourceTimer?,frames:[Int16]=[],offset=0,interruptions=0,completed=0
 init(path:String,queue:DispatchQueue)throws{
  let data=try Data(contentsOf:URL(fileURLWithPath:path));guard data.count<=16384 else{throw ClientFailure("fixture_plan_size")}
  steps=try JSONDecoder().decode([Step].self,from:data);self.queue=queue
  guard !steps.isEmpty,steps.count<=8 else{throw ClientFailure("fixture_plan_count")}
 }
 func start(){
  do{try load()}catch{finish(false);return}
  let timer=DispatchSource.makeTimerSource(queue:queue);self.timer=timer
  timer.schedule(deadline:.now()+1,repeating:.milliseconds(20))
  timer.setEventHandler{[weak self] in
   guard let self else{return}
   var frame=[Int16](repeating:0,count:320)
   let count=min(320,max(0,self.frames.count-self.offset))
   if count>0{frame.replaceSubrange(0..<count,with:self.frames[self.offset..<(self.offset+count)]);self.offset += count}
   self.sendPCM?(frame)
  }
  timer.resume()
  queue.asyncAfter(deadline:.now()+150){[weak self] in guard let self,self.timer != nil else{return};self.finish(false)}
 }
 private func load()throws{
  let file=try AVAudioFile(forReading:URL(fileURLWithPath:steps[index].wav))
  guard file.processingFormat.sampleRate==16000,file.processingFormat.channelCount==1,file.length>0,file.length<=16000*30,
   let buffer=AVAudioPCMBuffer(pcmFormat:file.processingFormat,frameCapacity:AVAudioFrameCount(file.length)) else{throw ClientFailure("fixture_wav_format")}
  try file.read(into:buffer)
  guard let source=buffer.floatChannelData?[0] else{throw ClientFailure("fixture_float")}
  frames=(0..<Int(buffer.frameLength)).map{Int16(max(-32768,min(32767,Int(source[$0]*32767))))};offset=0
  metadata("realtime.fixture_input",["index":index,"samples":frames.count])
 }
 func interrupted(){interruptions += 1}
 func progress(samples:Int,outputIndex:Int){
  guard outputIndex==index,let threshold=steps[index].interrupt_after_samples,samples>=threshold else{return}
  advance()
 }
 func outputCompleted(index:Int,generatedText:String){
  guard index==self.index,steps[index].interrupt_after_samples==nil else{return}
  if let pattern=steps[index].expected_pattern,generatedText.range(of:pattern,options:.regularExpression)==nil {
   metadata("realtime.fixture_assertion",["index":index,"matched_generated_text":false]);finish(false);return
  }
  completed += 1;metadata("realtime.fixture_assertion",["index":index,"matched_generated_text":true,"playback":"complete"])
  advance()
 }
 private func advance(){
  index += 1
  if index==steps.count{finish(true);return}
  do{try load()}catch{finish(false)}
 }
 private func finish(_ success:Bool){
  guard timer != nil || index==0 else{return}
  let interruptedExpected=steps.contains{$0.interrupt_after_samples != nil}
  let passed=success && (!interruptedExpected || interruptions>0)
  stop();metadata(passed ? "realtime.fixture_success":"realtime.fixture_failure",["normal_outputs":completed,"interruptions":interruptions,"microphone":false,"playback_basis":"CoreAudio_dataPlayedBack","text_assertions":"generated_transcript_only"])
  let callback=done;done=nil;callback?(passed)
 }
 func stop(){timer?.setEventHandler{};timer?.cancel();timer=nil;sendPCM=nil;frames.removeAll()}
}
