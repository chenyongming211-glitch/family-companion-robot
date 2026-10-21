"""Behavioral tests using real tar archives, without network or SDK access."""
import hashlib
import importlib.util
import io
import json
import shutil
import subprocess
import sys
from pathlib import Path
import tarfile
import tempfile
import unittest
import zlib


SCRIPT = Path(__file__).resolve().parents[1] / 'tools' / 'prepare.py'


def archive_at(path, name='source/main/file.txt', kind=tarfile.REGTYPE):
    with tarfile.open(path, 'w:gz') as output:
        info = tarfile.TarInfo(name)
        info.type = kind
        if kind in (tarfile.SYMTYPE, tarfile.LNKTYPE):
            info.linkname = '../../outside'
            output.addfile(info)
        else:
            data = b'source contents'
            info.size = len(data) if kind == tarfile.REGTYPE else 0
            output.addfile(info, io.BytesIO(data))
    return hashlib.sha256(path.read_bytes()).hexdigest()


class PrepareTests(unittest.TestCase):
    def setUp(self):
        spec = importlib.util.spec_from_file_location('prepare', SCRIPT)
        self.prepare = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.prepare)
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.archive = self.root / 'source.tar.gz'
        self.destination = self.root / 'prepared'

    def run_prepare(self, checksum):
        return self.prepare.prepare_source(self.archive, self.destination, checksum, 'source')

    def test_extracts_nested_contents_and_strips_root(self):
        checksum = archive_at(self.archive)
        self.assertEqual(self.run_prepare(checksum), self.destination)
        self.assertEqual((self.destination / 'main/file.txt').read_bytes(), b'source contents')
        self.assertFalse((self.destination / 'source').exists())

    def test_checksum_mismatch_leaves_destination_absent(self):
        archive_at(self.archive)
        with self.assertRaises(ValueError):
            self.run_prepare('0' * 64)
        self.assertFalse(self.destination.exists())

    def test_rejects_traversal_before_extracting_any_member(self):
        checksum = archive_at(self.archive, 'source/../escape')
        with self.assertRaises(ValueError):
            self.run_prepare(checksum)
        self.assertFalse((self.root / 'escape').exists())
        self.assertFalse(self.destination.exists())

    def test_rejects_absolute_paths(self):
        checksum = archive_at(self.archive, '/source/escape')
        with self.assertRaises(ValueError):
            self.run_prepare(checksum)
        self.assertFalse(self.destination.exists())

    def test_rejects_symlink(self):
        checksum = archive_at(self.archive, kind=tarfile.SYMTYPE)
        with self.assertRaises(ValueError):
            self.run_prepare(checksum)
        self.assertFalse(self.destination.exists())

    def test_rejects_hardlink(self):
        checksum = archive_at(self.archive, kind=tarfile.LNKTYPE)
        with self.assertRaises(ValueError):
            self.run_prepare(checksum)
        self.assertFalse(self.destination.exists())

    def test_rejects_device_node(self):
        checksum = archive_at(self.archive, kind=tarfile.CHRTYPE)
        with self.assertRaises(ValueError):
            self.run_prepare(checksum)
        self.assertFalse(self.destination.exists())

    def test_rejects_unexpected_root(self):
        checksum = archive_at(self.archive, 'other/main/file.txt')
        with self.assertRaises(ValueError):
            self.run_prepare(checksum)
        self.assertFalse(self.destination.exists())

    def test_existing_destination_preserves_sentinel(self):
        checksum = archive_at(self.archive)
        self.destination.mkdir()
        sentinel = self.destination / 'sentinel'
        sentinel.write_text('keep me')
        with self.assertRaises(FileExistsError):
            self.run_prepare(checksum)
        self.assertEqual(sentinel.read_text(), 'keep me')
        self.assertEqual(list(self.destination.iterdir()), [sentinel])

    def test_dangling_destination_symlink_is_not_overwritten(self):
        checksum = archive_at(self.archive)
        self.destination.symlink_to(self.root / 'missing')
        with self.assertRaises(FileExistsError):
            self.run_prepare(checksum)
        self.assertTrue(self.destination.is_symlink())

    def test_rejects_archive_larger_than_64_mib(self):
        with self.archive.open('wb') as output:
            output.truncate(64 * 1024 * 1024 + 1)
        checksum = hashlib.sha256(self.archive.read_bytes()).hexdigest()
        with self.assertRaises(ValueError):
            self.run_prepare(checksum)
        self.assertFalse(self.destination.exists())

    def test_rejects_cumulative_declared_sizes_above_64_mib(self):
        data = b'0' * (33 * 1024 * 1024)
        with tarfile.open(self.archive, 'w:gz') as output:
            for name in ['source/one', 'source/two']:
                info = tarfile.TarInfo(name)
                info.size = len(data)
                output.addfile(info, io.BytesIO(data))
        checksum = hashlib.sha256(self.archive.read_bytes()).hexdigest()
        with self.assertRaises(ValueError):
            self.run_prepare(checksum)
        self.assertFalse(self.destination.exists())
        self.assertEqual(set(self.root.iterdir()), {self.archive})

    def test_download_keeps_only_verified_archive(self):
        checksum = archive_at(self.archive)
        downloaded = self.root / 'cache' / 'download.tar.gz'
        self.prepare.download_archive(self.archive.as_uri(), downloaded, checksum)
        self.assertEqual(downloaded.read_bytes(), self.archive.read_bytes())
        self.assertEqual(list(downloaded.parent.iterdir()), [downloaded])

    def test_failed_download_checksum_leaves_no_cached_file(self):
        archive_at(self.archive)
        downloaded = self.root / 'cache' / 'download.tar.gz'
        with self.assertRaises(ValueError):
            self.prepare.download_archive(self.archive.as_uri(), downloaded, '0' * 64)
        self.assertEqual(list(downloaded.parent.iterdir()), [])

    def test_download_limit_leaves_no_cached_file(self):
        with self.archive.open('wb') as output:
            output.truncate(64 * 1024 * 1024 + 1)
        downloaded = self.root / 'cache' / 'download.tar.gz'
        with self.assertRaises(ValueError):
            self.prepare.download_archive(self.archive.as_uri(), downloaded, '0' * 64)
        self.assertEqual(list(downloaded.parent.iterdir()), [])

    def test_cli_uses_adjacent_lock_from_another_cwd_and_prints_only_path(self):
        checksum = archive_at(self.archive)
        tools = self.root / 'firmware' / 'tools'
        tools.mkdir(parents=True)
        script = tools / 'prepare.py'
        shutil.copyfile(SCRIPT, script)
        (tools.parent / 'upstream.lock.json').write_text(json.dumps({
            'commit': 'fixture', 'archive_sha256': checksum,
            'archive_root': 'source', 'archive_url': self.archive.as_uri(),
        }))
        elsewhere = self.root / 'elsewhere'
        elsewhere.mkdir()
        (elsewhere / 'upstream.lock.json').write_text('invalid unrelated lock')
        result = subprocess.run([sys.executable, str(script)], cwd=elsewhere,
                                capture_output=True, text=True)
        prepared = tools.parent / '.cache' / 'stock'
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, str(prepared.resolve()) + '\n')
        self.assertEqual(result.stderr, '')
        self.assertEqual((prepared / 'main/file.txt').read_bytes(), b'source contents')
        result = subprocess.run([sys.executable, str(script)], cwd=elsewhere,
                                capture_output=True, text=True)
        self.assertEqual(result.returncode, 1)
        self.assertEqual(result.stdout, '')
        self.assertEqual(result.stderr, 'prepare: destination already exists\n')
        self.assertEqual((prepared / 'main/file.txt').read_bytes(), b'source contents')

    def test_late_deflate_corruption_is_a_controlled_validation_error(self):
        info = tarfile.TarInfo('source/file')
        info.size = 64 * 1024
        compressor = zlib.compressobj(wbits=31)
        valid_prefix = info.tobuf() + b'x' * info.size
        self.archive.write_bytes(compressor.compress(valid_prefix)
                                 + compressor.flush(zlib.Z_SYNC_FLUSH)
                                 + b'\xff')
        checksum = hashlib.sha256(self.archive.read_bytes()).hexdigest()
        with self.assertRaises(ValueError):
            self.run_prepare(checksum)
        self.assertEqual(set(self.root.iterdir()), {self.archive})

    def test_corrupt_deflate_is_a_controlled_validation_error(self):
        # Valid gzip header followed by an invalid deflate block type.
        self.archive.write_bytes(bytes.fromhex('1f8b0800000000000003ff') + b'0' * 16)
        checksum = hashlib.sha256(self.archive.read_bytes()).hexdigest()
        with self.assertRaises(ValueError):
            self.run_prepare(checksum)
        self.assertEqual(set(self.root.iterdir()), {self.archive})

    def test_bad_gzip_leaves_no_temporary_output(self):
        self.archive.write_bytes(b'not gzip')
        checksum = hashlib.sha256(self.archive.read_bytes()).hexdigest()
        with self.assertRaises(ValueError):
            self.run_prepare(checksum)
        self.assertEqual(set(self.root.iterdir()), {self.archive})


if __name__ == '__main__':
    unittest.main()
