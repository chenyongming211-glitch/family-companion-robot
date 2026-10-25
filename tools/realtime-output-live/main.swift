import AppKit
import AVFoundation

final class LiveApp: NSObject, NSApplicationDelegate, NSWindowDelegate {
    let url: URL
    var window: NSWindow!, client: LiveClient?, generation = 0
    let button = NSButton(title: "开始对话", target: nil, action: nil)
    let label = NSTextField(wrappingLabelWithString: "点一次开始，自由聊天。播放中直接插问，听听旧尾音和新回复的间隔。")
    init(url: URL) { self.url = url }
    func applicationDidFinishLaunching(_ notification: Notification) {
        window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 620, height: 260),
            styleMask: [.titled, .closable, .miniaturizable], backing: .buffered, defer: false)
        window.title = "家庭陪伴 · Realtime 排空测试"; window.delegate = self; window.center()
        button.target = self; button.action = #selector(toggle); button.bezelStyle = .rounded
        button.font = .systemFont(ofSize: 21, weight: .medium)
        label.alignment = .center; label.font = .systemFont(ofSize: 15)
        let note = NSTextField(labelWithString: "插话后播完已收到的音频。窗口常开，关闭窗口即可结束测试。")
        note.textColor = .secondaryLabelColor
        let content = window.contentView!
        [button, label, note].forEach { $0.translatesAutoresizingMaskIntoConstraints = false; content.addSubview($0) }
        NSLayoutConstraint.activate([
            button.centerXAnchor.constraint(equalTo: content.centerXAnchor), button.topAnchor.constraint(equalTo: content.topAnchor, constant: 40),
            button.widthAnchor.constraint(equalToConstant: 200), button.heightAnchor.constraint(equalToConstant: 48),
            label.topAnchor.constraint(equalTo: button.bottomAnchor, constant: 25), label.leadingAnchor.constraint(equalTo: content.leadingAnchor, constant: 24),
            label.trailingAnchor.constraint(equalTo: content.trailingAnchor, constant: -24),
            note.bottomAnchor.constraint(equalTo: content.bottomAnchor, constant: -22), note.centerXAnchor.constraint(equalTo: content.centerXAnchor)])
        window.makeKeyAndOrderFront(nil); NSApp.activate(ignoringOtherApps: true)
    }
    @objc func toggle() {
        if let client { button.isEnabled = false; client.stop(); return }
        generation += 1; let token = generation; button.isEnabled = false
        AVCaptureDevice.requestAccess(for: .audio) { [weak self] granted in DispatchQueue.main.async {
            guard let self, token == self.generation else { return }
            guard granted else { self.label.stringValue = "请在系统设置中允许麦克风后重新打开。"; self.button.isEnabled = true; return }
            let client = LiveClient(url: self.url); self.client = client
            client.status = { [weak self] value in DispatchQueue.main.async {
                guard let self, token == self.generation else { return }; self.label.stringValue = value
            } }
            client.ended = { [weak self] success in DispatchQueue.main.async {
                guard let self, token == self.generation else { return }
                self.client = nil; self.button.title = "开始对话"; self.button.isEnabled = true
                if success { self.label.stringValue = "已停止收音，可以再次开始。" }
            } }
            self.button.title = "结束对话"; self.button.isEnabled = true; client.start()
        } }
    }
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { true }
    func windowWillClose(_ notification: Notification) { generation += 1; client?.stop() }
}

var args = Array(CommandLine.arguments.dropFirst()), port = 18032
var plan: String?
if let i = args.firstIndex(of: "--port"), i + 1 < args.count { port = Int(args[i + 1]) ?? 0; args.removeSubrange(i...(i + 1)) }
if let i = args.firstIndex(of: "--fixtures"), i + 1 < args.count { plan = args[i + 1]; args.removeSubrange(i...(i + 1)) }
guard args.isEmpty, (1024...65535).contains(port), let url = URL(string: "ws://127.0.0.1:\(port)/live") else { exit(2) }
if let plan {
    let client = LiveClient(url: url, plan: plan); client.ended = { exit($0 ? 0 : 1) }
    client.start(); withExtendedLifetime(client) { dispatchMain() }
} else {
    let app = NSApplication.shared; app.setActivationPolicy(.regular)
    let delegate = LiveApp(url: url); app.delegate = delegate; withExtendedLifetime(delegate) { app.run() }
}
