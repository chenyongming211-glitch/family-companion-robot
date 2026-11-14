from pathlib import Path
import os
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]


class AudioFaultTest(unittest.TestCase):
    def test_production_fault_reason_and_storage(self):
        with tempfile.TemporaryDirectory() as directory:
            binary = Path(directory) / 'audio-fault'
            compile_result = subprocess.run([
                'c++', '-std=c++17', '-Wall', '-Wextra', '-Werror', '-pedantic',
                '-fsanitize=address,undefined',
                '-I', str(ROOT / 'firmware/tests/audio_fault_stubs'),
                '-I', str(ROOT / 'firmware/overlays/main/audio'),
                str(ROOT / 'firmware/tests/audio_fault_host.cc'), '-o', str(binary),
            ], capture_output=True, text=True)
            self.assertEqual(compile_result.returncode, 0, compile_result.stderr)
            environment = dict(os.environ)
            if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                environment['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
            result = subprocess.run([str(binary)], env=environment,
                                    capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stderr)
            print(result.stdout.strip())
