import Foundation
import CoreFoundation
import Darwin

struct SpeakerConfiguration {
    let target: String, key: String, binary: String, model: String, sha: String
    init(environment: [String: String] = ProcessInfo.processInfo.environment) throws {
        func value(_ name: String) throws -> String {
            guard let v = environment[name], !v.isEmpty, !v.contains("\n"), !v.contains("\0") else { throw ClientFailure("configuration") }
            return v
        }
        target = try value("SPEAKER_SSH_TARGET"); key = try value("SPEAKER_SSH_KEY")
        binary = try value("SPEAKER_LAB_BINARY"); model = try value("SPEAKER_MODEL"); sha = try value("SPEAKER_MODEL_SHA")
        guard target.range(of: "^[A-Za-z0-9_][A-Za-z0-9_.@:-]*$", options: .regularExpression) != nil,
              key.hasPrefix("/"), binary.hasPrefix("/"), model.hasPrefix("/"),
              sha.range(of: "^[0-9a-f]{64}$", options: .regularExpression) != nil else { throw ClientFailure("configuration") }
    }
}

struct SpeakerEvent { let event: String; let id: UInt64; let label: String }

final class SessionTransport {
    private let queue = DispatchQueue(label: "speaker.session.transport")
    private let queueKey = DispatchSpecificKey<UInt8>()
    private let lock = NSLock()
    private var cancelled = false
    private var queuedReads = 0
    private var readOverflow = false
    private var process: Process?
    private var input: FileHandle?
    private var output: FileHandle?
    private var pending = Data()
    private var ended = false
    private var ready = false
    private var expectedID: UInt64 = 0
    private var nextID: UInt64 = 1
    private var sampleEnd: UInt64 = 0
    private let session = UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased()
    private let configuration: SpeakerConfiguration
    private let callback: (Result<SpeakerEvent, ClientFailure>) -> Void
    private var timeout: DispatchWorkItem?

