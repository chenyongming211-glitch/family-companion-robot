import AppKit
import AVFoundation
import Darwin

final class SpeakerWindow: NSObject, NSApplicationDelegate, NSWindowDelegate {
    private var window: NSWindow!
    private let captureButton = NSButton(title: "采集 4 秒", target: nil, action: nil)
    private let endButton = NSButton(title: "结束会话", target: nil, action: nil)
    private let status = NSTextField(wrappingLabelWithString: "尚未连接。点击后连接云端，再申请麦克风权限。")
    private let label = NSTextField(labelWithString: "当前：—")
    private let capture = ClipCapture()
    private var transport: SessionTransport?
    private var generation = 0
    private var busy = false
    private var clips = 0

    func applicationDidFinishLaunching(_ notification: Notification) {
        window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 540, height: 310), styleMask: [.titled, .closable, .miniaturizable], backing: .buffered, defer: false)
        window.title = "小满 · 会话内说话人测试"; window.delegate = self; window.center()
        let instructions = NSTextField(wrappingLabelWithString: "仅供自愿参与者手动测试。每次只让一人说话，采集 4 秒并经 SSH 发送云端。应用不保存录音、不登记姓名或声纹；标签仅在本次会话有效。\n\n短句、静音或不确定时可拒识。当前阈值来自合成实验，不能用于身份认证；单人声明并非算法验证。")
        instructions.font = .systemFont(ofSize: 13)
        label.font = .systemFont(ofSize: 22, weight: .medium)
        captureButton.target = self; captureButton.action = #selector(manualCapture)
        endButton.target = self; endButton.action = #selector(endSession); endButton.isEnabled = false
        let buttons = NSStackView(views: [captureButton, endButton]); buttons.spacing = 16
        let stack = NSStackView(views: [instructions, label, status, buttons]); stack.orientation = .vertical; stack.alignment = .leading; stack.spacing = 18; stack.translatesAutoresizingMaskIntoConstraints = false
        window.contentView!.addSubview(stack)
        NSLayoutConstraint.activate([stack.leadingAnchor.constraint(equalTo: window.contentView!.leadingAnchor, constant: 24), stack.trailingAnchor.constraint(equalTo: window.contentView!.trailingAnchor, constant: -24), stack.topAnchor.constraint(equalTo: window.contentView!.topAnchor, constant: 24)])
        window.makeKeyAndOrderFront(nil); NSApp.activate(ignoringOtherApps: true)
    }
    @objc private func manualCapture() {
        guard !busy else { return }
        busy = true; captureButton.isEnabled = false; endButton.isEnabled = true
        if transport == nil {
            generation += 1; let token = generation; clips = 0; label.stringValue = "当前：—"
            do {
                let config = try SpeakerConfiguration()
                let connection = SessionTransport(configuration: config) { [weak self] result in
                    guard let self, token == self.generation else { return }
                    switch result {
                    case .failure: self.finish("会话已结束或连接失败，内存标签已清除。")
                    case .success(let event):
                        switch event.event {
                        case "ready": self.askPermissionAndCapture(token)
                        case "observation":
                            self.clips += 1
                            self.label.stringValue = "当前：" + self.display(event.label)
                            self.status.stringValue = "本句处理完成；再次采集须手动点击。仅限当前会话。"
                            self.busy = false; self.captureButton.isEnabled = self.clips < 32
                        case "closed": self.finish("会话已关闭，内存标签已清除。")
                        default: self.finish("会话响应无效。")
                        }
                    }
                }
                transport = connection; status.stringValue = "正在连接独立云端实验，麦克风尚未开启…"; connection.start()
            } catch { finish("配置缺失或无效。请使用配置完整的 open.command 启动。") }
        } else { askPermissionAndCapture(generation) }
    }
    private func askPermissionAndCapture(_ token: Int) {
        guard generation == token, transport != nil, busy else { return }
        status.stringValue = "等待麦克风授权…"
        AVCaptureDevice.requestAccess(for: .audio) { [weak self] granted in
            DispatchQueue.main.async {
                guard let self, self.generation == token, self.transport != nil, self.busy else { return }
                guard granted else { self.finish("未获麦克风权限，已结束会话。"); return }
                do {
                    self.status.stringValue = "正在采集 4 秒，请只让一人说话…"
                    try self.capture.start { [weak self] result in
                        guard let self, self.generation == token, let transport = self.transport, self.busy else { return }
                        switch result {
                        case .success(let samples): self.status.stringValue = "采集已停止，等待云端标签…"; transport.send(samples)
                        case .failure: self.finish("采集失败，已结束并清除会话。")
                        }
                    }
                } catch { self.finish("无法启动麦克风，已结束会话。") }
            }
        }
    }
    private func display(_ value: String) -> String {
        if value == "uncertain" { return "不确定" }; if value == "unknown" { return "未知" }
        if let n = Int(value.replacingOccurrences(of: "guest_", with: "")), (1...8).contains(n) { return "访客 " + String(UnicodeScalar(64 + n)!) }
        return "不确定"
    }
    @objc private func endSession() { finish("会话已结束；再次点击将新建会话，从访客 A 开始。") }
    private func finish(_ message: String) {
        generation += 1; capture.stop(); transport?.stop(); transport = nil; busy = false; clips = 0
        captureButton.isEnabled = true; endButton.isEnabled = false; label.stringValue = "当前：—"; status.stringValue = message
    }
    func windowWillClose(_ notification: Notification) { finish("会话已结束。"); NSApp.terminate(nil) }
    func applicationWillTerminate(_ notification: Notification) { capture.stop(); transport?.stop() }
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { true }
}

// Application memory only: this does not establish the state of kernel buffers
// or existing swap. No system-wide swap setting is changed.
var coreLimit = rlimit(rlim_cur: 0, rlim_max: 0)
_ = setrlimit(RLIMIT_CORE, &coreLimit)
signal(SIGPIPE, SIG_IGN)
let application = NSApplication.shared
let delegate = SpeakerWindow()
application.delegate = delegate
application.setActivationPolicy(.regular)
application.run()
