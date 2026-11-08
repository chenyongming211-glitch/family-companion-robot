"""Retained actual-DMA/queue observations and new display lifecycle regressions."""
import os
import re
from pathlib import Path
import subprocess
import tempfile
import unittest

FIRMWARE=Path(__file__).resolve().parents[1]

class AudioUiActivityTests(unittest.TestCase):
    def test_production_observers_and_face_display(self):
        with tempfile.TemporaryDirectory() as directory:
            work=Path(directory)
            for name in ['sdkconfig.h','lvgl.h']:(work/name).write_text('')
            for name in ['playback_adapter_test.cc','playback_websocket_test.cc',
                         'audio_activity_host.cc','chick_face_display_host.cc','face_layout_host.cc',
                         'chick_face_view_host.cc']:
                binary=work/name.removesuffix('.cc')
                source=FIRMWARE/'tests'/name
                if name=='chick_face_view_host.cc':
                    view=(FIRMWARE/'overlays/main/display/chick_face_view.cc').read_text()
                    view=re.sub(r'^#include .*\n', '', view, flags=re.MULTILINE)
                    source=work/name
                    source.write_text((FIRMWARE/'tests'/name).read_text().replace('// PRODUCTION_VIEW',view))
                command=['c++','-std=c++20','-pthread','-Wall','-Wextra','-Werror','-pedantic',
                    '-Wno-unused-function','-Werror=deprecated-enum-enum-conversion',
                    '-fsanitize=address,undefined','-fno-sanitize-recover=all',
                    '-DCONFIG_CHICK_FACE_UI=1','-I',str(work),
                    '-I',str(FIRMWARE/'overlays/main/audio'),
                    '-I',str(FIRMWARE/'overlays/main/boards/m5stack/stopwatch'),
                    '-I',str(FIRMWARE/'overlays/main/display'),
                    str(source),'-o',str(binary)]
                built=subprocess.run(command,capture_output=True,text=True)
                self.assertEqual(built.returncode,0,built.stderr)
                environment=dict(os.environ,ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                                 UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
                if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                    environment['LD_PRELOAD']=os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
                ran=subprocess.run([str(binary)],capture_output=True,text=True,env=environment,timeout=30)
                self.assertEqual(ran.returncode,0,ran.stdout+ran.stderr)

if __name__=='__main__':unittest.main()
