"""Cloud host execution of production C3 MR capture path."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

FIRMWARE = Path(__file__).resolve().parents[1]
BOARD = FIRMWARE/'overlays/main/boards/xmini/c3'

class C3DuplexTests(unittest.TestCase):
    def test_production_bounded_duplex(self):
        with tempfile.TemporaryDirectory() as temporary:
            work = Path(temporary)
            for relative in ['codecs/es8311_audio_codec.h', 'driver/i2s_std.h',
                             'esp_heap_caps.h', 'esp_timer.h']:
                path = work/relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text('// SDK plumbing supplied by harness.\n')
            (work/'sdkconfig.h').write_text('#define CONFIG_XMINI_C3_BUTTON_ENTRY 1\n#define CONFIG_XMINI_C3_VOICE_BARGE 1\n')
            plumbing = (FIRMWARE/'tests/c3_codec_host.cc').read_text().split(
                '#include "family_c3_codec.h"', 1)[0]
            source = work/'c3-duplex.cc'
            source.write_text(plumbing+(FIRMWARE/'tests/c3_duplex_host.cc').read_text())
            base = ['c++', '-std=c++17', '-pthread', '-Wall', '-Wextra', '-Werror', '-pedantic',
                    '-Wno-unused-function', '-I', str(work), '-I', str(BOARD),
                    '-I', str(FIRMWARE/'overlays/main/audio'),
                    '-I', str(FIRMWARE/'overlays/main/boards/m5stack/stopwatch')]
            environment = dict(os.environ, ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                               UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
            if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                environment['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
            for mode, extra in [('optimized', ['-O2']), ('sanitized', ['-O1', '-g',
                    '-fsanitize=address,undefined', '-fno-sanitize-recover=all'])]:
                with self.subTest(mode=mode):
                    binary = work/mode
                    compiled = subprocess.run(base+extra+[str(source), '-o', str(binary)],
                                              capture_output=True, text=True)
                    self.assertEqual(compiled.returncode, 0, compiled.stderr)
                    result = subprocess.run([str(binary)], env=environment, capture_output=True,
                                            text=True, timeout=20)
                    self.assertEqual(result.returncode, 0, result.stdout+result.stderr)
                    print(mode+': '+result.stdout.strip())
