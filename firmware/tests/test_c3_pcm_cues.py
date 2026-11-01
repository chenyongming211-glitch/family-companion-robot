"""Cue format, corruption rejection, raw Ogg clocks, and optional locked assets."""
import ctypes
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import struct
import subprocess
import tempfile
import unittest
from unittest import mock
import zlib

FIRMWARE = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location('family_c3_pcm_cues', FIRMWARE / 'overlays/scripts/family_c3_pcm_cues.py')
cues = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(cues)


def ogg_crc(data):
    value = 0
    for byte in data:
        value ^= byte << 24
        for _ in range(8):
            value = ((value << 1) ^ (0x04c11db7 if value & 0x80000000 else 0)) & 0xffffffff
    return value


def page(payload, seq=0, flags=2, laces=None, serial=42):
    laces = [len(payload)] if laces is None else laces
    header = struct.pack('<4sBBQIIIB', b'OggS', 0, flags, 0, serial, seq, 0, len(laces))
    result = bytearray(header + bytes(laces) + payload)
    struct.pack_into('<I', result, 22, ogg_crc(result))
    return bytes(result)


HEAD = struct.pack('<8sBBHIhB', b'OpusHead', 1, 1, 312, 16000, 0, 0)
TAGS = b'OpusTags' + struct.pack('<I', 0) + struct.pack('<I', 0)


