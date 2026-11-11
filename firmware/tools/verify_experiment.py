#!/usr/bin/env python3
"""Verify and package allowlisted evidence from one isolated experimental CI job.

No device operations. Does not validate ESP image CRCs or acoustics;
those remain downloaded-artifact inspection and notified field gates.
"""
import argparse
import csv
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import struct
import subprocess
import sys
import tempfile
import tarfile
import zipfile

sys.path.insert(0, str(Path(__file__).resolve().parent))
from prepare import is_source_input
sys.path.insert(0, str(Path(__file__).resolve().parents[1]/'overlays/scripts'))
import family_c3_build as c3_build
import family_c3_network as c3_network
import family_c3_iram_audit as c3_iram

FIRMWARE = Path(__file__).resolve().parents[1]
WAVESHARE_VARIANT = 'esp32-s3-touch-amoled-2.16-xiaoman'
C3_VARIANT = 'xmini-c3-family'
VARIANTS = ('m5stack-stopwatch', 'm5stack-stopwatch-software-aec', 'm5stack-stopwatch-xiaoman', WAVESHARE_VARIANT, C3_VARIANT)
VARIANT_BOARDS = {variant: 'm5stack/stopwatch' for variant in VARIANTS[:3]}
VARIANT_BOARDS[WAVESHARE_VARIANT] = 'waveshare/esp32-s3-touch-amoled-2.16'
VARIANT_BOARDS[C3_VARIANT] = 'xmini/c3'
CANONICAL_RELEASE_NAMES = {
    'm5stack-stopwatch': 'm5stack-stopwatch',
    'm5stack-stopwatch-software-aec': 'm5stack-stopwatch-software-aec',
    'm5stack-stopwatch-xiaoman': 'm5stack-stopwatch-xiaoman',
    WAVESHARE_VARIANT: 'waveshare-'+WAVESHARE_VARIANT,
    C3_VARIANT: C3_VARIANT,
}
BUILD_FILES = ('build/xiaozhi.bin', 'build/merged-binary.bin', 'build/generated_assets.bin',
               'build/bootloader/bootloader.bin', 'build/partition_table/partition-table.bin',
               'build/flasher_args.json', 'build/flash_args', 'build/ota_data_initial.bin',
               'build/compile_commands.json')
NATIVE_NETWORK_FILES = tuple('managed_components/78__esp-ml307/'+name for name in c3_network.ORIGINAL_SHA256)
C3_BUILD_FILES = ('build/xiaozhi.elf', 'build/xiaozhi.map',
    'build/family_c3_pcm/family_c3_pcm_cues.pack', 'build/family_c3_pcm/family_c3_pcm_cues.json',
    'build/family_c3_pcm/family_c3_pcm_cues_generated.h',
    'build/family_c3_network/web_socket.cc', 'build/family_c3_network/esp_tcp.cc',
    'build/family_c3_network/family_c3_network.manifest.json',
    'build/family_c3_network/LICENSE.esp-ml307', 'build/family_c3_network/compiled-sources.txt',
    'build/family_c3_cache/iram-audit.json', 'build/family_c3_cache/link-command.txt',
    *('build/family_c3_cache/sdk/'+name for name in c3_iram.source_pins()))


def read_regular(path):
    return c3_build.regular(path).read_bytes()


def compiled_paths(path):
    commands = json.loads(read_regular(path))
    if not isinstance(commands, list):
        raise ValueError('compiled inputs are not a list')
    compiled = []
    for entry in commands:
        try:
            source = Path(entry['file'])
            source = source if source.is_absolute() else Path(entry['directory'])/source
            if not entry.get('command') and not entry.get('arguments'):
                raise ValueError('compiled input has no compiler invocation')
            compiled.append(source.resolve())
        except (KeyError, TypeError) as exc:
            raise ValueError('invalid compiled input record') from exc
    return compiled


def unique_json(data):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise ValueError('duplicate JSON key: '+key)
            result[key] = value
        return result
    return json.loads(data, object_pairs_hook=unique)


def flash_options(tokens):
    allowed = {'--flash-mode', '--flash-size', '--flash-freq'}
    result = {}
    if len(tokens) % 2:
        raise ValueError('invalid flash option/value pairs')
    for key, value in zip(tokens[::2], tokens[1::2]):
        if key not in allowed or key in result or not isinstance(value, str):
            raise ValueError('unknown/duplicate flash option')
        result[key] = value
    # All six approved builds use this profile. Fail closed rather than emulate
    # esptool changing a bootloader header/digest for an unsupported profile.
    if result != {'--flash-mode': 'dio', '--flash-size': '16MB', '--flash-freq': '80m'}:
        raise ValueError('unsupported native flash profile')
    return result


def flash_partitions(path):
    data = read_regular(path)
    if not 32 <= len(data) <= 0x1000 or len(data) % 32:
        raise ValueError('invalid native flash partition table size')
    entries = []
    for position in range(0, len(data), 32):
        record = data[position:position+32]
        if record[:2] == b'\xeb\xeb':
            if (record[2:16] != b'\xff'*14 or record[16:] != hashlib.md5(data[:position]).digest()
                    or data[position+32:] != b'\xff'*(len(data)-position-32)):
                raise ValueError('native flash partition checksum/trailer mismatch')
            break
        magic, typ, sub, offset, size, label, flags = struct.unpack('<HBBII16sI', record)
        if (magic != 0x50aa or flags or not size or offset < 0x9000 or offset+size > 0x1000000
                or offset % (0x10000 if typ == 0 else 0x1000)):
            raise ValueError('invalid native flash partition entry')
        entries.append((label.rstrip(b'\0').decode('ascii'), typ, sub, offset, size))
    else:
        raise ValueError('missing native flash partition checksum')
    ordered = sorted(entries, key=lambda entry: entry[3])
    if (not entries or len({entry[0] for entry in entries}) != len(entries)
            or any(right[3] < left[3]+left[4] for left, right in zip(ordered, ordered[1:]))):
        raise ValueError('duplicate/overlapping native flash partitions')
    return entries


