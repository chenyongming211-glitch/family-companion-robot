"""Portable production orb state/rendering and actual DMA-envelope regressions."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

FIRMWARE = Path(__file__).resolve().parents[1]


class OrbUiTests(unittest.TestCase):
    def test_production_orb_state_render_and_completion(self):
        audio = FIRMWARE / 'overlays/main/audio'
        board = FIRMWARE / 'overlays/main/boards/m5stack/stopwatch'
        self.assertTrue((audio / 'orb_activity.h').is_file(), 'missing production audio activity observer')
        self.assertTrue((board / 'orb_model.h').is_file(), 'missing production orb state model')
        self.assertTrue((board / 'orb_renderer.h').is_file(), 'missing production orb renderer')
        with tempfile.TemporaryDirectory() as directory:
            binary = Path(directory) / 'orb'
            result = subprocess.run([
                'c++', '-std=c++17', '-Wall', '-Wextra', '-Werror', '-pedantic',
                '-fsanitize=address,undefined', '-fno-sanitize-recover=all',
                '-DCONFIG_XIAOMAN_ORB_UI=1', '-I', str(audio), '-I', str(board),
                str(FIRMWARE / 'tests/orb_ui_test.cc'), '-o', str(binary)],
                capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            environment = dict(os.environ, ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                               UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
            if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                environment['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
            result = subprocess.run([str(binary)], capture_output=True, text=True,
                                    env=environment, timeout=20)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            print(result.stdout.strip())

    def test_production_output_and_validated_protocol_observers(self):
        with tempfile.TemporaryDirectory() as directory:
            for source in ['playback_adapter_test.cc', 'playback_websocket_test.cc', 'orb_display_host.cc']:
                binary = Path(directory) / source.removesuffix('.cc')
                result = subprocess.run([
                    'c++', '-std=c++17', '-pthread', '-Wall', '-Wextra', '-Werror', '-pedantic',
                    '-fsanitize=address,undefined', '-fno-sanitize-recover=all',
                    '-DCONFIG_XIAOMAN_ORB_UI=1',
                    '-I', str(FIRMWARE / 'overlays/main/audio'),
                    '-I', str(FIRMWARE / 'overlays/main/boards/m5stack/stopwatch'),
                    str(FIRMWARE / 'tests' / source), '-o', str(binary)], capture_output=True, text=True)
                self.assertEqual(result.returncode, 0, result.stderr)
                environment = dict(os.environ)
                if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                    environment['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
                result = subprocess.run([str(binary)], capture_output=True, text=True,
                                        env=environment, timeout=20)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)


if __name__ == '__main__':
    unittest.main()
