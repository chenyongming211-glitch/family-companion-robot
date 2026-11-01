"""Real host CMake graph tests; firmware target validation remains a CI gate."""
import importlib.util
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

FIRMWARE = Path(__file__).resolve().parents[1]
HOOK = FIRMWARE/'overlays/cmake/family_c3_pcm.cmake'


@unittest.skipUnless(os.environ.get('C3_NETWORK_COMPONENT') and os.environ.get('C3_PCM_CUE_ASSETS'),
                     'cloud build fixtures required')
class C3PcmBuild(unittest.TestCase):
    def setUp(self):
        self.assertTrue(HOOK.is_file(), 'C3 PCM CMake hook missing')
        self.temp = tempfile.TemporaryDirectory(prefix='c3-cmake-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.tree = self.root/'source'
        shutil.copytree(FIRMWARE/'overlays', self.tree)
        shutil.copytree(Path(os.environ['C3_PCM_CUE_ASSETS'])/'main/assets', self.tree/'main/assets')
        self.component = self.tree/'managed_components/78__esp-ml307'
        shutil.copytree(os.environ['C3_NETWORK_COMPONENT'], self.component)
        (self.tree/'main.cc').write_text('#include "family_c3_pcm_cues_generated.h"\n'
            'static_assert(kFamilyC3PcmPackBytes == 1490032);\nint main() { return 0; }\n')
        (self.tree/'dummy.cc').write_text('int fixture_dummy;\n')
        self.cmake = '''cmake_minimum_required(VERSION 3.20)
project(c3_graph CXX)
set(CMAKE_EXPORT_COMPILE_COMMANDS ON)
set(CONFIG_XMINI_C3_PCM ON)
set(CONFIG_BOARD_TYPE_XMINI_C3 ON)
set(CONFIG_XMINI_C3_BUTTON_ENTRY ON)
set(CONFIG_XMINI_C3_VOICE_BARGE ON)
set(IDF_TARGET esp32c3)
set(CONFIG_LANGUAGE_ZH_CN ON)
set(CONFIG_FLASH_DEFAULT_ASSETS ON)
set(BOARD_NAME xmini-c3-family)
set(LANG_DIR zh-CN)
set(PYTHON "''' + shutil.which('python3') + '''")
add_executable(main main.cc)
add_library(network STATIC dummy.cc
  managed_components/78__esp-ml307/src/web_socket.cc
  managed_components/78__esp-ml307/src/esp/esp_tcp.cc)
add_custom_target(flash)
add_custom_target(merge-bin)
function(idf_component_get_property out component property)
  if(component STREQUAL "main")
    set(value main)
  elseif(component STREQUAL "78__esp-ml307")
    if(property STREQUAL "COMPONENT_LIB")
      set(value network)
    else()
      set(value "${CMAKE_CURRENT_SOURCE_DIR}/managed_components/78__esp-ml307")
    endif()
  else()
    message(FATAL_ERROR "unexpected component")
  endif()
  set(${out} "${value}" PARENT_SCOPE)
endfunction()
function(idf_build_get_property out property)
  if(property STREQUAL "PROJECT_DIR")
    set(${out} "${CMAKE_CURRENT_SOURCE_DIR}" PARENT_SCOPE)
  elseif(property STREQUAL "PYTHON")
    set(${out} "${PYTHON}" PARENT_SCOPE)
  else()
    message(FATAL_ERROR "unexpected build property")
  endif()
endfunction()
function(esptool_py_flash_target_image target name offset image)
  file(WRITE "${CMAKE_BINARY_DIR}/registered.txt" "${target};${name};${offset};${image}")
endfunction()
include("${CMAKE_CURRENT_SOURCE_DIR}/cmake/family_c3_pcm.cmake")
family_c3_pcm_build()
get_target_property(selected network SOURCES)
file(WRITE "${CMAKE_BINARY_DIR}/selected.txt" "${selected}")
'''

    def configure(self, replacement=None):
        (self.tree/'CMakeLists.txt').write_text(replacement or self.cmake)
        return subprocess.run(['cmake', '-S', str(self.tree), '-B', str(self.root/'build')],
                              text=True, capture_output=True, timeout=60)

    def build(self):
        result = subprocess.run(['cmake', '--build', str(self.root/'build'), '--target', 'main', '-j8'],
                                text=True, capture_output=True, timeout=90)
        self.assertEqual(result.returncode, 0, result.stdout+result.stderr)

    def test_clean_parallel_build_and_stale_or_missing_header_are_rebuilt(self):
        result = self.configure()
        self.assertEqual(result.returncode, 0, result.stdout+result.stderr)
        self.build()
        out = self.root/'build/family_c3_pcm'
        header = out/'family_c3_pcm_cues_generated.h'
        expected = header.read_bytes()
        header.write_text('#error stale header\n')
        self.build()
        self.assertEqual(header.read_bytes(), expected)
        header.unlink()
        self.build()
        self.assertEqual(header.read_bytes(), expected)
        selected = (self.root/'build/selected.txt').read_text().split(';')
        self.assertEqual(sum(p.endswith('/family_c3_network/web_socket.cc') for p in selected), 1)
        self.assertEqual(sum(p.endswith('/family_c3_network/esp_tcp.cc') for p in selected), 1)
        self.assertFalse(any('managed_components' in p for p in selected))
        registered = (self.root/'build/registered.txt').read_text().split(';')
        self.assertEqual(registered[:3], ['flash', 'family_c3_pcm', '0x650000'])
        self.assertEqual(Path(registered[3]).read_bytes(), (out/'family_c3_pcm_cues.pack').read_bytes())

    def test_wrong_board_target_language_and_missing_cue_fail_configuration(self):
        for before, after in [('set(IDF_TARGET esp32c3)', 'set(IDF_TARGET esp32s3)'),
                              ('set(BOARD_NAME xmini-c3-family)', 'set(BOARD_NAME other)'),
                              ('set(CONFIG_LANGUAGE_ZH_CN ON)', 'set(CONFIG_LANGUAGE_ZH_CN OFF)')]:
            with self.subTest(after=after):
                result = self.configure(self.cmake.replace(before, after))
                self.assertNotEqual(result.returncode, 0)
        (self.tree/'main/assets/locales/zh-CN/0.ogg').unlink()
        self.assertNotEqual(self.configure().returncode, 0)

    def test_absent_duplicate_and_drifted_native_sources_fail_closed(self):
        source = '  managed_components/78__esp-ml307/src/web_socket.cc\n'
        for changed in [self.cmake.replace(source, ''), self.cmake.replace(source, source*2)]:
            with self.subTest(source=changed):
                self.assertNotEqual(self.configure(changed).returncode, 0)
        (self.component/'src/web_socket.cc').write_text('drift')
        self.assertNotEqual(self.configure().returncode, 0)

    def test_s3_never_generates_or_replaces_sources(self):
        cmake = self.cmake.replace('set(CONFIG_XMINI_C3_PCM ON)', 'set(CONFIG_XMINI_C3_PCM OFF)')
        result = self.configure(cmake)
        self.assertEqual(result.returncode, 0, result.stdout+result.stderr)
        self.assertIn('managed_components/78__esp-ml307/src/web_socket.cc',
                      (self.root/'build/selected.txt').read_text())
        self.assertFalse((self.root/'build/family_c3_pcm').exists())
        self.assertFalse((self.root/'build/family_c3_network').exists())
        self.assertFalse((self.root/'build/registered.txt').exists())


class C3FixtureTests(unittest.TestCase):
    def setUp(self):
        path = FIRMWARE/'tools/prepare_c3_fixtures.py'
        self.assertTrue(path.is_file(), 'verified fixture extractor missing')
        spec = importlib.util.spec_from_file_location('c3_fixture', path)
        self.module = importlib.util.module_from_spec(spec)
        sys.path.insert(0, str(FIRMWARE/'tools'))
        self.addCleanup(sys.path.remove, str(FIRMWARE/'tools'))
        spec.loader.exec_module(self.module)

    def test_source_archive_cache_is_verified_and_reused_without_overwrite(self):
        import hashlib
        self.assertTrue(hasattr(self.module, 'cache_source_archive'), 'verified test archive cache hook missing')
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp); source = root/'locked.tar.gz'; source.write_bytes(b'verified archive fixture')
            target = root/'cache/source.tar.gz'; checksum = hashlib.sha256(source.read_bytes()).hexdigest()
            self.module.cache_source_archive(source, target, checksum)
            before = target.stat().st_mtime_ns
            self.module.cache_source_archive(source, target, checksum)
            self.assertEqual(target.stat().st_mtime_ns, before)
            source.write_bytes(b'drift')
            with self.assertRaisesRegex(ValueError, 'checksum'):
                self.module.cache_source_archive(source, target, checksum)
            self.assertEqual(target.read_bytes(), b'verified archive fixture')
            source.write_bytes(b'verified archive fixture'); target.write_bytes(b'bad cache')
            with self.assertRaisesRegex(ValueError, 'checksum'):
                self.module.cache_source_archive(source, target, checksum)
            self.assertEqual(target.read_bytes(), b'bad cache')

    def test_zip_traversal_symlinks_duplicates_and_digest_drift_rejected_atomically(self):
        import hashlib
        import zipfile
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            for number, names in enumerate([['../escape'], ['/absolute'], ['a', 'a'], ['link']]):
                archive = root/f'{number}.zip'
                with zipfile.ZipFile(archive, 'w') as output:
                    for name in names:
                        entry = zipfile.ZipInfo(name)
                        if name == 'link': entry.external_attr = 0o120777 << 16
                        output.writestr(entry, b'x')
                destination = root/f'out{number}'
                with self.assertRaises(ValueError):
                    self.module.extract_component(archive, destination, hashlib.sha256(archive.read_bytes()).hexdigest())
                self.assertFalse(destination.exists())
            with self.assertRaisesRegex(ValueError, 'checksum'):
                self.module.extract_component(archive, root/'drift', '0'*64)


if __name__ == '__main__':
    unittest.main()