def verify_flash_images(tree, c3=False, pcm=False):
    """Bind both IDF input lists and the entire native raw merged output.

    Frozen IDF fff9895/esptool raw merge uses base 0, FF gaps and no final
    padding unless an explicit merge-only option is supplied. Only its three
    native flash flags are accepted here. The approved bootloader header must
    already match them, so merge does not need to patch its header or digest.
    Verified against all five trusted 6b79e4c experiment artifact byte streams.
    """
    build = tree.resolve()/'build'
    partitions = flash_partitions(build/'partition_table/partition-table.bin')
    def one(predicate):
        entries = [entry for entry in partitions if predicate(entry)]
        if len(entries) != 1:
            raise ValueError('native flash partition selection is not unique')
        return entries[0]
    app = one(lambda entry: entry[1:3] == (0, 0x10))
    assets = one(lambda entry: entry[0] == 'assets' and entry[1:3] == (1, 0x82))
    ota = one(lambda entry: entry[1:3] == (1, 0))
    if c3 and (app[3], assets[3], assets[4], ota[3]) != (0x20000, 0x600000, 0x200000, 0xd000):
        raise ValueError('C3 canonical flash partition offsets differ')
    expected = {0: build/'bootloader/bootloader.bin', 0x8000: build/'partition_table/partition-table.bin',
                ota[3]: build/'ota_data_initial.bin', app[3]: build/'xiaozhi.bin',
                assets[3]: build/'generated_assets.bin'}
    if pcm:
        if not c3:
            raise ValueError('PCM flash evidence requires an explicit C3 experiment')
        expected[0x650000] = build/'family_c3_pcm/family_c3_pcm_cues.pack'
    metadata = unique_json(read_regular(build/'flasher_args.json'))
    options = flash_options(metadata['write_flash_args'])
    if metadata['flash_settings'] != {key[2:].replace('-', '_'): value for key, value in options.items()}:
        raise ValueError('flash JSON options/settings mismatch')
    def pairs(records):
        result = {}
        for offset, name in records:
            offset = int(offset, 0)
            if offset in result:
                raise ValueError('duplicate numeric flash offset/alias')
            result[offset] = (build/name).resolve()
        if result != expected:
            raise ValueError('flash inputs differ from canonical offset/path map')
        return result
    pairs(metadata['flash_files'].items())
    arguments = shlex.split(read_regular(build/'flash_args').decode())
    prefix = 0
    while prefix < len(arguments) and arguments[prefix].startswith('--'):
        prefix += 2
    if flash_options(arguments[:prefix]) != options or len(arguments[prefix:]) % 2:
        raise ValueError('flash_args flags differ from JSON')
    pairs(zip(arguments[prefix::2], arguments[prefix+1::2]))
    if read_regular(expected[ota[3]]) != b'\xff'*ota[4]:
        raise ValueError('native OTA initializer differs from empty partition bytes')
    boot = read_regular(expected[0])
    chip = 5 if c3 else 9
    for relative in ('bootloader/bootloader.bin', 'xiaozhi.bin'):
        image = read_regular(build/relative)
        if len(image) < 24 or image[0] != 0xe9 or struct.unpack_from('<H', image, 12)[0] != chip:
            raise ValueError('native flash image chip header mismatch')
    if boot[2:4] != b'\x02\x4f':
        raise ValueError('bootloader header would require unsupported merge patching')
    sizes = {path: len(read_regular(path)) for path in expected.values()}
    limits = {expected[0]: 0x8000, expected[0x8000]: 0x1000,
              expected[app[3]]: app[4], expected[assets[3]]: 0x50000 if pcm else assets[4]}
    if pcm: limits[expected[0x650000]] = 0x1b0000
    if any(sizes[path] > limit for path, limit in limits.items()):
        raise ValueError('canonical flash image exceeds native partition/reserved range')
    ranges = sorted((offset, offset+sizes[path], path) for offset, path in expected.items())
    if any(right[0] < left[1] for left, right in zip(ranges, ranges[1:])):
        raise ValueError('canonical flash inputs overlap')
    merged = read_regular(build/'merged-binary.bin')
    end = ranges[-1][1]
    if end > 0x1000000 or len(merged) != end:
        raise ValueError('whole merged image length differs from canonical raw merge')
    canonical = bytearray(b'\xff'*end)
    for start, stop, path in ranges:
        canonical[start:stop] = read_regular(path)
    if merged != canonical:
        raise ValueError('whole merged image bytes differ from canonical raw merge')
    return dict(images={path.relative_to(build).as_posix(): dict(offset=offset, bytes=sizes[path], sha256=digest(path))
                        for offset, path in sorted(expected.items())},
                flash_args_sha256=digest(build/'flash_args'), merged_sha256=digest(build/'merged-binary.bin'),
                merged_bytes=len(merged), base_offset=0, gap_byte=255, final_padding='none', header_changes='none')


