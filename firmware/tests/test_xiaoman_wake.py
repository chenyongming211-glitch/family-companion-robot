"""Host checks of production MultiNet guards; no speech model runs on the host."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

FIRMWARE = Path(__file__).resolve().parents[1]


def method(text, signature):
    start = text.index(signature)
    brace = text.index('{', start)
    depth = 0
    for end in range(brace, len(text)):
        depth += (text[end] == '{') - (text[end] == '}')
        if depth == 0:
            return text[start:end + 1]
    raise ValueError('unterminated production method')


class XiaomanWakeTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory()
        cls.addClassCleanup(cls.temp.cleanup)
        cls.work = Path(cls.temp.name)
        override = os.environ.get('FIRMWARE_XIAOMAN_SOURCE_ROOT')
        if override:
            cls.source = Path(override)
        else:
            sys.path.insert(0, str(FIRMWARE / 'tools'))
            try:
                import prepare
                import prepare_experiment
                lock = json.loads((FIRMWARE / 'upstream.lock.json').read_text())
                archive = FIRMWARE.parent / '.superpowers/firmware-baseline/xiaozhi-v2.5.2.tar.gz'
                if not archive.exists():
                    archive = cls.work / 'locked.tar.gz'
                    prepare.download_archive(lock['archive_url'], archive, lock['archive_sha256'])
                cls.source = prepare_experiment.prepare_experiment(
                    archive, cls.work / 'source', lock, FIRMWARE / 'patches',
                    FIRMWARE / 'overlays', FIRMWARE / 'dependencies.lock')
            finally:
                sys.path.pop(0)

    def test_production_multinet_registration_and_epoch_boundaries(self):
        custom = (self.source / 'main/audio/wake_words/custom_wake_word.cc').read_text()
        self.assertIn('void CustomWakeWord::ResetCapture(', custom,
                      'MultiNet has no capture-epoch reset; old partial/model state can cross a restart')
        methods = [method(custom, signature) for signature in [
            'bool CustomWakeWord::Initialize(', 'void CustomWakeWord::Start(',
            'void CustomWakeWord::Stop(', 'void CustomWakeWord::ResetCapture(',
            'void CustomWakeWord::FeedSamples(', 'void CustomWakeWord::FeedMono(']]
        engine = (self.source / 'main/audio/engines/afe_audio_engine.cc').read_text()
        methods += [method(engine, 'void AfeAudioEngine::ResetWakeWordCapture('),
                    method(engine, 'void AfeAudioEngine::HandleWakeWordResult('),
                    method(engine, 'bool AfeAudioEngine::GetExperimentWakeWordOpus(')]
        program = (FIRMWARE / 'tests/xiaoman_wake_host.cc').read_text().replace(
            '// PRODUCTION_METHODS', '\n'.join(methods))
        for marker, name, owner in [
                ('// FEED_SAMPLES_DECLARATION', 'FeedSamples', 'CustomWakeWord'),
                ('// FEED_MONO_DECLARATION', 'FeedMono', 'CustomWakeWord'),
                ('// WAKE_RESULT_DECLARATION', 'HandleWakeWordResult', 'AfeAudioEngine')]:
            definition = next(body for body in methods if f'{owner}::{name}(' in body)
            declaration = definition[:definition.index('{')].replace(f'{owner}::', '') + ';'
            program = program.replace(marker, declaration)
        source = self.work / 'xiaoman.cc'
        source.write_text(program)
        binary = source.with_suffix('')
        build = subprocess.run([
            'c++', '-std=c++17', '-pthread', '-Wall', '-Wextra', '-Werror', '-pedantic',
            '-Wno-unused-parameter', '-Wno-sign-compare',
            '-fsanitize=address,undefined', '-fno-sanitize-recover=all',
            '-I', str(self.source / 'main/audio'), str(source), '-o', str(binary)],
            capture_output=True, text=True)
        self.assertEqual(build.returncode, 0, build.stderr)
        environment = dict(os.environ, ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                           UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
        # An optional runner override puts ASan before a system-wide loader preload.
        if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
            environment['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
        for scenario in ['valid', 'create-failure', 'add-failure', 'update-failure',
                         'empty-commands', 'software-cache', 'ordinary-cache',
                         'partial-reset', 'engine-cache-before-detect', 'engine-epoch-reset',
                         'invalid-command-id', 'epoch-during-detect', 'tagged-valid',
                         'tagged-old-batch', 'tagged-old-generation',
                         'engine-store-capture-only', 'engine-store-capture-and-wake',
                         'nonsoftware-stale-source', 'hardware-software-cache',
                         'hardware-partial-reset', 'hardware-epoch-during-detect',
                         'hardware-engine-cache-before-detect', 'hardware-engine-epoch-reset',
                         'hardware-engine-store-capture-only', 'hardware-engine-store-capture-and-wake',
                         'hardware-tagged-valid', 'hardware-tagged-old-batch',
                         'hardware-tagged-old-generation']:
            with self.subTest(scenario=scenario):
                result = subprocess.run([str(binary), scenario], capture_output=True,
                                        text=True, timeout=15, env=environment)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_experiment_encoder_and_both_reset_owners_include_multinet(self):
        engine = (self.source / 'main/audio/engines/afe_audio_engine.cc').read_text()
        for signature in ['void AfeAudioEngine::EncodeExperimentWakeWordData(',
                          'bool AfeAudioEngine::GetExperimentWakeWordOpus(']:
            body = method(engine, signature)
            self.assertNotIn('custom_wake_word_->', body,
                             'software AEC MultiNet still bypasses the tagged wake encoder')
            self.assertNotIn('wake_detector_ != WakeDetector::kWakeNet', body)
        for signature in ['void AfeAudioEngine::ApplyPendingReset(',
                          'void AfeAudioEngine::ProcessingTask(']:
            self.assertIn('ResetWakeWordCapture()', method(engine, signature))

    def test_xiaoman_variant_keeps_old_variants_and_uses_real_chinese_model(self):
        config = json.loads((self.source / 'main/boards/m5stack/stopwatch/config.json').read_text())
        variants = {row['name']: row for row in config['builds']}
        self.assertTrue({'m5stack-stopwatch', 'm5stack-stopwatch-software-aec'} <= variants.keys())
        self.assertIn('m5stack-stopwatch-xiaoman', variants)
        options = variants['m5stack-stopwatch-xiaoman']['sdkconfig_append']
        self.assertFalse(any(option.startswith(('CONFIG_LANGUAGE_', 'CONFIG_SR_WN_'))
                             for option in options),
                         'upstream build parameters own language and WakeNet selection')
        for expected in ['CONFIG_STOPWATCH_SOFTWARE_AEC=y', 'CONFIG_USE_DEVICE_AEC=y',
                         'CONFIG_USE_CUSTOM_WAKE_WORD=y', 'CONFIG_USE_AFE_WAKE_WORD=n',
                         'CONFIG_SR_MN_CN_MULTINET7_QUANT=y',
                         'CONFIG_CUSTOM_WAKE_WORD="xiao man"',
                         'CONFIG_CUSTOM_WAKE_WORD_DISPLAY="小满"',
                         'CONFIG_CUSTOM_WAKE_WORD_THRESHOLD=20', 'CONFIG_SEND_WAKE_WORD_DATA=y']:
            self.assertIn(expected, options)


if __name__ == '__main__':
    unittest.main()
