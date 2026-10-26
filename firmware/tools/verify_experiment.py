#!/usr/bin/env python3
"""Verify and package allowlisted evidence from one isolated experimental CI job.

No device operations. Does not validate ESP image CRCs or acoustics;
those remain downloaded-artifact inspection and notified field gates.
"""
import argparse
import csv
import hashlib
import json
from pathlib import Path
import re
import shutil
import struct
import subprocess

FIRMWARE = Path(__file__).resolve().parents[1]
WAVESHARE_VARIANT = 'esp32-s3-touch-amoled-2.16-xiaoman'
VARIANTS = ('m5stack-stopwatch', 'm5stack-stopwatch-software-aec', 'm5stack-stopwatch-xiaoman', WAVESHARE_VARIANT)
VARIANT_BOARDS = {variant: 'm5stack/stopwatch' for variant in VARIANTS[:3]}
VARIANT_BOARDS[WAVESHARE_VARIANT] = 'waveshare/esp32-s3-touch-amoled-2.16'
BUILD_FILES = ('build/xiaozhi.bin', 'build/merged-binary.bin', 'build/generated_assets.bin',
               'build/bootloader/bootloader.bin', 'build/partition_table/partition-table.bin',
               'build/flasher_args.json')


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def inputs(directory, pattern):
    return [dict(path=path.relative_to(directory).as_posix(), sha256=digest(path))
            for path in sorted(directory.glob(pattern)) if path.is_file()]


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


def verify(firmware, tree, variant, idf_commit):
    if variant not in VARIANTS:
        raise ValueError('unknown variant')
    lock = json.loads((firmware/'upstream.lock.json').read_text())
    if idf_commit != lock['idf_commit']:
        raise ValueError('SDK commit mismatch')
    expected_hash = (firmware/'dependencies.lock.sha256').read_text().strip()
    if (digest(firmware/'dependencies.lock') != expected_hash
            or digest(tree/'dependencies.lock') != expected_hash):
        raise ValueError('native dependencies.lock drift; deliberate changes require a reviewed pin')
    expected_provenance = dict(upstream_commit=lock['commit'], archive_sha256=lock['archive_sha256'],
                               patches=inputs(firmware/'patches', '*.patch'),
                               overlays=inputs(firmware/'overlays', '**/*'),
                               dependencies_sha256=expected_hash)
    if json.loads((tree/'.family-robot-source.json').read_text()) != expected_provenance:
        raise ValueError('prepared source provenance mismatch')
    # This is a version assertion on the EXACT checked-in, byte-validated native
    # lock, not a general YAML parser. IDF's component manager validates its schema.
    native = (tree/'dependencies.lock').read_text()
    versions = {'espressif/esp-sr': lock['esp_sr_version'],
                'espressif/esp_codec_dev': '1.6.2', 'idf': '6.1.0'}
    for name, version in versions.items():
        sections = re.findall(r'^  '+re.escape(name)+r':\n(.*?)(?=^  \S|\Z)', native, re.M | re.S)
        if len(sections) != 1 or re.findall(r'^    version: (\S+)\s*$', sections[0], re.M) != [version]:
            raise ValueError('critical component version mismatch: '+name)
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
    usb_console = experiment or waveshare
    required = {'CONFIG_IDF_TARGET': '"esp32s3"',
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
                'CONFIG_XIAOMAN_ORB_UI': 'y' if xiaoman else 'n',
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
    effective.setdefault('CONFIG_XIAOMAN_ORB_UI', 'n')
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
    if waveshare:
        evidence['partition_layout'] = verify_waveshare_layout(tree)
    if xiaoman:
        evidence['wake_word'] = verify_xiaoman_assets(tree/'build/generated_assets.bin')
    return evidence


def bundle(firmware, tree, output, source_sha, evidence):
    if not re.fullmatch('[0-9a-f]{40}', source_sha):
        raise ValueError('invalid source SHA')
    required = ['dependencies.lock', 'sdkconfig', '.family-robot-source.json', *BUILD_FILES]
    releases = list((tree/'releases').glob('*_'+evidence['variant']+'.zip'))
    if len(releases) != 1:
        raise ValueError('expected exactly one canonical release ZIP')
    required.append(releases[0].relative_to(tree).as_posix())
    for relative in required:
        path = tree/relative
        if not path.is_file() or path.is_symlink() or path.stat().st_size == 0:
            raise ValueError('missing/nonregular build evidence: '+relative)
    output.mkdir(parents=True, exist_ok=False)
    for relative in required:
        target = output/relative
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(tree/relative, target)
    for name in ['upstream.lock.json', 'dependencies.lock.sha256']:
        shutil.copyfile(firmware/name, output/name)
    (output/'third_party').mkdir()
    shutil.copyfile(firmware/'third_party/xiaozhi-LICENSE', output/'third_party/xiaozhi-LICENSE')
    shutil.copyfile(firmware.parent/'THIRD_PARTY_NOTICES.txt', output/'THIRD_PARTY_NOTICES.txt')
    for name in ['patches', 'overlays']:
        shutil.copytree(firmware/name, output/'source'/name)
    (output/'build-evidence.json').write_text(
        json.dumps(dict(evidence, source_sha=source_sha), indent=2)+'\n')
    (output/'SHA256SUMS').write_text(''.join(
        digest(path)+'  '+path.relative_to(output).as_posix()+'\n'
        for path in sorted(output.rglob('*')) if path.is_file()))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--tree', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--variant', choices=VARIANTS, required=True)
    parser.add_argument('--source-sha', required=True)
    parser.add_argument('--idf-commit', required=True)
    args = parser.parse_args()
    actual = subprocess.check_output(['git', '-c', f'safe.directory={FIRMWARE.parent}', '-C', str(FIRMWARE.parent), 'rev-parse', 'HEAD'], text=True).strip()
    if actual != args.source_sha:
        parser.error('checkout does not match requested source SHA')
    evidence = verify(FIRMWARE, args.tree, args.variant, args.idf_commit)
    bundle(FIRMWARE, args.tree, args.output, actual, evidence)
    print('Verified source, SDK, dependency bytes and effective '+args.variant+' configuration; acoustics pending')


if __name__ == '__main__':
    main()
