#!/usr/bin/env python3
"""C3-only cue inventory, generated-output and original assets contract.

The source hashes and raw packet counts below are locked upstream inputs, not a
host-decoder PCM hash. Decoder output is bound by the pack, manifest and header.
"""
import argparse
import hashlib
import json
from pathlib import Path
import struct
import zlib

import family_c3_pcm_cues as cues

PREFIX_BYTES = 269881
PREFIX_SHA256 = '56295f2ecb92f21bdb737a462807e9d2acc7781a606d6a90628f71c65d28a604'
PACK_OFFSET = 0x650000
PACK_LIMIT = 0x1b0000
# name, exact upstream Ogg SHA256, untrimmed 60 ms packet count
INVENTORY = (
 ('common/exclamation.ogg', '61d0a91d7658e62c2ee0a940bd8b99e2062c4b760848f0ba2d793c852def4716', 15),
 ('common/low_battery.ogg', 'a0d9eebf8b55db5f3da9b325f76eee5db9123e4215d134804204fc56eb3191d1', 18),
 ('common/popup.ogg', 'f9806e04bbbc7b496f53632ea00610ec239fec02ea8ab41489782f079a3fc19b', 9),
 ('common/success.ogg', 'c6bcb6322575df4ddaaa266c388d287338e8f857e85d4b5e3d90e8ee6ea2d678', 18),
 ('common/vibration.ogg', '576747b283d4fcfab690435d7abcb92e549dfdf7dfcea82dc39fa686d983f8d5', 14),
 ('locales/zh-CN/0.ogg', 'cadc88f48d9ef7bc761880e3d8da9c5c8cc54cfa9488e9b89dd4f3c0dddc7ab1', 17),
 ('locales/zh-CN/1.ogg', '4f47d7f6543dfd3805bb1a6f93ae5edddc9133106257897c00b6d8afca958397', 17),
 ('locales/zh-CN/2.ogg', '1560482e66c1ddcd04de9a52c702a2dc6c31e785339a61cf59bba6984423f094', 16),
 ('locales/zh-CN/3.ogg', '80e64ec8e33756fbc88e24d65d58786e538cbc136a097d96e9d3f05fdeb2c0da', 17),
 ('locales/zh-CN/4.ogg', '4dd1001918bb7e76a74cd02c9640fd8dc68321546e5263db5fe83358991e1df5', 18),
 ('locales/zh-CN/5.ogg', '271012d6165c88010a2b203f2b3ac24fb59ddcc5e623691b5e9e11781fb1d2a5', 17),
 ('locales/zh-CN/6.ogg', '0d352e834451ee2f7936a14a38d602bba6dc6712ed12b03aa2974ca65ecdceaf', 16),
 ('locales/zh-CN/7.ogg', '6a906b484c62284ef073d13c4d2c6ab170bdc80d10a5e453a74301ad35b263fb', 19),
 ('locales/zh-CN/8.ogg', '11d91766156da1e594cc6ed416256bd800856c89a270175d1bd0e59155b9b569', 17),
 ('locales/zh-CN/9.ogg', '65dda65d4fc7c230e06ba1f1a51d149274050c8c79bde2ca30475db463b01389', 16),
 ('locales/zh-CN/activation.ogg', '4d42e1afaaec891e772a60877c0e310eecc47b33c6d58e9a0d73c760971bfe0f', 79),
 ('locales/zh-CN/err_pin.ogg', 'a408f312ba76ed50df4f3a023f0d59916a08c50ea508c6e3698abc242be755eb', 35),
 ('locales/zh-CN/err_reg.ogg', 'e4cec242920be01874542518262b65288931ce1398c7d380c78896f3abd3ea34', 63),
 ('locales/zh-CN/upgrade.ogg', '5c1d297e61c648e3ff2bc29d14014e98c8d922b608498d0c962bb7bca9e29593', 29),
 ('locales/zh-CN/welcome.ogg', '216abec67fae3895e81ea1d24e733ff9556b22ee3558fa74ed1748b3245e4096', 35),
 ('locales/zh-CN/wificonfig.ogg', '1a2143fe9d818b914a01f39365d78fbaca624aef75ff11af03cd299e9833182f', 32),
)


def sha(data):
    return hashlib.sha256(data).hexdigest()


def regular(path):
    path = Path(path)
    if path.is_symlink() or any(p.is_symlink() for p in path.parents) or not path.is_file() or not path.stat().st_size:
        raise ValueError('missing/nonregular C3 build input: '+str(path))
    return path


