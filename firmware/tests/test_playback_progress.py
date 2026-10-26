from pathlib import Path
import os
import subprocess
import tempfile
import unittest
ROOT = Path(__file__).resolve().parents[2]
def host_environment():
    environment = dict(os.environ)
    if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
        environment['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
    return environment

class PlaybackProgressTest(unittest.TestCase):
    def test_production_core(self):
        with tempfile.TemporaryDirectory() as d:
            binary = Path(d) / 'playback'
            result = subprocess.run(['c++','-std=c++17','-Wall','-Wextra','-Werror','-pedantic',
                '-fsanitize=address,undefined','-I',str(ROOT/'firmware/overlays/main/audio'),
                str(ROOT/'firmware/tests/playback_progress_test.cc'),'-o',str(binary)],capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stderr)
            result = subprocess.run([str(binary)],env=host_environment(),capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stderr)
            print(result.stdout.strip())

    def test_production_adapter(self):
        with tempfile.TemporaryDirectory() as d:
            binary = Path(d) / 'adapter'
            result = subprocess.run(['c++','-std=c++17','-pthread','-Wall','-Wextra','-Werror','-pedantic',
                '-fsanitize=address,undefined','-I',str(ROOT/'firmware/overlays/main/audio'),
                '-I',str(ROOT/'firmware/overlays/main/boards/m5stack/stopwatch'),
                str(ROOT/'firmware/tests/playback_adapter_test.cc'),'-o',str(binary)],capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stderr)
            result = subprocess.run([str(binary)],env=host_environment(),capture_output=True,text=True,timeout=10)
            self.assertEqual(result.returncode,0,result.stderr)
            print(result.stdout.strip())

    def test_production_websocket_integration(self):
        with tempfile.TemporaryDirectory() as d:
            binary = Path(d) / 'websocket'
            result = subprocess.run(['c++','-std=c++17','-pthread','-Wall','-Wextra','-Werror','-pedantic',
                '-fsanitize=address,undefined','-I',str(ROOT/'firmware/overlays/main/audio'),
                str(ROOT/'firmware/tests/playback_websocket_test.cc'),'-o',str(binary)],capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stderr)
            result = subprocess.run([str(binary)],env=host_environment(),capture_output=True,text=True,timeout=10)
            self.assertEqual(result.returncode,0,result.stderr)
            print(result.stdout.strip())
