"""PCM evidence corruption cases against real locked cue/network fixtures."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import re
import struct
import tempfile
import unittest
import zipfile

FIRMWARE = Path(__file__).resolve().parents[1]


@unittest.skipUnless(os.environ.get('C3_PCM_CUE_ASSETS') and os.environ.get('C3_NETWORK_COMPONENT'),
                     'cloud real cue/network fixtures required')
class PcmEvidence(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        spec = importlib.util.spec_from_file_location('pcm_verify', FIRMWARE/'tools/verify_experiment.py')
        cls.verifier = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(cls.verifier)
        if not hasattr(cls.verifier, 'verify_c3_pcm'):
            raise AssertionError('PCM artifact verifier missing')
        cls.temp = tempfile.TemporaryDirectory(prefix='c3-evidence-base-')
        cls.addClassCleanup(cls.temp.cleanup)
        cls.base = Path(cls.temp.name)
        shutil.copytree(FIRMWARE/'overlays', cls.base, dirs_exist_ok=True)
        shutil.copytree(Path(os.environ['C3_PCM_CUE_ASSETS'])/'main/assets', cls.base/'main/assets')
        cls.component = cls.base/'managed_components/78__esp-ml307'
        shutil.copytree(os.environ['C3_NETWORK_COMPONENT'], cls.component)
        cls.verifier.c3_build.generate(cls.base, cls.base/'build/family_c3_pcm')
        cls.verifier.c3_network.generate(cls.component, cls.base/'build/family_c3_network')

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='c3-evidence-')
        self.addCleanup(self.temp.cleanup)
        self.tree = Path(self.temp.name)/'tree'
        shutil.copytree(self.base, self.tree)
        self.out = self.tree/'build/family_c3_pcm'
        self.net = self.tree/'build/family_c3_network'
        self.component = self.tree/'managed_components/78__esp-ml307'
        self.prefix = self.tree/'build/generated_assets.bin'
        self.prefix.write_bytes(b'fixture-prefix')
        # Layout/real stable prefix are tested separately. This synthetic image
        # keeps the rest of the PCM provenance verifier under test.
        from unittest.mock import patch
        self.prefix_check = patch.object(self.verifier.c3_build, 'verify_prefix',
                                        return_value={'fixture': True})
        self.prefix_check.start()
        self.addCleanup(self.prefix_check.stop)
        elf = self.tree/'build/xiaozhi.elf'; elf.write_bytes(b'\x7fELF fixture exact symbols')
        (self.tree/'build/xiaozhi.map').write_text('fixture map\n')
        app = bytearray(256); app[0] = 0xe9
        app[32:36] = struct.pack('<I', 0xabcd5432)
        app[176:208] = hashlib.sha256(elf.read_bytes()).digest()
        (self.tree/'build/xiaozhi.bin').write_bytes(app)
        self.files = {'0x20000': 'xiaozhi.bin', '0x600000': 'generated_assets.bin',
                      '0x650000': 'family_c3_pcm/family_c3_pcm_cues.pack'}
        self.write_flash()
        generated = [self.net/'web_socket.cc', self.net/'esp_tcp.cc']
        others = [self.component/name for name in re.findall(r'"([^"\n]+\.cc)"',
                  (self.component/'CMakeLists.txt').read_text())
                  if name not in ('src/web_socket.cc', 'src/esp/esp_tcp.cc')]
        self.compiled = self.tree/'build/compile_commands.json'
        self.compiled.write_text(json.dumps([dict(directory=str(self.tree/'build'), file=str(path),
            command='c++ -c '+str(path)) for path in generated+others]))
        (self.net/'compiled-sources.txt').write_text('\n'.join(map(str, generated+others))+'\n')

    def write_flash(self):
        (self.tree/'build/flasher_args.json').write_text(json.dumps({'flash_files': self.files}))
        merged = bytearray(b'\xff'*(0x650000+1490032))
        for offset, name in self.files.items():
            data = (self.tree/'build'/name).read_bytes()
            start = int(offset, 0); merged[start:start+len(data)] = data
        (self.tree/'build/merged-binary.bin').write_bytes(merged)
        releases = self.tree/'releases'; releases.mkdir(exist_ok=True)
        with zipfile.ZipFile(releases/'v2.5.2_xmini-c3-family.zip', 'w') as output:
            output.writestr('merged-binary.bin', merged)

    def verify(self):
        return self.verifier.verify_c3_pcm(self.tree)

    def test_valid_pack_network_symbols_and_merged_release_are_bound(self):
        evidence = self.verify()
        self.assertEqual(evidence['cues']['samples'], 744480)
        self.assertEqual(evidence['cues']['pack_bytes'], 1490032)

    def test_missing_stale_header_pack_manifest_and_source_clock_fail(self):
        cases = [self.out/'family_c3_pcm_cues_generated.h', self.out/'family_c3_pcm_cues.pack',
                 self.out/'family_c3_pcm_cues.json', self.tree/'main/assets/locales/zh-CN/0.ogg']
        for path in cases:
            original = path.read_bytes()
            with self.subTest(path=path):
                path.write_bytes(original+b'corrupt')
                with self.assertRaises(ValueError): self.verify()
                path.unlink()
                with self.assertRaises(ValueError): self.verify()
                path.write_bytes(original)
        manifest = self.out/'family_c3_pcm_cues.json'
        value = json.loads(manifest.read_text()); value['cues'][0]['media_packets'] += 1
        manifest.write_text(json.dumps(value))
        with self.assertRaisesRegex(ValueError, 'manifest'): self.verify()

    def test_wrong_offset_overlap_and_merged_or_release_corruption_fail(self):
        for offset in ['0x640000', '0x600010']:
            self.files[offset] = self.files.pop('0x650000')
            self.write_flash()
            with self.subTest(offset=offset), self.assertRaises(ValueError): self.verify()
            self.files['0x650000'] = self.files.pop(offset)
        self.write_flash()
        merged = self.tree/'build/merged-binary.bin'; original = merged.read_bytes()
        for offset in [0x600000, 0x650000]:
            data = bytearray(original); data[offset] ^= 1; merged.write_bytes(data)
            with self.subTest(offset=offset), self.assertRaisesRegex(ValueError, 'merged'): self.verify()
        merged.write_bytes(original)
        release = self.tree/'releases/v2.5.2_xmini-c3-family.zip'
        with zipfile.ZipFile(release, 'w') as output: output.writestr('merged-binary.bin', b'wrong')
        with self.assertRaisesRegex(ValueError, 'release'): self.verify()

    def test_network_original_generated_helper_recipe_and_compiled_inputs_must_match(self):
        for path in [self.component/'src/web_socket.cc', self.net/'web_socket.cc',
                     self.tree/'main/audio/c3_tcp_deadline.h', self.tree/'scripts/family_c3_network.py']:
            original = path.read_bytes(); path.write_bytes(original+b'drift')
            with self.subTest(path=path), self.assertRaises(ValueError): self.verify()
            path.write_bytes(original)
        original = self.compiled.read_text()
        for value in [[], json.loads(original)+[dict(directory=str(self.tree),
                      file=str(self.component/'src/web_socket.cc'), command='c++ -c original')]]:
            self.compiled.write_text(json.dumps(value))
            with self.assertRaisesRegex(ValueError, 'compiled'): self.verify()
        self.compiled.write_text(original)
        sources = self.net/'compiled-sources.txt'; sources.write_text(str(self.component/'src/web_socket.cc'))
        with self.assertRaisesRegex(ValueError, 'compiled'): self.verify()

    def test_exact_elf_app_digest_and_nonregular_evidence_are_required(self):
        elf = self.tree/'build/xiaozhi.elf'; original = elf.read_bytes(); elf.write_bytes(original+b'drift')
        with self.assertRaisesRegex(ValueError, 'ELF'): self.verify()
        elf.write_bytes(original)
        header = self.out/'family_c3_pcm_cues_generated.h'; target = self.tree/'header'
        header.rename(target); header.symlink_to(target)
        with self.assertRaisesRegex(ValueError, 'nonregular'): self.verify()

    def test_self_consistent_changed_network_copy_is_not_canonical_generator_output(self):
        path = self.net/'web_socket.cc'; path.write_bytes(path.read_bytes()+b'\n// altered\n')
        manifest = self.net/'family_c3_network.manifest.json'
        value = json.loads(manifest.read_text())
        value['patched_sha256']['web_socket.cc'] = hashlib.sha256(path.read_bytes()).hexdigest()
        manifest.write_text(json.dumps(value))
        with self.assertRaisesRegex(ValueError, 'canonical'): self.verify()

    def test_self_consistent_changed_pcm_is_not_canonical_decoder_output(self):
        manifest = json.loads((self.out/'family_c3_pcm_cues.json').read_text())
        pack = (self.out/'family_c3_pcm_cues.pack').read_bytes()
        entries = []
        for index, cue in enumerate(manifest['cues']):
            pcm = bytearray(pack[cue['pack_offset']:cue['pack_offset']+cue['pcm_bytes']])
            if index == 0: pcm[0] ^= 1
            entries.append(dict(name=cue['name'], source=(self.tree/'main/assets'/cue['name']).read_bytes(), pcm=pcm))
        changed, manifest = self.verifier.c3_build.cues.make_pack(entries, 'libopus 1.4')
        (self.out/'family_c3_pcm_cues.pack').write_bytes(changed)
        (self.out/'family_c3_pcm_cues.json').write_text(json.dumps(manifest))
        (self.out/'family_c3_pcm_cues_generated.h').write_text(self.verifier.c3_build.expected_header(changed))
        self.write_flash()
        with self.assertRaisesRegex(ValueError, 'canonical'): self.verify()

    def test_pack_table_clock_and_overlap_fail_even_with_repaired_body_digest(self):
        path = self.out/'family_c3_pcm_cues.pack'; original = path.read_bytes()
        for at, value in [(64+32, 64), (64+36, 1), (112+32, 1072)]:
            pack = bytearray(original); struct.pack_into('<I', pack, at, value)
            pack[32:64] = hashlib.sha256(pack[64:]).digest(); path.write_bytes(pack)
            with self.subTest(at=at), self.assertRaisesRegex(ValueError, 'clock/overlap'): self.verify()
        path.write_bytes(original)

    def test_stable_prefix_rejects_any_other_bytes_and_capacity_overflow(self):
        self.prefix_check.stop()
        for content in [b'wrong', bytes(0x50001)]:
            self.prefix.write_bytes(content)
            with self.assertRaisesRegex(ValueError, 'prefix'): self.verify()

    def test_non_pcm_requires_original_compilation_and_pinned_native_sources(self):
        stock = Path(self.temp.name)/'stock'
        component = stock/'managed_components/78__esp-ml307'
        shutil.copytree(os.environ['C3_NETWORK_COMPONENT'], component)
        commands = stock/'build/compile_commands.json'; commands.parent.mkdir(parents=True)
        original = [dict(directory=str(stock), file=str(component/name), command='c++ -c original')
                    for name in ('src/web_socket.cc', 'src/esp/esp_tcp.cc')]
        commands.write_text(json.dumps(original))
        self.verifier.verify_no_pcm(stock)
        for entries in [[], original[:1], original+original[:1]]:
            commands.write_text(json.dumps(entries))
            with self.subTest(entries=len(entries)), self.assertRaisesRegex(ValueError, 'compiled'):
                self.verifier.verify_no_pcm(stock)
        commands.write_text(json.dumps(original))
        path = component/'src/web_socket.cc'; path.write_bytes(path.read_bytes()+b'drift')
        with self.assertRaisesRegex(ValueError, 'original'): self.verifier.verify_no_pcm(stock)

    def test_stock_bundle_retains_compile_provenance_and_hashes_every_file(self):
        self.assertTrue(hasattr(self.verifier, 'bundle_baseline'), 'stock evidence bundler missing')
        stock = Path(self.temp.name)/'stock'
        component = stock/'managed_components/78__esp-ml307'
        shutil.copytree(os.environ['C3_NETWORK_COMPONENT'], component)
        for name in self.verifier.BUILD_FILES:
            path = stock/name; path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(b'synthetic '+name.encode())
        commands = [dict(directory=str(stock), file=str(component/name), command='c++ -c original')
                    for name in ('src/web_socket.cc', 'src/esp/esp_tcp.cc')]
        (stock/'build/compile_commands.json').write_text(json.dumps(commands))
        (stock/'build/flasher_args.json').write_text('{"flash_files":{}}')
        (stock/'sdkconfig').write_text('CONFIG_IDF_TARGET="esp32s3"\n# CONFIG_XMINI_C3_PCM is not set\n')
        shutil.copyfile(FIRMWARE/'dependencies.lock', stock/'dependencies.lock')
        releases = stock/'releases'; releases.mkdir()
        with zipfile.ZipFile(releases/'v2.5.2_m5stack-stopwatch.zip', 'w') as output:
            output.writestr('merged-binary.bin', (stock/'build/merged-binary.bin').read_bytes())
        evidence = Path(self.temp.name)/'stock-evidence'
        lock = json.loads((FIRMWARE/'upstream.lock.json').read_text())
        self.verifier.bundle_baseline(FIRMWARE, stock, evidence, 'a'*40, lock['idf_commit'])
        self.assertEqual((evidence/'build/compile_commands.json').read_bytes(),
                         (stock/'build/compile_commands.json').read_bytes())
        rows = (evidence/'SHA256SUMS').read_text().splitlines()
        self.assertEqual(len(rows), len([p for p in evidence.rglob('*') if p.is_file()])-1)
        for row in rows:
            digest, name = row.split('  ', 1)
            self.assertEqual(digest, hashlib.sha256((evidence/name).read_bytes()).hexdigest())
        for sha, sdk in [('a'*39, 'CONFIG_IDF_TARGET="esp32s3"\n'),
                         ('a'*40, 'CONFIG_IDF_TARGET="esp32s3"\nCONFIG_XMINI_C3_PCM=y\n')]:
            (stock/'sdkconfig').write_text(sdk)
            with self.assertRaises(ValueError):
                self.verifier.bundle_baseline(FIRMWARE, stock, Path(self.temp.name)/'invalid', sha, lock['idf_commit'])


if __name__ == '__main__':
    unittest.main()
