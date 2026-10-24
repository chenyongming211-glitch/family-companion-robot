import AppKit
import AVFoundation
import Foundation

func readTicket() throws -> String {
    guard let line = readLine(), line.utf8.count <= 4096 else { throw ClientFailure("ticket_stdin_required") }
    var value = line.trimmingCharacters(in: .whitespacesAndNewlines)
    if value.hasPrefix("{") {
        guard let data = value.data(using: .utf8),
              let object = try JSONSerialization.jsonObject(with: data) as? [String: Any],
              let ticket = object["ticket"] as? String else { throw ClientFailure("invalid_pair_response") }
        value = ticket
    }
    guard value.count >= 20, value.count <= 256, value.unicodeScalars.allSatisfy({
        CharacterSet(charactersIn: "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_").contains($0)
    }) else { throw ClientFailure("invalid_ticket") }
    return value
}

final class AppDelegate: NSObject, NSApplicationDelegate, NSWindowDelegate {
    let client: VoiceClient
    var window: NSWindow!
    let button = NSButton(title: "开始对话", target: nil, action: nil)
    let label = NSTextField(wrappingLabelWithString: "点一次开始，随后可直接开口插话。请在配对后两分钟内开始。")
    private var started = false
    init(client: VoiceClient) { self.client = client }
    func applicationDidFinishLaunching(_ notification: Notification) {
        window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 540, height: 220),
                          styleMask: [.titled, .closable, .miniaturizable], backing: .buffered, defer: false)
        window.title = "家庭陪伴 · 原生语音测试"
        window.center(); window.delegate = self
        button.target = self; button.action = #selector(toggle)
        button.bezelStyle = .rounded; button.font = .systemFont(ofSize: 21, weight: .medium)
        button.translatesAutoresizingMaskIntoConstraints = false
        label.alignment = .center; label.font = .systemFont(ofSize: 14)
        label.translatesAutoresizingMaskIntoConstraints = false
        let note = NSTextField(labelWithString: "Opus / CoreAudio · 连续双向语音 · 云端独立测试服务")
        note.textColor = .secondaryLabelColor; note.translatesAutoresizingMaskIntoConstraints = false
        if let content = window.contentView {
            [button, label, note].forEach { content.addSubview($0) }
            NSLayoutConstraint.activate([
                button.centerXAnchor.constraint(equalTo: content.centerXAnchor), button.topAnchor.constraint(equalTo: content.topAnchor, constant: 38),
                button.widthAnchor.constraint(equalToConstant: 200), button.heightAnchor.constraint(equalToConstant: 48),
                label.topAnchor.constraint(equalTo: button.bottomAnchor, constant: 22),
                label.leadingAnchor.constraint(equalTo: content.leadingAnchor, constant: 24), label.trailingAnchor.constraint(equalTo: content.trailingAnchor, constant: -24),
                note.bottomAnchor.constraint(equalTo: content.bottomAnchor, constant: -18), note.centerXAnchor.constraint(equalTo: content.centerXAnchor)])
        }
        client.status = { [weak self] text in DispatchQueue.main.async { self?.label.stringValue = text } }
        client.ended = { [weak self] success in DispatchQueue.main.async {
            self?.button.title = "对话已结束"; self?.button.isEnabled = false
            if success { self?.label.stringValue = "麦克风已关闭。再次对话请重新运行入口，取得新的一次性配对。" }
        } }
        window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }
    @objc func toggle() {
        if started { client.stop(); button.isEnabled = false; return }
        started = true; button.isEnabled = false; label.stringValue = "正在请求麦克风权限…"
        AVCaptureDevice.requestAccess(for: .audio) { [weak self] granted in
            DispatchQueue.main.async {
                guard let self else { return }
                if granted {
                    self.button.title = "结束对话"; self.button.isEnabled = true
                    self.client.start()
                } else {
                    self.label.stringValue = "麦克风未授权。请在系统设置中允许本应用使用麦克风后重新运行。"
                    metadata("microphone.permission", ["granted": false])
                }
            }
        }
    }
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { true }
    func applicationWillTerminate(_ notification: Notification) { client.stop() }
    func windowWillClose(_ notification: Notification) { client.stop() }
}

var args = Array(CommandLine.arguments.dropFirst())
if args == ["--self-test"] {
    let test = PlaybackSelfTest()
    test.start()
    withExtendedLifetime(test) { dispatchMain() }
}
var fixtures: [String] = []
var address = "ws://127.0.0.1:18020/xiaozhi/v1/"
if let index = args.firstIndex(of: "--url"), index + 1 < args.count {
    address = args[index + 1]; args.removeSubrange(index...(index + 1))
}
if args.first == "--fixtures", args.count == 2 {
    fixtures = args[1].split(separator: ",", omittingEmptySubsequences: false).map(String.init)
    args.removeAll()
}
guard args.isEmpty, let url = nativeEndpoint(address) else {
    fputs("Usage: NativeVoice [--url ws://127.0.0.1:18020/xiaozhi/v1/] [--fixtures path1,path2,path3] | --self-test\nTicket is accepted on stdin only.\n", stderr)
    exit(2)
}
do {
    let ticket = try readTicket()
    let client = VoiceClient(url: url, ticket: ticket, fixturePaths: fixtures)
    if !fixtures.isEmpty {
        client.ended = { success in exit(success ? 0 : 1) }
        client.start()
        withExtendedLifetime(client) { dispatchMain() }
    } else {
        let application = NSApplication.shared
        application.setActivationPolicy(.regular)
        let delegate = AppDelegate(client: client)
        application.delegate = delegate
        withExtendedLifetime(delegate) { application.run() }
    }
} catch {
    metadata("startup.failure", ["code": (error as? ClientFailure)?.description ?? "stdin_pair_response"])
    exit(2)
}
