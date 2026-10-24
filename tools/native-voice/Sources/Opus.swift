import Foundation
final class OpusEncoder {
    let handle: UnsafeMutableRawPointer
    let rate: Int
    init(rate: Int) throws {
        guard let handle = nv_encoder_create(Int32(rate)) else { throw ClientFailure("opus_encoder_create") }
        self.handle = handle; self.rate = rate
    }
    deinit { nv_encoder_destroy(handle) }
    var lookahead: Int { Int(nv_encoder_lookahead(handle)) }
    func encode(_ pcm: [Int16]) throws -> Data {
        var packet = [UInt8](repeating: 0, count: 4000)
        let result = nv_encode(handle, pcm, Int32(pcm.count), &packet, Int32(packet.count))
        guard result > 0 else { throw ClientFailure("opus_encode") }
        return Data(packet.prefix(Int(result)))
    }
}
final class OpusDecoder {
    let handle: UnsafeMutableRawPointer
    init(rate: Int) throws {
        guard let handle = nv_decoder_create(Int32(rate)) else { throw ClientFailure("opus_decoder_create") }
        self.handle = handle
    }
    deinit { nv_decoder_destroy(handle) }
    func decode(_ packet: Data) throws -> [Int16] {
        guard !packet.isEmpty, packet.count <= 4000 else { throw ClientFailure("opus_packet_size") }
        var samples = [Int16](repeating: 0, count: 2880)
        let result = packet.withUnsafeBytes { raw in
            nv_decode(handle, raw.bindMemory(to: UInt8.self).baseAddress, Int32(packet.count), &samples, Int32(samples.count))
        }
        guard result == 1440 else { throw ClientFailure("opus_downlink_duration") }
        return Array(samples.prefix(Int(result)))
    }
}
