"""Execute the actual production decode-rate selector with bounded SDK fakes."""
import os
from pathlib import Path
import subprocess
import unittest

import test_realtime_default as defaults

FIRMWARE=Path(__file__).resolve().parents[1]


class C3DecodeRateTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        defaults.RealtimeDefaultTests.setUpClass.__func__(cls)
        service=(cls.source/'main/audio/audio_service.cc').read_text()
        program=(FIRMWARE/'tests/c3_decode_rate_host.cc').read_text().replace(
            '// PRODUCTION_METHOD',defaults.method(service,'void AudioService::SetDecodeSampleRate('))
        source=cls.work/'decode-rate.cc';source.write_text(program)
        cls.binaries={}
        for enabled in (0,1):
            for mode,flags in [('optimized',['-O2']),('sanitized',['-O1','-g',
                    '-fsanitize=address,undefined','-fno-sanitize-recover=all'])]:
                binary=cls.work/f'decode-{enabled}-{mode}'
                result=subprocess.run(['c++','-std=c++17','-pthread','-Wall','-Wextra','-Werror',
                    '-pedantic',*flags,
                    f'-DCONFIG_XMINI_C3_VOICE_BARGE={enabled}',str(source),'-o',str(binary)],
                    capture_output=True,text=True)
                if result.returncode:
                    raise AssertionError(result.stderr)
                cls.binaries[enabled,mode]=binary

    def run_case(self,enabled,case):
        for mode in ('optimized','sanitized'):
            with self.subTest(mode=mode):
                env=dict(os.environ,ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                         UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
                if mode=='sanitized' and os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                    env['LD_PRELOAD']=os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
                result=subprocess.run([str(self.binaries[enabled,mode]),case],env=env,
                                      capture_output=True,text=True,timeout=20)
                self.assertEqual(result.returncode,0,result.stdout+result.stderr)

    def test_c3_local_cue_reuses_24k_decoder_without_output_src(self):
        self.run_case(1,'local-cue')

    def test_c3_preserves_packet_duration_at_physical_output_rate(self):
        self.run_case(1,'frame-duration')

    def test_c3_24k_server_downlink_keeps_existing_decoder(self):
        self.run_case(1,'server-downlink')

    def test_legacy_local_cue_retains_original_rate_and_src(self):
        self.run_case(0,'local-cue')

    def test_legacy_rate_changes_keep_original_timing(self):
        self.run_case(0,'frame-duration')

    def test_legacy_24k_server_downlink_keeps_existing_decoder(self):
        self.run_case(0,'server-downlink')


if __name__=='__main__':unittest.main()
