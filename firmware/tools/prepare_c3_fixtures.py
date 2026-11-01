#!/usr/bin/env python3
"""Publish a fresh immutable, checksum-pinned registry fixture for host tests."""
import argparse
import hashlib
import json
from pathlib import Path, PurePosixPath
import stat
import shutil
import sys
import tempfile
import zipfile

from prepare import FIRMWARE, LOCK_PATH, download_archive, refuse_existing, verify_checksum

sys.path.insert(0, str(Path(__file__).resolve().parents[1]/'overlays/scripts'))
from family_c3_network import ARCHIVE_SHA256, ORIGINAL_SHA256, SOURCE_URI

MAX_EXPANDED = 16 * 1024 * 1024


def cache_source_archive(source, destination, checksum):
    """Reuse verified bytes at the legacy tests' documented archive cache path."""
    source, destination = Path(source), Path(destination)
    if source.is_symlink() or not source.is_file():
        raise ValueError('source archive cache input must be regular')
    verify_checksum(source, checksum)
    if destination.exists() or destination.is_symlink():
        if destination.is_symlink() or not destination.is_file():
            raise ValueError('source archive cache must be regular')
        verify_checksum(destination, checksum)
        return destination
    destination.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='.source-cache-', dir=destination.parent) as temporary:
        candidate = Path(temporary)/'archive'
        shutil.copyfile(source, candidate)
        verify_checksum(candidate, checksum)
        refuse_existing(destination)
        candidate.rename(destination)
    return destination


def extract_component(archive, destination, checksum=ARCHIVE_SHA256):
    archive, destination = Path(archive), Path(destination)
    refuse_existing(destination)
    if archive.is_symlink() or not archive.is_file():
        raise ValueError('fixture archive must be regular')
    verify_checksum(archive, checksum)
    with zipfile.ZipFile(archive) as source:
        names, total, entries = set(), 0, []
        for entry in source.infolist():
            path = PurePosixPath(entry.filename)
            mode = entry.external_attr >> 16
            if (not path.parts or path.is_absolute() or '..' in path.parts
                    or '\\' in entry.filename or entry.filename in names
                    or stat.S_ISLNK(mode) or (stat.S_IFMT(mode) not in (0, stat.S_IFREG, stat.S_IFDIR))):
                raise ValueError('unsafe/duplicate registry archive path')
            names.add(entry.filename)
            total += entry.file_size
            if total > MAX_EXPANDED or entry.flag_bits & 1:
                raise ValueError('oversized/encrypted registry archive')
            entries.append((entry, path))
        destination.parent.mkdir(parents=True, exist_ok=True)
        with tempfile.TemporaryDirectory(prefix='.c3-fixture-', dir=destination.parent) as temporary:
            tree = Path(temporary)/'source'
            tree.mkdir()
            for entry, path in entries:
                target = tree.joinpath(*path.parts)
                if entry.is_dir():
                    target.mkdir(parents=True, exist_ok=True)
                else:
                    target.parent.mkdir(parents=True, exist_ok=True)
                    with source.open(entry) as contents, target.open('xb') as output:
                        output.write(contents.read(MAX_EXPANDED+1))
            for name, expected in ORIGINAL_SHA256.items():
                path = tree/name
                if not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest() != expected:
                    raise ValueError('registry original SHA256 mismatch: '+name)
            refuse_existing(destination)
            tree.rename(destination)
    return destination


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--archive', type=Path,
        default=Path(__file__).resolve().parents[1]/'.cache/esp-ml307-3.7.6.zip')
    parser.add_argument('--destination', type=Path, required=True)
    parser.add_argument('--cache-upstream-source', action='store_true',
        help='stage the already prepared locked archive for fresh per-class test extraction')
    args = parser.parse_args()
    refuse_existing(args.destination)
    if not args.archive.exists():
        download_archive(SOURCE_URI, args.archive, ARCHIVE_SHA256)
    if args.cache_upstream_source:
        lock = json.loads(LOCK_PATH.read_text())
        cache_source_archive(FIRMWARE/'.cache'/(lock['commit']+'.tar.gz'),
            FIRMWARE.parent/'.superpowers/firmware-baseline/xiaozhi-v2.5.2.tar.gz', lock['archive_sha256'])
    print(extract_component(args.archive, args.destination).resolve())


if __name__ == '__main__':
    main()
