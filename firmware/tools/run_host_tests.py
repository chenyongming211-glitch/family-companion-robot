#!/usr/bin/env python3
"""Run the supported ESP32-S3 and shared firmware host checks on the cloud."""
import argparse
import json
from pathlib import Path
import shutil
import sys
import unittest

from prepare import download_archive, verify_checksum

FIRMWARE = Path(__file__).resolve().parents[1]


def cases(suite):
    for item in suite:
        if isinstance(item, unittest.TestSuite):
            yield from cases(item)
        else:
            yield item


def supported_suite():
    # Do not even import the retired platform's modules: their SDK/network/cue
    # fixture preparation is outside the current product support boundary.
    directory = FIRMWARE / 'tests'
    sys.path.insert(0, str(directory))
    loader = unittest.TestLoader()
    selected = []
    for path in sorted(directory.glob('test_*.py')):
        if path.stem.startswith(('test_c3_', 'test_m5_')) or path.stem == 'test_experiment_evidence':
            continue
        for case in cases(loader.loadTestsFromName(path.stem)):
            method = case.id().rsplit('.', 1)[-1]
            if method.startswith(('test_c3_', 'test_cli_c3_', 'test_m5_')):
                continue
            if path.stem == 'test_realtime_default' and method in {
                    'test_xiaoman_realtime_intent_waits_for_eligible_reference_and_respects_off',
                    'test_old_software_aec_variant_starts_off_and_keeps_explicit_on_across_pause',
                    'test_original_stopwatch_variant_starts_off',
                    'test_only_xiaoman_variant_requests_default_realtime'}:
                continue
            if path.stem == 'test_xiaoman_wake' and method == 'test_xiaoman_variant_keeps_old_variants_and_uses_real_chinese_model':
                continue
            selected.append(case)
    if not selected:
        raise ValueError('no supported firmware tests selected')
    return unittest.TestSuite(selected)


def cache_source(archive):
    lock = json.loads((FIRMWARE / 'upstream.lock.json').read_text())
    source = archive or FIRMWARE / '.cache' / (lock['commit'] + '.tar.gz')
    if not source.exists():
        if archive is not None:
            raise ValueError('source archive not found')
        download_archive(lock['archive_url'], source, lock['archive_sha256'])
    verify_checksum(source, lock['archive_sha256'])
    # Existing S3 wrappers share this checksum-locked upstream source cache.
    # No C3 registry component or cue conversion is required to populate it.
    target = FIRMWARE.parent / '.superpowers/firmware-baseline/xiaozhi-v2.5.2.tar.gz'
    if target.exists() or target.is_symlink():
        if target.is_symlink() or not target.is_file():
            raise ValueError('unsafe firmware source cache')
        verify_checksum(target, lock['archive_sha256'])
    else:
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(source, target)
        verify_checksum(target, lock['archive_sha256'])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--archive', type=Path, help='existing checksum-locked upstream archive')
    parser.add_argument('--list', action='store_true', help='list selected test IDs without running them')
    args = parser.parse_args()
    if not args.list:
        cache_source(args.archive)
    suite = supported_suite()
    if args.list:
        print(json.dumps([case.id() for case in cases(suite)], indent=2))
        return 0
    print(f'Running {suite.countTestCases()} ESP32-S3/shared host tests; M5 and ESP32-C3 are retired.', flush=True)
    return 0 if unittest.TextTestRunner(verbosity=2).run(suite).wasSuccessful() else 1


if __name__ == '__main__':
    raise SystemExit(main())
