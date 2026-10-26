"""Compile actual AFE controls/readiness and the public wake-batch header guard."""
import os
from pathlib import Path
import subprocess
import unittest

import test_realtime_default as defaults


class WaveshareAfeTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        defaults.RealtimeDefaultTests.setUpClass.__func__(cls)

    def test_production_aec_applied_state_and_public_wake_batch_guard(self):
        engine = (self.source/'main/audio/engines/afe_audio_engine.cc').read_text()
        header = (self.source/'main/audio/engines/afe_audio_engine.h').read_text()
        self.assertTrue('void AfeAudioEngine::NotifyProcessedCaptureReady(' in engine,
                        'processed readiness must distinguish applied AEC from physical reference validity')
        methods = '\n'.join(defaults.method(engine, signature) for signature in [
            'void AfeAudioEngine::ApplyAfeControls(',
            'void AfeAudioEngine::NotifyProcessedCaptureReady('])
        batch = defaults.method(header, 'bool WakeAudioBatchValid()').replace(' override', '')
        processing = defaults.method(engine, 'void AfeAudioEngine::ProcessingTask(')
        self.assertIn('NotifyProcessedCaptureReady(capture);', processing)
        self.assertGreater(processing.index('NotifyProcessedCaptureReady(capture);'),
                           processing.index('result->ret_value == ESP_FAIL'))
        self.assertIn('applied_aec_enabled_', header)
        harness = (defaults.FIRMWARE/'tests/waveshare_afe_host.cc').read_text()
        # Reuse the same platform plumbing as the codec harness. Production
        # lifecycle/playback/DMA methods and all AFE decisions compile unchanged.
        platform = (defaults.FIRMWARE/'tests/waveshare_codec_host.cc').read_text().split('int main() {', 1)[0]
        source = self.work/'waveshare-afe.cc'
        source.write_text(platform+harness.replace('// PRODUCTION_METHODS', methods).replace(
            '// PRODUCTION_BATCH_GUARD', batch))
        (self.work/'codecs').mkdir()
        (self.work/'codecs/box_audio_codec.h').write_text('// Platform declared by the harness.\n')
        (self.work/'sdkconfig.h').write_text('#define CONFIG_WAVESHARE_XIAOMAN_REALTIME 1\n')
        binary = source.with_suffix('')
        board = defaults.FIRMWARE/'overlays/main/boards/waveshare/esp32-s3-touch-amoled-2.16'
        result = subprocess.run(['c++', '-std=c++17', '-pthread', '-Wall', '-Wextra', '-Werror', '-pedantic',
            '-fsanitize=address,undefined', '-fno-sanitize-recover=all',
            '-DCONFIG_XIAOMAN_ORB_UI=1',
            '-I', str(self.work), '-I', str(board), '-I', str(defaults.FIRMWARE/'overlays/main/audio'),
            str(source), '-o', str(binary)], capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        environment = dict(os.environ, ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                           UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
        if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
            environment['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
        result = subprocess.run([str(binary)], env=environment, capture_output=True, text=True, timeout=20)
        self.assertEqual(result.returncode, 0, result.stdout+result.stderr)
        print(result.stdout.strip())
