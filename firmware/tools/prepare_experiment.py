#!/usr/bin/env python3
"""Atomically prepare locked upstream source with tracked patches and overlays."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import urllib.error

from prepare import FIRMWARE, LOCK_PATH, download_archive, prepare_source, refuse_existing


DEPENDENCY_MAX_BYTES = 1024 * 1024


def file_digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def require_regular(path: Path) -> None:
    if path.is_symlink() or not path.is_file():
        raise ValueError('input must be a regular file')


def prepare_experiment(archive: Path, destination: Path, lock: dict,
                       patches: Path, overlays: Path, dependencies: Path) -> Path:
    """Publish a fresh experimental tree only after all controlled inputs apply."""
    refuse_existing(destination)
    require_regular(dependencies)
    if dependencies.stat().st_size > DEPENDENCY_MAX_BYTES:
        raise ValueError('dependency lock exceeds 1 MiB')
    checksum_path = dependencies.with_name(dependencies.name + '.sha256')
    require_regular(checksum_path)
    if checksum_path.stat().st_size > 65:
        raise ValueError('invalid dependency checksum')
    checksum = checksum_path.read_text(encoding='ascii').strip()
    dependency_bytes = dependencies.read_bytes()
    dependency_digest = hashlib.sha256(dependency_bytes).hexdigest()
    if checksum != dependency_digest:
        raise ValueError('dependency checksum mismatch')

    if patches.is_symlink() or overlays.is_symlink():
        raise ValueError('symbolic input directory is not allowed')
    if patches.exists() and not patches.is_dir():
        raise ValueError('patch input must be a directory')
    if not overlays.is_dir():
        raise ValueError('overlay input must be a directory')
    patch_paths = sorted(patches.glob('*.patch'))
    for path in patch_paths:
        require_regular(path)
    overlay_paths = []
    for path in sorted(overlays.rglob('*')):
        if path.is_symlink():
            raise ValueError('symbolic overlay input is not allowed')
        if path.is_dir():
            continue
        require_regular(path)
        overlay_paths.append(path)

    provenance = {
        'upstream_commit': lock['commit'],
        'archive_sha256': lock['archive_sha256'],
        'patches': [],
        'overlays': [],
        'dependencies_sha256': dependency_digest,
    }
    destination.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='.prepare-experiment-', dir=destination.parent) as temporary:
        tree = Path(temporary) / 'source'
        prepare_source(archive, tree, lock['archive_sha256'], lock['archive_root'])
        git_environment = {key: value for key, value in os.environ.items()
                           if not key.startswith('GIT_')}
        git_environment['GIT_CEILING_DIRECTORIES'] = str(tree.parent.resolve())
        for path in patch_paths:
            digest = file_digest(path)
            for check in [True, False]:
                command = ['git', 'apply']
                if check:
                    command.append('--check')
                command.extend(['--whitespace=error-all', str(path.resolve())])
                subprocess.run(command, cwd=tree, env=git_environment, check=True,
                               capture_output=True, timeout=30)
            provenance['patches'].append({'path': path.relative_to(patches).as_posix(),
                                          'sha256': digest})
        for path in overlay_paths:
            relative = path.relative_to(overlays)
            if relative.as_posix() in ('dependencies.lock', '.family-robot-source.json'):
                raise FileExistsError('overlay collides with generated output')
            target = tree / relative
            # Even in-tree aliases can bypass reserved output paths and provenance.
            if any((tree / parent).is_symlink() for parent in relative.parents):
                raise ValueError('overlay target has a symbolic parent')
            if not target.parent.resolve().is_relative_to(tree.resolve()):
                raise ValueError('overlay target escapes prepared tree')
            refuse_existing(target)
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(path, target)
            provenance['overlays'].append({'path': relative.as_posix(),
                                           'sha256': file_digest(target)})
        # These records are controlled preparation output, never upstream overlays.
        refuse_existing(tree / '.family-robot-source.json')
        dependency_target = tree / 'dependencies.lock'
        if dependency_target.is_symlink():
            raise ValueError('symbolic dependency target is not allowed')
        dependency_target.write_bytes(dependency_bytes)
        (tree / '.family-robot-source.json').write_text(
            json.dumps(provenance, indent=2) + '\n', encoding='utf-8')
        refuse_existing(destination)
        tree.rename(destination)
    return destination


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--archive', type=Path)
    parser.add_argument('--destination', type=Path, default=FIRMWARE / '.cache/experiment')
    args = parser.parse_args()
    try:
        refuse_existing(args.destination)
        lock = json.loads(LOCK_PATH.read_text(encoding='utf-8'))
        archive = args.archive or FIRMWARE / '.cache' / (lock['commit'] + '.tar.gz')
        if args.archive is None and not archive.exists():
            download_archive(lock['archive_url'], archive, lock['archive_sha256'])
        prepared = prepare_experiment(
            archive, args.destination, lock, FIRMWARE / 'patches',
            FIRMWARE / 'overlays', FIRMWARE / 'dependencies.lock')
    except (OSError, ValueError, KeyError, TypeError, urllib.error.URLError,
            subprocess.SubprocessError):
        print('prepare-experiment: experimental preparation failed', file=sys.stderr)
        return 1
    print(prepared.resolve())
    return 0


if __name__ == '__main__':
    sys.exit(main())
