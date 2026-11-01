#!/usr/bin/env python3
"""Build C3 cues using the firmware demuxer's untrimmed packet clock.

Self-contained when copied into a prepared tree's scripts directory. Run only
on the cloud build host with libopus 1.4. Output PCM, pack, manifest and header
are generated build artifacts and must stay outside Git. No ffmpeg or SDK.
"""
import argparse
import ctypes
import ctypes.util
import hashlib
import json
from pathlib import Path
import struct
import zlib

FRAME_SAMPLES = 1440
MAX_PACK_BYTES = 0x200000 - 0x50000
COMMON = ('exclamation', 'low_battery', 'popup', 'success', 'vibration')
LOCAL = tuple(str(i) for i in range(10)) + ('activation', 'err_pin', 'err_reg', 'upgrade', 'welcome', 'wificonfig')


def _ogg_crc(data):
    crc = 0
    for byte in data:
        crc ^= byte << 24
        for _ in range(8):
            crc = ((crc << 1) ^ (0x04c11db7 if crc & 0x80000000 else 0)) & 0xffffffff
    return crc


def ogg_packets(data):
    """Validate one complete logical Ogg stream without trimming its granules."""
    offset, serial, sequence, eos = 0, None, 0, False
    pending = bytearray()
    packets = []
    while offset < len(data):
        if eos:
            raise ValueError('Ogg bytes after EOS')
        if len(data) - offset < 27:
            raise ValueError('truncated Ogg page header')
        if data[offset:offset + 4] != b'OggS':
            raise ValueError('invalid Ogg capture pattern')
        if data[offset + 4] != 0 or data[offset + 5] & ~7:
            raise ValueError('unsupported Ogg page version/flags')
        flags = data[offset + 5]
        page_serial, page_sequence, checksum = struct.unpack_from('<III', data, offset + 14)
        segments = data[offset + 26]
        header_end = offset + 27 + segments
        if header_end > len(data):
            raise ValueError('truncated Ogg lacing table')
        laces = data[offset + 27:header_end]
        page_end = header_end + sum(laces)
        if page_end > len(data):
            raise ValueError('truncated Ogg page body')
        page_bytes = bytearray(data[offset:page_end])
        page_bytes[22:26] = bytes(4)
        if _ogg_crc(page_bytes) != checksum:
            raise ValueError('Ogg CRC mismatch')
        if serial is None:
            if not flags & 2 or flags & 1:
                raise ValueError('missing Ogg BOS')
            serial = page_serial
        elif flags & 2:
            raise ValueError('unexpected Ogg BOS')
        if page_serial != serial:
            raise ValueError('Ogg serial changed')
        if page_sequence != sequence or sequence > 0xffffffff:
            raise ValueError('Ogg sequence gap or wrap')
        sequence += 1
        if bool(flags & 1) != bool(pending):
            raise ValueError('Ogg continuation mismatch')
        cursor = header_end
        for length in laces:
            pending += data[cursor:cursor + length]
            cursor += length
            if length < 255:
                packets.append(bytes(pending))
                pending.clear()
        eos = bool(flags & 4)
        if eos and pending:
            raise ValueError('unfinished packet at Ogg EOS')
        offset = page_end
    if not eos:
        raise ValueError('missing Ogg EOS')
    if pending:
        raise ValueError('unfinished Ogg packet')
    return packets


