import AppKit
import AVFoundation

final class LiveApp: NSObject, NSApplicationDelegate, NSWindowDelegate {
    let url: URL
    let backend: String, remotePort: Int?
    var window: NSWindow!, client: LiveClient?, generation = 0
    let button = NSButton(title: "开始对话", target: nil, action: nil)
    let label = NSTextField(wrappingLabelWithString: "点一次开始，自由聊天。播放中直接插问，听听旧尾音和新回复的间隔。")
    init(url: URL, backend: String, remotePort: Int?) {
        self.url = url; self.backend = backend; self.remotePort = remotePort
    }
    var backendName: String { backend == "go" ? "Go 测试版" : backend == "python" ? "Python 对照版" : "未核对入口" }
    var endpointName: String { remotePort.map { "云端 \($0)" } ?? "本机端口 \(url.port ?? 0)" }
    var accent: NSColor { backend == "go" ? .systemBlue : backend == "python" ? .systemOrange : .secondaryLabelColor }
    func applicationDidFinishLaunching(_ notification: Notification) {
        window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 580, height: 310),
            styleMask: [.titled, .closable, .miniaturizable], backing: .buffered, defer: false)
        window.title = "小满 · \(backendName)（\(endpointName)）"; window.delegate = self
        window.center()
        if let screen = NSScreen.main, backend == "go" || backend == "python" {
            let frame = screen.visibleFrame
            let width = min(580, max(440, (frame.width - 48) / 2))
            window.setContentSize(NSSize(width: width, height: 310))
            let x = backend == "go" ? frame.midX - window.frame.width - 12 : frame.midX + 12
            window.setFrameOrigin(NSPoint(x: max(frame.minX, x), y: frame.midY - window.frame.height / 2))
        }
        let badge = NSTextField(labelWithString: "\(backendName)  ·  \(endpointName)")
        badge.font = .systemFont(ofSize: 22, weight: .bold); badge.textColor = accent
        badge.alignment = .center
        button.target = self; button.action = #selector(toggle); button.bezelStyle = .rounded
        button.font = .systemFont(ofSize: 21, weight: .medium)
        label.alignment = .center; label.font = .systemFont(ofSize: 15)
        let note = NSTextField(labelWithString: "先结束另一个窗口的对话，再开始这个窗口。关窗退出。")
        note.textColor = .secondaryLabelColor
        let content = window.contentView!
        [badge, button, label, note].forEach { $0.translatesAutoresizingMaskIntoConstraints = false; content.addSubview($0) }
        NSLayoutConstraint.activate([
            badge.topAnchor.constraint(equalTo: content.topAnchor, constant: 26),
            badge.leadingAnchor.constraint(equalTo: content.leadingAnchor, constant: 16),
            badge.trailingAnchor.constraint(equalTo: content.trailingAnchor, constant: -16),
            button.centerXAnchor.constraint(equalTo: content.centerXAnchor), button.topAnchor.constraint(equalTo: badge.bottomAnchor, constant: 24),
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

var args = Array(CommandLine.arguments.dropFirst()), port = 18040
var plan: String?
var backend = "unknown", remotePort: Int?
if let i = args.firstIndex(of: "--port"), i + 1 < args.count { port = Int(args[i + 1]) ?? 0; args.removeSubrange(i...(i + 1)) }
if let i = args.firstIndex(of: "--fixtures"), i + 1 < args.count { plan = args[i + 1]; args.removeSubrange(i...(i + 1)) }
if let i = args.firstIndex(of: "--backend"), i + 1 < args.count { backend = args[i + 1]; args.removeSubrange(i...(i + 1)) }
if let i = args.firstIndex(of: "--remote-port"), i + 1 < args.count { remotePort = Int(args[i + 1]) ?? 0; args.removeSubrange(i...(i + 1)) }
guard args.isEmpty, ["go", "python", "unknown"].contains(backend), remotePort.map({(1024...65535).contains($0)}) ?? true, (1024...65535).contains(port), let url = URL(string: "ws://127.0.0.1:\(port)/live") else { exit(2) }
if let plan {
    let client = LiveClient(url: url, plan: plan); client.ended = { exit($0 ? 0 : 1) }
    client.start(); withExtendedLifetime(client) { dispatchMain() }
} else {
    let app = NSApplication.shared; app.setActivationPolicy(.regular)
    let delegate = LiveApp(url: url, backend: backend, remotePort: remotePort); app.delegate = delegate; withExtendedLifetime(delegate) { app.run() }
}
