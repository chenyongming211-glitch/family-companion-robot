"""Clock contract and real production view; only LVGL/time/platform boundaries fake."""
import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest

FIRMWARE=Path(__file__).resolve().parents[1]

class DesktopClockTests(unittest.TestCase):
    def test_clock_build_uses_bounded_digit_subset(self):
        patch=(FIRMWARE/'patches/0026-desktop-clock.patch').read_text()
        self.assertIn('+    list(APPEND SOURCES "display/desktop_clock_digits_font.c")',patch)
        self.assertNotIn('CONFIG_LV_FONT_MONTSERRAT_48',patch)


    def test_clock_and_real_view(self):
        with tempfile.TemporaryDirectory() as directory:
            work=Path(directory)
            for name in ('sdkconfig.h','lvgl.h'):(work/name).write_text('')
            stubs=(FIRMWARE/'tests/chick_face_view_host.cc').read_text().split('// PRODUCTION_VIEW')[0]
            view=(FIRMWARE/'overlays/main/display/chick_face_view.cc').read_text()
            view=re.sub(r'^#include .*\n','',view,flags=re.MULTILINE).replace('time(nullptr)','HostWallTime(nullptr)')
            host=work/'clock.cc'
            host.write_text('#include <ctime>\nstatic time_t wall_seconds=0;static unsigned wall_samples=0;\n'
                'static time_t HostWallTime(time_t*){++wall_samples;return wall_seconds;}\n'+stubs+view+
                (FIRMWARE/'tests/desktop_clock_host.cc').read_text())
            for large in (0,1):
                with self.subTest(builtin48_library_available=large):
                    binary=work/f'clock-{large}'
                    command=['c++','-std=c++20','-pthread','-Wall','-Wextra','-Werror','-pedantic',
                        '-Wno-unused-function','-fsanitize=address,undefined','-fno-sanitize-recover=all',
                        '-DCONFIG_CHICK_FACE_UI=1',f'-DLV_FONT_MONTSERRAT_48={large}',
                        '-I',str(work),'-I',str(FIRMWARE/'overlays/main/audio'),
                        '-I',str(FIRMWARE/'overlays/main/boards/m5stack/stopwatch'),
                        '-I',str(FIRMWARE/'overlays/main/display'),str(host),'-o',str(binary)]
                    built=subprocess.run(command,capture_output=True,text=True)
                    self.assertEqual(built.returncode,0,built.stderr)
                    environment=dict(os.environ,ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                        UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
                    if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                        environment['LD_PRELOAD']=os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
                    ran=subprocess.run([str(binary)],capture_output=True,text=True,env=environment,timeout=30)
                    self.assertEqual(ran.returncode,0,ran.stdout+ran.stderr)
                    print(ran.stdout.strip())

if __name__=='__main__':unittest.main()
