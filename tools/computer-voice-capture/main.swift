import AppKit
import AVFoundation
import Darwin

var coreLimit = rlimit(rlim_cur: 0, rlim_max: 0)
setrlimit(RLIMIT_CORE, &coreLimit)
signal(SIGPIPE, SIG_IGN)

final class CaptureApp: NSObject, NSApplicationDelegate, NSWindowDelegate {
    private var window: NSWindow!
    private let record = NSButton(title: "手动录制 4 秒", target: nil, action: nil)
    private let cancel = NSButton(title: "取消本段", target: nil, action: nil)
    private let status = NSTextField(wrappingLabelWithString: "待机：尚未连接，尚未申请麦克风权限。")
    private let count = NSTextField(labelWithString: "本窗口已保存：0 段候选素材")
    private let prompt = NSTextField(wrappingLabelWithString: "")
    private let capture = ClipCapture()
    private var transport: CaptureTransport?
    private var state = CaptureState()
    private var cleaning = false
    private let prompts = [
        "今天我想和小满聊聊最近发生的有趣事情。",
        "窗外的天气不错，我准备出去慢慢走一走。",
        "我喜欢清晨安静的时光，也喜欢听家人的故事。",
        "如果周末有空，我们一起做一顿简单的晚饭。",
        "请记得每天喝水休息，照顾好自己的身体。",
        "这是一段新的声音素材，我正在自然地说话。",
        "傍晚的时候，院子里传来轻轻的脚步声。",
        "书架上放着几本旧书，每本都有不同的故事。",
        "雨停以后空气很清新，树叶上还留着水珠。",
        "我们可以先整理房间，再一起讨论明天的安排。",
        "一杯温热的茶，让忙碌的一天慢慢安静下来。",
        "下次见面时，我想分享一次难忘的旅行经历。"
    ]
    func applicationDidFinishLaunching(_ notification: Notification) {
        window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 660, height: 560), styleMask: [.titled, .closable, .miniaturizable], backing: .buffered, defer: false)
        window.title = "电脑麦克风登记候选采集"; window.delegate = self
        window.isReleasedWhenClosed = false
        let stack = NSStackView(); stack.orientation = .vertical; stack.alignment = .leading; stack.spacing = 16; stack.translatesAutoresizingMaskIntoConstraints = false
        let title = NSTextField(labelWithString: "每次手动点击，只录 4 秒")
        title.font = .boldSystemFont(ofSize: 23)
        let notice = NSTextField(wrappingLabelWithString: "请确保现场只有当前一位自愿参与者。使用系统当前电脑输入麦克风，录音仅传至私有云端保存。本窗口不播放声音。")
        let participant = (try? CaptureConfig(environment: ProcessInfo.processInfo.environment))?.participant ?? "配置未就绪"
        let scope = NSTextField(wrappingLabelWithString: "匿名参与者：\(participant)。所有片段由同一位参与者完成，不填写姓名。当前条件：电脑麦克风登记 → 机器人查询；手机条件待验。")
        let guidance = NSTextField(wrappingLabelWithString: "先自然朗读 6 段不同短句，每段尽量说满约 4 秒，再核对云端质量。需要补录时使用后续新短句，最多 12 段。候选数量不代表 20 秒净人声或验收通过。")
        let buttons = NSStackView(views: [record, cancel]); buttons.spacing = 12
        [title, notice, scope, guidance, prompt, buttons, count, status].forEach { stack.addArrangedSubview($0) }
        window.contentView!.addSubview(stack)
        NSLayoutConstraint.activate([stack.leadingAnchor.constraint(equalTo: window.contentView!.leadingAnchor, constant: 26), stack.trailingAnchor.constraint(equalTo: window.contentView!.trailingAnchor, constant: -26), stack.topAnchor.constraint(equalTo: window.contentView!.topAnchor, constant: 24)])
        record.target = self; record.action = #selector(begin)
        cancel.target = self; cancel.action = #selector(cancelClip)
        refresh(); window.center(); window.makeKeyAndOrderFront(nil); NSApp.activate(ignoringOtherApps: true)
    }
    private func refresh() {
        record.isEnabled = state.phase == .idle && !cleaning && state.saved < prompts.count
        cancel.isEnabled = state.phase != .idle && !cleaning
        count.stringValue = "本窗口已保存：\(state.saved) 段候选素材（待质量验证）"
        prompt.stringValue = state.saved < prompts.count ? "第 \(state.saved + 1) 段提示：\(prompts[state.saved])" : "本窗口的 12 条不同短句已采完，请等待云端质量核对。"
        prompt.font = .systemFont(ofSize: 18, weight: .medium)
    }
    @objc private func begin() {
        guard state.phase == .idle, !cleaning, state.saved < prompts.count else { return }
        let config: CaptureConfig
        do { config = try CaptureConfig(environment: ProcessInfo.processInfo.environment) }
        catch { status.stringValue = "启动配置缺失或无效；尚未连接或开麦。请按说明从 open.command 启动。"; return }
        let clip = UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased()
        guard let generation = state.beginManual(clip: clip) else { return }
        let transport = CaptureTransport(clip: clip); self.transport = transport
        status.stringValue = "正在连接私有云端，麦克风尚未启动……"; refresh()
        transport.start(config: config) { [weak self] event in
            guard let self, self.state.generation == generation else { return }
            switch event {
            case .ready:
                guard self.state.acceptReady(clip: clip, generation: generation) else { return }
                self.status.stringValue = "云端已就绪，正在请求麦克风权限……"
                AVCaptureDevice.requestAccess(for: .audio) { granted in
                    DispatchQueue.main.async {
                        guard self.state.generation == generation else { return }
                        guard granted, self.state.permissionGranted(generation: generation) else { self.fail(generation, "麦克风权限未允许，本段未采集。"); return }
                        self.status.stringValue = "正在录制 4 秒，请自然朗读提示句……"
                        do {
                            try self.capture.start { result in
                                guard self.state.generation == generation else { return }
                                switch result {
                                case .success(let record):
                                    guard self.state.captureCompleted(generation: generation) else { return }
                                    self.status.stringValue = "麦克风已停止，正在等待云端保存确认……"
                                    transport.send(record)
                                case .failure:
                                    self.fail(generation, "本段采集未完成；麦克风已停止。请检查输入后手动重试。")
                                }
                            }
                        } catch { self.fail(generation, "当前输入格式不可用或麦克风启动失败；本段未保存。") }
                    }
                }
            case .stored:
                guard self.state.stored(clip: clip, generation: generation) else { return }
                self.transport = nil
                self.status.stringValue = self.state.saved < self.prompts.count ? "本段已保存至私有云端。素材仍待质量验证；如需补充，可手动录制下一条新短句。" : "12 段不同短句已保存至私有云端，请等待质量验证。"; self.refresh()
            case .failed:
                self.fail(generation, "本段连接、传输或保存未完成；麦克风已停止，请手动重试。")
            }
        }
    }
    private func fail(_ generation: Int, _ message: String) {
        guard state.fail(generation: generation) else { return }
        capture.stop(); status.stringValue = message; cleanTransport()
    }
    private func cleanTransport() {
        guard let transport else { cleaning = false; refresh(); return }
        cleaning = true; refresh()
        transport.cancel { [weak self] in self?.transport = nil; self?.cleaning = false; self?.refresh() }
    }
    @objc private func cancelClip() {
        state.cancel(); capture.stop(); status.stringValue = "本段已取消，麦克风已停止；正在回收连接。"; cleanTransport()
    }
    func windowShouldClose(_ sender: NSWindow) -> Bool { NSApp.terminate(nil); return false }
    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        state.cancel(); capture.stop()
        guard let transport else { return .terminateNow }
        transport.cancel { NSApp.reply(toApplicationShouldTerminate: true) }
        return .terminateLater
    }
}
let app = NSApplication.shared
let delegate = CaptureApp()
app.delegate = delegate
app.setActivationPolicy(.regular)
app.run()
