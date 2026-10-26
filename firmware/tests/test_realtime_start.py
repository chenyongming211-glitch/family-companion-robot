"""Exercise the production deferred listening-start methods with explicit ready events."""
import os
import subprocess
import unittest

import test_realtime_default as defaults


class RealtimeStartTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        defaults.RealtimeDefaultTests.setUpClass.__func__(cls)

    def test_current_capture_ready_is_required_before_start_and_uplink(self):
        app = (self.source/'main/application.cc').read_text()
        service = (self.source/'main/audio/audio_service.cc').read_text()
        signatures = ['void Application::CancelPendingRealtimeStart(',
                      'void Application::ContinueRealtimeListeningStart(',
                      'void Application::StartListeningAudio(',
                      'bool Application::SetDeviceState(',
                      'ListeningMode Application::GetDefaultListeningMode(',
                      'void Application::SetAecMode(']
        for signature in signatures:
            self.assertTrue(signature in app, 'missing controlled startup method: '+signature)
        self.assertIn('void AudioService::NotifyCaptureReady(', service)
        methods = [defaults.method(app, signature) for signature in signatures]
        methods.append(defaults.method(service, 'void AudioService::NotifyCaptureReady('))
        program = (defaults.FIRMWARE/'tests/realtime_start_host.cc').read_text().replace(
            '// PRODUCTION_METHODS', '\n'.join(methods))
        source = self.work/'realtime-start.cc'
        source.write_text(program)
        for enabled, hardware in ((0, 0), (1, 0), (0, 1)):
            binary = self.work/f'realtime-start-{enabled}-{hardware}'
            result = subprocess.run([
                'c++', '-std=c++20', '-pthread', '-Wall', '-Wextra', '-Werror', '-pedantic',
                '-Wno-unused-parameter', '-fsanitize=address,undefined', '-fno-sanitize-recover=all',
                '-DCONFIG_STOPWATCH_SOFTWARE_AEC=1',
                f'-DCONFIG_STOPWATCH_DEFAULT_REALTIME={enabled}',
                f'-DCONFIG_WAVESHARE_XIAOMAN_REALTIME={hardware}', str(source), '-o', str(binary)],
                capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            environment = dict(os.environ, ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                               UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
            if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                environment['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
            result = subprocess.run([str(binary)], env=environment, capture_output=True,
                                    text=True, timeout=20)
            self.assertEqual(result.returncode, 0, result.stdout+result.stderr)

    def test_ready_hint_is_bound_to_fetch_capture_and_dispatched_on_main_task(self):
        app = (self.source/'main/application.cc').read_text()
        engine = (self.source/'main/audio/engines/afe_audio_engine.cc').read_text()
        service = (self.source/'main/audio/audio_service.cc').read_text()
        self.assertIn('MAIN_EVENT_CAPTURE_READY', defaults.method(app, 'void Application::Run('))
        self.assertIn('ContinueRealtimeListeningStart()', defaults.method(app, 'void Application::Run('))
        self.assertIn('callbacks.on_capture_ready', defaults.method(app, 'void Application::Initialize('))
        self.assertIn('emitted_capture_generation_ = capture;', defaults.method(engine, 'void AfeAudioEngine::ProcessingTask('))
        self.assertIn('NotifyCaptureReady()', defaults.method(service, 'void AudioService::Initialize('))


if __name__ == '__main__':
    unittest.main()
