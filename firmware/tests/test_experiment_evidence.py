"""Synthetic post-build evidence checks; these fixtures are not ESP images."""
import hashlib
import importlib.util
import json
from pathlib import Path
import shutil
import tempfile
import unittest

FIRMWARE = Path(__file__).resolve().parents[1]
SCRIPT = FIRMWARE/'tools/verify_experiment.py'


class EvidenceTests(unittest.TestCase):
    def setUp(self):
        self.assertTrue(SCRIPT.is_file(), 'experimental CI evidence verifier is missing')
        spec = importlib.util.spec_from_file_location('verify_experiment', SCRIPT)
        self.module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.module)
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.firmware = self.root/'firmware'
        self.firmware.mkdir()
        for name in ['dependencies.lock', 'dependencies.lock.sha256', 'upstream.lock.json']:
            shutil.copyfile(FIRMWARE/name, self.firmware/name)
        for directory in ['patches', 'overlays']:
            (self.firmware/directory).mkdir()
        (self.firmware/'patches/a.patch').write_bytes(b'synthetic patch')
        (self.firmware/'overlays/a.h').write_bytes(b'synthetic overlay')
        self.tree = self.root/'tree'
        self.tree.mkdir()
        shutil.copyfile(self.firmware/'dependencies.lock', self.tree/'dependencies.lock')
        self.lock = json.loads((self.firmware/'upstream.lock.json').read_text())
        provenance = dict(upstream_commit=self.lock['commit'], archive_sha256=self.lock['archive_sha256'],
                          patches=[dict(path='a.patch', sha256=self.digest(self.firmware/'patches/a.patch'))],
                          overlays=[dict(path='a.h', sha256=self.digest(self.firmware/'overlays/a.h'))],
                          dependencies_sha256=self.digest(self.tree/'dependencies.lock'))
        (self.tree/'.family-robot-source.json').write_text(json.dumps(provenance))
        self.config(True)

    def digest(self, path):
        return hashlib.sha256(path.read_bytes()).hexdigest()

    def config(self, experiment):
        options = {
            'CONFIG_IDF_TARGET': '"esp32s3"', 'CONFIG_BOARD_TYPE_M5STACK_STOPWATCH': 'y',
            'CONFIG_USE_AUDIO_PROCESSOR': 'y', 'CONFIG_SPIRAM': 'y',
            'CONFIG_I2S_ISR_IRAM_SAFE': 'n', 'CONFIG_USE_AUDIO_DEBUGGER': 'n',
            'CONFIG_USE_SERVER_AEC': 'n', 'CONFIG_BOOTLOADER_APP_ROLLBACK_ENABLE': 'y',
            'CONFIG_PARTITION_TABLE_OFFSET': '0x8000',
            'CONFIG_PARTITION_TABLE_CUSTOM_FILENAME': '"partitions/v2/16m.csv"',
            'CONFIG_STOPWATCH_SOFTWARE_AEC': 'y' if experiment else 'n',
            'CONFIG_USE_DEVICE_AEC': 'y' if experiment else 'n',
            'CONFIG_ESP_CONSOLE_UART_DEFAULT': 'n' if experiment else 'y',
            'CONFIG_ESP_CONSOLE_USB_SERIAL_JTAG': 'y' if experiment else 'n',
            'CONFIG_ESP_CONSOLE_SECONDARY_USB_SERIAL_JTAG': 'n' if experiment else 'y',
            'CONFIG_ESP_CONSOLE_SECONDARY_NONE': 'y' if experiment else 'n',
            'CONFIG_STOPWATCH_AEC_OFFSET_SAMPLES': '0',
        }
        (self.tree/'sdkconfig').write_text('\n'.join(f'# {k} is not set' if v == 'n' else f'{k}={v}'
                                                   for k, v in options.items())+'\n')

    def verify(self, variant='m5stack-stopwatch-software-aec'):
        return self.module.verify(self.firmware, self.tree, variant, self.lock['idf_commit'])

    def test_both_variants_validate_effective_configuration(self):
        config = self.tree/'sdkconfig'
        config.write_text(config.read_text().replace('# CONFIG_ESP_CONSOLE_SECONDARY_USB_SERIAL_JTAG is not set\n', ''))
        evidence = self.verify()
        self.assertEqual(evidence['critical_versions']['espressif/esp-sr'], '2.4.7')
        self.assertEqual(evidence['effective_config']['CONFIG_I2S_ISR_IRAM_SAFE'], 'n')
        self.config(False)
        config = self.tree/'sdkconfig'
        config.write_text(config.read_text().replace('# CONFIG_USE_DEVICE_AEC is not set\n', ''))
        self.assertEqual(self.verify('m5stack-stopwatch')['effective_config']['CONFIG_STOPWATCH_SOFTWARE_AEC'], 'n')

    def test_lock_drift_wrong_sdk_and_changed_provenance_fail(self):
        with self.assertRaises(ValueError):
            self.module.verify(self.firmware, self.tree, 'm5stack-stopwatch-software-aec', '0'*40)
        original = (self.tree/'dependencies.lock').read_bytes()
        (self.tree/'dependencies.lock').write_bytes(original+b'\n')
        with self.assertRaises(ValueError):
            self.verify()
        (self.tree/'dependencies.lock').write_bytes(original)
        (self.firmware/'overlays/a.h').write_bytes(b'changed after prepare')
        with self.assertRaises(ValueError):
            self.verify()

    def test_unsafe_or_missing_effective_config_and_offset_fail(self):
        original = (self.tree/'sdkconfig').read_text()
        for changed in [original.replace('# CONFIG_I2S_ISR_IRAM_SAFE is not set', 'CONFIG_I2S_ISR_IRAM_SAFE=y'),
                        original.replace('CONFIG_ESP_CONSOLE_USB_SERIAL_JTAG=y', ''),
                        original.replace('CONFIG_STOPWATCH_AEC_OFFSET_SAMPLES=0', 'CONFIG_STOPWATCH_AEC_OFFSET_SAMPLES=2401'),
                        original.replace('# CONFIG_USE_AUDIO_DEBUGGER is not set', 'CONFIG_USE_AUDIO_DEBUGGER=y')]:
            (self.tree/'sdkconfig').write_text(changed)
            with self.subTest(config=changed), self.assertRaises(ValueError):
                self.verify()

    def test_bundle_copies_required_evidence_and_hashes_every_file(self):
        for relative in self.module.BUILD_FILES:
            target = self.tree/relative
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(b'synthetic '+relative.encode())
        release = self.tree/'releases/v2.5.2_m5stack-stopwatch-software-aec.zip'
        release.parent.mkdir()
        release.write_bytes(b'synthetic release')
        output = self.root/'evidence'
        self.module.bundle(self.firmware, self.tree, output, 'a'*40, self.verify())
        hashes = (output/'SHA256SUMS').read_text().splitlines()
        self.assertEqual(len(hashes), len([p for p in output.rglob('*') if p.is_file()])-1)
        for row in hashes:
            digest, name = row.split('  ', 1)
            self.assertEqual(digest, self.digest(output/name))
        metadata = json.loads((output/'build-evidence.json').read_text())
        self.assertEqual(metadata['source_sha'], 'a'*40)
        self.assertEqual(metadata['acoustic_acceptance'], 'pending')
        self.assertEqual((output/'source/overlays/a.h').read_bytes(), b'synthetic overlay')


if __name__ == '__main__':
    unittest.main()
