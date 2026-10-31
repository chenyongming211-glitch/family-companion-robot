"""Bounded storage and ownership tests for the actual C3 uplink implementation."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

FIRMWARE=Path(__file__).resolve().parents[1]


class C3UplinkHandoffTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp=tempfile.TemporaryDirectory();cls.addClassCleanup(cls.temp.cleanup)
        cls.binaries={}
        for mode,flags in [('optimized',['-O2']),('sanitized',['-O1','-g',
                '-fsanitize=address,undefined','-fno-sanitize-recover=all'])]:
            binary=Path(cls.temp.name)/mode
            result=subprocess.run(['c++','-std=c++17','-pthread','-Wall','-Wextra','-Werror',
                '-pedantic',*flags,'-DCONFIG_XMINI_C3_VOICE_BARGE=1',
                '-I',str(FIRMWARE/'overlays/main/audio'),
                '-I',str(FIRMWARE/'tests/c3_sdk_stubs'),
                str(FIRMWARE/'tests/c3_uplink_handoff_host.cc'),'-o',str(binary)],
                capture_output=True,text=True)
            if result.returncode:raise AssertionError(result.stderr)
            cls.binaries[mode]=binary

    def run_case(self,case):
        for mode,binary in self.binaries.items():
            with self.subTest(mode=mode):
                env=dict(os.environ,ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                         UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
                if mode=='sanitized' and os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                    env['LD_PRELOAD']=os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
                result=subprocess.run([str(binary),case],env=env,capture_output=True,text=True,timeout=10)
                self.assertEqual(result.returncode,0,result.stdout+result.stderr)

    def test_working_pcm_storage_never_exceeds_one_opus_frame(self):self.run_case('bounded')
    def test_handoff_transfers_storage_and_keeps_queued_pcm_alive(self):self.run_case('ownership')
    def test_arbitrary_chunks_preserve_every_processed_sample_in_order(self):self.run_case('sequence')
    def test_epoch_and_capture_generation_drop_only_the_previous_partial_frame(self):self.run_case('epoch')
    def test_close_during_handoff_stops_remaining_frames(self):self.run_case('close')
    def test_invalid_frame_size_fails_without_emitting_or_looping(self):self.run_case('invalid-frame')


if __name__=='__main__':unittest.main()