def verify_c3_pcm(tree):
    """Explicit experimental-only validation; never selected by normal C3 CLI."""
    tree = tree.resolve()
    build, generated = tree/'build', tree/'build/family_c3_network'
    for relative in C3_BUILD_FILES:
        c3_build.regular(tree/relative)
    cache_audit=c3_iram.verify_report(tree)
    cues = c3_build.verify_cues(tree, build/'family_c3_pcm')
    prefix = c3_build.verify_prefix(build/'generated_assets.bin')
    elf = read_regular(build/'xiaozhi.elf')
    app = read_regular(build/'xiaozhi.bin')
    elf_sha = hashlib.sha256(elf).digest()
    if (elf[:4] != b'\x7fELF' or len(app) < 208 or app[32:36] != struct.pack('<I', 0xabcd5432)
            or app[176:208] != elf_sha):
        raise ValueError('C3 ELF/app descriptor SHA256 mismatch')
    component = tree/'managed_components/78__esp-ml307'
    manifest = json.loads(read_regular(generated/'family_c3_network.manifest.json'))
    for key, expected in dict(schema=1, source_uri=c3_network.SOURCE_URI,
        source_commit=c3_network.SOURCE_COMMIT, archive_sha256=c3_network.ARCHIVE_SHA256,
        component_hash=c3_network.COMPONENT_HASH, license='Apache-2.0',
        original_sha256=c3_network.ORIGINAL_SHA256).items():
        if manifest.get(key) != expected:
            raise ValueError('C3 network manifest pin mismatch: '+key)
    for name, expected in c3_network.ORIGINAL_SHA256.items():
        if hashlib.sha256(read_regular(component/name)).hexdigest() != expected:
            raise ValueError('C3 network managed original SHA256 mismatch: '+name)
    if read_regular(generated/'LICENSE.esp-ml307') != read_regular(component/'LICENSE'):
        raise ValueError('C3 network retained license mismatch')
    for name, field, directory in [('web_socket.cc', 'patched_sha256', generated),
                                  ('esp_tcp.cc', 'patched_sha256', generated),
                                  *[(name, 'helper_sha256', tree/'main/audio') for name in c3_network.HELPERS]]:
        actual = hashlib.sha256(read_regular(directory/name)).hexdigest()
        if manifest.get(field, {}).get(name) != actual:
            raise ValueError('C3 network generated/helper SHA256 mismatch: '+name)
    recipe_sha = hashlib.sha256(read_regular(tree/'scripts/family_c3_network.py')).hexdigest()
    if manifest.get('recipe_sha256') != recipe_sha:
        raise ValueError('C3 network recipe SHA256 mismatch')
    for name in ('family_c3_network.py', 'family_c3_build.py', 'family_c3_pcm_cues.py'):
        if read_regular(tree/'scripts'/name) != read_regular(FIRMWARE/'overlays/scripts'/name):
            raise ValueError('C3 prepared build recipe differs from reviewed input: '+name)
    for name in c3_network.HELPERS:
        if read_regular(tree/'main/audio'/name) != read_regular(FIRMWARE/'overlays/main/audio'/name):
            raise ValueError('C3 prepared network helper differs from reviewed input: '+name)
    # Re-run the reviewed recipes. Self-consistent manifests alone cannot prove
    # that a changed PCM payload or network source came from those recipes.
    with tempfile.TemporaryDirectory(prefix='verify-c3-recipes-') as temporary:
        expected_network = Path(temporary)/'network'
        expected_manifest = c3_network.generate(component, expected_network)
        if (manifest != expected_manifest or any(read_regular(generated/name) != read_regular(expected_network/name)
                for name in ('web_socket.cc', 'esp_tcp.cc', 'LICENSE.esp-ml307'))):
            raise ValueError('C3 network copies differ from canonical generator output')
        expected_cues = Path(temporary)/'cues'
        expected_cue_manifest = c3_build.generate(tree, expected_cues)
        if (cues != expected_cue_manifest or read_regular(build/'family_c3_pcm/family_c3_pcm_cues.pack')
                != read_regular(expected_cues/'family_c3_pcm_cues.pack')):
            raise ValueError('C3 PCM pack differs from canonical libopus 1.4 decoder output')
    # Compare real exported compiler inputs to the final component SOURCES.
    compiled = compiled_paths(build/'compile_commands.json')
    selected = []
    for line in read_regular(generated/'compiled-sources.txt').decode().splitlines():
        path = Path(line)
        selected.append((path if path.is_absolute() else component/path).resolve())
    originals = {component/'src/web_socket.cc', component/'src/esp/esp_tcp.cc'}
    replacements = {generated/'web_socket.cc', generated/'esp_tcp.cc'}
    if (any(p in compiled or p in selected for p in originals)
            or any(compiled.count(p) != 1 or selected.count(p) != 1 for p in replacements)
            or len(selected) != len(set(selected))
            or any(compiled.count(p) != 1 for p in selected)):
        raise ValueError('C3 compiled network input/source selection mismatch')
    # Every native component source is retained except the exact two replacements.
    native_sources = {component/name for name in re.findall(r'"([^"\n]+\.cc)"',
                     read_regular(component/'CMakeLists.txt').decode())}
    if set(selected) != ((native_sources-originals) | replacements):
        raise ValueError('C3 compiled network sources changed beyond the two reviewed replacements')
    flash = verify_flash_images(tree, c3=True, pcm=True)
    merged = read_regular(build/'merged-binary.bin')
    releases = list((tree/'releases').glob('*_xmini-c3-family.zip'))
    if len(releases) != 1:
        raise ValueError('C3 expected one canonical release ZIP')
    c3_build.regular(releases[0])
    try:
        with zipfile.ZipFile(releases[0]) as release:
            entries = release.infolist()
            if (len(entries) != 1 or entries[0].filename != 'merged-binary.bin'
                    or entries[0].file_size != len(merged) or release.read(entries[0]) != merged):
                raise ValueError('C3 release merged image differs from verified build')
    except zipfile.BadZipFile as exc:
        raise ValueError('invalid C3 release ZIP') from exc
    return dict(cues=cues, assets_prefix=prefix, elf_sha256=elf_sha.hex(), network=manifest, cache_audit=cache_audit,
                flash_inputs=flash,
                compile_commands_sha256=digest(build/'compile_commands.json'),
                merged_sha256=hashlib.sha256(merged).hexdigest(), release_sha256=digest(releases[0]))


def verify_no_pcm(tree):
    for directory in ('build/family_c3_pcm', 'build/family_c3_network', 'build/family_c3_cache'):
        if (tree/directory).exists():
            raise ValueError('non-PCM build contains PCM generated artifacts')
    flash = tree/'build/flasher_args.json'
    if flash.exists() and 'family_c3_' in read_regular(flash).decode():
        raise ValueError('non-PCM flash contains PCM trailer')
    commands = tree/'build/compile_commands.json'
    if commands.exists():
        if 'family_c3_network' in read_regular(commands).decode():
            raise ValueError('non-PCM compiled inputs contain C3 replacements')
        compiled = compiled_paths(commands)
        component = tree.resolve()/'managed_components/78__esp-ml307'
        if any(compiled.count(component/name) != 1 for name in ('src/web_socket.cc', 'src/esp/esp_tcp.cc')):
            raise ValueError('non-PCM compiled inputs must retain both original network sources exactly once')
        for name, expected in c3_network.ORIGINAL_SHA256.items():
            if hashlib.sha256(read_regular(component/name)).hexdigest() != expected:
                raise ValueError('non-PCM network original SHA256 mismatch: '+name)
        return dict(compile_commands_sha256=digest(commands), original_sha256=c3_network.ORIGINAL_SHA256)


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def inputs(directory, pattern):
    records = []
    for path in sorted(directory.glob(pattern)):
        if not is_source_input(path.relative_to(directory)):
            continue
        if path.is_symlink():
            raise ValueError('symbolic controlled source input')
        if path.is_file():
            c3_build.regular(path)
            records.append(dict(path=path.relative_to(directory).as_posix(), sha256=digest(path)))
    return records


