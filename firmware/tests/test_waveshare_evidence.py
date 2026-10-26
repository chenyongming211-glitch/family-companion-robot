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
        for relative in self.module.BUILD_FILES:
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
        self.module.bundle(self.firmware, self.tree, output, 'a'*40, self.verify())
        return output

    def test_bundle_uses_actual_manufacturer_prefixed_waveshare_release_name(self):
        name = 'v2.5.2_waveshare-'+VARIANT+'.zip'
        output = self.bundle_fixture([name])
        self.assertEqual((output/'releases'/name).read_bytes(), b'synthetic release ZIP fixture')
        evidence = json.loads((output/'build-evidence.json').read_text())
        self.assertEqual(evidence['variant'], VARIANT)  # wire identity is unchanged

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
