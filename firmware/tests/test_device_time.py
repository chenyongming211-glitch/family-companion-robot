"""Run the freshly prepared OTA time block and early application startup policy."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

FIRMWARE = Path(__file__).resolve().parents[1]


class DeviceTimeTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temporary = tempfile.TemporaryDirectory()
        cls.addClassCleanup(cls.temporary.cleanup)
        cls.work = Path(cls.temporary.name)
        sys.path.insert(0, str(FIRMWARE / 'tools'))
        try:
            import prepare
            import prepare_experiment
            lock = json.loads((FIRMWARE / 'upstream.lock.json').read_text())
            archive = Path(os.environ.get('FIRMWARE_TEST_ARCHIVE', str(
                FIRMWARE.parent / '.superpowers/firmware-baseline/xiaozhi-v2.5.2.tar.gz')))
            if not archive.exists():
                archive = cls.work / 'locked.tar.gz'
                prepare.download_archive(lock['archive_url'], archive, lock['archive_sha256'])
            cls.source = prepare_experiment.prepare_experiment(
                archive, cls.work / 'source', lock, FIRMWARE / 'patches',
                FIRMWARE / 'overlays', FIRMWARE / 'dependencies.lock')
        finally:
            sys.path.pop(0)
        ota = (cls.source / 'main/ota.cc').read_text()
        cls.ota = ota
        # Keep the entire real parser block, including its JSON type checks.
        block = ota[ota.index('    has_server_time_ = false;'):
                    ota.index('    has_new_version_ = false;')]
        app = (cls.source / 'main/application.cc').read_text()
        cls.app = app
        startup = app.split('void Application::Initialize() {', 1)[1].split(
            '    auto& board = Board::GetInstance();', 1)[0]
        helper = '#include "device_time.h"\n' if (cls.source / 'main/device_time.h').exists() else ''
        host = (FIRMWARE / 'tests/device_time_host.cc').read_text().replace(
            '// PRODUCTION_TIME_HEADER', helper).replace('// PRODUCTION_TIME_BLOCK', block).replace(
            '// PRODUCTION_STARTUP_PRELUDE', startup)
        path = cls.work / 'time.cc'
        path.write_text(host)
        cjson = Path(os.environ.get('FAMILY_CONTROL_CJSON_DIR', str(FIRMWARE / '.cache/control-cjson')))
        for name, digest in {
            'cJSON.c': '607e756460fa0de37d20a7a9181f2de29c97bfb7ce5a0e6c2f548243836cd852',
            'cJSON.h': '25b0145150d500498e4d209cec69c18c42cf818bffcc54690be3b895a2a16dee',
        }.items():
            if hashlib.sha256((cjson / name).read_bytes()).hexdigest() != digest:
                raise AssertionError('cJSON fixture checksum mismatch: ' + name)
        obj = cls.work / 'cjson.o'
        flags = ['-fsanitize=address,undefined', '-fno-sanitize-recover=all']
        subprocess.run(['cc', '-std=c11', *flags, '-I' + str(cjson), '-c',
                        str(cjson / 'cJSON.c'), '-o', str(obj)], check=True, capture_output=True)
        cls.binary = cls.work / 'time'
        built = subprocess.run(['c++', '-std=c++20', '-Wall', '-Wextra', '-Werror', '-pedantic',
            *flags, '-I' + str(cjson), '-I' + str(cls.source / 'main'),
            '-I' + str(cls.source / 'main/display'),
            '-I' + str(cls.source / 'main/audio'),
            '-I' + str(cls.source / 'main/boards/m5stack/stopwatch'),
            str(path), str(obj), '-o', str(cls.binary)], capture_output=True, text=True)
        if built.returncode:
            raise AssertionError(built.stderr)

    def run_case(self, case):
        environment = dict(os.environ, TZ='UTC0', ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                           UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
        if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
            environment['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
        result = subprocess.run([str(self.binary), case], capture_output=True, text=True,
                                env=environment, timeout=30)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_ota_writes_utc_without_offset_or_accumulation(self):
        self.run_case('utc')

    def test_sntp_after_ota_preserves_beijing_and_day_rollover(self):
        self.run_case('sntp')

    def test_server_offset_validation_preserves_existing_timezone(self):
        self.run_case('offsets')

    def test_startup_sets_beijing_before_board_or_first_sntp_display(self):
        self.run_case('startup')

    def test_time_policy_is_in_both_real_compilation_units(self):
        for source in (self.ota, self.app):
            self.assertIn('#include "device_time.h"', source)


if __name__ == '__main__':
    unittest.main()
