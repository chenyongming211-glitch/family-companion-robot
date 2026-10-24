import AppKit
import AVFoundation
import Foundation

final class RealtimeApp:NSObject,NSApplicationDelegate,NSWindowDelegate {
 let url:URL
 var window:NSWindow!,client:RealtimeClient?,generation=0
 let button=NSButton(title:"开始对话",target:nil,action:nil)
 let label=NSTextField(wrappingLabelWithString:"点一次开始，自由说话；不测时关闭窗口。")
 init(url:URL){self.url=url}
 func applicationDidFinishLaunching(_ notification:Notification){
  window=NSWindow(contentRect:NSRect(x:0,y:0,width:580,height:230),styleMask:[.titled,.closable,.miniaturizable],backing:.buffered,defer:false)
  window.title="家庭陪伴 · Realtime 独立测试";window.delegate=self;window.center()
  button.target=self;button.action=#selector(toggle);button.bezelStyle = .rounded;button.font = .systemFont(ofSize:21,weight:.medium)
  label.alignment = .center;label.font = .systemFont(ofSize:14)
  let note=NSTextField(labelWithString:"独立 Realtime 会话 · 与三段式窗口交替使用")
  note.textColor = .secondaryLabelColor
  guard let content=window.contentView else{return}
  [button,label,note].forEach{$0.translatesAutoresizingMaskIntoConstraints=false;content.addSubview($0)}
  NSLayoutConstraint.activate([
   button.centerXAnchor.constraint(equalTo:content.centerXAnchor),button.topAnchor.constraint(equalTo:content.topAnchor,constant:38),button.widthAnchor.constraint(equalToConstant:200),button.heightAnchor.constraint(equalToConstant:48),
   label.topAnchor.constraint(equalTo:button.bottomAnchor,constant:22),label.leadingAnchor.constraint(equalTo:content.leadingAnchor,constant:24),label.trailingAnchor.constraint(equalTo:content.trailingAnchor,constant:-24),
   note.bottomAnchor.constraint(equalTo:content.bottomAnchor,constant:-18),note.centerXAnchor.constraint(equalTo:content.centerXAnchor)])
  window.makeKeyAndOrderFront(nil);NSApp.activate(ignoringOtherApps:true)
 }
 @objc func toggle(){
  if let client{button.isEnabled=false;client.stop();return}
  generation += 1;let token=generation;button.isEnabled=false;label.stringValue="正在请求麦克风权限…"
  AVCaptureDevice.requestAccess(for:.audio){[weak self] granted in DispatchQueue.main.async{
   guard let self,self.generation==token else{return}
   guard granted else{self.label.stringValue="请在系统设置中允许本应用使用麦克风后重新打开。";self.button.isEnabled=true;return}
   let client=RealtimeClient(url:self.url);self.client=client
   client.status={[weak self] text in DispatchQueue.main.async{guard let self,self.generation==token else{return};self.label.stringValue=text}}
   client.ended={[weak self] success in DispatchQueue.main.async{
    guard let self,self.generation==token else{return};self.client=nil;self.button.title="开始对话";self.button.isEnabled=true
    if success{self.label.stringValue="已停止收音，点击开始建立新的 Realtime 会话。"}
   }}
   self.button.title="结束对话";self.button.isEnabled=true;client.start()
  }}
 }
 func applicationShouldTerminateAfterLastWindowClosed(_ sender:NSApplication)->Bool{true}
 func windowWillClose(_ notification:Notification){generation += 1;client?.stop()}
 func applicationWillTerminate(_ notification:Notification){generation += 1;client?.stop()}
}

var args=Array(CommandLine.arguments.dropFirst()),address="ws://127.0.0.1:18030/xiaozhi/v1/"
var plan:String?
if let i=args.firstIndex(of:"--url"),i+1<args.count{address=args[i+1];args.removeSubrange(i...(i+1))}
if let i=args.firstIndex(of:"--fixtures"),i+1<args.count{plan=args[i+1];args.removeSubrange(i...(i+1))}
guard args.isEmpty,let url=realtimeEndpoint(address) else{fputs("Usage: RealtimeVoice [--url ws://127.0.0.1:18030/xiaozhi/v1/] [--fixtures synthetic-plan.json]\n",stderr);exit(2)}
if let plan {
 let client=RealtimeClient(url:url,fixturePlan:plan);client.ended={exit($0 ? 0:1)};client.start();withExtendedLifetime(client){dispatchMain()}
}else{
 let app=NSApplication.shared;app.setActivationPolicy(.regular)
 let delegate=RealtimeApp(url:url);app.delegate=delegate;withExtendedLifetime(delegate){app.run()}
}
