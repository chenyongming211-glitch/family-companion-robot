"""Compile the production constructor, mode selector and runtime AEC guard on the host."""
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


class RealtimeDefaultTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory()
        cls.addClassCleanup(cls.temp.cleanup)
        cls.work = Path(cls.temp.name)
        override = os.environ.get('FIRMWARE_REALTIME_SOURCE_ROOT')
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
        app = (cls.source / 'main/application.cc').read_text()
        methods = [method(app, signature) for signature in [
            'Application::Application()', 'ListeningMode Application::GetDefaultListeningMode()',
            'void Application::SetAecMode(']]
        cls.program = (FIRMWARE / 'tests/realtime_default_host.cc').read_text().replace(
            '// PRODUCTION_METHODS', '\n'.join(methods))

    def run_config(self, software, device, server, default_realtime):
        source = self.work / f'realtime-{software}-{device}-{server}-{default_realtime}.cc'
        source.write_text(self.program)
        binary = source.with_suffix('')
        result = subprocess.run([
            'c++', '-std=c++20', '-pthread', '-Wall', '-Wextra', '-Werror', '-pedantic',
            '-Wno-unused-parameter', '-fsanitize=address,undefined', '-fno-sanitize-recover=all',
            f'-DCONFIG_STOPWATCH_SOFTWARE_AEC={software}', f'-DCONFIG_USE_DEVICE_AEC={device}',
            f'-DCONFIG_USE_SERVER_AEC={server}', f'-DCONFIG_STOPWATCH_DEFAULT_REALTIME={default_realtime}',
            str(source), '-o', str(binary)], capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        environment = dict(os.environ, ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                           UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
        if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
            environment['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
        result = subprocess.run([str(binary)], env=environment, capture_output=True,
                                text=True, timeout=20)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_xiaoman_realtime_intent_waits_for_eligible_reference_and_respects_off(self):
        self.run_config(1, 1, 0, 1)

    def test_old_software_aec_variant_starts_off_and_keeps_explicit_on_across_pause(self):
        self.run_config(1, 1, 0, 0)

    def test_original_stopwatch_variant_starts_off(self):
        self.run_config(0, 0, 0, 0)

    def test_native_device_aec_keeps_existing_realtime_behavior(self):
        self.run_config(0, 1, 0, 0)

    def test_server_aec_keeps_existing_realtime_behavior(self):
        self.run_config(0, 0, 1, 0)

    def test_only_xiaoman_variant_requests_default_realtime(self):
        config = json.loads((self.source / 'main/boards/m5stack/stopwatch/config.json').read_text())
        for variant in config['builds']:
            enabled = 'CONFIG_STOPWATCH_DEFAULT_REALTIME=y' in variant['sdkconfig_append']
            self.assertEqual(enabled, variant['name'] == 'm5stack-stopwatch-xiaoman')
        kconfig = (self.source / 'main/Kconfig.projbuild').read_text()
        self.assertIn('config STOPWATCH_DEFAULT_REALTIME', kconfig)
        block = kconfig.split('config STOPWATCH_DEFAULT_REALTIME', 1)[1].split('\nconfig ', 1)[0]
        self.assertIn('default n', block)
        self.assertIn('depends on STOPWATCH_SOFTWARE_AEC && USE_DEVICE_AEC', block)


if __name__ == '__main__':
    unittest.main()