class Opus24Decoder:
    """One fresh mono24k Opus decoder per cue, with a pinned decoder version."""
    def __init__(self):
        library = ctypes.util.find_library('opus')
        if not library:
            raise ValueError('libopus 1.4 is required')
        self.lib = ctypes.CDLL(library)
        self.lib.opus_get_version_string.restype = ctypes.c_char_p
        self.version = self.lib.opus_get_version_string().decode('ascii')
        if self.version != 'libopus 1.4':
            raise ValueError(f'expected libopus 1.4, got {self.version!r}')
        self.lib.opus_decoder_create.argtypes = [ctypes.c_int, ctypes.c_int, ctypes.POINTER(ctypes.c_int)]
        self.lib.opus_decoder_create.restype = ctypes.c_void_p
        self.lib.opus_decoder_destroy.argtypes = [ctypes.c_void_p]
        self.lib.opus_decode.argtypes = [ctypes.c_void_p, ctypes.POINTER(ctypes.c_ubyte), ctypes.c_int,
                                       ctypes.POINTER(ctypes.c_int16), ctypes.c_int, ctypes.c_int]
        self.lib.opus_packet_get_nb_samples.argtypes = [ctypes.POINTER(ctypes.c_ubyte), ctypes.c_int, ctypes.c_int]
        self.handle = None

    def close(self):
        if self.handle:
            self.lib.opus_decoder_destroy(self.handle)
            self.handle = None

    def begin(self):
        self.close()
        error = ctypes.c_int()
        self.handle = self.lib.opus_decoder_create(24000, 1, ctypes.byref(error))
        if error.value or not self.handle:
            self.close()
            raise ValueError(f'opus_decoder_create failed: {error.value}')

    def packet(self, packet):
        if not packet or len(packet) > 0x7fffffff or not self.handle:
            raise ValueError('invalid Opus media packet')
        data = (ctypes.c_ubyte * len(packet)).from_buffer_copy(packet)
        samples = self.lib.opus_packet_get_nb_samples(data, len(packet), 24000)
        if samples != FRAME_SAMPLES:
            raise ValueError(f'Opus media clock must be 1440 samples, got {samples}')
        pcm = (ctypes.c_int16 * FRAME_SAMPLES)()
        decoded = self.lib.opus_decode(self.handle, data, len(packet), pcm, FRAME_SAMPLES, 0)
        if decoded != FRAME_SAMPLES:
            raise ValueError(f'Opus decode must emit 1440 samples, got {decoded}')
        return struct.pack('<1440h', *pcm)


def _validate_tags(packet):
    if len(packet) < 16 or packet[:8] != b'OpusTags':
        raise ValueError('invalid OpusTags header')
    vendor_length = struct.unpack_from('<I', packet, 8)[0]
    offset = 12 + vendor_length
    if offset + 4 > len(packet):
        raise ValueError('truncated OpusTags vendor')
    comments = struct.unpack_from('<I', packet, offset)[0]
    offset += 4
    if comments > (len(packet) - offset) // 4:
        raise ValueError('truncated OpusTags comment table')
    for _ in range(comments):
        if offset + 4 > len(packet):
            raise ValueError('truncated OpusTags comment length')
        length = struct.unpack_from('<I', packet, offset)[0]
        offset += 4
        if length > len(packet) - offset:
            raise ValueError('truncated OpusTags comment')
        offset += length


def decode_ogg(data, decoder):
    packets = ogg_packets(data)
    if len(packets) < 3:
        raise ValueError('Opus stream requires headers and media')
    head = packets[0]
    if (len(head) != 19 or head[:8] != b'OpusHead' or head[8] != 1 or head[9] != 1
            or struct.unpack_from('<I', head, 12)[0] != 16000 or head[18] != 0
            or struct.unpack_from('<h', head, 16)[0] != 0):
        raise ValueError('OpusHead must be version1 mono input16k mapping0 gain0')
    _validate_tags(packets[1])
    decoder.begin()
    try:
        pcm = b''.join(decoder.packet(packet) for packet in packets[2:])
    finally:
        decoder.close()
    return pcm, len(packets) - 2


