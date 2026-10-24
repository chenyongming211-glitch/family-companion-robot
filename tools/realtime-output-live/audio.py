"""Persistent Opus state; ASR decodes exactly the packets sent to the Mac."""
import ctypes as c


class Opus:
    def __init__(self, rate, frame):
        self.rate, self.frame = rate, frame
        lib = self.lib = c.CDLL('libopus.so.0')
        lib.opus_encoder_create.argtypes = [c.c_int, c.c_int, c.c_int, c.POINTER(c.c_int)]
        lib.opus_encoder_create.restype = c.c_void_p
        lib.opus_decoder_create.argtypes = [c.c_int, c.c_int, c.POINTER(c.c_int)]
        lib.opus_decoder_create.restype = c.c_void_p
        lib.opus_encode.argtypes = [c.c_void_p, c.POINTER(c.c_int16), c.c_int, c.POINTER(c.c_ubyte), c.c_int]
        lib.opus_decode.argtypes = [c.c_void_p, c.POINTER(c.c_ubyte), c.c_int, c.POINTER(c.c_int16), c.c_int, c.c_int]
        lib.opus_encoder_destroy.argtypes = [c.c_void_p]
        lib.opus_decoder_destroy.argtypes = [c.c_void_p]
        error = c.c_int()
        self.encoder = lib.opus_encoder_create(rate, 1, 2049, c.byref(error))
        if not self.encoder or error.value:
            raise ValueError('opus_encoder_create')
        self.decoder = lib.opus_decoder_create(rate, 1, c.byref(error))
        if not self.decoder or error.value:
            lib.opus_encoder_destroy(self.encoder)
            self.encoder = None
            raise ValueError('opus_decoder_create')

    def encode(self, pcm):
        if len(pcm) != self.frame * 2:
            raise ValueError('opus_input_size')
        source = (c.c_int16 * self.frame).from_buffer_copy(pcm)
        target = (c.c_ubyte * 4000)()
        size = self.lib.opus_encode(self.encoder, source, self.frame, target, 4000)
        if size <= 0:
            raise ValueError('opus_encode')
        return bytes(target[:size])

    def decode(self, packet):
        if not 0 < len(packet) <= 4000:
            raise ValueError('opus_packet_size')
        source = (c.c_ubyte * len(packet)).from_buffer_copy(packet)
        target = (c.c_int16 * self.frame)()
        size = self.lib.opus_decode(self.decoder, source, len(packet), target, self.frame, 0)
        if size != self.frame:
            raise ValueError('opus_frame_size')
        return bytes(target)

    def close(self):
        if self.encoder:
            self.lib.opus_encoder_destroy(self.encoder)
            self.encoder = None
        if self.decoder:
            self.lib.opus_decoder_destroy(self.decoder)
            self.decoder = None
