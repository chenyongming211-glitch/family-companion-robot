"""Shared S3 connecting transition must revoke capture before blocking work."""
import os
import subprocess
import unittest

import test_realtime_default as defaults


class S3ConnectCaptureTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        defaults.RealtimeDefaultTests.setUpClass.__func__(cls)

    def test_dma_does_not_accumulate_before_scheduled_connection(self):
        app = (self.source / 'main/application.cc').read_text()
        body = '\n'.join(defaults.method(app, signature) for signature in [
            'bool Application::SetDeviceState(', 'void Application::HandleStateChangedEvent()',
            'void Application::ContinueOpenAudioChannel(', 'void Application::BeginWakeWordInvoke(',
            'void Application::ContinueWakeWordInvoke(', 'void Application::SetListeningMode('])
        program = (defaults.FIRMWARE / 'tests/s3_connect_capture_host.cc').read_text()
        source = self.work / 'connect-capture.cc'
        source.write_text(program.replace('// PRODUCTION_TRANSITION', body))
        for software, hardware in ((1, 0), (0, 1)):
            with self.subTest(software=software, hardware=hardware):
                binary = self.work / f'connect-capture-{software}-{hardware}'
                compiled = subprocess.run([
                    'c++', '-std=c++20', '-pthread', '-Wall', '-Wextra', '-Werror',
                    '-pedantic', '-fsanitize=address,undefined',
                    '-fno-sanitize-recover=all',
                    '-DCONFIG_SEND_WAKE_WORD_DATA=1',
                    f'-DCONFIG_STOPWATCH_SOFTWARE_AEC={software}',
                    f'-DCONFIG_STOPWATCH_DEFAULT_REALTIME={software}',
                    f'-DCONFIG_WAVESHARE_XIAOMAN_REALTIME={hardware}',
                    '-I', str(defaults.FIRMWARE / 'overlays/main/audio'),
                    '-I', str(defaults.FIRMWARE / 'overlays/main/boards/m5stack/stopwatch'),
                    str(source), '-o', str(binary)], capture_output=True, text=True)
                self.assertEqual(compiled.returncode, 0, compiled.stderr)
                environment = dict(os.environ, ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                                   UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
                if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                    environment['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
                executed = subprocess.run([str(binary)], env=environment, capture_output=True,
                                          text=True, timeout=20)
                self.assertEqual(executed.returncode, 0, executed.stdout + executed.stderr)


if __name__ == '__main__':
    unittest.main()
