"""Native fault injection through the actual Waveshare adapter methods."""
import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest

FIRMWARE = Path(__file__).resolve().parents[1]
BOARD = FIRMWARE/'overlays/main/boards/waveshare/esp32-s3-touch-amoled-2.16'


class WaveshareCodecTests(unittest.TestCase):
    def test_production_adapter_lifecycle_and_physical_completion(self):
        self.assertTrue((BOARD/'xiaoman_box_codec.h').is_file(), 'Waveshare adapter is missing')
        with tempfile.TemporaryDirectory() as temporary:
            work = Path(temporary)
            (work/'codecs').mkdir()
            (work/'codecs/box_audio_codec.h').write_text('// Platform plumbing is defined by the harness.\n')
            (work/'sdkconfig.h').write_text('#define CONFIG_WAVESHARE_XIAOMAN_REALTIME 1\n')
            binary = work/'waveshare'
            result = subprocess.run([
                'c++', '-std=c++17', '-pthread', '-Wall', '-Wextra', '-Werror', '-pedantic',
                '-fsanitize=address,undefined', '-fno-sanitize-recover=all',
                '-DCONFIG_XIAOMAN_ORB_UI=1',
                '-I', str(work), '-I', str(BOARD), '-I', str(FIRMWARE/'overlays/main/audio'),
                str(FIRMWARE/'tests/waveshare_codec_host.cc'), '-o', str(binary)],
                capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            environment = dict(os.environ, ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                               UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
            if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                environment['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
            result = subprocess.run([str(binary)], env=environment, capture_output=True,
                                    text=True, timeout=20)
            self.assertEqual(result.returncode, 0, result.stdout+result.stderr)
            print(result.stdout.strip())

    def test_standalone_production_translation_unit_loads_sdkconfig_before_its_guard(self):
        with tempfile.TemporaryDirectory() as temporary:
            work = Path(temporary)
            for relative in ['codecs/box_audio_codec.h', 'driver/i2s_tdm.h',
                             'esp_heap_caps.h', 'esp_timer.h']:
                path = work/relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text('// Platform declarations are supplied by platform.h.\n')
            # Reuse platform/Box plumbing; compile the actual codec .cc as its
            # own TU. Inline linkage lets the caller share this platform state.
            platform = (FIRMWARE/'tests/waveshare_codec_host.cc').read_text().split(
                '#include "xiaoman_box_codec.h"', 1)[0]
            (work/'platform.h').write_text(re.sub(r'^static ', 'inline ', platform, flags=re.M))
            flags = ['c++', '-std=c++17', '-pthread', '-Wall', '-Wextra', '-Werror', '-pedantic',
                '-fsanitize=address,undefined', '-fno-sanitize-recover=all',
                '-DCONFIG_XIAOMAN_ORB_UI=1', '-include', str(work/'platform.h'),
                '-I', str(work), '-I', str(BOARD), '-I', str(FIRMWARE/'overlays/main/audio')]
            environment = dict(os.environ, ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                               UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
            if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                environment['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
            for enabled in (1, 0):
                with self.subTest(enabled=enabled):
                    (work/'sdkconfig.h').write_text(f'#define CONFIG_WAVESHARE_XIAOMAN_REALTIME {enabled}\n')
                    obj = work/f'codec-{enabled}.o'
                    compiled = subprocess.run(flags+['-c', str(BOARD/'xiaoman_box_codec.cc'),
                        '-o', str(obj)], capture_output=True, text=True)
                    self.assertEqual(compiled.returncode, 0, compiled.stderr)
                    caller = work/f'caller-{enabled}.cc'
                    body = ('XiaomanBoxCodec codec(nullptr,24000,24000,0,0,0,0,0,0,1,2,true);'
                            'codec.Start();return codec.PlaybackSupported()?0:1;') if enabled else 'return 0;'
                    caller.write_text('#include "sdkconfig.h"\n#include "xiaoman_box_codec.h"\n'
                                      'int main(){'+body+'}\n')
                    binary = work/f'standalone-{enabled}'
                    linked = subprocess.run(flags+[str(caller), str(obj), '-o', str(binary)],
                                            capture_output=True, text=True)
                    self.assertEqual(linked.returncode, 0, linked.stderr)
                    result = subprocess.run([str(binary)], env=environment, capture_output=True,
                                            text=True, timeout=20)
                    self.assertEqual(result.returncode, 0, result.stdout+result.stderr)
                    if not enabled:
                        symbols = subprocess.run(['nm', '-C', str(obj)], capture_output=True, text=True)
                        self.assertEqual(symbols.returncode, 0, symbols.stderr)
                        self.assertNotIn('XiaomanBoxCodec', symbols.stdout)