    init(configuration: SpeakerConfiguration, callback: @escaping (Result<SpeakerEvent, ClientFailure>) -> Void) {
        self.configuration = configuration; self.callback = callback
        queue.setSpecific(key: queueKey, value: 1)
    }
    private var stopped: Bool { lock.lock(); defer { lock.unlock() }; return cancelled }
    private func emit(_ value: Result<SpeakerEvent, ClientFailure>) { DispatchQueue.main.async { [weak self] in guard let self, !self.stopped else { return }; self.callback(value) } }
    private func quote(_ value: String) -> String { "'" + value.replacingOccurrences(of: "'", with: "'\\''") + "'" }
    func start(launch: ((Process) throws -> Void)? = nil) {
        queue.async { [weak self] in
            guard let self, !self.stopped else { return }
            let child = Process(), stdin = Pipe(), stdout = Pipe()
            child.executableURL = URL(fileURLWithPath: "/usr/bin/ssh")
            let remote = [self.configuration.binary, "session", "--model", self.configuration.model, "--sha256", self.configuration.sha, "--declared-single-speaker"].map(self.quote).joined(separator: " ")
            child.arguments = ["-T", "-F", "/dev/null", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "ServerAliveInterval=5", "-o", "ServerAliveCountMax=2", "-o", "ClearAllForwardings=yes", "-o", "IdentitiesOnly=yes", "-i", self.configuration.key, "--", self.configuration.target, remote]
            child.environment = ["PATH": "/usr/bin:/bin", "LANG": "C", "LC_ALL": "C", "HOME": NSHomeDirectory()]
            child.standardInput = stdin; child.standardOutput = stdout; child.standardError = FileHandle.nullDevice
            stdout.fileHandleForReading.readabilityHandler = { [weak self] handle in
                guard let data = self?.availableChunk() else { return }
                self?.enqueueRead(data)
            }
            _ = fcntl(stdout.fileHandleForReading.fileDescriptor, F_SETFL, O_NONBLOCK)
            child.terminationHandler = { [weak self] _ in self?.queue.async { [weak self] in self?.fail() } }
            self.lock.lock()
            if self.cancelled { self.lock.unlock(); stdout.fileHandleForReading.readabilityHandler = nil; return }
            self.process = child; self.input = stdin.fileHandleForWriting; self.output = stdout.fileHandleForReading
            do { if let launch { try launch(child) } else { try child.run() }; _ = fcntl(stdin.fileHandleForWriting.fileDescriptor, F_SETFL, O_NONBLOCK); self.lock.unlock() } catch { self.lock.unlock(); self.fail(); return }
            self.armTimeout(15)
            self.queue.asyncAfter(deadline: .now() + 300) { [weak self] in self?.fail() }
        }
    }
    private func availableChunk() -> Data? {
        // The descriptor stays owned while reading; shutdown removes the
        // reference under this same lock before closing it on the queue.
        lock.lock(); defer { lock.unlock() }
        guard !cancelled, let output else { return nil }
        var bytes = [UInt8](repeating: 0, count: 16385)
        let count = Darwin.read(output.fileDescriptor, &bytes, bytes.count)
        if count < 0 && (errno == EAGAIN || errno == EINTR) { return nil }
        return count > 0 ? Data(bytes[0..<count]) : Data()
    }
    private func enqueueRead(_ data: Data) {
        lock.lock()
        guard !cancelled, !readOverflow else { lock.unlock(); return }
        if queuedReads >= 4 {
            readOverflow = true; lock.unlock()
            queue.async { [weak self] in self?.fail() }
            return
        }
        queuedReads += 1; lock.unlock()
        queue.async { [weak self] in
            guard let self else { return }
            self.lock.lock(); self.queuedReads -= 1; self.lock.unlock()
            guard data.count <= 16384 else { self.fail(); return }
            self.consume(data)
        }
    }
    private func armTimeout(_ seconds: Double) {
        timeout?.cancel()
        let work = DispatchWorkItem { [weak self] in self?.fail() }
        timeout = work; queue.asyncAfter(deadline: .now() + seconds, execute: work)
    }
    private func consume(_ data: Data) {
        guard !stopped, !ended else { return }
        guard !data.isEmpty, pending.count + data.count <= 16384 else { fail(); return }
        pending.append(data)
        while let newline = pending.firstIndex(of: 10) {
            let line = pending.subdata(in: 0..<newline); pending.removeSubrange(0...newline)
            guard let object = try? JSONSerialization.jsonObject(with: line) as? [String: Any],
                  object["evidence"] as? String == "user_declared_test", let event = object["event"] as? String else { fail(); return }
            switch event {
            case "ready":
                guard !ready, Set(object.keys) == Set(["event", "evidence"]) else { fail(); return }
                ready = true; timeout?.cancel(); emit(.success(SpeakerEvent(event: event, id: 0, label: "")))
            case "observation":
                guard ready, expectedID > 0, Set(object.keys) == Set(["event", "id", "label", "duration_ms", "evidence"]),
                      let id = object["id"] as? NSNumber, CFGetTypeID(id) != CFBooleanGetTypeID(), id.stringValue == String(expectedID),
                      let label = object["label"] as? String, label.range(of: "^(guest_[1-8]|uncertain|unknown)$", options: .regularExpression) != nil,
                      let duration = object["duration_ms"] as? NSNumber, CFGetTypeID(duration) != CFBooleanGetTypeID(), duration.stringValue == String(duration.int64Value), (0...4000).contains(duration.intValue) else { fail(); return }
                expectedID = 0; timeout?.cancel(); emit(.success(SpeakerEvent(event: event, id: id.uint64Value, label: label)))
            case "closed":
                guard Set(object.keys) == Set(["event", "evidence"]) else { fail(); return }
                ended = true; timeout?.cancel()
                emit(.success(SpeakerEvent(event: event, id: 0, label: "")))
                shutdown()
            default: fail(); return
            }
        }
    }
    func send(_ samples: [Int16]) {
        queue.async { [weak self] in
            guard let self, !self.stopped, self.ready, self.expectedID == 0, samples.count == 64000, self.nextID <= 32 else { self?.fail(); return }
            let id = self.nextID
            let header: [String: Any] = ["version": 1, "id": id, "session": self.session, "scope": "manual-test", "generation": 1, "start": self.sampleEnd, "end": self.sampleEnd + 64000, "model_sha256": self.configuration.sha, "sample_rate": 16000, "pcm_bytes": 128000]
            guard let json = try? JSONSerialization.data(withJSONObject: header), let handle = self.input else { self.fail(); return }
            var size = UInt32(json.count).bigEndian
            var frame = withUnsafeBytes(of: &size) { Data($0) }; frame.append(json)
            for sample in samples { var value = sample.littleEndian; withUnsafeBytes(of: &value) { frame.append(contentsOf: $0) } }
            defer { frame.resetBytes(in: 0..<frame.count) }
            self.expectedID = id; self.nextID += 1; self.sampleEnd += 64000
            let deadline = ProcessInfo.processInfo.systemUptime + 12
            var offset = 0
            while offset < frame.count {
                guard !self.stopped, ProcessInfo.processInfo.systemUptime < deadline else { self.fail(); return }
                let written = frame.withUnsafeBytes { Darwin.write(handle.fileDescriptor, $0.baseAddress!.advanced(by: offset), frame.count - offset) }
                if written > 0 { offset += written }
                else if errno == EAGAIN || errno == EINTR { var fd = pollfd(fd: handle.fileDescriptor, events: Int16(POLLOUT), revents: 0); _ = poll(&fd, 1, 50) }
                else { self.fail(); return }
            }
            self.armTimeout(15)
        }
    }
    private func fail() { guard !stopped, !ended else { return }; ended = true; emit(.failure(ClientFailure("session_ended"))); shutdown() }
    private func shutdown() {
        lock.lock(); let child = process; let stdin = input; let stdout = output; process = nil; input = nil; output = nil; lock.unlock()
        stdout?.readabilityHandler = nil
        try? stdin?.close(); try? stdout?.close()
        retire(child)
        timeout?.cancel(); pending.resetBytes(in: 0..<pending.count); pending.removeAll()
    }
    // EOF first, then bounded TERM/KILL escalation and reaping. This happens
    // before stop returns, including when the application is about to exit.
    private func retire(_ child: Process?) {
        guard let child, child.processIdentifier > 0 else { return }
        func awaitExit(_ seconds: Double) {
            let deadline = ProcessInfo.processInfo.systemUptime + seconds
            while child.isRunning && ProcessInfo.processInfo.systemUptime < deadline { usleep(10000) }
        }
        child.terminationHandler = nil
        awaitExit(0.2)
        if child.isRunning { child.terminate(); awaitExit(0.5) }
        if child.isRunning { kill(child.processIdentifier, SIGKILL) }
        // SIGKILL cannot be ignored. Foundation reaps the actual child before
        // this returns; there is no deferred cleanup relying on a live app.
        child.waitUntilExit()
    }
    func stop() {
        lock.lock(); cancelled = true; lock.unlock()
        // The nonblocking write loop observes cancellation within its 50 ms
        // poll. Close descriptors only after it has relinquished the queue.
        if DispatchQueue.getSpecific(key: queueKey) != nil { shutdown() }
        else { queue.sync { self.shutdown() } }
    }
    deinit {
        output?.readabilityHandler = nil
        try? input?.close(); try? output?.close()
        retire(process)
    }
}
