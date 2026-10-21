#!/usr/bin/env python3
"""Prepare a checksum-locked upstream source tree; never execute its contents."""
import argparse
import copy
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import sys
import tarfile
import tempfile
import urllib.error
import urllib.request
import zlib


MAX_BYTES = 64 * 1024 * 1024
FIRMWARE = Path(__file__).resolve().parents[1]
LOCK_PATH = FIRMWARE / 'upstream.lock.json'


def verify_checksum(path: Path, checksum: str) -> None:
    digest = hashlib.sha256()
    total = 0
    with path.open('rb') as source:
        while chunk := source.read(1024 * 1024):
            total += len(chunk)
            if total > MAX_BYTES:
                raise ValueError('archive exceeds 64 MiB')
            digest.update(chunk)
    if digest.hexdigest() != checksum:
        raise ValueError('archive checksum mismatch')


def refuse_existing(destination: Path) -> None:
    if os.path.lexists(destination):
        raise FileExistsError('destination already exists')


def prepare_source(archive: Path, destination: Path, checksum: str, expected_root: str) -> Path:
    """Validate every entry, then strip the locked root into a fresh directory."""
    refuse_existing(destination)
    verify_checksum(archive, checksum)
    if not expected_root or '/' in expected_root or expected_root in ('.', '..'):
        raise ValueError('invalid configured archive root')
    try:
        with tarfile.open(archive, 'r:gz') as source:
            members = []
            total = 0
            for member in source:
                path = PurePosixPath(member.name)
                if path.is_absolute() or '..' in path.parts:
                    raise ValueError('unsafe archive path')
                if not path.parts or path.parts[0] != expected_root:
                    raise ValueError('unexpected archive root')
                if not (member.isfile() or member.isdir()):
                    raise ValueError('archive contains a link or unsupported entry')
                if member.size < 0:
                    raise ValueError('invalid archive entry size')
                if member.isfile():
                    total += member.size
                    if total > MAX_BYTES:
                        raise ValueError('expanded archive exceeds 64 MiB')
                if len(path.parts) == 1:
                    if not member.isdir():
                        raise ValueError('archive root must be a directory')
                    continue
                stripped = copy.copy(member)
                stripped.name = str(PurePosixPath(*path.parts[1:]))
                members.append(stripped)
            if not members:
                raise ValueError('archive has no source entries')
            destination.parent.mkdir(parents=True, exist_ok=True)
            with tempfile.TemporaryDirectory(prefix='.prepare-', dir=destination.parent) as temporary:
                tree = Path(temporary) / 'source'
                tree.mkdir()
                source.extractall(tree, members=members, filter='data')
                refuse_existing(destination)
                tree.rename(destination)
    except (tarfile.TarError, EOFError, zlib.error) as error:
        raise ValueError('invalid source archive') from error
    return destination


def download_archive(url: str, destination: Path, checksum: str) -> None:
    """Keep a downloaded archive only after bounded transfer and verification."""
    destination.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(prefix='.download-', dir=destination.parent, delete=False) as output:
        temporary = Path(output.name)
        try:
            with urllib.request.urlopen(url, timeout=30) as response:
                total = 0
                while chunk := response.read(1024 * 1024):
                    total += len(chunk)
                    if total > MAX_BYTES:
                        raise ValueError('download exceeds 64 MiB')
                    output.write(chunk)
            output.flush()
            verify_checksum(temporary, checksum)
            temporary.replace(destination)
        finally:
            temporary.unlink(missing_ok=True)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--archive', type=Path)
    parser.add_argument('--destination', type=Path, default=FIRMWARE / '.cache' / 'stock')
    args = parser.parse_args()
    try:
        refuse_existing(args.destination)
        lock = json.loads(LOCK_PATH.read_text(encoding='utf-8'))
        archive = args.archive or FIRMWARE / '.cache' / (lock['commit'] + '.tar.gz')
        if args.archive is None and not archive.exists():
            download_archive(lock['archive_url'], archive, lock['archive_sha256'])
        prepared = prepare_source(archive, args.destination, lock['archive_sha256'], lock['archive_root'])
    except (OSError, ValueError, KeyError, urllib.error.URLError) as error:
        # Avoid URLs, local paths, or environment values in controlled diagnostics.
        message = str(error) if type(error) in (ValueError, FileExistsError) else 'source preparation failed'
        print(f'prepare: {message}', file=sys.stderr)
        return 1
    print(prepared)
    return 0


if __name__ == '__main__':
    sys.exit(main())
