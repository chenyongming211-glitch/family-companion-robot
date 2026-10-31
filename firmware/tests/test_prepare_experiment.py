"""Exercise atomic experimental preparation with real archives and git apply."""
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch


FIRMWARE = Path(__file__).resolve().parents[1]
SCRIPT = FIRMWARE / 'tools/prepare_experiment.py'


def sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def value_patch(before='old', after='new', path='main/value.txt'):
    return (f'diff --git a/{path} b/{path}\n--- a/{path}\n+++ b/{path}\n'
            f'@@ -1 +1 @@\n-{before}\n+{after}\n')


class ExperimentTests(unittest.TestCase):
    def setUp(self):
        spec = importlib.util.spec_from_file_location('prepare_experiment', SCRIPT)
        self.module = importlib.util.module_from_spec(spec)
        sys.path.insert(0, str(SCRIPT.parent))
        try:
            spec.loader.exec_module(self.module)
        finally:
            sys.path.pop(0)
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.archive = self.root / 'source.tar.gz'
        with tarfile.open(self.archive, 'w:gz') as output:
            info = tarfile.TarInfo('source/main/value.txt')
            info.size = len(b'old\n')
            output.addfile(info, io.BytesIO(b'old\n'))
        self.lock = {'commit': 'fixture-commit', 'archive_root': 'source',
                     'archive_sha256': sha256(self.archive),
                     'archive_url': self.archive.as_uri()}
        self.patches = self.root / 'patches'
        self.patches.mkdir()
        (self.patches / '01-value.patch').write_text(value_patch())
        self.overlays = self.root / 'overlays'
        (self.overlays / 'main').mkdir(parents=True)
        (self.overlays / 'main/extra.h').write_bytes(b'fixture overlay\n')
        self.dependencies = self.root / 'dependencies.lock'
        self.dependencies.write_bytes(b'dependencies:\n  fixture:\n    version: 1.0.0\n')
        self.pin = self.root / 'dependencies.lock.sha256'
        self.pin.write_text(sha256(self.dependencies) + '\n')
        self.destination = self.root / 'output' / 'experiment'
        self.destination.parent.mkdir()
        self.sibling = self.destination.parent / '.prepare-experiment-unrelated'
        self.sibling.mkdir()
        (self.sibling / 'sentinel').write_bytes(b'keep temporary sibling\n')

    def prepare(self, target=None):
        options = {} if target is None else {'target': target}
        return self.module.prepare_experiment(
            self.archive, self.destination, self.lock, self.patches,
            self.overlays, self.dependencies, **options)

    def add_manifest(self, content=b'dependencies:\n  espressif/esp-sr: ==2.4.7\n'):
        with tarfile.open(self.archive, 'w:gz') as output:
            for name, data in [('main/value.txt', b'old\n'), ('main/idf_component.yml', content)]:
                info = tarfile.TarInfo('source/' + name)
                info.size = len(data)
                output.addfile(info, io.BytesIO(data))
        self.lock['archive_sha256'] = sha256(self.archive)
        self.lock['esp_sr_version'] = '2.4.7'

    def prepared_bytes(self):
        return {path.relative_to(self.destination).as_posix(): path.read_bytes()
                for path in self.destination.rglob('*') if path.is_file()}

    def assert_clean_failure(self, error):
        with self.assertRaises(error):
            self.prepare()
        self.assertFalse(os.path.lexists(self.destination))
        self.assertEqual(list(self.destination.parent.iterdir()), [self.sibling])
        self.assertEqual((self.sibling / 'sentinel').read_bytes(), b'keep temporary sibling\n')

    def assert_prepared(self):
        self.assertEqual((self.destination / 'main/value.txt').read_bytes(), b'new\n')
        self.assertEqual((self.destination / 'main/extra.h').read_bytes(), b'fixture overlay\n')
        self.assertEqual((self.destination / 'dependencies.lock').read_bytes(),
                         self.dependencies.read_bytes())
        provenance = json.loads((self.destination / '.family-robot-source.json').read_text())
        self.assertEqual(provenance['upstream_commit'], self.lock['commit'])
        self.assertEqual(provenance['archive_sha256'], sha256(self.archive))
        self.assertEqual(provenance['patches'], [
            {'path': '01-value.patch', 'sha256': sha256(self.patches / '01-value.patch')}])
        self.assertEqual(provenance['overlays'], [
            {'path': 'main/extra.h', 'sha256': sha256(self.overlays / 'main/extra.h')}])
        self.assertEqual(provenance['dependencies_sha256'], sha256(self.dependencies))

    def test_real_patch_overlay_lock_and_provenance(self):
        self.assertEqual(self.prepare(), self.destination)
        self.assert_prepared()

    def test_c3_override_changes_only_fresh_manifest_and_provenance(self):
        self.add_manifest()
        self.prepare()
        original = self.prepared_bytes()
        source_bytes = self.archive.read_bytes()
        dependency_bytes = self.dependencies.read_bytes()
        self.destination = self.destination.with_name('c3')
        self.lock['esp_sr_version_esp32c3'] = '2.6.1'
        self.prepare(target='esp32c3')
        changed = self.prepared_bytes()
        self.assertEqual({name for name in original if original[name] != changed[name]},
                         {'main/idf_component.yml', '.family-robot-source.json'})
        self.assertEqual(changed['main/idf_component.yml'],
                         b'dependencies:\n  espressif/esp-sr: ==2.6.1\n')
        provenance = json.loads(changed['.family-robot-source.json'])
        self.assertEqual(provenance.pop('target_dependency_overrides'),
                         {'espressif/esp-sr': '2.6.1'})
        self.assertEqual(provenance, json.loads(original['.family-robot-source.json']))
        self.assertEqual(self.archive.read_bytes(), source_bytes)
        self.assertEqual(self.dependencies.read_bytes(), dependency_bytes)

    def test_s3_ignores_c3_override_with_byte_identical_outputs(self):
        self.add_manifest()
        self.prepare()
        original = self.prepared_bytes()
        for override in ['2.6.1', 'invalid-unused-value']:
            self.destination = self.destination.with_name('s3-' + override)
            self.lock['esp_sr_version_esp32c3'] = override
            self.prepare(target='esp32s3')
            self.assertEqual(self.prepared_bytes(), original)

    def test_c3_override_replaces_only_the_exact_manifest_line(self):
        self.add_manifest(b'dependencies:\n  espressif/esp-sr: ==2.4.7\n'
                          b'#  espressif/esp-sr: ==2.4.7\n')
        self.lock['esp_sr_version_esp32c3'] = '2.6.1'
        self.prepare(target='esp32c3')
        self.assertEqual((self.destination / 'main/idf_component.yml').read_bytes(),
                         b'dependencies:\n  espressif/esp-sr: ==2.6.1\n'
                         b'#  espressif/esp-sr: ==2.4.7\n')

    def test_c3_without_override_or_with_same_version_preserves_outputs(self):
        self.add_manifest()
        self.prepare()
        original = self.prepared_bytes()
        for override in [None, '2.4.7']:
            self.destination = self.destination.with_name('c3-' + str(override))
            if override is not None:
                self.lock['esp_sr_version_esp32c3'] = override
            self.prepare(target='esp32c3')
            self.assertEqual(self.prepared_bytes(), original)

    def test_c3_without_override_does_not_require_unused_global_version(self):
        self.prepare()
        original = self.prepared_bytes()
        self.destination = self.destination.with_name('c3-no-version')
        self.prepare(target='esp32c3')
        self.assertEqual(self.prepared_bytes(), original)

    def test_c3_override_rejects_wrong_missing_or_duplicate_manifest_pin_atomically(self):
        for content in [b'dependencies:\n  espressif/esp-sr: ==2.4.6\n',
                        b'dependencies:\n  other/component: ==2.4.7\n',
                        b'dependencies:\n    espressif/esp-sr: ==2.4.7\n',
                        b'dependencies:\n' + b'  espressif/esp-sr: ==2.4.7\n' * 2,
                        b'dependencies:\n  espressif/esp-sr: ==2.4.7\n  espressif/esp-sr: ==2.6.1\n']:
            self.add_manifest(content)
            self.lock['esp_sr_version_esp32c3'] = '2.6.1'
            with self.subTest(content=content), self.assertRaises(ValueError):
                self.prepare(target='esp32c3')
            self.assertFalse(os.path.lexists(self.destination))
            self.assertEqual(list(self.destination.parent.iterdir()), [self.sibling])

    def test_c3_override_rejects_nonexact_versions_atomically(self):
        self.add_manifest()
        for version in ['^2.6.1', '2.6', '2.6.1\n', '2.6.1; other/component: ==1.0.0',
                        '２.６.１', '', None, 261, {'espressif/esp-sr': '2.6.1'}]:
            self.lock['esp_sr_version_esp32c3'] = version
            with self.subTest(version=version), self.assertRaises(ValueError):
                self.prepare(target='esp32c3')
            self.assertFalse(os.path.lexists(self.destination))
            self.assertEqual(list(self.destination.parent.iterdir()), [self.sibling])

    def test_c3_override_validates_global_pin_version_atomically(self):
        self.add_manifest()
        self.lock.update(esp_sr_version='^2.4.7', esp_sr_version_esp32c3='2.6.1')
        with self.assertRaises(ValueError):
            self.prepare(target='esp32c3')
        self.assertFalse(os.path.lexists(self.destination))

    def test_parent_git_repository_and_inherited_git_overrides_are_untouched(self):
        subprocess.run(['git', 'init', '-q', str(self.root)], check=True)
        sentinel = self.root / 'main/value.txt'
        sentinel.parent.mkdir()
        sentinel.write_bytes(b'old\n')
        subprocess.run(['git', '-C', str(self.root), 'add', 'main/value.txt'], check=True)
        index = self.root / '.git/index'
        index_before = index.read_bytes()
        elsewhere = self.root / 'elsewhere'
        elsewhere.mkdir()
        previous = Path.cwd()
        try:
            os.chdir(elsewhere)
            with patch.dict(os.environ, {'GIT_DIR': str(self.root / '.git'),
                                         'GIT_WORK_TREE': str(self.root),
                                         'GIT_INDEX_FILE': str(index),
                                         'GIT_CONFIG_COUNT': '1',
                                         'GIT_CONFIG_KEY_0': 'apply.whitespace',
                                         'GIT_CONFIG_VALUE_0': 'nowarn'}):
                self.prepare()
        finally:
            os.chdir(previous)
        self.assert_prepared()
        self.assertEqual(sentinel.read_bytes(), b'old\n')
        self.assertEqual(index.read_bytes(), index_before)

    def test_failed_patch_context_is_atomic(self):
        (self.patches / '01-value.patch').write_text(value_patch(before='missing'))
        self.assert_clean_failure(subprocess.CalledProcessError)

    def test_bad_archive_checksum_is_atomic(self):
        self.lock['archive_sha256'] = '0' * 64
        self.assert_clean_failure(ValueError)

    def test_overlay_collision_is_atomic(self):
        (self.overlays / 'main/value.txt').write_text('collision\n')
        self.assert_clean_failure(FileExistsError)

    def test_dependency_output_overlay_is_rejected_atomically(self):
        (self.overlays / 'dependencies.lock').write_bytes(
            b'dependencies:\n  other:\n    version: 9.9.9\n')
        self.assert_clean_failure(FileExistsError)

    def test_provenance_output_overlay_is_rejected_atomically(self):
        (self.overlays / '.family-robot-source.json').write_text('{}\n')
        self.assert_clean_failure(FileExistsError)

    def add_alias_overlay(self, name):
        (self.patches / '02-alias.patch').write_text(
            'diff --git a/alias b/alias\n'
            'new file mode 120000\n'
            'index 0000000..945c9b4\n'
            '--- /dev/null\n'
            '+++ b/alias\n'
            '@@ -0,0 +1 @@\n'
            '+.\n'
            '\\ No newline at end of file\n')
        alias = self.overlays / 'alias'
        alias.mkdir()
        (alias / name).write_bytes(b'overlay aliases generated root output\n')

    def test_patch_symlink_cannot_alias_dependency_output(self):
        self.add_alias_overlay('dependencies.lock')
        self.assert_clean_failure((ValueError, FileExistsError))

    def test_patch_symlink_cannot_alias_provenance_output(self):
        self.add_alias_overlay('.family-robot-source.json')
        self.assert_clean_failure((ValueError, FileExistsError))

    def test_overlay_cannot_traverse_patch_symlink_for_ordinary_file(self):
        self.add_alias_overlay('ordinary.h')
        self.assert_clean_failure(ValueError)

    def test_existing_destination_preserves_sentinel(self):
        self.destination.mkdir()
        sentinel = self.destination / 'sentinel'
        sentinel.write_bytes(b'keep destination\n')
        with self.assertRaises(FileExistsError):
            self.prepare()
        self.assertEqual(list(self.destination.iterdir()), [sentinel])
        self.assertEqual(sentinel.read_bytes(), b'keep destination\n')
        self.assertEqual(set(self.destination.parent.iterdir()), {self.destination, self.sibling})

    def test_dangling_destination_symlink_is_preserved(self):
        self.destination.symlink_to(self.root / 'missing')
        with self.assertRaises(FileExistsError):
            self.prepare()
        self.assertTrue(self.destination.is_symlink())
        self.assertEqual(set(self.destination.parent.iterdir()), {self.destination, self.sibling})

    def test_overlay_file_symlink_is_rejected(self):
        (self.overlays / 'main/extra.h').unlink()
        (self.overlays / 'main/extra.h').symlink_to(self.dependencies)
        self.assert_clean_failure(ValueError)

    def test_overlay_directory_symlink_is_rejected(self):
        (self.overlays / 'linked').symlink_to(self.root, target_is_directory=True)
        self.assert_clean_failure(ValueError)

    def test_patch_symlink_is_rejected(self):
        (self.patches / '02-link.patch').symlink_to(self.patches / '01-value.patch')
        self.assert_clean_failure(ValueError)

    def test_wrong_dependency_checksum_is_atomic(self):
        self.pin.write_text('0' * 64 + '\n')
        self.assert_clean_failure(ValueError)

    def test_dependency_symlink_is_rejected(self):
        original = self.root / 'original.lock'
        self.dependencies.rename(original)
        self.dependencies.symlink_to(original)
        self.assert_clean_failure(ValueError)

    def test_oversized_dependency_is_rejected(self):
        with self.dependencies.open('wb') as output:
            output.truncate(1024 * 1024 + 1)
        self.pin.write_text(sha256(self.dependencies) + '\n')
        self.assert_clean_failure(ValueError)

    def test_two_patches_are_applied_in_lexical_order(self):
        (self.patches / '02-value.patch').write_text(value_patch('new', 'last'))
        self.prepare()
        self.assertEqual((self.destination / 'main/value.txt').read_bytes(), b'last\n')
        provenance = json.loads((self.destination / '.family-robot-source.json').read_text())
        self.assertEqual([entry['path'] for entry in provenance['patches']],
                         ['01-value.patch', '02-value.patch'])

    def test_missing_or_empty_patch_directory_is_valid(self):
        (self.patches / '01-value.patch').unlink()
        self.prepare()
        self.assertEqual((self.destination / 'main/value.txt').read_bytes(), b'old\n')
        shutil.rmtree(self.destination)
        self.patches.rmdir()
        self.prepare()
        self.assertEqual((self.destination / 'main/value.txt').read_bytes(), b'old\n')

    def test_path_traversal_patch_cannot_mutate_external_file(self):
        external = self.destination.parent / 'sentinel'
        external.write_bytes(b'old\n')
        (self.patches / '01-value.patch').write_text(value_patch(path='../sentinel'))
        with self.assertRaises(subprocess.CalledProcessError):
            self.prepare()
        self.assertEqual(external.read_bytes(), b'old\n')
        self.assertFalse(self.destination.exists())
        self.assertEqual(set(self.destination.parent.iterdir()), {external, self.sibling})

    def make_cli_fixture(self):
        tools = self.root / 'firmware/tools'
        tools.mkdir(parents=True)
        for name in ['prepare.py', 'prepare_experiment.py']:
            shutil.copyfile(FIRMWARE / 'tools' / name, tools / name)
        (tools.parent / 'upstream.lock.json').write_text(json.dumps(self.lock))
        cache = tools.parent / '.cache'
        cache.mkdir()
        shutil.copyfile(self.archive, cache / (self.lock['commit'] + '.tar.gz'))
        shutil.copytree(self.patches, tools.parent / 'patches')
        shutil.copytree(self.overlays, tools.parent / 'overlays')
        shutil.copyfile(self.dependencies, tools.parent / self.dependencies.name)
        shutil.copyfile(self.pin, tools.parent / self.pin.name)
        return tools / 'prepare_experiment.py'

    def test_cli_defaults_resolve_adjacent_inputs_from_unrelated_cwd(self):
        script = self.make_cli_fixture()
        elsewhere = self.root / 'elsewhere'
        elsewhere.mkdir()
        (elsewhere / 'upstream.lock.json').write_text('unrelated invalid JSON')
        result = subprocess.run([sys.executable, str(script)], cwd=elsewhere,
                                capture_output=True, text=True, check=False)
        self.destination = script.parent.parent / '.cache/experiment'
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, str(self.destination.resolve()) + '\n')
        self.assertEqual(result.stderr, '')
        self.assert_prepared()

    def test_cli_patch_error_is_controlled_and_omits_private_paths(self):
        (self.patches / '01-value.patch').write_text(value_patch(before='private-context'))
        script = self.make_cli_fixture()
        result = subprocess.run([sys.executable, str(script)], cwd=self.root,
                                capture_output=True, text=True, check=False)
        self.assertEqual(result.returncode, 1)
        self.assertEqual(result.stdout, '')
        self.assertEqual(result.stderr, 'prepare-experiment: experimental preparation failed\n')
        self.assertFalse((script.parent.parent / '.cache/experiment').exists())

    def test_cli_c3_uses_separate_pinned_dependency_bytes(self):
        script = self.make_cli_fixture()
        native = b'dependencies:\n  fixture:\n    version: 1.0.0\ntarget: esp32c3\n'
        lock = script.parent.parent / 'dependencies.esp32c3.lock'
        lock.write_bytes(native)
        lock.with_name(lock.name + '.sha256').write_text(hashlib.sha256(native).hexdigest() + '\n')
        result = subprocess.run([sys.executable, str(script), '--target', 'esp32c3'],
                                cwd=self.root, capture_output=True, text=True, check=False)
        self.assertEqual(result.returncode, 0, result.stderr)
        prepared = script.parent.parent / '.cache/experiment'
        self.assertEqual((prepared / 'dependencies.lock').read_bytes(), native)
        provenance = json.loads((prepared / '.family-robot-source.json').read_text())
        self.assertEqual(provenance['dependencies_sha256'], hashlib.sha256(native).hexdigest())
        self.assertEqual((script.parent.parent / 'dependencies.lock').read_bytes(), self.dependencies.read_bytes())

    def test_cli_c3_passes_target_to_manifest_preparation(self):
        self.add_manifest()
        self.lock['esp_sr_version_esp32c3'] = '2.6.1'
        script = self.make_cli_fixture()
        native = script.parent.parent / 'dependencies.esp32c3.lock'
        shutil.copyfile(self.dependencies, native)
        native.with_name(native.name + '.sha256').write_text(sha256(native) + '\n')
        result = subprocess.run([sys.executable, str(script), '--target', 'esp32c3'],
                                cwd=self.root, capture_output=True, text=True, check=False)
        self.assertEqual(result.returncode, 0, result.stderr)
        prepared = script.parent.parent / '.cache/experiment'
        self.assertEqual((prepared / 'main/idf_component.yml').read_bytes(),
                         b'dependencies:\n  espressif/esp-sr: ==2.6.1\n')


if __name__ == '__main__':
    unittest.main()
