"""Compile production methods from the checksum-locked, fully patched source.

Only hardware/task/transport dependencies are host fakes; wake sequencing and
control JSON serialization are extracted verbatim, never reimplemented here.
FIRMWARE_WAKE_SOURCE_ROOT can select an older prepared source for RED evidence.
"""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

FIRMWARE = Path(__file__).resolve().parents[1]
ROOT = FIRMWARE.parent


def method(text, signature):
    start = text.index(signature)
    brace = text.index('{', start)
    depth = 0
    # These methods have balanced braces in their strings/comments too.
    for end in range(brace, len(text)):
        depth += (text[end] == '{') - (text[end] == '}')
        if depth == 0:
            return text[start:end + 1]
    raise ValueError('unterminated production method')


class WakeRecoveryTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory()
        cls.addClassCleanup(cls.temp.cleanup)
        cls.work = Path(cls.temp.name)
        override = os.environ.get('FIRMWARE_WAKE_SOURCE_ROOT')
        if override:
            cls.source = Path(override)
        else:
            sys.path.insert(0, str(FIRMWARE / 'tools'))
            try:
                import prepare_experiment
                import prepare
                lock = json.loads((FIRMWARE / 'upstream.lock.json').read_text())
                archive = ROOT / '.superpowers/firmware-baseline/xiaozhi-v2.5.2.tar.gz'
                if not archive.exists():
                    archive = cls.work / 'locked.tar.gz'
                    prepare.download_archive(lock['archive_url'], archive, lock['archive_sha256'])
                cls.source = prepare_experiment.prepare_experiment(
                    archive, cls.work / 'source', lock, FIRMWARE / 'patches',
                    FIRMWARE / 'overlays', FIRMWARE / 'dependencies.lock')
            finally:
                sys.path.pop(0)
        app = (cls.source / 'main/application.cc').read_text()
        protocol = (cls.source / 'main/protocols/protocol.cc').read_text()
        methods = [method(app, 'void Application::' + name + '(') for name in
                   ['BeginWakeWordInvoke', 'ContinueWakeWordInvoke', 'StartListeningAudio', 'SetListeningMode', 'HandleWakeWordDetectedEvent']]
        if 'void Application::RecoverListeningFailure(' in app:
            methods.append(method(app, 'void Application::RecoverListeningFailure('))
        methods.append(method((cls.source / 'main/audio/playback_application.inc').read_text(),
                              'void Application::ScheduleAudioChannelClosed('))
        controls = [method(protocol, prefix + ' Protocol::' + name + '(')
                    for name in ['SendWakeWordDetected', 'SendStartListening']
                    for prefix in ['bool' if 'bool Protocol::' + name + '(' in protocol else 'void']]
        declarations = '\n'.join(x[:x.index('{')].replace('Protocol::', '') + ';' for x in controls)
        cls.program = (FIRMWARE / 'tests/wake_recovery_host.cc').read_text().replace(
            '// PROTOCOL_DECLARATIONS', declarations).replace('// PRODUCTION_METHODS', '\n'.join(controls + methods))

    def run_config(self, aec, cache):
        source = self.work / f'host-{aec}-{cache}.cc'
        source.write_text(self.program)
        binary = source.with_suffix('')
        result = subprocess.run(['c++', '-std=c++17', '-pthread', '-Wall', '-Wextra', '-Werror', '-Wno-unused-parameter', '-pedantic',
                                 '-fsanitize=address,undefined', '-fno-sanitize-recover=all',
                                 f'-DCONFIG_STOPWATCH_SOFTWARE_AEC={aec}',
                                 f'-DCONFIG_SEND_WAKE_WORD_DATA={cache}', str(source), '-o', str(binary)],
                                capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        environment = dict(os.environ, ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                           UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
        if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
            environment['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
        for scenario in [[], ['close-order']]:
            with self.subTest(scenario=scenario):
                result = subprocess.run([str(binary)] + scenario, env=environment, capture_output=True, text=True, timeout=20)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_software_aec_wake_cache(self):
        self.run_config(1, 1)

    def test_non_aec_wake_cache(self):
        self.run_config(0, 1)

    def test_software_aec_without_wake_cache(self):
        self.run_config(1, 0)

    def test_non_aec_without_wake_cache(self):
        self.run_config(0, 0)