def fixture_pack():
    pcm = [struct.pack('<1440h', *(i - 720 for i in range(1440))), struct.pack('<2880h', *([-1234] * 2880))]
    payload = bytearray()
    entries = bytearray()
    offset = 160
    for i, data in enumerate(pcm):
        entries += hashlib.sha256(bytes([i + 1])).digest() + struct.pack('<IIII', offset, len(data) // 2, len(data), zlib.crc32(data))
        payload += data
        offset += len(data)
    body = entries + payload
    return b'FPCM24V1' + struct.pack('<6I', 64 + len(body), 2, 64, 24000, 1, 16) + hashlib.sha256(body).digest() + body


class C3PcmCuePackTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory()
        cls.addClassCleanup(cls.temp.cleanup)
        cls.fixture = Path(cls.temp.name) / 'fixture.pack'
        cls.fixture.write_bytes(fixture_pack())
        cls.binaries = {}
        for mode, flags in [('optimized', ['-O2']), ('sanitized', ['-O1', '-g',
                '-fsanitize=address,undefined', '-fno-sanitize-recover=all'])]:
            binary = Path(cls.temp.name) / mode
            result = subprocess.run(['c++', '-std=c++17', '-Wall', '-Wextra', '-Werror',
                '-pedantic', *flags, '-I', str(FIRMWARE / 'overlays/main/audio'),
                str(FIRMWARE / 'tests/c3_pcm_cues_host.cc'), '-lcrypto', '-o', str(binary)],
                capture_output=True, text=True)
            if result.returncode: raise AssertionError(result.stderr)
            cls.binaries[mode] = binary

    def run_case(self, case):
        for mode, binary in self.binaries.items():
            with self.subTest(mode=mode, case=case):
                env = dict(os.environ, ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                           UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
                if mode == 'sanitized' and os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                    env['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
                result = subprocess.run([str(binary), str(self.fixture), case], env=env,
                                        capture_output=True, text=True, timeout=10)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_odd_address_streaming_lookup_signed_samples_and_no_allocation(self): self.run_case('valid')
    def test_hash_callback_and_app_digest_are_mandatory(self):
        for case in ['digest', 'null-hash', 'body']: self.run_case(case)
    def test_malformed_headers_are_rejected(self):
        for case in ['magic', 'total', 'count', 'table', 'rate', 'channels', 'bits']: self.run_case(case)
    def test_table_bounds_duplicates_overflow_and_overlap_are_rejected(self):
        for case in ['duplicate', 'alignment', 'table-overlap', 'data-overlap', 'range', 'samples', 'clock', 'bytes', 'crc']: self.run_case(case)


class C3PcmOggTests(unittest.TestCase):
    def test_valid_multi_page_continuation_preserves_raw_packets(self):
        packet = bytes(range(256)) + b'z'
        data = page(HEAD) + page(TAGS, 1, 0) + page(packet[:255], 2, 0, [255]) + page(packet[255:], 3, 5)
        self.assertEqual(cues.ogg_packets(data), [HEAD, TAGS, packet])

    def test_malformed_page_fixtures(self):
        cases = [
            (b'OggS', 'truncated'), (page(HEAD)[:-1], 'truncated'),
            (b'Bad!' + page(HEAD)[4:], 'capture'),
            (page(HEAD)[:-1] + b'x', 'CRC'),
            (page(HEAD, flags=10), 'version/flags'),
            (page(b'')[:26] + b'\xff', 'truncated'),
            (page(HEAD) + page(TAGS, 2, 4), 'sequence'),
            (page(HEAD) + page(TAGS, 1, 4, serial=43), 'serial'),
            (page(HEAD) + page(TAGS, 1, 5), 'continuation'),
            (page(HEAD) + page(bytes(255), 1, 0, [255]) + page(b'x', 2, 4), 'continuation'),
            (page(HEAD) + page(TAGS, 1, 4) + page(b'x', 2, 4), 'EOS'),
            (page(HEAD) + page(TAGS, 1, 0), 'EOS'),
            (page(HEAD, flags=0) + page(TAGS, 1, 4), 'BOS'),
            (page(HEAD) + page(bytes(255), 1, 4, [255]), 'unfinished'),
        ]
        for data, reason in cases:
            with self.subTest(reason=reason):
                with self.assertRaisesRegex(ValueError, reason): cues.ogg_packets(data)

    def test_raw_decoder_ignores_preskip_and_granule_and_validates_opus_headers(self):
        decoder = cues.Opus24Decoder()
        lib = decoder.lib
        lib.opus_encoder_create.argtypes = [ctypes.c_int, ctypes.c_int, ctypes.c_int, ctypes.POINTER(ctypes.c_int)]
        lib.opus_encoder_create.restype = ctypes.c_void_p
        lib.opus_encode.argtypes = [ctypes.c_void_p, ctypes.POINTER(ctypes.c_int16), ctypes.c_int, ctypes.POINTER(ctypes.c_ubyte), ctypes.c_int]
        lib.opus_encoder_destroy.argtypes = [ctypes.c_void_p]
        error = ctypes.c_int()
        encoder = lib.opus_encoder_create(16000, 1, 2049, ctypes.byref(error))
        self.assertEqual(error.value, 0)
        samples = (ctypes.c_int16 * 960)()
        packet = (ctypes.c_ubyte * 4000)()
        try: size = lib.opus_encode(encoder, samples, 960, packet, 4000)
        finally: lib.opus_encoder_destroy(encoder)
        self.assertGreater(size, 0)
        media = bytes(packet[:size])
        def stream(head=HEAD, tags=TAGS, audio=media):
            return page(head) + page(tags, 1, 0) + page(audio, 2, 4)
        pcm, count = cues.decode_ogg(stream(), decoder)
        self.assertEqual((len(pcm), count), (2880, 1))
        altered = bytearray(HEAD)
        struct.pack_into('<H', altered, 10, 65535)
        self.assertEqual(cues.decode_ogg(stream(head=bytes(altered)), decoder), (pcm, 1))
        altered_granule = bytearray(page(media, 2, 4))
        struct.pack_into('<Q', altered_granule, 6, 1)
        struct.pack_into('<I', altered_granule, 22, 0)
        struct.pack_into('<I', altered_granule, 22, ogg_crc(altered_granule))
        self.assertEqual(cues.decode_ogg(page(HEAD) + page(TAGS, 1, 0) + altered_granule, decoder), (pcm, 1))
        invalid_heads = [HEAD[:9] + b'\x02' + HEAD[10:], HEAD[:12] + struct.pack('<I', 48000) + HEAD[16:], HEAD[:-1] + b'\x01', HEAD[:8] + b'\x02' + HEAD[9:]]
        for head in invalid_heads:
            with self.assertRaisesRegex(ValueError, 'OpusHead'): cues.decode_ogg(stream(head=head), decoder)
        for tags in [b'OpusTags', b'OpusTags' + struct.pack('<II', 100, 0), b'OpusTags' + struct.pack('<II', 0, 1),
                     b'OpusTags' + struct.pack('<III', 0, 2, 4) + bytes(4)]:
            with self.assertRaisesRegex(ValueError, 'OpusTags'): cues.decode_ogg(stream(tags=tags), decoder)
        with self.assertRaisesRegex(ValueError, '1440'): cues.decode_ogg(stream(audio=b'\xf8\xff\xfe'), decoder)
        decoder.close()

    def test_writer_layout_and_manifest_hashes(self):
        source = b'one'
        pcm = bytes(2880)
        pack, manifest = cues.make_pack([{'name': 'one.ogg', 'source': source, 'pcm': pcm}], 'libopus 1.4')
        self.assertEqual(pack[:8], b'FPCM24V1')
        self.assertEqual(struct.unpack_from('<6I', pack, 8), (2992, 1, 64, 24000, 1, 16))
        self.assertEqual(pack[32:64], hashlib.sha256(pack[64:]).digest())
        self.assertEqual(struct.unpack_from('<IIII', pack, 96), (112, 1440, 2880, zlib.crc32(pcm)))
        self.assertEqual(manifest['pack_sha256'], hashlib.sha256(pack).hexdigest())
        self.assertEqual(manifest['cues'][0]['source_sha256'], hashlib.sha256(source).hexdigest())
        self.assertEqual(manifest['cues'][0]['pcm_sha256'], hashlib.sha256(pcm).hexdigest())
        for entries in [[], [{'name': 'bad', 'source': source, 'pcm': bytes(2)}], [{'name': 'huge', 'source': source, 'pcm': bytes(2880 * 615)}], [{'name': str(i), 'source': source, 'pcm': pcm} for i in range(2)]]:
            with self.assertRaises(ValueError): cues.make_pack(entries, 'libopus 1.4')

    def test_locale_fallback_occurs_only_for_missing_zh_source(self):
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / 'source'
            assets = source / 'main/assets'
            for group, names in [('common', cues.COMMON), ('locales/zh-CN', cues.LOCAL), ('locales/en-US', cues.LOCAL)]:
                (assets / group).mkdir(parents=True)
                for name in names:
                    (assets / group / f'{name}.ogg').write_bytes(f'{group}/{name}'.encode())
            (assets / 'locales/zh-CN/0.ogg').unlink()
            seen = []
            def decode(data, _):
                seen.append(data)
                return bytes(2880), 1
            with mock.patch.object(cues, 'decode_ogg', side_effect=decode):
                manifest = cues.convert(source, Path(directory) / 'out')
            self.assertEqual(manifest['count'], 21)
            self.assertIn(b'locales/en-US/0', seen)
            self.assertNotIn(b'locales/en-US/1', seen)
            self.assertIn(b'locales/zh-CN/1', seen)

    @unittest.skipUnless(os.environ.get('C3_PCM_CUE_ASSETS'), 'set C3_PCM_CUE_ASSETS for locked 21-cue validation')
    def test_all_locked_cues_have_unchanged_packet_clocks_and_reproducible_outputs(self):
        with tempfile.TemporaryDirectory() as directory:
            out = Path(directory)
            manifest = cues.convert(Path(os.environ['C3_PCM_CUE_ASSETS']), out)
            self.assertEqual(manifest['count'], 21)
            self.assertEqual(manifest['pcm_bytes'], 1488960)
            self.assertEqual(manifest['samples'], 744480)
            self.assertEqual(manifest['decoder_version'], 'libopus 1.4')
            self.assertEqual(json.loads((out / 'family_c3_pcm_cues.json').read_text()), manifest)
            first = (out / 'family_c3_pcm_cues.pack').read_bytes()
            self.assertLessEqual(len(first), 1769472)
            cues.convert(Path(os.environ['C3_PCM_CUE_ASSETS']), out)
            self.assertEqual((out / 'family_c3_pcm_cues.pack').read_bytes(), first)
            header = (out / 'family_c3_pcm_cues_generated.h').read_text()
            self.assertIn('kFamilyC3PcmPackSha256', header)
            for cue in manifest['cues']:
                self.assertEqual(cue['samples'] % 1440, 0)
            # Compile the real generated app digest, then validate and stream
            # all 517 media frames from an odd-address mapping in both modes.
            for mode, flags in [('optimized', ['-O2']), ('sanitized', ['-O1', '-g',
                    '-fsanitize=address,undefined', '-fno-sanitize-recover=all'])]:
                binary = out / f'real-{mode}'
                result = subprocess.run(['c++', '-std=c++17', '-Wall', '-Wextra', '-Werror',
                    '-pedantic', *flags, '-I', str(FIRMWARE / 'overlays/main/audio'),
                    f'-DFAMILY_C3_PCM_GENERATED_HEADER="{out / "family_c3_pcm_cues_generated.h"}"',
                    str(FIRMWARE / 'tests/c3_pcm_cues_host.cc'), '-lcrypto', '-o', str(binary)],
                    capture_output=True, text=True)
                self.assertEqual(result.returncode, 0, result.stderr)
                env = dict(os.environ, ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                           UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
                if mode == 'sanitized' and os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                    env['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
                result = subprocess.run([str(binary), str(out / 'family_c3_pcm_cues.pack'), 'real'],
                    env=env, capture_output=True, text=True, timeout=20)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)


if __name__ == '__main__': unittest.main()