def verify_xiaoman_assets(path):
    """Inspect the locked upstream mmap-assets and srmodels containers, without execution."""
    data = path.read_bytes()
    if not 12 <= len(data) <= 8 * 1024 * 1024:
        raise ValueError('invalid Xiaoman assets size')
    count, checksum, size = struct.unpack_from('<III', data)
    body = data[12:]
    if (count == 0 or count > 4096 or size != len(body) or count * 44 > size
            or checksum != sum(body) & 0xffff):
        raise ValueError('invalid Xiaoman assets header/checksum')
    files = {}
    content = body[count * 44:]
    for number in range(count):
        name, length, offset, _, _ = struct.unpack_from('<32sIIHH', body, number * 44)
        name = name.split(b'\0', 1)[0].decode('utf-8')
        if (name in files or offset + 2 + length > len(content)
                or content[offset:offset + 2] != b'\x5a\x5a'):
            raise ValueError('invalid Xiaoman assets file entry')
        files[name] = content[offset + 2:offset + 2 + length]
    try:
        index = json.loads(files['index.json'])
        models = files[index['srmodels']]
    except (KeyError, ValueError, TypeError) as exc:
        raise ValueError('missing/invalid Xiaoman assets index or models') from exc
    expected = dict(language='cn', duration=3000, threshold=0.2,
                    commands=[dict(command='xiao man', text='小满', action='wake')])
    if index.get('multinet_model') != expected:
        raise ValueError('Xiaoman wake word configuration differs from xiao man')
    if len(models) < 4:
        raise ValueError('invalid Xiaoman model table')
    model_count, = struct.unpack_from('<I', models)
    if not 0 < model_count <= 16:
        raise ValueError('invalid Xiaoman model count')
    position = 4
    names, payloads = [], []
    for _ in range(model_count):
        if position + 36 > len(models):
            raise ValueError('truncated Xiaoman model table')
        name, file_count = struct.unpack_from('<32sI', models, position)
        names.append(name.split(b'\0', 1)[0].decode('ascii'))
        position += 36
        if file_count == 0 or file_count > 512 or position + file_count * 40 > len(models):
            raise ValueError('invalid Xiaoman model file count')
        for _ in range(file_count):
            _, offset, length = struct.unpack_from('<32sII', models, position)
            if length == 0 or offset + length > len(models):
                raise ValueError('invalid Xiaoman model payload')
            payloads.append((offset, length))
            position += 40
    if sorted(names) != ['fst', 'mn7_cn'] or any(offset < position for offset, _ in payloads):
        raise ValueError('Xiaoman assets must contain only mn7_cn and fst models')
    return dict(command='xiao man', display='小满', language='cn', threshold=0.2,
                duration_ms=3000, models=sorted(names), assets_sha256=digest(path))


def verify_waveshare_layout(tree):
    """Bind the dedicated CSV, native binary table and flashed artifact ranges."""
    expected = [('nvs', 1, 2, 0x9000, 0x6000), ('otadata', 1, 0, 0xf000, 0x2000),
                ('phy_init', 1, 1, 0x11000, 0x1000), ('ota_0', 0, 0x10, 0x20000, 0x300000),
                ('ota_1', 0, 0x11, 0x320000, 0x300000), ('assets', 1, 0x82, 0x620000, 0x600000),
                ('storage', 1, 0x82, 0xc20000, 0x300000)]
    table = tree/'build/partition_table/partition-table.bin'
    data = table.read_bytes()
    if not 32 <= len(data) <= 0x1000 or len(data) % 32:
        raise ValueError('invalid Waveshare partition table size')
    entries, md5_seen = [], False
    for position in range(0, len(data), 32):
        entry = data[position:position+32]
        if entry[:2] == b'\xeb\xeb':
            if (md5_seen or entry[2:16] != b'\xff'*14
                    or entry[16:] != hashlib.md5(data[:position]).digest()
                    or data[position+32:] != b'\xff'*(len(data)-position-32)):
                raise ValueError('invalid Waveshare partition table checksum/trailer')
            md5_seen = True
            break
        magic, type_, subtype, offset, size, label, flags = struct.unpack('<HBBII16sI', entry)
        if magic != 0x50aa or flags or size == 0 or offset+size > 0x1000000:
            raise ValueError('invalid Waveshare partition entry')
        try:
            name = label.rstrip(b'\0').decode('ascii')
        except UnicodeDecodeError as exc:
            raise ValueError('invalid Waveshare partition name') from exc
        entries.append((name, type_, subtype, offset, size))
    if not md5_seen or entries != expected:
        raise ValueError('Waveshare partition layout differs from dedicated 16MB contract')
    previous_end = 0x9000
    for _, _, _, offset, size in entries:
        if offset < previous_end:
            raise ValueError('overlapping Waveshare partitions')
        previous_end = offset+size
    csv_rows = []
    types = {'app': 0, 'data': 1}
    subtypes = {'nvs': 2, 'ota': 0, 'phy': 1, 'ota_0': 0x10, 'ota_1': 0x11, 'spiffs': 0x82}
    for row in csv.reader((tree/'partitions/waveshare-xiaoman-16m.csv').read_text().splitlines()):
        if not row or row[0].lstrip().startswith('#'):
            continue
        if len(row) != 5:
            raise ValueError('invalid Waveshare partition CSV')
        name, type_, subtype, offset, size = [value.strip() for value in row]
        try:
            csv_rows.append((name, types[type_], subtypes[subtype], int(offset, 0), int(size, 0)))
        except (KeyError, ValueError) as exc:
            raise ValueError('invalid Waveshare partition CSV value') from exc
    if csv_rows != expected:
        raise ValueError('Waveshare partition CSV differs from binary contract')
    sizes = {}
    for name, limit in [('xiaozhi.bin', 0x300000), ('generated_assets.bin', 0x600000)]:
        size = (tree/'build'/name).stat().st_size
        if not 0 < size <= limit:
            raise ValueError('Waveshare artifact exceeds dedicated partition: '+name)
        sizes[name] = size
    try:
        flash_files = json.loads((tree/'build/flasher_args.json').read_text())['flash_files']
        locations = {}
        for offset, name in flash_files.items():
            basename = Path(name).name
            if basename in ('xiaozhi.bin', 'generated_assets.bin'):
                if basename in locations:
                    raise ValueError('duplicate flashed artifact')
                locations[basename] = int(offset, 0)
        if locations != {'xiaozhi.bin': 0x20000, 'generated_assets.bin': 0x620000}:
            raise ValueError('wrong flashed artifact address')
    except (KeyError, TypeError, ValueError) as exc:
        raise ValueError('Waveshare flasher addresses differ from partition contract') from exc
    return dict(partition_table_sha256=digest(table), partitions=[dict(
        name=name, type=type_, subtype=subtype, offset=offset, size=size)
        for name, type_, subtype, offset, size in entries], app_bytes=sizes['xiaozhi.bin'],
        assets_bytes=sizes['generated_assets.bin'], app_limit=0x300000, assets_limit=0x600000)


