"""Synthetic stock provenance/board/ZIP negatives; never hardware build proof."""
import hashlib
import io
import json
import subprocess
import sys
import tarfile
import unittest
import zipfile

import test_experiment_evidence as existing
import test_flash_evidence as flash

BOARD = 'waveshare/esp32-s3-touch-amoled-2.16'
VARIANT = 'esp32-s3-touch-amoled-2.16'


class StockWaveshareTests(unittest.TestCase):
    digest = existing.EvidenceTests.digest
    config = existing.EvidenceTests.config

    def setUp(self):
        existing.EvidenceTests.setUp(self)
        self.board = 'main/boards/'+BOARD
        self.sources = {self.board+'/config.json': json.dumps(dict(manufacturer='waveshare',
            target='esp32s3', builds=[dict(name=VARIANT)])).encode(),
            self.board+'/esp32-s3-touch-amoled-2.16.cc': b'// synthetic stock Wave board\n'}
        archive = self.firmware/'.cache'/ (self.lock['commit']+'.tar.gz')
        archive.parent.mkdir()
        with tarfile.open(archive, 'w:gz') as t:
            for name, data in self.sources.items():
                info = tarfile.TarInfo(self.lock['archive_root']+'/'+name); info.size = len(data)
                t.addfile(info, io.BytesIO(data))
                path = self.tree/name; path.parent.mkdir(parents=True, exist_ok=True); path.write_bytes(data)
        self.lock['archive_sha256'] = self.digest(archive)
        (self.firmware/'upstream.lock.json').write_text(json.dumps(self.lock))
        (self.tree/'sdkconfig').write_text('CONFIG_IDF_TARGET="esp32s3"\nCONFIG_BOARD_TYPE_WAVESHARE_ESP32_S3_TOUCH_AMOLED_2_16=y\n# CONFIG_BOARD_TYPE_M5STACK_STOPWATCH is not set\nCONFIG_USE_DEVICE_AEC=y\n')
        flash.fixture(self.tree)
        for relative in (*self.module.BUILD_FILES, *self.module.NATIVE_NETWORK_FILES):
            path = self.tree/relative
            if not path.exists(): path.parent.mkdir(parents=True, exist_ok=True); path.write_bytes(b'fixture')
        self.commands = self.tree/'build/compile_commands.json'
        self.commands.write_text(json.dumps([dict(file=str(self.tree/self.board/'esp32-s3-touch-amoled-2.16.cc'), directory=str(self.tree), command='c++ -c board.cc')]))
        self.release = self.tree/('releases/v2.5.2_waveshare-'+VARIANT+'.zip')
        self.release.parent.mkdir()
        with zipfile.ZipFile(self.release, 'w') as z: z.write(self.tree/'build/merged-binary.bin', 'merged-binary.bin')
        self.output = self.root/'stock-evidence'

    def bundle(self):
        with existing.mock.patch.object(self.module, 'verify_no_pcm', return_value={'fixture': True}):
            self.module.bundle_baseline(self.firmware, self.tree, self.output, 'a'*40, self.lock['idf_commit'])

    def test_stock_waveshare_preserves_canonical_zip_and_locked_board_provenance(self):
        self.bundle()
        evidence = json.loads((self.output/'build-evidence.json').read_text())
        self.assertEqual(evidence['variant'], 'stock-'+VARIANT)
        self.assertEqual(evidence['board'], BOARD)
        self.assertEqual(evidence['stock_source_inputs'], {k:self.digest(self.tree/k) for k in self.sources})
        self.assertEqual((self.output/self.release.relative_to(self.tree)).read_bytes(), self.release.read_bytes())

    def test_wrong_board_and_duplicate_configuration_reject_before_output(self):
        p=self.tree/'sdkconfig'; original=p.read_text()
        for changed in [original.replace('WAVESHARE_ESP32_S3_TOUCH_AMOLED_2_16=y','WAVESHARE_ESP32_S3_TOUCH_AMOLED_2_16=n'), original+'CONFIG_BOARD_TYPE_M5STACK_STOPWATCH=y\n', original+'# CONFIG_USE_DEVICE_AEC is not set\n']:
            p.write_text(changed)
            with self.subTest(config=changed), self.assertRaises(ValueError): self.bundle()
            self.assertFalse(self.output.exists())
        p.write_text(original)

    def test_changed_stock_source_or_uncompiled_board_rejected(self):
        p=self.tree/(self.board+'/config.json'); original=p.read_bytes();p.write_bytes(original+b' ')
        with self.assertRaisesRegex(ValueError, 'stock source input'): self.bundle()
        p.write_bytes(original);self.commands.write_text('[]')
        with self.assertRaisesRegex(ValueError, 'stock compiled board'): self.bundle()
        self.assertFalse(self.output.exists())

    def test_retired_m5_or_noncanonical_zip_rejected(self):
        self.release.rename(self.release.with_name('v2.5.2_m5stack-stopwatch.zip'))
        with self.assertRaises(ValueError): self.bundle()
        self.assertFalse(self.output.exists())


class SupportedHostTests(unittest.TestCase):
    def test_current_selection_keeps_shared_checks_without_retired_board_gates(self):
        import importlib.util
        spec=importlib.util.spec_from_file_location('supported_host', existing.FIRMWARE/'tools/run_host_tests.py')
        module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
        ids=[c.id() for c in module.cases(module.supported_suite())]
        self.assertTrue(any('test_waveshare_codec.' in x for x in ids))
        self.assertTrue(any('test_device_control.' in x for x in ids))
        self.assertTrue(any('test_onboarding_configure.' in x for x in ids))
        self.assertTrue(any('test_s3_default_heap.' in x for x in ids))
        self.assertFalse(any(x.startswith(('test_c3_', 'test_m5_', 'test_experiment_evidence.')) for x in ids))
        self.assertFalse(any('test_original_stopwatch_variant' in x or 'test_old_software_aec_variant' in x or 'test_m5_screen_real_nvs' in x for x in ids))

    def test_public_evidence_cli_rejects_retired_variants(self):
        for variant in ('m5stack-stopwatch', 'm5stack-stopwatch-software-aec', 'm5stack-stopwatch-xiaoman', 'xmini-c3-family'):
            result = subprocess.run([sys.executable, str(existing.SCRIPT), '--tree', 'absent', '--output', 'absent', '--source-sha', 'a'*40, '--idf-commit', 'a'*40, '--variant', variant], capture_output=True, text=True)
            self.assertEqual(result.returncode, 2)
            self.assertIn('invalid choice', result.stderr)
