"""Real host CMake graph tests; firmware target validation remains a CI gate."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import struct
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
  elseif(property STREQUAL "IDF_PATH")
    set(${out} "${CMAKE_CURRENT_SOURCE_DIR}/sdk" PARENT_SCOPE)
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
get_target_property(main_interface main INTERFACE_INCLUDE_DIRECTORIES)
file(WRITE "${CMAKE_BINARY_DIR}/main-interface.txt" "${main_interface}")
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

    def test_native_systimer_header_chain_is_private_to_pcm_main(self):
        def includes(source):
            entries = json.loads((self.root/'build/compile_commands.json').read_text())
            command = next(entry['command'] for entry in entries
                           if Path(entry['file']).name == source)
            return {arg[2:] for arg in shlex.split(command) if arg.startswith('-I')}

        systimer = self.tree/'sdk/components/esp_hal_systimer'
        required = {str(systimer/'include'), str(systimer/'esp32c3/include')}
        result = self.configure()
        self.assertEqual(result.returncode, 0, result.stdout+result.stderr)
        self.assertTrue(required <= includes('main.cc'),
                        'native systimer_hal.h and its C3 systimer_ll.h must both resolve')
        self.assertFalse(required & set((self.root/'build/main-interface.txt').read_text().split(';')),
                         'native layout includes must not become a public main dependency')
        self.assertFalse(required & includes('dummy.cc'), 'main private headers leaked to network')

        disabled = self.cmake.replace('set(CONFIG_XMINI_C3_PCM ON)', 'set(CONFIG_XMINI_C3_PCM OFF)')
        result = self.configure(disabled)
        self.assertEqual(result.returncode, 0, result.stdout+result.stderr)
        self.assertFalse(required & includes('main.cc'), 'PCM0 must not acquire native layout includes')

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


@unittest.skipUnless((os.environ.get('C3_LDGEN_SDK') or os.environ.get('IDF_PATH'))
                     and os.environ.get('C3_PCM_CUE_ASSETS'), 'fixed SDK and real cue fixtures required')
class C3EmbeddedArchiveTests(unittest.TestCase):
    def test_actual_component_graph_keeps_embed_bytes_symbols_and_avoids_numeric_member_boundary(self):
        sdk=Path(os.environ.get('C3_LDGEN_SDK') or os.environ['IDF_PATH'])
        prepared=Path(os.environ['C3_PCM_CUE_ASSETS'])
        text=(prepared/'main/CMakeLists.txt').read_text()
        start=text.index('# Register with the component, before ldgen creates its executable target.')
        registration=text[start:text.index('# Keep project-owned C++',start)]
        utilities=(sdk/'tools/cmake/utilities.cmake').read_text()
        start=utilities.index('function(target_add_binary_data ')
        binary_function=utilities[start:utilities.index('endfunction()',start)+len('endfunction()')]
        assets=sorted((prepared/'main/assets/locales/zh-CN').glob('*.ogg'))
        assets+=sorted((prepared/'main/assets/common').glob('*.ogg'))
        hashes={path:hashlib.sha256(path.read_bytes()).hexdigest() for path in assets}
        numeric=[path for path in assets if re.fullmatch(r'[0-9]+\.ogg',path.name)]
        self.assertEqual([path.name for path in numeric],[f'{i}.ogg' for i in range(10)])
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary);source=root/'source';(source/'main').mkdir(parents=True)
            (source/'cmake').mkdir();shutil.copyfile(HOOK,source/'cmake/family_c3_pcm.cmake')
            (source/'dummy.c').write_text('int preceding_archive_member;\n')
            (source/'main/CMakeLists.txt').write_text(registration)
            cmake='''cmake_minimum_required(VERSION 3.20)
project(embed_graph C ASM)
set(CMAKE_EXPORT_COMPILE_COMMANDS ON)
set(CONFIG_XMINI_C3_PCM @PCM@)
set(LANG_SOUNDS "@ASSETS@")
function(idf_build_get_property out property)
  if(property STREQUAL "BUILD_DIR")
    set(value "${CMAKE_BINARY_DIR}")
  elseif(property STREQUAL "IDF_PATH")
    set(value "@SDK@")
  else()
    message(FATAL_ERROR "unexpected build property")
  endif()
  set(${out} "${value}" PARENT_SCOPE)
endfunction()
@BINARY_FUNCTION@
function(idf_component_register)
  cmake_parse_arguments(_ "WHOLE_ARCHIVE" "" "SRCS;EMBED_FILES;INCLUDE_DIRS;LDFRAGMENTS;PRIV_REQUIRES" ${ARGN})
  add_library(main STATIC "${CMAKE_SOURCE_DIR}/dummy.c")
  foreach(file IN LISTS __EMBED_FILES)
    target_add_binary_data(main "${file}" BINARY)
  endforeach()
  set(COMPONENT_LIB main PARENT_SCOPE)
endfunction()
add_subdirectory(main)
'''.replace('@SDK@',str(sdk)).replace('@ASSETS@',';'.join(map(str,assets))).replace('@BINARY_FUNCTION@',binary_function)
            builds={};members={}
            for pcm in ('OFF','ON'):
                build=root/pcm;builds[pcm]=build
                (source/'CMakeLists.txt').write_text(cmake.replace('@PCM@',pcm))
                for command in (['cmake','-G','Unix Makefiles','-S',str(source),'-B',str(build)],
                                ['cmake','--build',str(build),'--parallel','8']):
                    result=subprocess.run(command,text=True,capture_output=True,timeout=60)
                    self.assertEqual(result.returncode,0,result.stdout+result.stderr)
                archive=build/'main/libmain.a'
                members[pcm]=subprocess.check_output(['ar','t',str(archive)],text=True).splitlines()
                commands=json.loads((build/'compile_commands.json').read_text())
                asm=[Path(row['file']).name for row in commands if row['file'].endswith('.S')]
                if pcm=='ON':
                    self.assertFalse(any(re.match(r'^[0-9]+\.ogg\.S',name) for name in asm+members[pcm]))
                    self.assertEqual(len([name for name in asm if name.startswith('family_c3_embedded_')]),10)
                else:self.assertEqual(len([name for name in asm if re.match(r'^[0-9]+\.ogg\.S$',name)]),10)
            expected=['family_c3_embedded_'+name if re.match(r'^[0-9]+\.ogg\.S[.]',name) else name
                      for name in members['OFF']]
            self.assertEqual(members['ON'],expected)
            assembly=builds['ON']/'family_c3_embedded_0.ogg.S';expected_assembly=assembly.read_bytes()
            assembly.unlink()
            subprocess.run(['cmake','--build',str(builds['ON']),'--parallel','8'],check=True,capture_output=True)
            self.assertEqual(assembly.read_bytes(),expected_assembly)
            for asset in assets:
                original=asset.name+'.S';renamed=('family_c3_embedded_' if asset in numeric else '')+original
                self.assertEqual((builds['OFF']/original).read_bytes(),(builds['ON']/renamed).read_bytes())
                objects=[]
                for pcm,name in [('OFF',original),('ON',renamed)]:
                    member=next(name_ for name_ in members[pcm] if name_.startswith(name+'.'))
                    obj=root/(pcm+'-'+asset.name+'.o');objects.append(obj)
                    obj.write_bytes(subprocess.check_output(['ar','p',str(builds[pcm]/'main/libmain.a'),member]))
                    data=root/(pcm+'-'+asset.name+'.data')
                    subprocess.run(['objcopy','--dump-section',f'.data={data}',str(obj)],check=True,capture_output=True)
                    self.assertEqual(data.read_bytes(),asset.read_bytes()+struct.pack('<I',asset.stat().st_size))
                self.assertEqual(subprocess.check_output(['nm','-g','--defined-only',str(objects[0])]),
                                 subprocess.check_output(['nm','-g','--defined-only',str(objects[1])]))
            self.assertEqual(hashes,{path:hashlib.sha256(path.read_bytes()).hexdigest() for path in assets})


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