def verify_c3_layout(tree):
    """Check the C3 retained table, source CSV, chip headers and flash addresses."""
    expected = [('nvs', 1, 2, 0x9000, 0x4000), ('otadata', 1, 0, 0xd000, 0x2000),
                ('phy_init', 1, 1, 0xf000, 0x1000), ('ota_0', 0, 0x10, 0x20000, 0x2f0000),
                ('ota_1', 0, 0x11, 0x310000, 0x2f0000), ('assets', 1, 0x82, 0x600000, 0x200000)]
    table = tree/'build/partition_table/partition-table.bin'
    data = table.read_bytes()
    if not 32 <= len(data) <= 0x1000 or len(data) % 32:
        raise ValueError('invalid C3 partition table size')
    entries, checksum = [], False
    for position in range(0, len(data), 32):
        entry = data[position:position+32]
        if entry[:2] == b'\xeb\xeb':
            checksum = (entry[2:16] == b'\xff'*14 and entry[16:] == hashlib.md5(data[:position]).digest()
                        and data[position+32:] == b'\xff'*(len(data)-position-32))
            break
        magic, typ, sub, offset, size, label, flags = struct.unpack('<HBBII16sI', entry)
        if magic != 0x50aa or flags or not size or offset+size > 0x1000000:
            raise ValueError('invalid C3 partition entry')
        entries.append((label.rstrip(b'\0').decode('ascii'), typ, sub, offset, size))
    if not checksum or entries != expected:
        raise ValueError('C3 partition layout/checksum differs from native contract')
    def number(value):
        value = value.strip()
        if value[-1:].upper() in ('K', 'M'):
            return int(value[:-1], 0)*(1024 if value[-1:].upper() == 'K' else 1024*1024)
        return int(value, 0)
    csv_rows, previous = [], 0x9000
    types = {'app': 0, 'data': 1}
    subs = {'nvs': 2, 'ota': 0, 'phy': 1, 'ota_0': 0x10, 'ota_1': 0x11, 'spiffs': 0x82}
    for row in csv.reader((tree/'partitions/xmini-c3-family-16m.csv').read_text().splitlines()):
        if not row or row[0].lstrip().startswith('#'): continue
        if len(row) not in (5, 6) or (len(row) == 6 and row[5].strip()):
            raise ValueError('invalid C3 partition CSV')
        name, typ, sub, offset, size = [value.strip() for value in row[:5]]
        alignment = 0x10000 if typ == 'app' else 0x1000
        offset = number(offset) if offset else (previous+alignment-1)//alignment*alignment
        size = number(size)
        csv_rows.append((name, types[typ], subs[sub], offset, size)); previous = offset+size
    if csv_rows != expected:
        raise ValueError('C3 partition CSV differs from binary contract')
    sizes = {}
    for name, limit in [('xiaozhi.bin', 0x2f0000), ('generated_assets.bin', 0x200000)]:
        size = (tree/'build'/name).stat().st_size
        if not 0 < size <= limit: raise ValueError('C3 artifact exceeds partition: '+name)
        sizes[name] = size
    for relative in ['xiaozhi.bin', 'bootloader/bootloader.bin']:
        image = tree/'build'/relative
        with image.open('rb') as source: header = source.read(24)
        if len(header) != 24 or header[0] != 0xe9 or struct.unpack_from('<H', header, 12)[0] != 5:
            raise ValueError('C3 image chip header mismatch: '+relative)
    files = json.loads((tree/'build/flasher_args.json').read_text())['flash_files']
    locations = {}
    wanted = {'xiaozhi.bin': 0x20000, 'generated_assets.bin': 0x600000,
              'bootloader.bin': 0, 'partition-table.bin': 0x8000}
    for offset, name in files.items():
        basename = Path(name).name
        if basename in wanted:
            if basename in locations: raise ValueError('duplicate C3 flashed artifact')
            locations[basename] = int(offset, 0)
    if locations != wanted: raise ValueError('C3 flasher addresses differ from partition contract')
    return dict(partition_table_sha256=digest(table), partitions=[dict(
        name=name, type=typ, subtype=sub, offset=offset, size=size)
        for name, typ, sub, offset, size in entries], app_bytes=sizes['xiaozhi.bin'],
        assets_bytes=sizes['generated_assets.bin'], app_limit=0x2f0000, assets_limit=0x200000)


