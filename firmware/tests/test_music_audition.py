from pathlib import Path
import subprocess
import tempfile
import unittest
ROOT = Path(__file__).resolve().parents[2]
class MusicAuditionProtocolTest(unittest.TestCase):
    def test_protocol_state_and_crc(self):
        with tempfile.TemporaryDirectory() as directory:
            binary = Path(directory)/'music-audition'
            result = subprocess.run(['c++','-std=c++17','-Wall','-Wextra','-Werror','-pedantic',
                 '-I',str(ROOT/'firmware/overlays/main/audio'),str(ROOT/'firmware/tests/music_audition_host.cc'),
                 '-o',str(binary)],capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stderr)
            result = subprocess.run([str(binary)],capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stderr)