def check_sources(source):
    assets = Path(source)/'main/assets'
    for directory in ('common', 'locales/zh-CN'):
        actual = {p.relative_to(assets).as_posix() for p in (assets/directory).glob('*.ogg')}
        expected = {name for name, _, _ in INVENTORY if name.startswith(directory+'/')}
        if actual != expected:
            raise ValueError('C3 PCM requires the exact common/zh-CN inventory; no fallback')
    for name, expected, frames in INVENTORY:
        data = regular(assets/name).read_bytes()
        if sha(data) != expected or len(cues.ogg_packets(data))-2 != frames:
            raise ValueError('C3 PCM source SHA256/packet clock mismatch: '+name)


def expected_header(pack):
    digest = ', '.join(f'0x{b:02x}' for b in hashlib.sha256(pack).digest())
    return ('#pragma once\n#include <cstdint>\n'
        f'inline constexpr uint32_t kFamilyC3PcmPackBytes = {len(pack)}u;\n'
        f'inline constexpr uint8_t kFamilyC3PcmPackSha256[32] = {{{digest}}};\n')


def verify_cues(source, output):
    check_sources(source)
    out = Path(output)
    pack = regular(out/'family_c3_pcm_cues.pack').read_bytes()
    manifest = json.loads(regular(out/'family_c3_pcm_cues.json').read_text())
    header = regular(out/'family_c3_pcm_cues_generated.h').read_text()
    if (len(pack) != 1490032 or len(pack) > PACK_LIMIT or PACK_OFFSET+len(pack) >= 0x800000
            or pack[:8] != b'FPCM24V1' or struct.unpack_from('<6I', pack, 8) != (len(pack), 21, 64, 24000, 1, 16)
            or pack[32:64] != hashlib.sha256(pack[64:]).digest()):
        raise ValueError('C3 PCM pack header/body/size mismatch')
    entries, offset = [], 1072
    for index, (name, source_sha, frames) in enumerate(INVENTORY):
        record = pack[64+index*48:112+index*48]
        location, samples, length, crc = struct.unpack_from('<4I', record, 32)
        if (record[:32].hex() != source_sha or location != offset or samples != frames*1440
                or length != samples*2 or location+length > len(pack)):
            raise ValueError('C3 PCM source/table/packet clock/overlap mismatch')
        pcm = pack[location:location+length]
        if zlib.crc32(pcm) != crc:
            raise ValueError('C3 PCM cue CRC mismatch')
        entries.append(dict(name=name, source_sha256=source_sha, pcm_sha256=sha(pcm), samples=samples,
            pcm_bytes=length, pack_offset=location, pack_crc32=f'{crc:08x}', media_packets=frames))
        offset += length
    expected = dict(format='FPCM24V1', sample_rate=24000, channels=1, bits=16,
        decoder_version='libopus 1.4', clock='raw opus packets; pre-skip and granule ignored',
        count=21, pcm_bytes=1488960, samples=744480, pack_bytes=len(pack), pack_sha256=sha(pack),
        body_sha256=sha(pack[64:]), trailer_assets_offset=0x50000,
        trailer_flash_offset=PACK_OFFSET, cues=entries)
    if offset != len(pack) or manifest != expected:
        raise ValueError('C3 PCM manifest/hash/clock mismatch')
    if header != expected_header(pack):
        raise ValueError('C3 PCM generated header stale/mismatched')
    return manifest


def verify_prefix(path):
    data = regular(path).read_bytes()
    if len(data) > 0x50000 or len(data) != PREFIX_BYTES or sha(data) != PREFIX_SHA256:
        raise ValueError('C3 original assets prefix changed or exceeds reserved prefix')
    return dict(bytes=len(data), sha256=sha(data), limit=0x50000)


def generate(source, output):
    check_sources(source)
    output = Path(output)
    for name in ('family_c3_pcm_cues.pack', 'family_c3_pcm_cues.json', 'family_c3_pcm_cues_generated.h'):
        if (output/name).is_symlink() or any(p.is_symlink() for p in (output/name).parents):
            raise ValueError('symbolic C3 output refused')
    cues.convert(source, output)
    return verify_cues(source, output)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source-root', type=Path, required=True)
    parser.add_argument('--output-dir', type=Path)
    parser.add_argument('--check-sources', action='store_true')
    args = parser.parse_args()
    try:
        if args.check_sources:
            check_sources(args.source_root)
        elif args.output_dir:
            manifest = generate(args.source_root, args.output_dir)
            print(json.dumps({key: manifest[key] for key in
                ('count', 'samples', 'pcm_bytes', 'pack_bytes', 'pack_sha256', 'decoder_version')}, sort_keys=True))
        else:
            parser.error('output directory required')
    except (ValueError, OSError) as error:
        parser.exit(1, f'{error}\n')


if __name__ == '__main__':
    main()