def verify(firmware, tree, variant, idf_commit):
    if variant not in VARIANTS:
        raise ValueError('unknown variant')
    lock = json.loads((firmware/'upstream.lock.json').read_text())
    if idf_commit != lock['idf_commit']:
        raise ValueError('SDK commit mismatch')
    c3 = variant == C3_VARIANT
    dependency_name = 'dependencies.esp32c3.lock' if c3 else 'dependencies.lock'
    expected_hash = (firmware/(dependency_name+'.sha256')).read_text().strip()
    if (digest(firmware/dependency_name) != expected_hash
            or digest(tree/'dependencies.lock') != expected_hash):
        raise ValueError('native dependencies.lock drift; deliberate changes require a reviewed pin')
    expected_provenance = dict(upstream_commit=lock['commit'], archive_sha256=lock['archive_sha256'],
                               patches=inputs(firmware/'patches', '*.patch'),
                               overlays=inputs(firmware/'overlays', '**/*'),
                               dependencies_sha256=expected_hash)
    esp_sr_version = lock['esp_sr_version']
    overrides = {}
    if c3 and 'esp_sr_version_esp32c3' in lock:
        target_version = lock['esp_sr_version_esp32c3']
        for version in (esp_sr_version, target_version):
            if not isinstance(version, str) or not re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+', version):
                raise ValueError('invalid exact ESP-SR version')
        if target_version != esp_sr_version:
            overrides['espressif/esp-sr'] = target_version
            esp_sr_version = target_version
            expected_provenance['target_dependency_overrides'] = overrides
    if json.loads((tree/'.family-robot-source.json').read_text()) != expected_provenance:
        raise ValueError('prepared source provenance mismatch')
    if overrides:
        manifest = tree/'main/idf_component.yml'
        if manifest.is_symlink() or manifest.parent.is_symlink() or not manifest.is_file():
            raise ValueError('missing/nonregular prepared component manifest')
        content = manifest.read_bytes()
        pin = f'  espressif/esp-sr: =={esp_sr_version}\n'.encode('ascii')
        component_keys = re.findall(rb'^[ \t]*espressif/esp-sr[ \t]*:', content, re.M)
        if content.splitlines(keepends=True).count(pin) != 1 or len(component_keys) != 1:
            raise ValueError('prepared component manifest ESP-SR pin mismatch')
    # This is a version assertion on the EXACT checked-in, byte-validated native
    # lock, not a general YAML parser. IDF's component manager validates its schema.
    native = (tree/'dependencies.lock').read_text()
    if c3 and re.findall(r'^target: (\S+)\s*$', native, re.M) != ['esp32c3']:
        raise ValueError('native dependency target mismatch')
    versions = {'espressif/esp-sr': esp_sr_version,
                'espressif/esp_codec_dev': '1.6.2', 'idf': '6.1.0'}
    for name, version in versions.items():
        sections = re.findall(r'^  '+re.escape(name)+r':\n(.*?)(?=^  \S|\Z)', native, re.M | re.S)
        if len(sections) != 1 or re.findall(r'^    version: (\S+)\s*$', sections[0], re.M) != [version]:
            raise ValueError('critical component version mismatch: '+name)
    if c3 and expected_hash != 'e178b87a1832979ff7143ae2c5cde006b7ab62bc8067f18e718b4b26be558bbc':
        raise ValueError('C3 native dependency pin differs from reviewed ESP-SR 2.6.1 lock')
    effective = {}
    for line in (tree/'sdkconfig').read_text().splitlines():
        if line.startswith('CONFIG_') and '=' in line:
            key, value = line.split('=', 1)
        elif line.startswith('# CONFIG_') and line.endswith(' is not set'):
            key, value = line[2:-len(' is not set')], 'n'
        else:
            continue
        # Native SDK output repeats deprecated aliases with identical values.
        if key in effective and effective[key] != value:
            raise ValueError('conflicting SDK config key: '+key)
        effective[key] = value
    waveshare = variant == WAVESHARE_VARIANT
    experiment = variant == 'm5stack-stopwatch-software-aec' or variant == 'm5stack-stopwatch-xiaoman'
    xiaoman = variant == 'm5stack-stopwatch-xiaoman' or waveshare
    usb_console = experiment or waveshare or c3
    required = {'CONFIG_IDF_TARGET': '"esp32s3"',
                'CONFIG_XMINI_C3_PCM': 'n',
                'CONFIG_BOARD_TYPE_M5STACK_STOPWATCH': 'n' if waveshare else 'y',
                'CONFIG_BOARD_TYPE_WAVESHARE_ESP32_S3_TOUCH_AMOLED_2_16': 'y' if waveshare else 'n',
                'CONFIG_WAVESHARE_XIAOMAN_REALTIME': 'y' if waveshare else 'n',
                'CONFIG_SPIRAM': 'y', 'CONFIG_USE_AUDIO_PROCESSOR': 'y',
                'CONFIG_I2S_ISR_IRAM_SAFE': 'n', 'CONFIG_USE_AUDIO_DEBUGGER': 'n',
                'CONFIG_USE_SERVER_AEC': 'n', 'CONFIG_BOOTLOADER_APP_ROLLBACK_ENABLE': 'y',
                'CONFIG_PARTITION_TABLE_OFFSET': '0x8000',
                'CONFIG_PARTITION_TABLE_CUSTOM_FILENAME': '"partitions/waveshare-xiaoman-16m.csv"' if waveshare else '"partitions/v2/16m.csv"',
                'CONFIG_STOPWATCH_SOFTWARE_AEC': 'y' if experiment else 'n',
                'CONFIG_STOPWATCH_DEFAULT_REALTIME': 'y' if variant == 'm5stack-stopwatch-xiaoman' else 'n',
                'CONFIG_CHICK_FACE_UI': 'y' if xiaoman else 'n',
                'CONFIG_WAVESHARE_WAKE_SPEECH_WINDOW': 'n',
                'CONFIG_USE_DEVICE_AEC': 'y' if usb_console else 'n',
                'CONFIG_ESP_CONSOLE_UART_DEFAULT': 'n' if usb_console else 'y',
                'CONFIG_ESP_CONSOLE_USB_SERIAL_JTAG': 'y' if usb_console else 'n'}
    if usb_console:
        required.update(CONFIG_ESP_CONSOLE_SECONDARY_USB_SERIAL_JTAG='n',
                        CONFIG_ESP_CONSOLE_SECONDARY_NONE='y')
        # SDK esp_stdio/Kconfig makes secondary USB depend on !primary USB.
        # When primary USB is selected Kconfig may omit the disabled symbol.
        effective.setdefault('CONFIG_ESP_CONSOLE_SECONDARY_USB_SERIAL_JTAG', 'n')
    if waveshare:
        required.update(CONFIG_ESPTOOLPY_FLASHSIZE_16MB='y', CONFIG_PARTITION_TABLE_CUSTOM='y')
    if c3:
        required.update(CONFIG_IDF_TARGET='"esp32c3"', CONFIG_BOARD_TYPE_M5STACK_STOPWATCH='n',
                        CONFIG_BOARD_TYPE_XMINI_C3='y', CONFIG_XMINI_C3_BUTTON_ENTRY='y',
                        CONFIG_XMINI_C3_VOICE_BARGE='n',
                        CONFIG_LANGUAGE_ZH_CN='y', CONFIG_FLASH_DEFAULT_ASSETS='y',
                        CONFIG_SPIRAM='n', CONFIG_USE_AUDIO_PROCESSOR='n', CONFIG_USE_DEVICE_AEC='n',
                        CONFIG_ESPTOOLPY_FLASHSIZE_16MB='y', CONFIG_PARTITION_TABLE_CUSTOM='y',
                        CONFIG_PARTITION_TABLE_CUSTOM_FILENAME='"partitions/xmini-c3-family-16m.csv"',
                        CONFIG_WAKE_WORD_DISABLED='y', CONFIG_USE_ESP_WAKE_WORD='n',
                        CONFIG_USE_AFE_WAKE_WORD='n', CONFIG_USE_CUSTOM_WAKE_WORD='n',
                        CONFIG_OTA_URL='""', CONFIG_SECURE_BOOT='n',
                        CONFIG_SECURE_FLASH_ENC_ENABLED='n', CONFIG_BOOTLOADER_APP_ANTI_ROLLBACK='n')
        for key, value in required.items():
            if value == 'n': effective.setdefault(key, 'n')
    if experiment:
        try:
            offset = int(effective['CONFIG_STOPWATCH_AEC_OFFSET_SAMPLES'])
        except (KeyError, ValueError) as exc:
            raise ValueError('missing/invalid calibration offset') from exc
        if not -2400 <= offset <= 2400:
            raise ValueError('calibration offset out of range')
        required['CONFIG_STOPWATCH_AEC_OFFSET_SAMPLES'] = str(offset)
    if xiaoman:
        required.update(CONFIG_USE_CUSTOM_WAKE_WORD='y', CONFIG_USE_AFE_WAKE_WORD='n',
                        CONFIG_USE_DEFAULT_MESSAGE_STYLE='y',
                        CONFIG_SR_MN_CN_MULTINET7_QUANT='y',
                        CONFIG_CUSTOM_WAKE_WORD='"xiao man"',
                        CONFIG_CUSTOM_WAKE_WORD_DISPLAY='"小满"',
                        CONFIG_CUSTOM_WAKE_WORD_THRESHOLD='20', CONFIG_SEND_WAKE_WORD_DATA='y')
    # The default-realtime option is absent when software AEC is not selected.
    effective.setdefault('CONFIG_BOARD_TYPE_M5STACK_STOPWATCH', 'n')
    effective.setdefault('CONFIG_BOARD_TYPE_WAVESHARE_ESP32_S3_TOUCH_AMOLED_2_16', 'n')
    effective.setdefault('CONFIG_WAVESHARE_XIAOMAN_REALTIME', 'n')
    effective.setdefault('CONFIG_STOPWATCH_SOFTWARE_AEC', 'n')
    effective.setdefault('CONFIG_STOPWATCH_DEFAULT_REALTIME', 'n')
    effective.setdefault('CONFIG_CHICK_FACE_UI', 'n')
    effective.setdefault('CONFIG_WAVESHARE_WAKE_SPEECH_WINDOW', 'n')
    effective.setdefault('CONFIG_XMINI_C3_PCM', 'n')
    # Kconfig omits the device-AEC symbol when this stock board's dependency
    # expression is false. Absent means disabled, never an enabled core path.
    if not experiment:
        effective.setdefault('CONFIG_USE_DEVICE_AEC', 'n')
    for key, value in required.items():
        if effective.get(key) != value:
            raise ValueError('effective configuration mismatch: '+key)
    evidence = dict(variant=variant, board=VARIANT_BOARDS[variant], idf_commit=idf_commit, critical_versions=versions,
                    dependencies_sha256=expected_hash, effective_config=required,
                    acoustic_acceptance='pending')
    if c3:
        evidence['dependency_lock_source'] = dependency_name
        evidence['partition_layout'] = verify_c3_layout(tree)
    evidence['native_network_inputs'] = verify_no_pcm(tree)
    if waveshare:
        evidence['partition_layout'] = verify_waveshare_layout(tree)
    if xiaoman:
        evidence['wake_word'] = verify_xiaoman_assets(tree/'build/generated_assets.bin')
    return evidence


