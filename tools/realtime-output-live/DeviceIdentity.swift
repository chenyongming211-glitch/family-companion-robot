import Foundation
import Darwin

// Installation credential only. Model/provider credentials never reach the Mac.
struct DeviceIdentity: Codable {
    let deviceID: String
    let credential: String
    let clientType: String
    enum CodingKeys: String, CodingKey {
        case deviceID = "device_id", credential, clientType = "client_type"
    }
    static func validProfile(_ value: String) -> Bool {
        value.range(of: "^[a-z0-9][a-z0-9_-]{0,39}$", options: .regularExpression) != nil
    }
    func validate(kind: String) throws {
        guard clientType == kind,
              deviceID.range(of: "^dev_[a-f0-9]{32}$", options: .regularExpression) != nil,
              credential.range(of: "^[A-Za-z0-9_-]{43}$", options: .regularExpression) != nil
        else { throw ClientFailure("terminal_identity_invalid") }
    }
    static func location(profile: String, kind: String) throws -> URL {
        guard validProfile(profile), ["mac_native", "mac_fixture"].contains(kind) else {
            throw ClientFailure("terminal_profile_invalid")
        }
        let manager = FileManager.default
        let base = try manager.url(for: .applicationSupportDirectory, in: .userDomainMask,
                                   appropriateFor: nil, create: true)
        var dir = base
        for part in ["FamilyCompanion", "Realtime18040"] {
            dir.appendPathComponent(part, isDirectory: true)
            if !manager.fileExists(atPath: dir.path) {
                try manager.createDirectory(at: dir, withIntermediateDirectories: false,
                    attributes: [.posixPermissions: 0o700])
            }
            let attrs = try manager.attributesOfItem(atPath: dir.path)
            guard attrs[.type] as? FileAttributeType == .typeDirectory,
                  (attrs[.posixPermissions] as? NSNumber)?.intValue == 0o700 else {
                throw ClientFailure("terminal_directory_not_private")
            }
        }
        return dir.appendingPathComponent("\(profile)-\(kind).json")
    }
    static func load(_ file: URL, kind: String) throws -> DeviceIdentity? {
        let attrs: [FileAttributeKey: Any]
        do { attrs = try FileManager.default.attributesOfItem(atPath: file.path) }
        catch let error as NSError where error.domain == NSCocoaErrorDomain && [NSFileNoSuchFileError, NSFileReadNoSuchFileError].contains(error.code) { return nil }
        guard attrs[.type] as? FileAttributeType == .typeRegular,
              (attrs[.posixPermissions] as? NSNumber)?.intValue == 0o600,
              (attrs[.size] as? NSNumber)?.intValue ?? 4097 <= 4096 else {
            throw ClientFailure("terminal_credential_not_private")
        }
        let value = try JSONDecoder().decode(DeviceIdentity.self, from: Data(contentsOf: file))
        try value.validate(kind: kind)
        return value
    }
    func persist(_ file: URL) throws -> DeviceIdentity {
        let temp = file.deletingLastPathComponent().appendingPathComponent(".identity-" + UUID().uuidString)
        let fd = Darwin.open(temp.path, O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW, 0o600)
        guard fd >= 0 else { throw ClientFailure("terminal_credential_write") }
        defer { Darwin.close(fd); try? FileManager.default.removeItem(at: temp) }
        let raw = try JSONEncoder().encode(self)
        let wrote = raw.withUnsafeBytes { Darwin.write(fd, $0.baseAddress, $0.count) }
        guard wrote == raw.count, Darwin.fsync(fd) == 0 else { throw ClientFailure("terminal_credential_write") }
        // Publish only the complete private file and never replace another instance.
        if Darwin.link(temp.path, file.path) != 0 {
            guard errno == EEXIST, let existing = try Self.load(file, kind: clientType) else {
                throw ClientFailure("terminal_credential_publish")
            }
            return existing
        }
        return self
    }
    static func prepare(session: URLSession, endpoint: URL, profile: String, kind: String) async throws -> DeviceIdentity {
        let file = try location(profile: profile, kind: kind)
        if let saved = try load(file, kind: kind) { return saved }
        var parts = URLComponents(url: endpoint, resolvingAgainstBaseURL: false)!
        parts.scheme = "http"; parts.path = "/devices"
        var request = URLRequest(url: parts.url!); request.httpMethod = "POST"; request.timeoutInterval = 8
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONSerialization.data(withJSONObject: ["client_type": kind])
        let (raw, response) = try await session.data(for: request)
        guard (response as? HTTPURLResponse)?.statusCode == 201, raw.count <= 4096 else {
            throw ClientFailure("terminal_enrollment_failed")
        }
        let value = try JSONDecoder().decode(DeviceIdentity.self, from: raw)
        try value.validate(kind: kind)
        return try value.persist(file)
    }
}
