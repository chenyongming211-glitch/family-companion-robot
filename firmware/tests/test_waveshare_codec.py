"""Native fault injection through the actual Waveshare adapter methods."""
import os
from pathlib import Path
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
            binary = work/'waveshare'
            result = subprocess.run([
                'c++', '-std=c++17', '-pthread', '-Wall', '-Wextra', '-Werror', '-pedantic',
                '-fsanitize=address,undefined', '-fno-sanitize-recover=all',
                '-DCONFIG_WAVESHARE_XIAOMAN_REALTIME=1', '-DCONFIG_XIAOMAN_ORB_UI=1',
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
