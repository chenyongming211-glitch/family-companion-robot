"""Execute the production C3 adapter with SDK plumbing replaced on a host."""
import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest

FIRMWARE = Path(__file__).resolve().parents[1]
BOARD = FIRMWARE/'overlays/main/boards/xmini/c3'


class C3CodecTests(unittest.TestCase):
    def test_production_codec_faults_and_physical_completion(self):
        self.assertTrue((BOARD/'family_c3_codec.h').is_file(), 'C3 trusted playback driver is missing')
        with tempfile.TemporaryDirectory() as temporary:
            work = Path(temporary)
            for relative in ['codecs/es8311_audio_codec.h', 'driver/i2s_std.h',
                             'esp_heap_caps.h', 'esp_timer.h', 'esp_attr.h']:
                path = work/relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text('// SDK plumbing is supplied by the harness.\n')
            (work/'sdkconfig.h').write_text('#define CONFIG_XMINI_C3_BUTTON_ENTRY 1\n#define CONFIG_XMINI_C3_PCM 1\n')
            base = ['c++', '-std=c++17', '-pthread', '-Wall', '-Wextra', '-Werror', '-pedantic',
                    '-I', str(work), '-I', str(BOARD), '-I', str(FIRMWARE/'overlays/main/audio')]
            environment = dict(os.environ, ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                               UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
            if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                environment['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
            for mode, extra in [('optimized', ['-O2']), ('sanitized', ['-O1', '-g',
                    '-fsanitize=address,undefined', '-fno-sanitize-recover=all']), ('optimized-pcm', ['-O2']), ('sanitized-pcm', ['-O2', '-g', '-fsanitize=address,undefined', '-fno-sanitize-recover=all'])]:
                with self.subTest(mode=mode):
                    (work/'sdkconfig.h').write_text('#define CONFIG_XMINI_C3_BUTTON_ENTRY 1\n#define CONFIG_XMINI_C3_PCM '+str(int('pcm' in mode))+'\n')
                    binary = work/mode
                    compiled = subprocess.run(base+extra+[str(FIRMWARE/'tests/c3_codec_host.cc'),
                        '-o', str(binary)], capture_output=True, text=True)
                    self.assertEqual(compiled.returncode, 0, compiled.stderr)
                    result = subprocess.run([str(binary)], env=environment, capture_output=True,
                                            text=True, timeout=20)
                    self.assertEqual(result.returncode, 0, result.stdout+result.stderr)
                    print(mode+': '+result.stdout.strip())

    def test_standalone_translation_unit_enabled_and_disabled(self):
        self.assertTrue((BOARD/'family_c3_codec.h').is_file(), 'C3 trusted playback driver is missing')
        with tempfile.TemporaryDirectory() as temporary:
            work = Path(temporary)
            for relative in ['codecs/es8311_audio_codec.h', 'driver/i2s_std.h',
                             'esp_heap_caps.h', 'esp_timer.h', 'esp_attr.h']:
                path = work/relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text('#define DRAM_ATTR\n' if relative == 'esp_attr.h' else '// SDK plumbing supplied by platform.h.\n')
            plumbing = (FIRMWARE/'tests/c3_codec_host.cc').read_text().split(
                '#include "family_c3_codec.h"', 1)[0]
            (work/'platform.h').write_text(re.sub(r'^static ', 'inline ', plumbing, flags=re.M))
            flags = ['c++', '-O2', '-std=c++17', '-pthread', '-Wall', '-Wextra', '-Werror',
                     '-pedantic', '-include', str(work/'platform.h'), '-I', str(work),
                     '-I', str(BOARD), '-I', str(FIRMWARE/'overlays/main/audio')]
            for enabled, voice, pcm in ((1, 0, 0), (0, 0, 0), (1, 1, 0), (0, 1, 0), (1, 1, 1)):
                with self.subTest(enabled=enabled, voice=voice, pcm=pcm):
                    (work/'sdkconfig.h').write_text(f'#define CONFIG_XMINI_C3_BUTTON_ENTRY {enabled}\n#define CONFIG_XMINI_C3_VOICE_BARGE {voice}\n#define CONFIG_XMINI_C3_PCM {pcm}\n#define CONFIG_I2S_ISR_IRAM_SAFE {pcm}\n')
                    obj = work/f'codec-{enabled}-{voice}.o'
                    result = subprocess.run(flags+['-c', str(BOARD/'family_c3_codec.cc'),
                        '-o', str(obj)], capture_output=True, text=True)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    body = ('FamilyC3Codec codec(nullptr,0,24000,24000,0,0,0,0,0,0,1);'
                            'codec.Start();return codec.PlaybackSupported()?0:1;') if enabled else 'return 0;'
                    caller = work/f'caller-{enabled}-{voice}.cc'
                    caller.write_text('#include "family_c3_codec.h"\nint main(){'+body+'}\n')
                    binary = work/f'standalone-{enabled}-{voice}'
                    extra_objects=[]
                    if pcm:
                        isr=work/'isr.o'
                        result=subprocess.run(flags+['-c', str(BOARD/'family_c3_isr.cc'), '-o', str(isr)], capture_output=True, text=True)
                        self.assertEqual(result.returncode,0,result.stderr)
                        # Flash originals are SDK plumbing; all wrapper calls stay real.
                        caller.write_text(caller.read_text()+'\nextern \"C\" void __real_spi_flash_disable_interrupts_caches_and_other_cpu(){}\nextern \"C\" void __real_spi_flash_enable_interrupts_caches_and_other_cpu(){}\n')
                        extra_objects=[str(isr)]
                    result = subprocess.run(flags+[str(caller), str(obj), *extra_objects, '-o', str(binary)],
                                            capture_output=True, text=True)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    result = subprocess.run([str(binary)], capture_output=True, text=True, timeout=20)
                    self.assertEqual(result.returncode, 0, result.stdout+result.stderr)
                    if enabled and not pcm:
                        symbols = subprocess.run(['nm', '-C', str(obj)], capture_output=True, text=True)
                        self.assertEqual(symbols.returncode, 0, symbols.stderr)
                        for diagnostic in ('StartupDiagnostics', 'RecordStartupFaultLocked', 'RecordHardwareCheck',
                                           'DiagnosticNow', 'C3Startup', 'C3CodecStartup'):
                            self.assertNotIn(diagnostic, symbols.stdout)
                        self.assertIn('FamilyC3Codec::FaultLocked()', symbols.stdout)
                        if voice:
                            self.assertIn('FamilyC3Codec::PublishReferenceLocked()', symbols.stdout)
                    if not pcm:
                        for name in ('family_c3_isr.cc','family_c3_cache_report.cc','family_c3_sdk_layout.c'):
                            empty=work/(name+'.o')
                            empty_flags=flags.copy();forced=empty_flags.index('-include');del empty_flags[forced:forced+2]
                            result=subprocess.run(empty_flags+['-c',str(BOARD/name),'-o',str(empty)],capture_output=True,text=True)
                            self.assertEqual(result.returncode,0,result.stderr)
                            symbols=subprocess.run(['nm','-C',str(empty)],capture_output=True,text=True)
                            self.assertEqual(symbols.returncode,0,symbols.stderr)
                            self.assertNotIn('family_c3_cache',symbols.stdout)
                            self.assertNotIn('family_c3_sdk_layout',symbols.stdout)
                    if not enabled:
                        result = subprocess.run(['nm', '-C', str(obj)], capture_output=True, text=True)
                        self.assertEqual(result.returncode, 0, result.stderr)
                        self.assertNotIn('FamilyC3Codec', result.stdout)
            for pcm,iram in ((0,1),(1,0)):
                (work/'sdkconfig.h').write_text(f'#define CONFIG_XMINI_C3_BUTTON_ENTRY 1\n#define CONFIG_XMINI_C3_VOICE_BARGE 1\n#define CONFIG_XMINI_C3_PCM {pcm}\n#define CONFIG_I2S_ISR_IRAM_SAFE {iram}\n')
                result=subprocess.run(flags+['-c',str(BOARD/'family_c3_codec.cc'),'-o',str(work/'rejected.o')],capture_output=True,text=True)
                self.assertNotEqual(result.returncode,0)
                self.assertIn('requires',result.stderr)