def c3_retained_inputs(firmware):
    paths = {'main/idf_component.yml', 'CMakeLists.txt', 'managed_components/78__esp-ml307/CMakeLists.txt'}
    paths.update('managed_components/78__esp-ml307/'+name for name in c3_network.ORIGINAL_SHA256)
    paths.update('main/assets/'+name for name, _, _ in c3_build.INVENTORY)
    paths.update(path.relative_to(firmware/'overlays').as_posix()
                 for path in (firmware/'overlays').rglob('*')
                 if path.is_file() and is_source_input(path.relative_to(firmware/'overlays')))
    for patch in (firmware/'patches').glob('*.patch'):
        paths.update(re.findall(r'^\+\+\+ b/([^\n]+)$', patch.read_text(), re.M))
    return sorted(paths)


def bundle(firmware, tree, output, source_sha, evidence):
    if not re.fullmatch('[0-9a-f]{40}', source_sha):
        raise ValueError('invalid source SHA')
    flash = verify_flash_images(tree, c3=evidence['variant'] == C3_VARIANT)
    evidence = dict(evidence, flash_inputs=flash)
    required = ['dependencies.lock', 'sdkconfig', '.family-robot-source.json', *BUILD_FILES]
    required.extend(NATIVE_NETWORK_FILES)
    release_name = CANONICAL_RELEASE_NAMES.get(evidence['variant'])
    if release_name is None:
        raise ValueError('unknown canonical release variant')
    releases = list((tree/'releases').glob('*_'+release_name+'.zip'))
    if len(releases) != 1:
        raise ValueError('expected exactly one canonical release ZIP')
    if evidence['variant'] == C3_VARIANT:
        c3_build.regular(releases[0])
        with zipfile.ZipFile(releases[0]) as release:
            entries = release.infolist()
            merged = read_regular(tree/'build/merged-binary.bin')
            if (len(entries) != 1 or entries[0].filename != 'merged-binary.bin'
                    or entries[0].file_size != len(merged) or release.read(entries[0]) != merged):
                raise ValueError('C3 release merged image differs from verified build')
    required.append(releases[0].relative_to(tree).as_posix())
    for relative in required:
        path = tree/relative
        if (not path.is_file() or path.is_symlink() or path.stat().st_size == 0
                or any(parent.is_symlink() for parent in path.parents)):
            raise ValueError('missing/nonregular build evidence: '+relative)
    output.mkdir(parents=True, exist_ok=False)
    for relative in required:
        target = output/relative
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(tree/relative, target)
    shutil.copyfile(firmware/'upstream.lock.json', output/'upstream.lock.json')
    dependency_pin = 'dependencies.esp32c3.lock.sha256' if evidence['variant'] == C3_VARIANT else 'dependencies.lock.sha256'
    shutil.copyfile(firmware/dependency_pin, output/'dependencies.lock.sha256')
    (output/'third_party').mkdir()
    shutil.copyfile(firmware/'third_party/xiaozhi-LICENSE', output/'third_party/xiaozhi-LICENSE')
    shutil.copyfile(firmware.parent/'THIRD_PARTY_NOTICES.txt', output/'THIRD_PARTY_NOTICES.txt')
    for name in ['patches', 'overlays']:
        shutil.copytree(firmware/name, output/'source'/name,
            ignore=lambda directory, names: [name for name in names if not is_source_input(Path(name))])
    (output/'build-evidence.json').write_text(
        json.dumps(dict(evidence, source_sha=source_sha), indent=2)+'\n')
    (output/'SHA256SUMS').write_text(''.join(
        digest(path)+'  '+path.relative_to(output).as_posix()+'\n'
        for path in sorted(output.rglob('*')) if path.is_file()))


