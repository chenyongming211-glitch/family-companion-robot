"""Run the actual task/heap diagnostic method; no hardware or PCM fixtures."""
import os
from pathlib import Path
import subprocess
import unittest

import test_realtime_default as defaults

FIRMWARE=Path(__file__).resolve().parents[1]


class C3ResourceTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        defaults.RealtimeDefaultTests.setUpClass.__func__(cls)

    def run_case(self,enabled,case):
        service=(self.source/'main/audio/audio_service.cc').read_text()
        signature='void AudioService::PrintC3Resources() const'
        self.assertTrue(signature in service,'the C3 task resource diagnostic is absent')
        methods='\n'.join([defaults.method(service,signature),
                           defaults.method(service,'void AudioService::Start(')])
        program=(FIRMWARE/'tests/c3_resources_host.cc').read_text().replace(
            '// PRODUCTION_METHOD',methods)
        source=self.work/'c3-resources.cc';source.write_text(program)
        for mode,flags in [('optimized',['-O2']),('sanitized',['-O1','-g',
                '-fsanitize=address,undefined','-fno-sanitize-recover=all'])]:
            with self.subTest(mode=mode):
                binary=self.work/f'resources-{enabled}-{mode}'
                result=subprocess.run(['c++','-std=c++17','-Wall','-Wextra','-Werror',
                    '-pedantic',*flags,
                    f'-DCONFIG_XMINI_C3_VOICE_BARGE={enabled}',str(source),'-o',str(binary)],
                    capture_output=True,text=True)
                self.assertEqual(result.returncode,0,result.stderr)
                env=dict(os.environ,ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                         UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
                if mode=='sanitized' and os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                    env['LD_PRELOAD']=os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
                result=subprocess.run([str(binary),case],env=env,capture_output=True,text=True,timeout=20)
                self.assertEqual(result.returncode,0,result.stdout+result.stderr)

    def test_c3_reports_internal_heap_and_its_input_and_opus_tasks(self):
        self.run_case(1,'started')

    def test_c3_not_started_does_not_accidentally_measure_the_main_task(self):
        self.run_case(1,'not-started')

    def test_other_variants_do_not_measure_or_log_c3_resources(self):
        self.run_case(0,'started')

    def test_stopped_service_does_not_dereference_stale_worker_handles(self):
        self.run_case(1,'stopped')

    def test_restart_publishes_no_deleted_handles_before_creating_new_workers(self):
        self.run_case(1,'restart')


if __name__=='__main__':unittest.main()
