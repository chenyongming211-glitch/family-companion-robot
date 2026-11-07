import Foundation
import Darwin

struct CaptureConfig {
    let target: String
    let key: String
    let receiver: String
    let root: String
    let participant: String
    init(environment: [String: String]) throws {
        func path(_ name: String) throws -> String {
            guard let value = environment[name], value.hasPrefix("/"), !value.contains("\0"), !value.contains("\n"), !value.contains("\r"), !value.split(separator: "/").contains("..") else { throw ClientFailure("configuration_missing") }
            return value
        }
        guard let target = environment["COMPUTER_CAPTURE_SSH_TARGET"], target.range(of: #"^[A-Za-z0-9_][A-Za-z0-9_.-]*@[A-Za-z0-9][A-Za-z0-9.-]*$"#, options: .regularExpression) != nil else { throw ClientFailure("configuration_missing") }
        let participant = environment["COMPUTER_CAPTURE_PARTICIPANT"] ?? "M01"
        guard participant.range(of: #"^M0[1-5]$"#, options: .regularExpression) != nil else { throw ClientFailure("configuration_missing") }
        self.target = target; self.participant = participant
        key = try path("COMPUTER_CAPTURE_SSH_KEY")
        receiver = try path("COMPUTER_CAPTURE_RECEIVER")
        root = try path("COMPUTER_CAPTURE_ROOT")
    }
    func arguments(clip: String) -> [String] {
        func quote(_ value: String) -> String { "'" + value.replacingOccurrences(of: "'", with: "'\\''") + "'" }
        return ["-F", "/dev/null", "-T", "-o", "BatchMode=yes", "-o", "IdentitiesOnly=yes", "-o", "StrictHostKeyChecking=yes", "-o", "ControlMaster=no", "-o", "ControlPath=none", "-o", "ConnectTimeout=8", "-i", key, target, "python3 \(quote(receiver)) --root \(quote(root)) --clip \(quote(clip)) --participant \(quote(participant))"]
    }
}

enum ReceiverReply: Equatable { case ready, stored }
struct ReplyParser {
    let clip: String
    private var pending = Data()
    private var ready = false
    private var stored = false
    private var total = 0
    init(clip: String) { self.clip = clip }
    mutating func append(_ bytes: Data) throws -> [ReceiverReply] {
        total += bytes.count
        guard total <= 4096 else { throw ClientFailure("receiver_invalid") }
        pending.append(bytes)
        var events: [ReceiverReply] = []
        while let newline = pending.firstIndex(of: 10) {
            let raw = pending.subdata(in: 0..<newline)
            pending.removeSubrange(0...newline)
            guard raw.count <= 2048, let obj = try JSONSerialization.jsonObject(with: raw) as? [String: Any], let version = obj["schema_version"] as? NSNumber, String(cString: version.objCType) != "c", version.intValue == 1, version.doubleValue == 1, obj["clip"] as? String == clip, let event = obj["event"] as? String else { throw ClientFailure("receiver_invalid") }
            // Each allowed key must occur once, rejecting duplicate JSON fields.
            let text = String(decoding: raw, as: UTF8.self)
            for key in obj.keys {
                let pattern = "\"" + NSRegularExpression.escapedPattern(for: key) + "\"\\s*:"
                guard text.ranges(of: pattern).count == 1 else { throw ClientFailure("receiver_invalid") }
            }
            if event == "ready", !ready, !stored, Set(obj.keys) == Set(["schema_version", "event", "clip"]) {
                ready = true; events.append(.ready)
            } else if event == "stored", ready, !stored, Set(obj.keys) == Set(["schema_version", "event", "clip", "samples", "duration_seconds", "rms"]), let samples = obj["samples"] as? NSNumber, String(cString: samples.objCType) != "c", samples.intValue == 64000, samples.doubleValue == 64000, let duration = obj["duration_seconds"] as? NSNumber, String(cString: duration.objCType) != "c", duration.doubleValue == 4, let rms = obj["rms"] as? NSNumber, String(cString: rms.objCType) != "c", rms.doubleValue.isFinite, rms.doubleValue >= 0, rms.doubleValue <= 1 {
                stored = true; events.append(.stored)
            } else if event == "incomplete" { throw ClientFailure("receiver_incomplete") }
            else { throw ClientFailure("receiver_invalid") }
        }
        guard pending.count <= 2048 else { throw ClientFailure("receiver_invalid") }
        return events
    }
    func finish() throws {
        guard stored, pending.isEmpty else { throw ClientFailure("receiver_incomplete") }
    }
}
private extension String {
    func ranges(of pattern: String) -> [NSTextCheckingResult] {
        (try? NSRegularExpression(pattern: pattern).matches(in: self, range: NSRange(startIndex..., in: self))) ?? []
    }
}

// Each instance owns exactly one nonmultiplexed SSH child. No retries.
final class CaptureTransport {
    enum Event { case ready, stored, failed(String) }
    private let queue = DispatchQueue(label: "computer-capture.transport")
    private let writer = DispatchQueue(label: "computer-capture.writer")
    private var process: Process?
    private var input: FileHandle?
    private var sources: [DispatchSourceRead] = []
    private var parser: ReplyParser
    private var outputEOF = false
    private var receivedStored = false
    private var exited: Int32?
    private var finished = false
    private var stopping = false
    private var stderrBytes = 0
    private var timer: DispatchWorkItem?
    private var callback: ((Event) -> Void)?
    private var cancellationCallbacks: [() -> Void] = []
    let clip: String

    init(clip: String) { self.clip = clip; parser = ReplyParser(clip: clip) }
    func start(config: CaptureConfig, executable: String = "/usr/bin/ssh", arguments: [String]? = nil, timeout: Double = 70, callback: @escaping (Event) -> Void) {
        queue.async {
            guard self.process == nil, !self.stopping else { return }
            self.callback = callback
            let process = Process()
            let input = Pipe(), output = Pipe(), errors = Pipe()
            process.executableURL = URL(fileURLWithPath: executable)
            process.arguments = arguments ?? config.arguments(clip: self.clip)
            process.standardInput = input; process.standardOutput = output; process.standardError = errors
            self.process = process; self.input = input.fileHandleForWriting
            process.terminationHandler = { [weak self] child in
                guard let self else { return }
                self.queue.async { self.exited = child.terminationStatus; self.finishIfExited() }
            }
            do {
                try process.run()
                self.observe(output.fileHandleForReading, isOutput: true)
                self.observe(errors.fileHandleForReading, isOutput: false)
                self.armTimeout(min(timeout, 10), readyOnly: true)
            } catch { self.fail("ssh_start_failed") }
        }
    }
    private func observe(_ handle: FileHandle, isOutput: Bool) {
        let fd = handle.fileDescriptor
        _ = fcntl(fd, F_SETFL, fcntl(fd, F_GETFL) | O_NONBLOCK)
        let source = DispatchSource.makeReadSource(fileDescriptor: fd, queue: queue)
        source.setCancelHandler { try? handle.close() }
        source.setEventHandler { [weak self, weak source] in
            guard let self, let source, !self.finished else { return }
            var bytes = [UInt8](repeating: 0, count: 4096)
            let count = Darwin.read(fd, &bytes, bytes.count)
            if count == 0 {
                source.cancel()
                if isOutput { self.outputEOF = true; self.finishIfExited() }
            } else if count > 0 {
                if isOutput {
                    do {
                        for reply in try self.parser.append(Data(bytes.prefix(count))) {
                            if reply == .ready { self.armTimeout(60, readyOnly: false); self.emit(.ready) }
                            else { self.receivedStored = true }
                        }
                    } catch let error as ClientFailure { self.fail(error.code) }
                    catch { self.fail("receiver_invalid") }
                } else {
                    self.stderrBytes += count
                    if self.stderrBytes > 16384 { self.fail("ssh_output_limit") }
                }
            } else if errno != EAGAIN && errno != EINTR { self.fail("ssh_read_failed") }
        }
        sources.append(source); source.resume()
    }
    func send(_ record: NativeRecord) {
        queue.async {
            guard !self.finished, !self.stopping, let input = self.input else { return }
            self.writer.async {
                do {
                    let bytes = try record.framed()
                    var offset = 0
                    while offset < bytes.count {
                        let end = min(offset + 65536, bytes.count)
                        try input.write(contentsOf: bytes.subdata(in: offset..<end)); offset = end
                    }
                    try input.close()
                } catch { self.queue.async { self.fail("ssh_write_failed") } }
            }
        }
    }
    private func armTimeout(_ seconds: Double, readyOnly: Bool) {
        timer?.cancel()
        let timer = DispatchWorkItem { [weak self] in self?.fail(readyOnly ? "ssh_ready_timeout" : "receiver_timeout") }
        self.timer = timer; queue.asyncAfter(deadline: .now() + seconds, execute: timer)
    }
    private func emit(_ event: Event) {
        guard let callback else { return }
        DispatchQueue.main.async { callback(event) }
    }
    private func finishIfExited() {
        guard !finished, !stopping, let exited, outputEOF else { return }
        do {
            try parser.finish()
            guard exited == 0, receivedStored else { fail("ssh_exit_failed"); return }
            finished = true; timer?.cancel(); sources.forEach { $0.cancel() }; emit(.stored); callback = nil; input = nil; process = nil
        } catch { fail("receiver_incomplete") }
    }
    private func fail(_ code: String) {
        guard !finished, !stopping else { return }
        stop { self.emit(.failed(code)); self.callback = nil }
    }
    private func stop(completion: @escaping () -> Void) {
        stopping = true; timer?.cancel()
        sources.forEach { $0.cancel() }; sources.removeAll()
        let process = self.process
        // A blocked pipe writer unblocks when this owned child exits.
        DispatchQueue.global().async {
            if let process, process.isRunning {
                kill(process.processIdentifier, SIGTERM)
                let end = Date().addingTimeInterval(0.5)
                while process.isRunning && Date() < end { usleep(10000) }
                if process.isRunning { kill(process.processIdentifier, SIGKILL) }
                process.waitUntilExit()
            }
            self.queue.async {
                try? self.input?.close(); self.input = nil; self.process = nil
                self.finished = true; completion()
                let callbacks = self.cancellationCallbacks; self.cancellationCallbacks.removeAll()
                callbacks.forEach { callback in DispatchQueue.main.async(execute: callback) }
            }
        }
    }
    func cancel(completion: @escaping () -> Void = {}) {
        queue.async {
            self.callback = nil
            if self.finished { DispatchQueue.main.async(execute: completion) }
            else {
                self.cancellationCallbacks.append(completion)
                if !self.stopping { self.stop {} }
            }
        }
    }
}
