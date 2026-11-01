"""Canonical IDF flash/merge binding, including self-consistent forgeries."""
import hashlib
import importlib.util
import json
from pathlib import Path
import struct
import tempfile
import unittest
import zipfile

FIRMWARE = Path(__file__).resolve().parents[1]
FLAGS = ['--flash-mode', 'dio', '--flash-size', '16MB', '--flash-freq', '80m']


def image(chip):
    result = bytearray(256)
    result[0], result[2], result[3] = 0xe9, 2, 0x4f
    struct.pack_into('<H', result, 12, chip)
    return result


def fixture(tree, c3=True, pcm=True):
    build = tree/'build'; build.mkdir(parents=True, exist_ok=True)
    rows = [('nvs', 1, 2, 0x9000, 0x4000 if c3 else 0x6000),
            ('otadata', 1, 0, 0xd000 if c3 else 0xf000, 0x2000),
            ('phy_init', 1, 1, 0xf000 if c3 else 0x11000, 0x1000),
            ('ota_0', 0, 0x10, 0x20000, 0x2f0000 if c3 else 0x300000),
            ('ota_1', 0, 0x11, 0x310000 if c3 else 0x320000, 0x2f0000 if c3 else 0x300000),
            ('assets', 1, 0x82, 0x600000 if c3 else 0x620000, 0x200000 if c3 else 0x600000)]
    table = b''.join(struct.pack('<HBBII16sI', 0x50aa, typ, sub, off, size, name.encode(), 0)
                     for name, typ, sub, off, size in rows)
    table += b'\xeb\xeb'+b'\xff'*14+hashlib.md5(table).digest()
    paths = {'bootloader/bootloader.bin': bytes(image(5 if c3 else 9)),
             'partition_table/partition-table.bin': table+b'\xff'*(3072-len(table)),
             'ota_data_initial.bin': b'\xff'*0x2000, 'xiaozhi.bin': bytes(image(5 if c3 else 9)),
             'generated_assets.bin': b'fixture original assets'}
    if c3 and pcm: paths['family_c3_pcm/family_c3_pcm_cues.pack'] = b'fixture separate pack'
    for name, data in paths.items():
        path = build/name; path.parent.mkdir(parents=True, exist_ok=True)
        if name.endswith('.pack') and path.exists(): continue
        path.write_bytes(data)
    files = {'0x0': 'bootloader/bootloader.bin', '0x8000': 'partition_table/partition-table.bin',
             hex(rows[1][3]): 'ota_data_initial.bin', '0x20000': 'xiaozhi.bin',
             hex(rows[-1][3]): 'generated_assets.bin'}
    if c3 and pcm: files['0x650000'] = 'family_c3_pcm/family_c3_pcm_cues.pack'
    write_flash(tree, files)
    return files


def write_flash(tree, files):
    build = tree/'build'
    metadata = dict(flash_files=files, write_flash_args=FLAGS,
                    flash_settings=dict(flash_mode='dio', flash_size='16MB', flash_freq='80m'))
    (build/'flasher_args.json').write_text(json.dumps(metadata))
    # Frozen IDF emits flags in a different valid order from JSON.
    (build/'flash_args').write_text('--flash-mode dio --flash-freq 80m --flash-size 16MB\n'
        + ''.join(offset+' '+name+'\n' for offset, name in files.items()))
    merged = bytearray(b'\xff'*max(int(offset, 0)+(build/name).stat().st_size for offset, name in files.items()))
    for offset, name in files.items():
        data = (build/name).read_bytes(); start = int(offset, 0); merged[start:start+len(data)] = data
    (build/'merged-binary.bin').write_bytes(merged)


