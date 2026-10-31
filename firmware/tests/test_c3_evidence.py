"""C3 target/lock/layout rejection using synthetic image-header fixtures."""
import hashlib
import json
from pathlib import Path
import re
import shutil
import struct
import unittest
import test_experiment_evidence as existing

VARIANT = 'xmini-c3-family'
PARTITIONS = [('nvs', 1, 2, 0x9000, 0x4000), ('otadata', 1, 0, 0xd000, 0x2000),
              ('phy_init', 1, 1, 0xf000, 0x1000), ('ota_0', 0, 0x10, 0x20000, 0x2f0000),
              ('ota_1', 0, 0x11, 0x310000, 0x2f0000), ('assets', 1, 0x82, 0x600000, 0x200000)]


class C3EvidenceTests(unittest.TestCase):
    digest = existing.EvidenceTests.digest
    config = existing.EvidenceTests.config

    def setUp(self):
        existing.EvidenceTests.setUp(self)
        for name in ['dependencies.esp32c3.lock', 'dependencies.esp32c3.lock.sha256']:
            shutil.copyfile(existing.FIRMWARE/name, self.firmware/name)
        shutil.copyfile(self.firmware/'dependencies.esp32c3.lock', self.tree/'dependencies.lock')
        provenance = self.tree/'.family-robot-source.json'
        data = json.loads(provenance.read_text())
        data['dependencies_sha256'] = self.digest(self.tree/'dependencies.lock')
        version = self.lock.get('esp_sr_version_esp32c3', self.lock['esp_sr_version'])
        if version != self.lock['esp_sr_version']:
            data['target_dependency_overrides'] = {'espressif/esp-sr': version}
            manifest = self.tree/'main/idf_component.yml'
            manifest.parent.mkdir()
            manifest.write_text(f'dependencies:\n  espressif/esp-sr: =={version}\n')
        provenance.write_text(json.dumps(data))
        options = {
            'CONFIG_IDF_TARGET': '"esp32c3"', 'CONFIG_BOARD_TYPE_XMINI_C3': 'y',
            'CONFIG_XMINI_C3_BUTTON_ENTRY': 'y', 'CONFIG_WAKE_WORD_DISABLED': 'y',
            'CONFIG_XMINI_C3_VOICE_BARGE': 'y',
            'CONFIG_OTA_URL': '""',
            'CONFIG_ESPTOOLPY_FLASHSIZE_16MB': 'y', 'CONFIG_PARTITION_TABLE_CUSTOM': 'y',
            'CONFIG_BOOTLOADER_APP_ROLLBACK_ENABLE': 'y',
            'CONFIG_PARTITION_TABLE_OFFSET': '0x8000',
            'CONFIG_PARTITION_TABLE_CUSTOM_FILENAME': '"partitions/xmini-c3-family-16m.csv"',
            'CONFIG_ESP_CONSOLE_USB_SERIAL_JTAG': 'y', 'CONFIG_ESP_CONSOLE_SECONDARY_NONE': 'y',
        }
        for name in ['BOARD_TYPE_M5STACK_STOPWATCH', 'BOARD_TYPE_WAVESHARE_ESP32_S3_TOUCH_AMOLED_2_16',
                     'WAVESHARE_XIAOMAN_REALTIME', 'STOPWATCH_SOFTWARE_AEC', 'STOPWATCH_DEFAULT_REALTIME',
                     'XIAOMAN_ORB_UI', 'USE_AUDIO_PROCESSOR', 'SPIRAM', 'USE_DEVICE_AEC', 'USE_SERVER_AEC',
                     'USE_AUDIO_DEBUGGER', 'I2S_ISR_IRAM_SAFE', 'USE_ESP_WAKE_WORD', 'USE_AFE_WAKE_WORD',
                     'USE_CUSTOM_WAKE_WORD', 'ESP_CONSOLE_UART_DEFAULT', 'ESP_CONSOLE_SECONDARY_USB_SERIAL_JTAG']:
            options['CONFIG_'+name] = 'n'
        for name in ['SECURE_BOOT', 'SECURE_FLASH_ENC_ENABLED', 'BOOTLOADER_APP_ANTI_ROLLBACK']:
            options['CONFIG_'+name] = 'n'
        (self.tree/'sdkconfig').write_text('\n'.join(
            f'# {key} is not set' if value == 'n' else f'{key}={value}'
            for key, value in options.items())+'\n')
        self.table = self.tree/'build/partition_table/partition-table.bin'
        self.table.parent.mkdir(parents=True)
        self.write_table(PARTITIONS)
        header = bytearray(64); header[0] = 0xe9
        struct.pack_into('<H', header, 12, 5)
        self.app = self.tree/'build/xiaozhi.bin'; self.app.write_bytes(header)
        boot = self.tree/'build/bootloader/bootloader.bin'; boot.parent.mkdir(); boot.write_bytes(header)
        (self.tree/'build/generated_assets.bin').write_bytes(b'synthetic assets')
        (self.tree/'build/flasher_args.json').write_text(json.dumps(dict(flash_files={
            '0x0': 'bootloader/bootloader.bin', '0x8000': 'partition_table/partition-table.bin',
            '0x20000': 'xiaozhi.bin', '0x600000': 'generated_assets.bin'})))
        csv = self.tree/'partitions/xmini-c3-family-16m.csv'; csv.parent.mkdir(parents=True)
        csv.write_text('nvs,data,nvs,0x9000,0x4000,\notadata,data,ota,0xd000,0x2000,\n'
                       'phy_init,data,phy,0xf000,0x1000,\nota_0,app,ota_0,0x20000,0x2f0000,\n'
                       'ota_1,app,ota_1,,0x2f0000,\nassets,data,spiffs,0x600000,2048K,\n')

    def write_table(self, rows):
        data = b''.join(struct.pack('<HBBII16sI', 0x50aa, typ, sub, offset, size,
                                    name.encode(), 0) for name, typ, sub, offset, size in rows)
        data += b'\xeb\xeb'+b'\xff'*14+hashlib.md5(data).digest()
        self.table.write_bytes(data+b'\xff'*(3072-len(data)))

    def verify(self):
        return self.module.verify(self.firmware, self.tree, VARIANT, self.lock['idf_commit'])

    def c3_dependency_override(self, native_version='2.6.1'):
        self.lock['esp_sr_version_esp32c3'] = '2.6.1'
        (self.firmware/'upstream.lock.json').write_text(json.dumps(self.lock))
        native = self.tree/'dependencies.lock'
        content, count = re.subn(r'(^  espressif/esp-sr:\n.*?^    version: )\S+',
                                 lambda match: match[1] + native_version,
                                 native.read_text(), flags=re.M | re.S)
        self.assertEqual(count, 1)
        native.write_text(content)
        shutil.copyfile(native, self.firmware/'dependencies.esp32c3.lock')
        expected_hash = self.digest(native)
        (self.firmware/'dependencies.esp32c3.lock.sha256').write_text(expected_hash + '\n')
        provenance = self.tree/'.family-robot-source.json'
        data = json.loads(provenance.read_text())
        data['dependencies_sha256'] = expected_hash
        data['target_dependency_overrides'] = {'espressif/esp-sr': '2.6.1'}
        provenance.write_text(json.dumps(data))
        manifest = self.tree/'main/idf_component.yml'
        manifest.parent.mkdir(exist_ok=True)
        manifest.write_text('dependencies:\n  espressif/esp-sr: ==2.6.1\n')
        return manifest, provenance

    def test_exact_c3_target_lock_and_layout_are_accepted(self):
        evidence = self.verify()
        self.assertEqual(evidence['board'], 'xmini/c3')
        self.assertEqual(evidence['dependency_lock_source'], 'dependencies.esp32c3.lock')
        self.assertEqual(evidence['partition_layout']['app_limit'], 0x2f0000)
        self.assertEqual(evidence['effective_config']['CONFIG_USE_DEVICE_AEC'], 'n')

    def test_product_c3_evidence_requires_playback_voice_capture(self):
        config = self.tree/'sdkconfig'
        config.write_text(config.read_text().replace('CONFIG_XMINI_C3_VOICE_BARGE=y\n',
                                                    'CONFIG_XMINI_C3_VOICE_BARGE=n\n'))
        with self.assertRaisesRegex(ValueError, 'CONFIG_XMINI_C3_VOICE_BARGE'):
            self.verify()

    def test_c3_override_requires_reviewed_native_version_and_prepared_manifest(self):
        self.c3_dependency_override()
        evidence = self.verify()
        self.assertEqual(evidence['critical_versions']['espressif/esp-sr'], '2.6.1')

    def test_c3_override_rejects_old_critical_version_even_with_reviewed_native_bytes(self):
        self.c3_dependency_override(native_version='2.4.7')
        with self.assertRaisesRegex(ValueError, 'critical component version mismatch: espressif/esp-sr'):
            self.verify()

    def test_c3_override_rejects_missing_wrong_or_extra_provenance_record(self):
        _, provenance = self.c3_dependency_override()
        original = json.loads(provenance.read_text())
        for record in [None, {}, {'espressif/esp-sr': '2.4.7'},
                       {'espressif/esp-sr': '2.6.1', 'other/component': '1.0.0'}]:
            value = dict(original)
            if record is None:
                value.pop('target_dependency_overrides')
            else:
                value['target_dependency_overrides'] = record
            provenance.write_text(json.dumps(value))
            with self.subTest(record=record), self.assertRaisesRegex(ValueError, 'provenance'):
                self.verify()

    def test_c3_override_rejects_wrong_missing_duplicate_or_aliased_prepared_pin(self):
        manifest, _ = self.c3_dependency_override()
        for content in [None, 'dependencies:\n  espressif/esp-sr: ==2.4.7\n',
                        'dependencies:\n  other/component: ==2.6.1\n',
                        'dependencies:\n    espressif/esp-sr: ==2.6.1\n',
                        'dependencies:\n' + '  espressif/esp-sr: ==2.6.1\n' * 2,
                        'dependencies:\n  espressif/esp-sr: ==2.6.1\n  espressif/esp-sr: ==2.4.7\n']:
            if content is None:
                manifest.unlink()
            else:
                manifest.write_text(content)
            with self.subTest(content=content), self.assertRaisesRegex(ValueError, 'manifest'):
                self.verify()
        manifest.unlink()
        external = self.root/'external-manifest'
        external.write_text('dependencies:\n  espressif/esp-sr: ==2.6.1\n')
        manifest.symlink_to(external)
        with self.assertRaisesRegex(ValueError, 'manifest'):
            self.verify()

    def test_c3_override_rejects_nonexact_lock_version(self):
        self.c3_dependency_override()
        self.lock['esp_sr_version_esp32c3'] = '^2.6.1'
        (self.firmware/'upstream.lock.json').write_text(json.dumps(self.lock))
        with self.assertRaisesRegex(ValueError, 'version'):
            self.verify()

    def test_c3_override_keeps_native_byte_drift_gate(self):
        self.c3_dependency_override()
        path = self.tree/'dependencies.lock'
        path.write_bytes(path.read_bytes() + b'\n')
        with self.assertRaisesRegex(ValueError, 'native dependencies.lock drift'):
            self.verify()

    def test_s3_target_wake_aec_or_wrong_board_is_rejected(self):
        path = self.tree/'sdkconfig'; original = path.read_text()
        for before, after in [('"esp32c3"', '"esp32s3"'),
                              ('CONFIG_XMINI_C3_BUTTON_ENTRY=y', '# CONFIG_XMINI_C3_BUTTON_ENTRY is not set'),
                              ('# CONFIG_USE_DEVICE_AEC is not set', 'CONFIG_USE_DEVICE_AEC=y'),
                              ('# CONFIG_USE_ESP_WAKE_WORD is not set', 'CONFIG_USE_ESP_WAKE_WORD=y'),
                              ('# CONFIG_SECURE_BOOT is not set', 'CONFIG_SECURE_BOOT=y'),
                              ('# CONFIG_BOARD_TYPE_M5STACK_STOPWATCH is not set', 'CONFIG_BOARD_TYPE_M5STACK_STOPWATCH=y')]:
            path.write_text(original.replace(before, after))
            with self.subTest(option=before), self.assertRaises(ValueError): self.verify()

    def test_dependency_drift_and_wrong_native_target_fail_even_with_matching_pin(self):
        path = self.tree/'dependencies.lock'; original = path.read_bytes()
        path.write_bytes(original+b'\n')
        with self.assertRaises(ValueError): self.verify()
        wrong = original.replace(b'target: esp32c3', b'target: esp32s3')
        path.write_bytes(wrong)
        (self.firmware/'dependencies.esp32c3.lock').write_bytes(wrong)
        (self.firmware/'dependencies.esp32c3.lock.sha256').write_text(hashlib.sha256(wrong).hexdigest()+'\n')
        provenance = self.tree/'.family-robot-source.json'; value = json.loads(provenance.read_text())
        value['dependencies_sha256'] = hashlib.sha256(wrong).hexdigest(); provenance.write_text(json.dumps(value))
        with self.assertRaisesRegex(ValueError, 'target'): self.verify()

    def test_wrong_chip_layout_checksum_and_flasher_address_fail(self):
        original = self.app.read_bytes(); changed = bytearray(original); changed[12] = 9
        self.app.write_bytes(changed)
        with self.assertRaisesRegex(ValueError, 'chip'): self.verify()
        self.app.write_bytes(original)
        self.write_table(PARTITIONS[:4]+[('ota_1', 0, 0x11, 0x300000, 0x2f0000)]+PARTITIONS[5:])
        with self.assertRaises(ValueError): self.verify()
        self.write_table(PARTITIONS); data = bytearray(self.table.read_bytes()); data[40] ^= 1
        self.table.write_bytes(data)
        with self.assertRaises(ValueError): self.verify()
        self.write_table(PARTITIONS)
        flasher = self.tree/'build/flasher_args.json'
        flasher.write_text(flasher.read_text().replace('0x600000', '0x620000'))
        with self.assertRaises(ValueError): self.verify()

    def test_assets_above_c3_partition_capacity_are_rejected(self):
        asset = self.tree/'build/generated_assets.bin'
        with asset.open('wb') as output: output.truncate(0x200000+1)
        with self.assertRaisesRegex(ValueError, 'partition'): self.verify()

    def test_bundle_keeps_c3_native_pin_and_canonical_release_name(self):
        for relative in self.module.BUILD_FILES:
            path = self.tree/relative
            if not path.exists():
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(b'synthetic '+relative.encode())
        releases = self.tree/'releases'; releases.mkdir()
        name = 'v2.5.2_xmini-c3-family.zip'
        (releases/name).write_bytes(b'synthetic release')
        output = self.root/'evidence'
        self.module.bundle(self.firmware, self.tree, output, 'a'*40, self.verify())
        self.assertEqual((output/'dependencies.lock.sha256').read_text().strip(),
                         self.digest(output/'dependencies.lock'))
        self.assertNotEqual((output/'dependencies.lock.sha256').read_bytes(),
                            (self.firmware/'dependencies.lock.sha256').read_bytes())
        self.assertTrue((output/'releases'/name).is_file())
