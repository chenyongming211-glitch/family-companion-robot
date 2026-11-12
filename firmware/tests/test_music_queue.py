"""Compile the actual queue method and hold its lock across music acquisition."""
import os
import subprocess
import unittest
import test_realtime_default as defaults

class MusicQueueTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):defaults.RealtimeDefaultTests.setUpClass.__func__(cls)
    def test_admission_rechecks_exclusion_after_lock_and_wait(self):
        actual=defaults.method((self.source/'main/audio/audio_service.cc').read_text(),
                              'bool AudioService::PushPacketToDecodeQueue(')
        source=self.work/'music-queue.cc';binary=self.work/'music-queue'
        source.write_text((defaults.FIRMWARE/'tests/music_queue_host.cc').read_text().replace('// PRODUCTION_QUEUE_METHOD',actual))
        result=subprocess.run(['c++','-std=c++20','-pthread','-Wall','-Wextra','-Werror',
            '-fsanitize=address,undefined','-fno-sanitize-recover=all','-DCONFIG_FAMILY_MUSIC_AUDITION=1',
            str(source),'-o',str(binary)],capture_output=True,text=True)
        self.assertEqual(result.returncode,0,result.stderr)
        env=dict(os.environ,ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
        if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):env['LD_PRELOAD']=os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
        result=subprocess.run([str(binary)],env=env,capture_output=True,text=True,timeout=15)
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)
