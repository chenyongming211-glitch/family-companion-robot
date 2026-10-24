import Foundation

func nativeEndpoint(_ address: String) -> URL? {
    guard let url = URL(string: address), url.scheme == "ws", url.host == "127.0.0.1",
          url.user == nil, url.password == nil,
          URLComponents(url: url, resolvingAgainstBaseURL: false)?.percentEncodedPath == "/xiaozhi/v1/",
          url.query == nil, url.fragment == nil else { return nil }
    return url
}
