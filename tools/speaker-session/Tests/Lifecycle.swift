import Foundation
import Darwin

struct ClientFailure: Error { let code: String; init(_ code: String) { self.code = code } }

@main
struct LifecycleSpec {
    static func require(_ condition: @autoclosure () -> Bool, _ message: String) {
        if !condition() { fatalError(message) }
    }
    static func makeTransport() throws -> (SessionTransport, () -> Process?) {
        var helper: Process?
        var ready = false
        let config = try SpeakerConfiguration(environment: ["SPEAKER_SSH_TARGET":"fixture@localhost", "SPEAKER_SSH_KEY":"/fixture/key", "SPEAKER_LAB_BINARY":"/fixture/lab", "SPEAKER_MODEL":"/fixture/model", "SPEAKER_MODEL_SHA":String(repeating:"a",count:64)])
        let transport = SessionTransport(configuration: config) { result in
            if case .success(let event) = result, event.event == "ready" { ready = true }
        }
        transport.start(launch: { child in
            child.executableURL = URL(fileURLWithPath: CommandLine.arguments[0])
            child.arguments = ["--helper"]
            helper = child
            try child.run()
        })
        let deadline = Date().addingTimeInterval(3)
        while !ready && Date() < deadline { _ = RunLoop.current.run(mode:.default,before:Date().addingTimeInterval(0.01)) }
        require(ready, "fake child did not become ready")
        return (transport, { helper })
    }
    static func checkStopped(_ child: Process, since start: TimeInterval) {
        require(!child.isRunning, "child survives stop")
        require(ProcessInfo.processInfo.systemUptime-start < 2, "stop exceeded bounded grace")
        require(kill(child.processIdentifier,0) == -1 && errno == ESRCH, "child PID survives cleanup")
    }
    static func main() throws {
        signal(SIGPIPE,SIG_IGN)
        if CommandLine.arguments.contains("--helper") {
            signal(SIGTERM,SIG_IGN)
            let ready = Data("{\"event\":\"ready\",\"evidence\":\"user_declared_test\"}\n".utf8)
            try FileHandle.standardOutput.write(contentsOf:ready)
            while true { pause() } // Ignore both stdin EOF and TERM; cleanup must KILL/reap.
        }
        if CommandLine.arguments.contains("--owner-exit") {
            var transport: SessionTransport?
            let (created, helper) = try makeTransport(); transport=created
            let child=helper()!
            print(child.processIdentifier); fflush(stdout)
            transport?.stop(); transport=nil
            require(!child.isRunning,"app-exit child survives")
            return
        }
        do {
            var transport: SessionTransport?
            var helper: (() -> Process?)!
            (transport,helper)=try makeTransport()
            weak let weakTransport=transport
            let child=helper()!
            let start=ProcessInfo.processInfo.systemUptime
            transport?.stop(); transport=nil
            checkStopped(child,since:start)
            require(weakTransport == nil,"idle transport retained after stop")
        }
        do {
            let (transport,helper)=try makeTransport()
            transport.send([Int16](repeating:1000,count:64000))
            usleep(100000) // Fake child never reads; a 128 KB frame fills stdin.
            let child=helper()!, start=ProcessInfo.processInfo.systemUptime
            transport.stop()
            checkStopped(child,since:start)
        }
        do {
            let probe=Process(), output=Pipe()
            probe.executableURL=URL(fileURLWithPath:CommandLine.arguments[0]);probe.arguments=["--owner-exit"]
            probe.standardOutput=output;probe.standardError=FileHandle.nullDevice
            try probe.run();probe.waitUntilExit()
            require(probe.terminationStatus == 0,"app-exit regression failed")
            let data=output.fileHandleForReading.readDataToEndOfFile()
            guard let text=String(data:data,encoding:.utf8),let pid=Int32(text.trimmingCharacters(in:.whitespacesAndNewlines)) else {fatalError("missing synthetic helper PID")}
            require(kill(pid,0) == -1 && errno == ESRCH,"child outlives app exit")
        }
        print("Lifecycle: idle owner release, TERM-ignore/app-exit, blocked-write cancellation PASS")
    }
}
