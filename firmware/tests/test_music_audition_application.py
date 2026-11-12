"""Execute actual audition worker/application code against host platform stubs.

Run on the cloud host only. USB, scheduler, button and physical DMA completion
are explicit simulations; passing is not device or acoustic playback evidence.
"""
from pathlib import Path
import os
import re
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class MusicAuditionApplicationTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory()
        cls.work = Path(cls.temp.name)
        (cls.work / 'driver').mkdir()
        for name in ('usb_serial_jtag.h', 'usb_serial_jtag_vfs.h'):
            (cls.work / 'driver' / name).write_text('// Platform supplied by host harness.\n')
        patch = (ROOT / 'patches/0025-music-audition.patch').read_text()
        state = re.findall(r'^\+    if\(MusicAudition::Active\(\) && state!=kDeviceStateNotifying\).*$', patch, re.M)
        button = re.search(r' void Application::HandleToggleChatEvent\(\) \{.*?^\+(    if\(MusicAudition::Active\(\)\).*)$', patch, re.M | re.S)
        if len(state) != 1 or button is None:
            raise AssertionError('actual application guards not found in scoped patch')
        (cls.work / 'actual_state_guard.inc').write_text(state[0][1:] + '\n')
        (cls.work / 'actual_button_guard.inc').write_text(button.group(1).splitlines()[0] + '\n')
        cls.binary = cls.work / 'music-application'
        compile_result = subprocess.run([
            'c++', '-std=c++17', '-Wall', '-Wextra', '-Werror',
            '-fsanitize=address,undefined', '-fno-sanitize-recover=all',
            '-DCONFIG_FAMILY_MUSIC_AUDITION=1',
            '-I', str(cls.work), '-I', str(ROOT / 'overlays/main/audio'),
            str(ROOT / 'tests/music_audition_application_host.cc'), '-o', str(cls.binary),
        ], text=True, capture_output=True, timeout=60)
        if compile_result.returncode:
            raise AssertionError(compile_result.stdout + compile_result.stderr)

    @classmethod
    def tearDownClass(cls):
        cls.temp.cleanup()

    def test_actual_worker_and_application_scenarios(self):
        env = dict(os.environ, ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                   UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
        if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
            env['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
        for scenario in (
            'app-admission', 'guards', 'valid24', 'valid48', 'fragmented',
            'busy-start', 'busy-notifying', 'invalid-start', 'crc', 'replay', 'bad-count',
            'bad-size', 'start-active', 'short-end', 'partial-header',
            'cancel', 'read-timeout', 'cancel-drain', 'drain-timeout',
            'begin-failure', 'restore-failure',
            'main-timeout', 'quiet-drain', 'quiet-timeout', 'quiet-cancel',
        ):
            with self.subTest(scenario=scenario):
                result = subprocess.run([str(self.binary), scenario], env=env,
                                        text=True, capture_output=True, timeout=10)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)


if __name__ == '__main__':
    unittest.main()
