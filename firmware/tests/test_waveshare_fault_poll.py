"""Run the actual shared read entry with the production Waveshare fault adapter."""
import os
import subprocess
import unittest
import test_realtime_default as defaults

class WaveshareFaultPollTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        defaults.RealtimeDefaultTests.setUpClass.__func__(cls)

    def test_actual_read_entry_flushes_fault_before_gate(self):
        method = defaults.method((self.source/'main/audio/audio_service.cc').read_text(),
                                 'bool AudioService::ReadAudioData(')
        source = self.work/'fault-poll.cc'
        source.write_text((defaults.FIRMWARE/'tests/waveshare_fault_poll_host.cc').read_text().replace(
            '// PRODUCTION_METHOD', method))
        (self.work/'codecs').mkdir()
        (self.work/'codecs/box_audio_codec.h').write_text('// Platform provided by harness.\n')
        (self.work/'sdkconfig.h').write_text('#define CONFIG_WAVESHARE_XIAOMAN_REALTIME 1\n')
        binary = self.work/'fault-poll'
        board = defaults.FIRMWARE/'overlays/main/boards/waveshare/esp32-s3-touch-amoled-2.16'
        compiled = subprocess.run(['c++', '-std=c++17', '-pthread', '-Wall', '-Wextra', '-Werror',
            '-fsanitize=address,undefined', '-fno-sanitize-recover=all', '-DCONFIG_CHICK_FACE_UI=1',
            '-I', str(self.work), '-I', str(defaults.FIRMWARE/'tests'), '-I', str(board),
            '-I', str(defaults.FIRMWARE/'overlays/main/audio'), str(source), '-o', str(binary)],
            capture_output=True, text=True)
        self.assertEqual(compiled.returncode, 0, compiled.stderr)
        env = dict(os.environ, ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                   UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
        if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
            env['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
        result = subprocess.run([str(binary)], env=env, capture_output=True, text=True, timeout=20)
        self.assertEqual(result.returncode, 0, result.stdout+result.stderr)
        print(result.stdout.strip())