class FlashEvidence(unittest.TestCase):
    def setUp(self):
        spec = importlib.util.spec_from_file_location('flash_verify', FIRMWARE/'tools/verify_experiment.py')
        self.module = importlib.util.module_from_spec(spec); spec.loader.exec_module(self.module)
        self.assertTrue(hasattr(self.module, 'verify_flash_images'), 'whole-image flash binding missing')
        self.temp = tempfile.TemporaryDirectory(); self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.tree = self.root/'c3'; self.files = fixture(self.tree)

    def verify(self):
        return self.module.verify_flash_images(self.tree, c3=True, pcm=True)

    def test_stable_c3_requires_native_opus_map_without_pcm_trailer(self):
        tree = self.root/'stable'; files = fixture(tree, pcm=False)
        # Stable Opus assets may use the original full partition, not PCM's
        # experimental 0x50000 prefix reservation.
        (tree/'build/generated_assets.bin').write_bytes(b'assets'+bytes(0x50000))
        write_flash(tree, files)
        result = self.module.verify_flash_images(tree, c3=True)
        self.assertEqual(result['images']['ota_data_initial.bin']['offset'], 0xd000)
        self.assertNotIn('family_c3_pcm/family_c3_pcm_cues.pack', result['images'])
        pack = tree/'build/family_c3_pcm/family_c3_pcm_cues.pack'
        pack.parent.mkdir(); pack.write_bytes(b'experimental trailer')
        files['0x650000'] = pack.relative_to(tree/'build').as_posix()
        write_flash(tree, files)
        with self.assertRaisesRegex(ValueError, 'canonical'):
            self.module.verify_flash_images(tree, c3=True)

    def test_canonical_c3_and_stock_use_their_native_partition_offsets(self):
        evidence = self.verify()
        self.assertEqual(evidence['merged_bytes'], (self.tree/'build/merged-binary.bin').stat().st_size)
        stock = self.root/'stock'; fixture(stock, c3=False)
        result = self.module.verify_flash_images(stock, c3=False)
        self.assertEqual(result['images']['ota_data_initial.bin']['offset'], 0xf000)
        self.assertEqual(result['images']['generated_assets.bin']['offset'], 0x620000)

    def test_redirected_same_basename_app_prefix_boot_table_and_pack_fail(self):
        for canonical in ('xiaozhi.bin', 'generated_assets.bin', 'bootloader/bootloader.bin',
                          'partition_table/partition-table.bin', 'family_c3_pcm/family_c3_pcm_cues.pack'):
            offset = next(o for o, p in self.files.items() if p == canonical)
            other = 'other/'+Path(canonical).name
            path = self.tree/'build'/other; path.parent.mkdir(exist_ok=True)
            path.write_bytes((self.tree/'build'/canonical).read_bytes()+b'changed')
            self.files[offset] = other; write_flash(self.tree, self.files)
            with self.subTest(canonical=canonical), self.assertRaisesRegex(ValueError, 'canonical'):
                self.verify()
            self.files[offset] = canonical
        write_flash(self.tree, self.files)

    def test_missing_ota_unexpected_entry_and_duplicate_numeric_offset_fail(self):
        for changed in [dict((k, v) for k, v in self.files.items() if v != 'ota_data_initial.bin'),
                        dict(self.files, **{'0x10000': 'extra.bin'}),
                        dict(self.files, **{'0x0000': 'bootloader/bootloader.bin'})]:
            (self.tree/'build/extra.bin').write_bytes(b'extra')
            write_flash(self.tree, changed)
            with self.assertRaises(ValueError): self.verify()

    def test_duplicate_json_keys_and_source_aliases_fail(self):
        path = self.tree/'build/flasher_args.json'
        original = path.read_text()
        path.write_text(original.replace('"flash_files": {', '"flash_files": {"0x0":"bootloader/bootloader.bin",'))
        with self.assertRaisesRegex(ValueError, 'duplicate'): self.verify()
        path.write_text(original)
        self.files['0x20000'] = './xiaozhi.bin'
        write_flash(self.tree, self.files)
        # Equivalent resolved paths are canonical; a second numeric/alias entry is not.
        self.verify()
        self.files['0x21000'] = './xiaozhi.bin'; write_flash(self.tree, self.files)
        with self.assertRaises(ValueError): self.verify()

    def test_json_flash_args_pairs_flags_and_settings_must_agree(self):
        args = self.tree/'build/flash_args'; original = args.read_text()
        for changed in [original.replace('0x20000 xiaozhi.bin', '0x30000 xiaozhi.bin'),
                        original.replace('80m', '40m'), original+'--flash-mode dio\n',
                        original.replace('--flash-mode dio', '--encrypt'),
                        original.replace('--flash-size 16MB', '--flash-size 16MB --pad-to-size 16MB')]:
            args.write_text(changed)
            with self.subTest(args=changed), self.assertRaises(ValueError): self.verify()
        args.write_text(original)
        path = self.tree/'build/flasher_args.json'; metadata = path.read_text()
        for flags in [FLAGS+['--flash-mode', 'dio'], FLAGS+['--unknown', 'yes'], FLAGS[:-2]]:
            value = json.loads(metadata); value['write_flash_args'] = flags; path.write_text(json.dumps(value))
            with self.assertRaises(ValueError): self.verify()
        value = json.loads(metadata)
        value['flash_settings']['flash_freq'] = '40m'; path.write_text(json.dumps(value))
        with self.assertRaises(ValueError): self.verify()

    def test_ota_initializer_and_flash_args_files_are_mandatory(self):
        ota = self.tree/'build/ota_data_initial.bin'; original = ota.read_bytes()
        ota.unlink()
        with self.assertRaisesRegex(ValueError, 'nonregular'): self.verify()
        ota.write_bytes(b'\x00'+original[1:]); write_flash(self.tree, self.files)
        with self.assertRaisesRegex(ValueError, 'OTA initializer'): self.verify()
        ota.write_bytes(original); write_flash(self.tree, self.files)
        (self.tree/'build/flash_args').unlink()
        with self.assertRaisesRegex(ValueError, 'nonregular'): self.verify()

    def test_bootloader_header_drift_is_rejected_instead_of_silently_patched(self):
        boot = self.tree/'build/bootloader/bootloader.bin'; data = bytearray(boot.read_bytes())
        data[3] = 0x40; boot.write_bytes(data); write_flash(self.tree, self.files)
        with self.assertRaisesRegex(ValueError, 'header'): self.verify()

    def test_entire_merge_rejects_modified_gaps_padding_and_tail_with_consistent_release(self):
        path = self.tree/'build/merged-binary.bin'; original = path.read_bytes()
        for data in [original[:0x5000]+b'\x00'+original[0x5001:], original+b'\xff',
                     original+b'\x00', original[:-1]]:
            path.write_bytes(data)
            release = self.tree/'releases'; release.mkdir(exist_ok=True)
            with zipfile.ZipFile(release/'v2.5.2_xmini-c3-family.zip', 'w') as output:
                output.writestr('merged-binary.bin', data)
            with self.assertRaisesRegex(ValueError, 'whole merged'): self.verify()


if __name__ == '__main__':
    unittest.main()
