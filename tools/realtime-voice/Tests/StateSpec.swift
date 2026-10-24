import Foundation

@main struct StateSpec {
 static func main() throws {
  precondition(realtimeEndpoint("ws://127.0.0.1:18030/xiaozhi/v1/") != nil)
  for value in ["wss://example.test/xiaozhi/v1/","ws://u:p@127.0.0.1:18030/xiaozhi/v1/","ws://127.0.0.1:18030/xiaozhi/v1/?token=x","ws://127.0.0.1:18030/other"] {precondition(realtimeEndpoint(value)==nil)}
  let state=RealtimePlaybackState()
  let old=try state.start()
  try state.packet()
  precondition(!state.seal(),"tts.stop is not playback completion")
  let normalFinished=try state.observe(samples:1440,generation:old);precondition(normalFinished)
  state.finish()
  let interrupted=try state.start();try state.packet();state.interrupt()
  let fresh=try state.start();try state.packet()
  let staleFinished=try state.observe(samples:1440,generation:interrupted)
  precondition(!staleFinished,"late completion finished new response")
  precondition(state.completed==0 && fresh != interrupted)
  precondition(!state.seal())
  let freshFinished=try state.observe(samples:1440,generation:fresh);precondition(freshFinished);state.finish()
  do {try state.packet();preconditionFailure("audio outside tts accepted")}catch{}
  _=try state.start()
  for _ in 0..<16 {try state.packet()}
  do {try state.packet();preconditionFailure("unbounded output queue")}catch{}
  do {_=try state.observe(samples:30000,generation:state.generation);preconditionFailure("false playback evidence")}catch{}
  print("PASS realtime state: endpoint, normal completion, interrupted generations, bounded queue and receipts")
 }
}
