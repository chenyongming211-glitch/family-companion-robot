import Foundation

func nativeEndpoint(_ address: String) -> URL? {
    guard let url = URL(string: address), url.scheme == "ws", url.host == "127.0.0.1",
          url.user == nil, url.password == nil,
          URLComponents(url: url, resolvingAgainstBaseURL: false)?.percentEncodedPath == "/xiaozhi/v1/",
          url.query == nil, url.fragment == nil else { return nil }
    return url
}

func nativePairingEndpoint(_ address: String) -> URL? {
    guard let native = nativeEndpoint(address), var parts = URLComponents(url: native, resolvingAgainstBaseURL: false) else { return nil }
    parts.scheme = "http"; parts.path = "/v1/pair"
    return parts.url
}

func pairedNativeTicket(_ data: Data) throws -> String {
    guard data.count <= 4096, let object = try JSONSerialization.jsonObject(with: data) as? [String: Any],
          object["device_id"] as? String == "native-mac", let ticket = object["ticket"] as? String,
          ticket.count >= 20, ticket.count <= 256,
          ticket.unicodeScalars.allSatisfy({ CharacterSet(charactersIn: "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_").contains($0) }) else {
        throw ClientFailure("invalid_pair_response")
    }
    return ticket
}
