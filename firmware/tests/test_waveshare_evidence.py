"""Synthetic layout/evidence fixtures, never claimed to be ESP hardware images."""
import hashlib
import json
import struct
import unittest

import test_experiment_evidence as existing

VARIANT = 'esp32-s3-touch-amoled-2.16-xiaoman'
PARTITIONS = [('nvs', 1, 2, 0x9000, 0x6000), ('otadata', 1, 0, 0xf000, 0x2000),
              ('phy_init', 1, 1, 0x11000, 0x1000), ('ota_0', 0, 0x10, 0x20000, 0x300000),
              ('ota_1', 0, 0x11, 0x320000, 0x300000), ('assets', 1, 0x82, 0x620000, 0x600000),
              ('storage', 1, 0x82, 0xc20000, 0x300000)]


class WaveshareEvidenceTests(unittest.TestCase):
    digest = existing.EvidenceTests.digest
    config = existing.EvidenceTests.config
    xiaoman_assets = existing.EvidenceTests.xiaoman_assets

    test_s3_ignores_optional_c3_dependency_override_and_keeps_old_pin = existing.EvidenceTests.test_s3_ignores_optional_c3_dependency_override_and_keeps_old_pin
    test_native_style_identical_sdk_config_repeats_are_accepted = existing.EvidenceTests.test_native_style_identical_sdk_config_repeats_are_accepted
    test_conflicting_critical_sdk_config_repeats_are_rejected = existing.EvidenceTests.test_conflicting_critical_sdk_config_repeats_are_rejected
    test_bytecode_created_after_prepare_does_not_change_source_provenance = existing.EvidenceTests.test_bytecode_created_after_prepare_does_not_change_source_provenance

    def test_unsafe_or_missing_common_effective_config_is_rejected(self):
        config = self.tree/'sdkconfig'; original = config.read_text()
        # The retired software-AEC calibration offset is unused by Wave hardware AEC.
        for changed in [original.replace('# CONFIG_I2S_ISR_IRAM_SAFE is not set', 'CONFIG_I2S_ISR_IRAM_SAFE=y'),
                        original.replace('CONFIG_ESP_CONSOLE_USB_SERIAL_JTAG=y', ''),
                        original.replace('# CONFIG_USE_AUDIO_DEBUGGER is not set', 'CONFIG_USE_AUDIO_DEBUGGER=y')]:
            config.write_text(changed)
            with self.subTest(config=changed), self.assertRaises(ValueError): self.verify()

    def test_supported_s3_rejects_pcm_config_and_generated_trailer(self):
        config = self.tree/'sdkconfig'; original = config.read_text()
        config.write_text(original+'CONFIG_XMINI_C3_PCM=y\n')
        with self.assertRaisesRegex(ValueError, 'CONFIG_XMINI_C3_PCM'): self.verify()
        config.write_text(original)
        (self.tree/'build/family_c3_pcm').mkdir()
        with self.assertRaisesRegex(ValueError, 'non-PCM'): self.verify()

    def test_lock_sdk_and_source_provenance_rejections(self):
        with self.assertRaises(ValueError):
            self.module.verify(self.firmware, self.tree, VARIANT, '0'*40)
        original = (self.tree/'dependencies.lock').read_bytes()
        (self.tree/'dependencies.lock').write_bytes(original+b'\n')
        with self.assertRaises(ValueError): self.verify()
        (self.tree/'dependencies.lock').write_bytes(original)
        (self.firmware/'overlays/a.h').write_bytes(b'changed after prepare')
        with self.assertRaises(ValueError): self.verify()

    def setUp(self):
        existing.EvidenceTests.setUp(self)
        self.asset = self.xiaoman_assets()
        config = self.tree/'sdkconfig'
        config.write_text(config.read_text().replace('CONFIG_BOARD_TYPE_M5STACK_STOPWATCH=y',
            '# CONFIG_BOARD_TYPE_M5STACK_STOPWATCH is not set').replace('CONFIG_STOPWATCH_SOFTWARE_AEC=y',
            '# CONFIG_STOPWATCH_SOFTWARE_AEC is not set').replace('CONFIG_STOPWATCH_DEFAULT_REALTIME=y',
            '# CONFIG_STOPWATCH_DEFAULT_REALTIME is not set').replace('"partitions/v2/16m.csv"',
            '"partitions/waveshare-xiaoman-16m.csv"') +
            'CONFIG_BOARD_TYPE_WAVESHARE_ESP32_S3_TOUCH_AMOLED_2_16=y\nCONFIG_WAVESHARE_XIAOMAN_REALTIME=y\n'
            'CONFIG_ESPTOOLPY_FLASHSIZE_16MB=y\nCONFIG_PARTITION_TABLE_CUSTOM=y\n')
        self.table = self.tree/'build/partition_table/partition-table.bin'
        self.table.parent.mkdir(parents=True)
        self.write_table(PARTITIONS)
        (self.tree/'build/xiaozhi.bin').write_bytes(b'synthetic app')
        (self.tree/'build/flasher_args.json').write_text(json.dumps(dict(flash_files={
            '0x20000': 'xiaozhi.bin', '0x620000': 'generated_assets.bin'})))
        source = self.tree/'partitions/waveshare-xiaoman-16m.csv'
        source.parent.mkdir()
        source.write_bytes((self.module.FIRMWARE/'overlays/partitions/waveshare-xiaoman-16m.csv').read_bytes())

    def write_table(self, rows):
        data = b''.join(struct.pack('<HBBII16sI', 0x50aa, type_, subtype, offset, size,
                                    name.encode(), 0) for name, type_, subtype, offset, size in rows)
        data += b'\xeb\xeb'+b'\xff'*14+hashlib.md5(data).digest()
        self.table.write_bytes(data+b'\xff'*(3072-len(data)))

    def verify(self):
        return self.module.verify(self.firmware, self.tree, VARIANT, self.lock['idf_commit'])

    def test_separate_board_hardware_aec_realtime_models_and_partition_evidence(self):
        evidence = self.verify()
        self.assertEqual(evidence['board'], 'waveshare/esp32-s3-touch-amoled-2.16')
        self.assertEqual(evidence['effective_config']['CONFIG_STOPWATCH_SOFTWARE_AEC'], 'n')
        self.assertEqual(evidence['wake_word']['models'], ['fst', 'mn7_cn'])
        self.assertEqual(evidence['partition_layout']['app_limit'], 0x300000)
        self.assertEqual(evidence['partition_layout']['assets_limit'], 0x600000)

    def replace_packed_assets(self, **options):
        # The legacy asset fixture also changes StopWatch sdkconfig defaults.
        # Keep this already-valid Wave config unchanged while replacing bytes.
        config = self.tree/'sdkconfig'
        original = config.read_bytes()
        try:
            return existing.EvidenceTests.xiaoman_assets(self, **options)
        finally:
            config.write_bytes(original)

    def test_wrong_packed_wake_command_is_rejected_on_valid_waveshare(self):
        self.verify()
        self.replace_packed_assets(command='ni hao xiao zhi')
        with self.assertRaisesRegex(ValueError, 'Xiaoman wake word configuration differs from xiao man'):
            self.verify()

    def test_wrong_packed_acoustic_model_is_rejected_on_valid_waveshare(self):
        self.verify()
        self.replace_packed_assets(model='mn6_cn')
        with self.assertRaisesRegex(ValueError, 'must contain only mn7_cn and fst models'):
            self.verify()

    def test_missing_multinet_configuration_is_rejected_on_valid_waveshare(self):
        self.verify()
        config = self.tree/'sdkconfig'
        original = config.read_text()
        self.assertEqual(original.count('CONFIG_SR_MN_CN_MULTINET7_QUANT=y'), 1)
        config.write_text(original.replace('CONFIG_SR_MN_CN_MULTINET7_QUANT=y',
            '# CONFIG_SR_MN_CN_MULTINET7_QUANT is not set'))
        with self.assertRaisesRegex(ValueError,
                'effective configuration mismatch: CONFIG_SR_MN_CN_MULTINET7_QUANT'):
            self.verify()

    def test_truncated_packed_assets_are_rejected_on_valid_waveshare(self):
        self.verify()
        self.asset.write_bytes(self.asset.read_bytes()[:-1])
        with self.assertRaisesRegex(ValueError, 'invalid Xiaoman assets header/checksum'):
            self.verify()

    def test_corrupt_packed_assets_are_rejected_on_valid_waveshare(self):
        self.verify()
        data = bytearray(self.asset.read_bytes())
        data[-1] ^= 1
        self.asset.write_bytes(data)
        with self.assertRaisesRegex(ValueError, 'invalid Xiaoman assets header/checksum'):
            self.verify()

    def test_wrong_board_aec_realtime_or_partition_configuration_is_rejected(self):
        path = self.tree/'sdkconfig'
        original = path.read_text()
        for before, after in [
                ('# CONFIG_BOARD_TYPE_M5STACK_STOPWATCH is not set', 'CONFIG_BOARD_TYPE_M5STACK_STOPWATCH=y'),
                ('# CONFIG_STOPWATCH_SOFTWARE_AEC is not set', 'CONFIG_STOPWATCH_SOFTWARE_AEC=y'),
                ('CONFIG_USE_DEVICE_AEC=y', '# CONFIG_USE_DEVICE_AEC is not set'),
                ('CONFIG_WAVESHARE_XIAOMAN_REALTIME=y', '# CONFIG_WAVESHARE_XIAOMAN_REALTIME is not set'),
                ('CONFIG_ESPTOOLPY_FLASHSIZE_16MB=y', '# CONFIG_ESPTOOLPY_FLASHSIZE_16MB is not set'),
                ('partitions/waveshare-xiaoman-16m.csv', 'partitions/v2/16m.csv')]:
            path.write_text(original.replace(before, after))
            with self.subTest(option=before), self.assertRaises(ValueError):
                self.verify()
        path.write_text(original)

    def test_layout_drift_overlap_corrupt_table_and_oversize_artifacts_are_rejected(self):
        for row in [PARTITIONS[:3]+[('ota_0', 0, 0x10, 0x20000, 0x400000)]+PARTITIONS[4:],
                    PARTITIONS[:-1]+[('storage', 1, 0x82, 0xc10000, 0x300000)]]:
            self.write_table(row)
            with self.assertRaises(ValueError): self.verify()
        self.write_table(PARTITIONS)
        original = self.table.read_bytes()
        self.table.write_bytes(original[:40]+bytes([original[40]^1])+original[41:])
        with self.assertRaises(ValueError): self.verify()
        self.table.write_bytes(original)
        for name, limit in [('xiaozhi.bin', 0x300000), ('generated_assets.bin', 0x600000)]:
            path = self.tree/'build'/name
            data = path.read_bytes()
            with path.open('wb') as output: output.truncate(limit+1)
            with self.subTest(artifact=name), self.assertRaisesRegex(ValueError, 'partition'):
                self.verify()
            path.write_bytes(data)

    def test_flasher_addresses_must_match_dedicated_layout(self):
        path = self.tree/'build/flasher_args.json'
        path.write_text(json.dumps(dict(flash_files={'0x20000': 'xiaozhi.bin',
                                                    '0x400000': 'generated_assets.bin'})))
        with self.assertRaises(ValueError): self.verify()

    def bundle_fixture(self, release_names):
        for relative in (*self.module.BUILD_FILES, *self.module.NATIVE_NETWORK_FILES):
            path = self.tree/relative
            if not path.exists():
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(b'synthetic '+relative.encode())
        releases = self.tree/'releases'
        releases.mkdir(exist_ok=True)
        for path in releases.iterdir():
            path.unlink()
        for name in release_names:
            (releases/name).write_bytes(b'synthetic release ZIP fixture')
        output = self.root/'evidence'
        # Native input validation uses real pinned fixtures in the PCM evidence
        # suite; these synthetic fixtures exercise layout and ZIP selection.
        with existing.mock.patch.object(self.module, 'verify_no_pcm', return_value={'fixture': True}), \
             existing.mock.patch.object(self.module, 'verify_flash_images', return_value={'fixture': True}):
            evidence = self.verify()
            self.module.bundle(self.firmware, self.tree, output, 'a'*40, evidence)
        return output

    def test_bundle_uses_actual_manufacturer_prefixed_waveshare_release_name(self):
        name = 'v2.5.2_waveshare-'+VARIANT+'.zip'
        output = self.bundle_fixture([name])
        self.assertEqual((output/'releases'/name).read_bytes(), b'synthetic release ZIP fixture')
        evidence = json.loads((output/'build-evidence.json').read_text())
        self.assertEqual(evidence['variant'], VARIANT)  # wire identity is unchanged
        self.assertEqual((output/'third_party/xiaozhi-LICENSE').read_bytes(), self.license_bytes)
        self.assertEqual((output/'THIRD_PARTY_NOTICES.txt').read_bytes(), self.notices_bytes)
        hashes = (output/'SHA256SUMS').read_text().splitlines()
        self.assertEqual(len(hashes), len([p for p in output.rglob('*') if p.is_file()])-1)
        for row in hashes:
            digest, name = row.split('  ', 1)
            self.assertEqual(digest, self.digest(output/name))

    def test_bundle_rejects_wrong_or_missing_manufacturer_release_name(self):
        for name in ['v2.5.2_'+VARIANT+'.zip', 'v2.5.2_other-'+VARIANT+'.zip',
                     'v2.5.2_waveshare-'+VARIANT+'.zip.bak']:
            with self.subTest(name=name), self.assertRaisesRegex(ValueError, 'exactly one canonical release ZIP'):
                self.bundle_fixture([name])
            self.assertFalse((self.root/'evidence').exists())

    def test_bundle_rejects_multiple_canonical_release_versions(self):
        names = ['v2.5.2_waveshare-'+VARIANT+'.zip', 'v2.5.3_waveshare-'+VARIANT+'.zip']
        with self.assertRaisesRegex(ValueError, 'exactly one canonical release ZIP'):
            self.bundle_fixture(names)
        self.assertFalse((self.root/'evidence').exists())
