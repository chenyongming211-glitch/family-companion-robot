import AppKit
import AVFoundation

final class LiveApp: NSObject, NSApplicationDelegate, NSWindowDelegate {
    let url: URL
    let backend: String, remotePort: Int?
    let deviceProfile: String
    let speakerTest: Bool, speakerOffURL: URL?
    var window: NSWindow!, client: LiveClient?, generation = 0
    let button = NSButton(title: "开始对话", target: nil, action: nil)
    let label = NSTextField(wrappingLabelWithString: "点一次开始，自由聊天。播放中直接插问，听听旧尾音和新回复的间隔。")
    let speakerLabel = NSTextField(wrappingLabelWithString: "本次发言：—")
    var speakerGuide = SpeakerGuide()
    let guideLabel = NSTextField(wrappingLabelWithString: "")
    let prepareButton = NSButton(title: "准备本步／记录下一句", target: nil, action: nil)
    let speakerSwitch = NSButton(checkboxWithTitle: "本会话开启声纹观测", target: nil, action: nil)
    init(url: URL, backend: String, remotePort: Int?, deviceProfile: String, speakerTest: Bool = false, speakerOffURL: URL? = nil) {
        self.url = url; self.backend = backend; self.remotePort = remotePort; self.deviceProfile = deviceProfile
        self.speakerTest = speakerTest; self.speakerOffURL = speakerOffURL
    }
    var backendName: String { backend == "go" ? "Go 测试版" : backend == "python" ? "Python 对照版" : "未核对入口" }
    var endpointName: String { remotePort.map { "云端 \($0)" } ?? "本机端口 \(url.port ?? 0)" }
    var accent: NSColor { backend == "go" ? .systemBlue : backend == "python" ? .systemOrange : .secondaryLabelColor }
    func applicationDidFinishLaunching(_ notification: Notification) {
        let height: CGFloat = speakerTest ? 720 : 310
        window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 580, height: height),
            styleMask: [.titled, .closable, .miniaturizable], backing: .buffered, defer: false)
        window.title = speakerTest ? "小满 · Realtime 声纹聊天测试" : "小满 · \(backendName)（\(endpointName)）"; window.delegate = self
        window.center()
        if let screen = NSScreen.main, !speakerTest, backend == "go" || backend == "python" {
            let frame = screen.visibleFrame
            let width = min(580, max(440, (frame.width - 48) / 2))
            window.setContentSize(NSSize(width: width, height: 310))
            let x = backend == "go" ? frame.midX - window.frame.width - 12 : frame.midX + 12
            window.setFrameOrigin(NSPoint(x: max(frame.minX, x), y: frame.midY - window.frame.height / 2))
        }
        let badge = NSTextField(labelWithString: speakerTest ? "单人三句 · 标签一致性测试" : "\(backendName)  ·  \(endpointName)")
        badge.font = .systemFont(ofSize: 22, weight: .bold); badge.textColor = accent
        badge.alignment = .center
        button.target = self; button.action = #selector(toggle); button.bezelStyle = .rounded
        button.font = .systemFont(ofSize: 21, weight: .medium)
        label.alignment = .center; label.font = .systemFont(ofSize: 15)
        let note = NSTextField(wrappingLabelWithString: speakerTest ? "同一位参与者、保持同一连接、一次一句、正常语速。等回答和原因结果后再进行下一步，三步之间不要点结束。访客标签是临时候选，不保存声纹，不代表真人姓名；多人及重叠尚未验证。" : "先结束另一个窗口的对话，再开始这个窗口。关窗退出。")
        note.textColor = .secondaryLabelColor
        note.alignment = .center
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
            note.bottomAnchor.constraint(equalTo: content.bottomAnchor, constant: -22),
            note.leadingAnchor.constraint(equalTo: content.leadingAnchor, constant: 24),
            note.trailingAnchor.constraint(equalTo: content.trailingAnchor, constant: -24)])
        if speakerTest {
            label.stringValue = "点击开始后才连接和收音，三步引导不会自动操作麦克风。"
            speakerSwitch.state = .on
            speakerLabel.alignment = .center; speakerLabel.font = .systemFont(ofSize: 18, weight: .medium)
            speakerSwitch.target = self; speakerSwitch.action = #selector(speakerModeChanged)
            prepareButton.target = self; prepareButton.action = #selector(prepareSpeakerStep)
            prepareButton.bezelStyle = .rounded
            guideLabel.alignment = .center; guideLabel.font = .systemFont(ofSize: 15)
            [speakerSwitch, speakerLabel, guideLabel, prepareButton].forEach { $0.translatesAutoresizingMaskIntoConstraints = false; content.addSubview($0) }
            NSLayoutConstraint.activate([
                speakerSwitch.topAnchor.constraint(equalTo: label.bottomAnchor, constant: 22),
                speakerSwitch.centerXAnchor.constraint(equalTo: content.centerXAnchor),
                speakerLabel.topAnchor.constraint(equalTo: speakerSwitch.bottomAnchor, constant: 20),
                speakerLabel.leadingAnchor.constraint(equalTo: content.leadingAnchor, constant: 24),
                speakerLabel.trailingAnchor.constraint(equalTo: content.trailingAnchor, constant: -24),
                prepareButton.topAnchor.constraint(equalTo: speakerLabel.bottomAnchor, constant: 16),
                prepareButton.centerXAnchor.constraint(equalTo: content.centerXAnchor),
                prepareButton.heightAnchor.constraint(equalToConstant: 36),
                guideLabel.topAnchor.constraint(equalTo: prepareButton.bottomAnchor, constant: 16),
                guideLabel.leadingAnchor.constraint(equalTo: content.leadingAnchor, constant: 24),
                guideLabel.trailingAnchor.constraint(equalTo: content.trailingAnchor, constant: -24),
                guideLabel.bottomAnchor.constraint(lessThanOrEqualTo: note.topAnchor, constant: -20)
            ])
            renderSpeakerGuide()
        }
        window.makeKeyAndOrderFront(nil); NSApp.activate(ignoringOtherApps: true)
    }
    @objc func speakerModeChanged() { renderSpeakerGuide() }
    @objc func prepareSpeakerStep() { client?.prepareSpeakerStep() }
    func renderSpeakerGuide() {
        guard speakerTest else { return }
        let enabled = speakerSwitch.state == .on
        prepareButton.isHidden = !enabled; guideLabel.isHidden = !enabled
        prepareButton.isEnabled = enabled && speakerGuide.canPrepare
        guard speakerGuide.isConnected else { guideLabel.stringValue = speakerGuide.message; return }
        let step = speakerGuide.step <= 3 ? "第\(speakerGuide.step)／3步\n\(speakerGuide.prompt)\n\n" : ""
        guideLabel.stringValue = step + speakerGuide.instruction + "\n\n" + speakerGuide.message
    }
    @objc func toggle() {
        speakerLabel.stringValue = "本次发言：—"
        if let client { button.isEnabled = false; prepareButton.isEnabled = false; client.stop(); return }
        generation += 1; let token = generation; button.isEnabled = false
        speakerSwitch.isEnabled = false
        let enabled = speakerSwitch.state == .on
        if speakerTest { speakerGuide.connect(generation: token, enabled: enabled); renderSpeakerGuide() }
        let sessionURL = speakerTest && !enabled ? speakerOffURL! : url
        let profile = speakerTest ? deviceProfile + (enabled ? "-on" : "-off") : deviceProfile
        AVCaptureDevice.requestAccess(for: .audio) { [weak self] granted in DispatchQueue.main.async {
            guard let self, token == self.generation else { return }
            guard granted else { self.label.stringValue = "请在系统设置中允许麦克风后重新打开。"; self.button.isEnabled = true; self.speakerSwitch.isEnabled = true; self.speakerGuide.disconnect(generation: token); self.renderSpeakerGuide(); return }
            let client = LiveClient(url: sessionURL, ownsIdentity: self.backend != "python", deviceProfile: profile, speakerTest: self.speakerTest, speakerEnabled: enabled, generation: token); self.client = client
            client.status = { [weak self] value in DispatchQueue.main.async {
                guard let self, token == self.generation else { return }; self.label.stringValue = value
            } }
            client.speakerStatus = { [weak self] value in DispatchQueue.main.async {
                guard let self, token == self.generation else { return }
                self.speakerLabel.stringValue = value.isEmpty ? "本次发言：—" : value
            } }
            client.speakerGuideEvent = { [weak self] event, eventGeneration in DispatchQueue.main.async {
                guard let self, eventGeneration == self.generation else { return }
                self.speakerGuide.handle(event, generation: eventGeneration); self.renderSpeakerGuide()
            } }
            client.ended = { [weak self] success in DispatchQueue.main.async {
                guard let self, token == self.generation else { return }
                self.client = nil; self.button.title = "开始对话"; self.button.isEnabled = true
                self.speakerSwitch.isEnabled = true; self.speakerLabel.stringValue = "本次发言：—"
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
var deviceProfile = "default"
var speakerTest = false, speakerOffPort: Int?
if let i = args.firstIndex(of: "--speaker-test") { speakerTest = true; args.remove(at: i) }
if let i = args.firstIndex(of: "--speaker-off-port"), i + 1 < args.count { speakerOffPort = Int(args[i + 1]) ?? 0; args.removeSubrange(i...(i + 1)) }
if let i = args.firstIndex(of: "--device-profile"), i + 1 < args.count { deviceProfile = args[i + 1]; args.removeSubrange(i...(i + 1)) }
if let i = args.firstIndex(of: "--port"), i + 1 < args.count { port = Int(args[i + 1]) ?? 0; args.removeSubrange(i...(i + 1)) }
if let i = args.firstIndex(of: "--fixtures"), i + 1 < args.count { plan = args[i + 1]; args.removeSubrange(i...(i + 1)) }
if let i = args.firstIndex(of: "--backend"), i + 1 < args.count { backend = args[i + 1]; args.removeSubrange(i...(i + 1)) }
if let i = args.firstIndex(of: "--remote-port"), i + 1 < args.count { remotePort = Int(args[i + 1]) ?? 0; args.removeSubrange(i...(i + 1)) }
guard args.isEmpty, DeviceIdentity.validProfile(deviceProfile), ["go", "python", "unknown"].contains(backend), remotePort.map({(1024...65535).contains($0)}) ?? true, (1024...65535).contains(port), let url = URL(string: "ws://127.0.0.1:\(port)/live") else { exit(2) }
var speakerOffURL: URL?
if speakerTest {
    guard backend == "go", plan == nil, port == 18052, speakerOffPort == 18053,
          remotePort == nil || remotePort == 18052,
          DeviceIdentity.validProfile(deviceProfile + "-on"), DeviceIdentity.validProfile(deviceProfile + "-off") else { exit(2) }
    speakerOffURL = URL(string: "ws://127.0.0.1:18053/live")
    unsetenv("NATIVE_AUDIO_TRACE"); unsetenv("NATIVE_AUDIO_CORRELATION")
} else if speakerOffPort != nil { exit(2) }
if let plan {
    let client = LiveClient(url: url, plan: plan, ownsIdentity: backend != "python", deviceProfile: deviceProfile); client.ended = { exit($0 ? 0 : 1) }
    client.start(); withExtendedLifetime(client) { dispatchMain() }
} else {
    let app = NSApplication.shared; app.setActivationPolicy(.regular)
    let delegate = LiveApp(url: url, backend: backend, remotePort: remotePort, deviceProfile: deviceProfile, speakerTest: speakerTest, speakerOffURL: speakerOffURL); app.delegate = delegate; withExtendedLifetime(delegate) { app.run() }
}
