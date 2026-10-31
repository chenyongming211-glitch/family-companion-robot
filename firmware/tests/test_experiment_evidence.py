"""Synthetic post-build evidence checks; these fixtures are not ESP images."""
import hashlib
import importlib.util
import json
from pathlib import Path
import shutil
import struct
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
        self.license_bytes = b'Synthetic retained license notice\nfixture copyright\n'
        self.notices_bytes = b'Synthetic retained third-party notices\nfixture attribution\n'
        (self.firmware/'third_party').mkdir()
        (self.firmware/'third_party/xiaozhi-LICENSE').write_bytes(self.license_bytes)
        (self.root/'THIRD_PARTY_NOTICES.txt').write_bytes(self.notices_bytes)
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
            'CONFIG_STOPWATCH_DEFAULT_REALTIME': 'n',
            'CONFIG_XIAOMAN_ORB_UI': 'n',
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

    def test_s3_ignores_optional_c3_dependency_override_and_keeps_old_pin(self):
        original = self.verify()
        provenance = (self.tree/'.family-robot-source.json').read_bytes()
        manifest = self.tree/'main/idf_component.yml'
        manifest.parent.mkdir()
        manifest.write_text('dependencies:\n  espressif/esp-sr: ==2.4.7\n')
        for version in ['2.6.1', 'invalid-unused-value']:
            self.lock['esp_sr_version_esp32c3'] = version
            (self.firmware/'upstream.lock.json').write_text(json.dumps(self.lock))
            self.assertEqual(self.verify(), original)
            self.assertEqual((self.tree/'.family-robot-source.json').read_bytes(), provenance)
            self.assertEqual(manifest.read_text(), 'dependencies:\n  espressif/esp-sr: ==2.4.7\n')

    def xiaoman_assets(self, command='xiao man', model='mn7_cn'):
        index = json.dumps(dict(version=1, srmodels='srmodels.bin', multinet_model=dict(
            language='cn', duration=3000, threshold=0.2,
            commands=[dict(command=command, text='小满', action='wake')]))).encode()
        # Native srmodels table with two bounded, nonempty synthetic payloads.
        entries = [(model, 'weights'), ('fst', 'tokens')]
        header_size = 4 + len(entries) * (32 + 4 + 32 + 4 + 4)
        models = bytearray(struct.pack('<I', len(entries)))
        payload = bytearray()
        for name, file in entries:
            models.extend(struct.pack('<32sI32sII', name.encode(), 1, file.encode(),
                                      header_size + len(payload), 4))
            payload.extend(b'test')
        models.extend(payload)
        records = bytearray()
        data = bytearray()
        for name, content in [('index.json', index), ('srmodels.bin', models)]:
            records.extend(struct.pack('<32sIIHH', name.encode(), len(content), len(data), 0, 0))
            data.extend(b'\x5a\x5a' + content)
        body = records + data
        asset = self.tree/'build/generated_assets.bin'
        asset.parent.mkdir(exist_ok=True)
        asset.write_bytes(struct.pack('<III', 2, sum(body) & 0xffff, len(body)) + body)
        config = self.tree/'sdkconfig'
        config.write_text(config.read_text().replace('# CONFIG_STOPWATCH_DEFAULT_REALTIME is not set',
                                                    'CONFIG_STOPWATCH_DEFAULT_REALTIME=y').replace('# CONFIG_XIAOMAN_ORB_UI is not set',
                                                    'CONFIG_XIAOMAN_ORB_UI=y') + '\n'.join([
            'CONFIG_USE_DEFAULT_MESSAGE_STYLE=y', 'CONFIG_USE_CUSTOM_WAKE_WORD=y', '# CONFIG_USE_AFE_WAKE_WORD is not set',
            'CONFIG_SR_MN_CN_MULTINET7_QUANT=y', 'CONFIG_CUSTOM_WAKE_WORD="xiao man"',
            'CONFIG_CUSTOM_WAKE_WORD_DISPLAY="小满"', 'CONFIG_CUSTOM_WAKE_WORD_THRESHOLD=20',
            'CONFIG_SEND_WAKE_WORD_DATA=y']) + '\n')
        return asset

    def test_xiaoman_requires_acoustic_model_and_command_in_packed_assets(self):
        self.xiaoman_assets()
        evidence = self.verify('m5stack-stopwatch-xiaoman')
        self.assertEqual(evidence['wake_word']['command'], 'xiao man')
        self.assertEqual(evidence['wake_word']['models'], ['fst', 'mn7_cn'])

    def test_xiaoman_rejects_label_only_change_and_wrong_model_or_config(self):
        self.xiaoman_assets(command='ni hao xiao zhi')
        with self.assertRaisesRegex(ValueError, 'wake word'):
            self.verify('m5stack-stopwatch-xiaoman')
        self.config(True)
        self.xiaoman_assets(model='mn6_cn')
        with self.assertRaisesRegex(ValueError, 'model'):
            self.verify('m5stack-stopwatch-xiaoman')
        self.config(True)
        self.xiaoman_assets()
        config = self.tree/'sdkconfig'
        config.write_text(config.read_text().replace('CONFIG_SR_MN_CN_MULTINET7_QUANT=y',
                                                    '# CONFIG_SR_MN_CN_MULTINET7_QUANT is not set'))
        with self.assertRaisesRegex(ValueError, 'CONFIG_SR_MN_CN_MULTINET7_QUANT'):
            self.verify('m5stack-stopwatch-xiaoman')

    def test_xiaoman_rejects_corrupt_assets(self):
        asset = self.xiaoman_assets()
        asset.write_bytes(asset.read_bytes()[:-1])
        with self.assertRaisesRegex(ValueError, 'assets'):
            self.verify('m5stack-stopwatch-xiaoman')

    def test_realtime_boot_intent_is_scoped_to_xiaoman(self):
        for experiment, variant in [(False, 'm5stack-stopwatch'),
                                    (True, 'm5stack-stopwatch-software-aec')]:
            self.config(experiment)
            config = self.tree/'sdkconfig'
            config.write_text(config.read_text().replace('# CONFIG_STOPWATCH_DEFAULT_REALTIME is not set',
                                                        'CONFIG_STOPWATCH_DEFAULT_REALTIME=y'))
            with self.subTest(variant=variant), self.assertRaisesRegex(ValueError, 'CONFIG_STOPWATCH_DEFAULT_REALTIME'):
                self.verify(variant)
        self.config(True)
        self.xiaoman_assets()
        config = self.tree/'sdkconfig'
        config.write_text(config.read_text().replace('# CONFIG_STOPWATCH_DEFAULT_REALTIME is not set',
                                                    'CONFIG_STOPWATCH_DEFAULT_REALTIME=y'))
        self.assertEqual(self.verify('m5stack-stopwatch-xiaoman')['effective_config']['CONFIG_STOPWATCH_DEFAULT_REALTIME'], 'y')

    def test_native_style_identical_sdk_config_repeats_are_accepted(self):
        config = self.tree/'sdkconfig'
        config.write_text(config.read_text()
                          + 'CONFIG_ESP32_WIFI_STATIC_RX_BUFFER_NUM=3\n'
                          + 'CONFIG_ESP32_WIFI_STATIC_RX_BUFFER_NUM=3\n'
                          + 'CONFIG_USE_DEVICE_AEC=y\n'
                          + '# CONFIG_I2S_ISR_IRAM_SAFE is not set\n')
        evidence = self.verify()
        self.assertEqual(evidence['effective_config']['CONFIG_USE_DEVICE_AEC'], 'y')
        self.assertEqual(evidence['effective_config']['CONFIG_I2S_ISR_IRAM_SAFE'], 'n')

    def test_conflicting_critical_sdk_config_repeats_are_rejected(self):
        config = self.tree/'sdkconfig'
        config.write_text(config.read_text() + '# CONFIG_USE_DEVICE_AEC is not set\n')
        with self.assertRaisesRegex(ValueError, 'conflicting SDK config key: CONFIG_USE_DEVICE_AEC'):
            self.verify()

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
        self.assertTrue((output/'third_party/xiaozhi-LICENSE').is_file())
        self.assertEqual((output/'third_party/xiaozhi-LICENSE').read_bytes(), self.license_bytes)
        self.assertTrue((output/'THIRD_PARTY_NOTICES.txt').is_file())
        self.assertEqual((output/'THIRD_PARTY_NOTICES.txt').read_bytes(), self.notices_bytes)
        hashes = (output/'SHA256SUMS').read_text().splitlines()
        names = {row.split('  ', 1)[1] for row in hashes}
        self.assertTrue({'third_party/xiaozhi-LICENSE', 'THIRD_PARTY_NOTICES.txt'} <= names)
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
