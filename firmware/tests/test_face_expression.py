"""Bound expression lifecycle, using production DMA ledger and face model."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
FIRMWARE=Path(__file__).resolve().parents[1]
class FaceExpressionTests(unittest.TestCase):
    def test_actual_dma_lifecycle(self):
        with tempfile.TemporaryDirectory() as directory:
            binary=Path(directory)/'face_expression'
            built=subprocess.run(['c++','-std=c++20','-Wall','-Wextra','-Werror','-pedantic',
                '-fsanitize=address,undefined','-fno-sanitize-recover=all',
                '-DCONFIG_CHICK_FACE_UI=1','-I',str(FIRMWARE/'overlays/main/display'),
                '-I',str(FIRMWARE/'overlays/main/audio'),str(FIRMWARE/'tests/face_expression_host.cc'),
                '-o',str(binary)],capture_output=True,text=True)
            self.assertEqual(built.returncode,0,built.stderr)
            environment=dict(os.environ,ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
            if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                environment['LD_PRELOAD']=os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
            for case in ("dma","timeline","windows","hidden","invalid","delayed","legacy","backward","stale","late_drain","hidden_drain","shown_drain","terminal_late","terminal_unseen","hidden_terminal"):
                with self.subTest(case=case):
                    ran=subprocess.run([str(binary),case],capture_output=True,text=True,env=environment,timeout=30)
                    self.assertEqual(ran.returncode,0,ran.stdout+ran.stderr)
                    print(ran.stdout.strip())
if __name__=='__main__':unittest.main()