def bundle_baseline(firmware, tree, output, source_sha, idf_commit):
    """Keep stock sources unpatched while retaining auditable original inputs."""
    if not re.fullmatch('[0-9a-f]{40}', source_sha):
        raise ValueError('invalid source SHA')
    lock = json.loads(read_regular(firmware/'upstream.lock.json'))
    if idf_commit != lock['idf_commit']:
        raise ValueError('SDK commit mismatch')
    config = read_regular(tree/'sdkconfig').decode()
    if (re.findall(r'^CONFIG_IDF_TARGET=(\S+)\s*$', config, re.M) != ['"esp32s3"']
            or any(value != 'n' for value in re.findall(r'^CONFIG_XMINI_C3_PCM=(\S+)\s*$', config, re.M))):
        raise ValueError('stock baseline target/PCM configuration mismatch')
    effective = {}
    for line in config.splitlines():
        if line.startswith('CONFIG_') and '=' in line:
            key, value = line.split('=', 1)
        elif line.startswith('# CONFIG_') and line.endswith(' is not set'):
            key, value = line[2:-len(' is not set')], 'n'
        else:
            continue
        if key in effective and effective[key] != value:
            raise ValueError('stock baseline conflicting SDK config key: '+key)
        effective[key] = value
    board = 'waveshare/esp32-s3-touch-amoled-2.16'
    variant = 'esp32-s3-touch-amoled-2.16'
    if (effective.get('CONFIG_BOARD_TYPE_WAVESHARE_ESP32_S3_TOUCH_AMOLED_2_16') != 'y'
            or effective.get('CONFIG_BOARD_TYPE_M5STACK_STOPWATCH', 'n') != 'n'
            or effective.get('CONFIG_USE_DEVICE_AEC') != 'y'):
        raise ValueError('stock baseline board/AEC configuration mismatch')
    # Compare the actual board inputs with the checksum-locked original archive,
    # independent of the patched product tree and its historical M5 overlays.
    archive = firmware/'.cache'/(lock['commit']+'.tar.gz')
    if hashlib.sha256(read_regular(archive)).hexdigest() != lock['archive_sha256']:
        raise ValueError('stock source archive checksum mismatch')
    board_source = 'main/boards/'+board+'/esp32-s3-touch-amoled-2.16.cc'
    compiled = compiled_paths(tree/'build/compile_commands.json')
    originals = {'main/boards/'+board+'/config.json', board_source}
    for path in compiled:
        try:
            relative = path.relative_to(tree.resolve()).as_posix()
        except ValueError:
            continue
        if relative.startswith('main/'):
            originals.add(relative)
    source_inputs = {}
    with tarfile.open(archive) as upstream:
        for relative in sorted(originals):
            member = upstream.getmember(lock['archive_root']+'/'+relative)
            if not member.isfile():
                raise ValueError('stock source archive board is not regular')
            original = upstream.extractfile(member).read()
            if read_regular(tree/relative) != original:
                raise ValueError('stock source input differs from locked upstream')
            source_inputs[relative] = hashlib.sha256(original).hexdigest()
    if (compiled.count((tree/board_source).resolve()) != 1
            or any('/main/boards/' in p.as_posix() and '/main/boards/common/' not in p.as_posix()
                   and '/main/boards/'+board+'/' not in p.as_posix() for p in compiled)):
        raise ValueError('stock compiled board input mismatch')
    # Required even though config-only unit fixtures may omit compiler output.
    c3_build.regular(tree/'build/compile_commands.json')
    native = verify_no_pcm(tree)
    flash = verify_flash_images(tree)
    releases = list((tree/'releases').glob('*.zip'))
    if len(releases) != 1 or not re.fullmatch(r'v[^/]+_waveshare-esp32-s3-touch-amoled-2\.16\.zip', releases[0].name):
        raise ValueError('expected one stock canonical release ZIP')
    required = ['dependencies.lock', 'sdkconfig', *source_inputs, *BUILD_FILES, *NATIVE_NETWORK_FILES,
                releases[0].relative_to(tree).as_posix()]
    for relative in required:
        c3_build.regular(tree/relative)
    with zipfile.ZipFile(releases[0]) as release:
        entries = release.infolist()
        merged = read_regular(tree/'build/merged-binary.bin')
        if (len(entries) != 1 or entries[0].filename != 'merged-binary.bin'
                or entries[0].file_size != len(merged) or release.read(entries[0]) != merged):
            raise ValueError('stock release differs from merged image')
    output.mkdir(parents=True, exist_ok=False)
    for relative in required:
        target = output/relative
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(tree/relative, target)
    for source, relative in [(firmware/'upstream.lock.json', 'upstream.lock.json'),
                             (firmware/'third_party/xiaozhi-LICENSE', 'third_party/xiaozhi-LICENSE'),
                             (firmware.parent/'THIRD_PARTY_NOTICES.txt', 'THIRD_PARTY_NOTICES.txt')]:
        target = output/relative
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(read_regular(source))
    (output/'build-evidence.json').write_text(json.dumps(dict(source_sha=source_sha, idf_commit=idf_commit,
        variant='stock-'+variant, board=board, effective_config=effective,
        stock_source_inputs=source_inputs, upstream_commit=lock['commit'], archive_sha256=lock['archive_sha256'],
        native_network_inputs=native,
        flash_inputs=flash,
        dependencies_sha256=digest(tree/'dependencies.lock'), acoustic_acceptance='pending'), indent=2)+'\n')
    (output/'SHA256SUMS').write_text(''.join(digest(path)+'  '+path.relative_to(output).as_posix()+'\n'
        for path in sorted(output.rglob('*')) if path.is_file()))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--tree', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--variant', choices=(WAVESHARE_VARIANT,))
    parser.add_argument('--baseline', action='store_true', help='unpatched stock S3 evidence')
    parser.add_argument('--source-sha', required=True)
    parser.add_argument('--idf-commit', required=True)
    args = parser.parse_args()
    if args.baseline == bool(args.variant):
        parser.error('choose exactly one of --baseline or --variant')
    actual = subprocess.check_output(['git', '-c', f'safe.directory={FIRMWARE.parent}', '-C', str(FIRMWARE.parent), 'rev-parse', 'HEAD'], text=True).strip()
    if actual != args.source_sha:
        parser.error('checkout does not match requested source SHA')
    if args.baseline:
        bundle_baseline(FIRMWARE, args.tree, args.output, actual, args.idf_commit)
    else:
        evidence = verify(FIRMWARE, args.tree, args.variant, args.idf_commit)
        bundle(FIRMWARE, args.tree, args.output, actual, evidence)
    print('Verified source and build evidence; acoustics pending')


if __name__ == '__main__':
    main()