def make_pack(entries, version):
    if not entries or len(entries) > 32:
        raise ValueError('cue count must be 1..32')
    data_start = (64 + 48 * len(entries) + 15) & ~15
    table, payload, manifest_cues, seen = bytearray(), bytearray(), [], set()
    for entry in entries:
        source, pcm = entry['source'], entry['pcm']
        source_sha = hashlib.sha256(source).digest()
        if source_sha in seen:
            raise ValueError('duplicate cue source SHA256')
        seen.add(source_sha)
        if not pcm or len(pcm) % (FRAME_SAMPLES * 2):
            raise ValueError('PCM cue must contain whole nonempty 1440-sample frames')
        offset = data_start + len(payload)
        if offset + len(pcm) > MAX_PACK_BYTES:
            raise ValueError('PCM pack exceeds trailer capacity')
        table += source_sha + struct.pack('<IIII', offset, len(pcm) // 2, len(pcm), zlib.crc32(pcm))
        payload += pcm
        manifest_cues.append({'name': entry['name'], 'source_sha256': source_sha.hex(),
            'pcm_sha256': hashlib.sha256(pcm).hexdigest(), 'samples': len(pcm) // 2,
            'pcm_bytes': len(pcm), 'pack_offset': offset, 'pack_crc32': f'{zlib.crc32(pcm):08x}',
            'media_packets': len(pcm) // (FRAME_SAMPLES * 2)})
    body = bytes(table) + bytes(data_start - 64 - len(table)) + bytes(payload)
    pack = b'FPCM24V1' + struct.pack('<6I', 64 + len(body), len(entries), 64, 24000, 1, 16) + hashlib.sha256(body).digest() + body
    manifest = {'format': 'FPCM24V1', 'sample_rate': 24000, 'channels': 1, 'bits': 16,
        'decoder_version': version, 'clock': 'raw opus packets; pre-skip and granule ignored',
        'count': len(entries), 'pcm_bytes': len(payload), 'samples': len(payload) // 2,
        'pack_bytes': len(pack), 'pack_sha256': hashlib.sha256(pack).hexdigest(),
        'body_sha256': hashlib.sha256(body).hexdigest(), 'trailer_assets_offset': 0x50000,
        'trailer_flash_offset': 0x650000, 'cues': manifest_cues}
    return pack, manifest


def convert(source, output):
    source, output = Path(source), Path(output)
    assets = source / 'main/assets'
    entries, decoder, used_fallback = [], Opus24Decoder(), False
    expected_common = {f'{name}.ogg' for name in COMMON}
    expected_local = {f'{name}.ogg' for name in LOCAL}
    if {path.name for path in (assets / 'common').glob('*.ogg')} != expected_common:
        raise ValueError('locked common cue set changed')
    if {path.name for path in (assets / 'locales/zh-CN').glob('*.ogg')} - expected_local:
        raise ValueError('unexpected zh-CN cue source')
    for group, names in [('common', COMMON), ('locales/zh-CN', LOCAL)]:
        for name in names:
            path = assets / group / f'{name}.ogg'
            if not path.is_file() and group == 'locales/zh-CN':
                path = assets / 'locales/en-US' / f'{name}.ogg'
                used_fallback = True
            data = path.read_bytes()
            pcm, _ = decode_ogg(data, decoder)
            entries.append({'name': path.relative_to(assets).as_posix(), 'source': data, 'pcm': pcm})
    pack, manifest = make_pack(entries, decoder.version)
    if manifest['count'] != 21 or (not used_fallback and manifest['pcm_bytes'] != 1488960):
        raise ValueError('locked 21-cue raw clock changed; expected 1488960 PCM bytes')
    output.mkdir(parents=True, exist_ok=True)
    (output / 'family_c3_pcm_cues.pack').write_bytes(pack)
    (output / 'family_c3_pcm_cues.json').write_text(json.dumps(manifest, indent=2, ensure_ascii=False) + '\n')
    digest = ', '.join(f'0x{byte:02x}' for byte in hashlib.sha256(pack).digest())
    header = ('#pragma once\n#include <cstdint>\n'
              f'inline constexpr uint32_t kFamilyC3PcmPackBytes = {len(pack)}u;\n'
              f'inline constexpr uint8_t kFamilyC3PcmPackSha256[32] = {{{digest}}};\n')
    (output / 'family_c3_pcm_cues_generated.h').write_text(header)
    return manifest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source-root', type=Path, required=True, help='locked prepared firmware tree')
    parser.add_argument('--output-dir', type=Path, required=True, help='generated artifacts directory outside Git')
    args = parser.parse_args()
    try:
        manifest = convert(args.source_root, args.output_dir)
    except (ValueError, OSError) as error:
        parser.exit(1, f'{error}\n')
    print(json.dumps({key: manifest[key] for key in ('count', 'samples', 'pcm_bytes', 'pack_bytes', 'pack_sha256', 'decoder_version')}))


if __name__ == '__main__':
    main()
