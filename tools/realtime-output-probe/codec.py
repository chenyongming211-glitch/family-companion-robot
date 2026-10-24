"""Same 24k/60ms Opus packet domain as the Mac; decode reference for ASR."""
import ctypes as c


def opus_roundtrip(pcm):
    lib = c.CDLL('libopus.so.0')
    lib.opus_encoder_create.argtypes = [c.c_int, c.c_int, c.c_int, c.POINTER(c.c_int)]
    lib.opus_encoder_create.restype = c.c_void_p
    lib.opus_decoder_create.argtypes = [c.c_int, c.c_int, c.POINTER(c.c_int)]
    lib.opus_decoder_create.restype = c.c_void_p
    lib.opus_encode.argtypes = [c.c_void_p, c.POINTER(c.c_int16), c.c_int, c.POINTER(c.c_ubyte), c.c_int]
    lib.opus_decode.argtypes = [c.c_void_p, c.POINTER(c.c_ubyte), c.c_int, c.POINTER(c.c_int16), c.c_int, c.c_int]
    lib.opus_encoder_destroy.argtypes = [c.c_void_p]
    lib.opus_decoder_destroy.argtypes = [c.c_void_p]
    error = c.c_int()
    encoder = lib.opus_encoder_create(24000, 1, 2049, c.byref(error))
    decoder = lib.opus_decoder_create(24000, 1, c.byref(error))
    if not encoder or not decoder or error.value:
        raise ValueError('opus_creation_failed')
    try:
        lookahead = c.c_int()
        if lib.opus_encoder_ctl(c.c_void_p(encoder), c.c_int(4027), c.byref(lookahead)):
            raise ValueError('opus_lookahead_failed')
        frames = (len(pcm) // 2 + lookahead.value + 1439) // 1440
        pcm = pcm + b'\0' * (frames * 2880 - len(pcm))
        packets, decoded = [], bytearray()
        for offset in range(0, len(pcm), 2880):
            source = (c.c_int16 * 1440).from_buffer_copy(pcm[offset:offset + 2880])
            packet = (c.c_ubyte * 4000)()
            size = lib.opus_encode(encoder, source, 1440, packet, 4000)
            if size <= 0:
                raise ValueError('opus_encode_failed')
            out = (c.c_int16 * 1440)()
            if lib.opus_decode(decoder, packet, size, out, 1440, 0) != 1440:
                raise ValueError('opus_decode_failed')
            packets.append(bytes(packet[:size]))
            decoded.extend(bytes(out))
        return packets, bytes(decoded)
    finally:
        lib.opus_encoder_destroy(encoder)
        lib.opus_decoder_destroy(decoder)
