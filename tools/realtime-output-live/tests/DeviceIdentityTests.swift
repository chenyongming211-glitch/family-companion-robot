import Foundation

// Mac test-client files only: no server, microphone, audio or model requests.
@main struct DeviceIdentityTests {
    static func main() throws {
        let profile = "verify-local-" + UUID().uuidString.prefix(8).lowercased()
        let file = try DeviceIdentity.location(profile: profile, kind: "mac_fixture")
        defer { try? FileManager.default.removeItem(at: file) }
        let missing = try DeviceIdentity.load(file, kind: "mac_fixture")
        precondition(missing == nil)
        let first = DeviceIdentity(deviceID: "dev_" + String(repeating: "a", count: 32),
            credential: String(repeating: "x", count: 43), clientType: "mac_fixture")
        let saved = try first.persist(file)
        precondition(saved.deviceID == first.deviceID)
        let restored = try DeviceIdentity.load(file, kind: "mac_fixture")!
        precondition(restored.credential == first.credential)
        let other = DeviceIdentity(deviceID: "dev_" + String(repeating: "b", count: 32),
            credential: String(repeating: "y", count: 43), clientType: "mac_fixture")
        let winner = try other.persist(file)
        precondition(winner.deviceID == first.deviceID)
        try FileManager.default.setAttributes([.posixPermissions: 0o644], ofItemAtPath: file.path)
        do { _ = try DeviceIdentity.load(file, kind: "mac_fixture"); fatalError("public credential file accepted") } catch {}
        try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: file.path)
        try Data("broken".utf8).write(to: file)
        do { _ = try DeviceIdentity.load(file, kind: "mac_fixture"); fatalError("corrupt credential file accepted") } catch {}
        precondition(!DeviceIdentity.validProfile("../bad"))
        print("Mac identity file checks: 7 passed; no network or audio")
    }
}
