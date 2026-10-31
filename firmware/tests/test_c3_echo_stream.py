"""Exercise production MR frame assembly and capture-boundary invalidation."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

FIRMWARE=Path(__file__).resolve().parents[1]

class C3EchoStreamTests(unittest.TestCase):
    def test_real_frame_assembler(self):
        with tempfile.TemporaryDirectory() as temporary:
            for mode,flags in [('O2',['-O2']),('ASan',['-O1','-g','-fsanitize=address,undefined',
                                                    '-fno-sanitize-recover=all'])]:
                with self.subTest(mode=mode):
                    binary=Path(temporary)/mode
                    result=subprocess.run(['c++','-std=c++17','-Wall','-Wextra','-Werror','-pedantic',
                        *flags,'-I',str(FIRMWARE/'overlays/main/audio'),
                        str(FIRMWARE/'tests/c3_echo_stream_test.cc'),'-o',str(binary)],
                        capture_output=True,text=True)
                    self.assertEqual(result.returncode,0,result.stderr)
                    env=dict(os.environ,ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                             UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
                    if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                        env['LD_PRELOAD']=os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
                    result=subprocess.run([str(binary)],env=env,capture_output=True,text=True,timeout=20)
                    self.assertEqual(result.returncode,0,result.stdout+result.stderr)
