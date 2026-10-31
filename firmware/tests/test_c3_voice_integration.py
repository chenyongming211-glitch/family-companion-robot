"""Run actual C3 voice integration paths; all execution belongs on the cloud host."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
import test_realtime_default as defaults

FIRMWARE=Path(__file__).resolve().parents[1]

class C3VoiceIntegrationTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        defaults.RealtimeDefaultTests.setUpClass.__func__(cls)

    def run_host(self,program,enabled,include=None):
        for flags in (['-O2'],['-fsanitize=address,undefined','-fno-sanitize-recover=all']):
            with self.subTest(enabled=enabled,flags=flags),tempfile.TemporaryDirectory() as tmp:
                source=Path(tmp)/'test.cc';binary=Path(tmp)/'test';source.write_text(program)
                result=subprocess.run(['c++','-std=c++17','-pthread','-Wall','-Wextra','-Werror',
                    '-pedantic','-Wno-unused-parameter',*flags,
                    '-DCONFIG_XMINI_C3_BUTTON_ENTRY=1',f'-DCONFIG_XMINI_C3_VOICE_BARGE={enabled}',
                    '-I',str(FIRMWARE/'overlays/main/audio'),*(['-I',str(include)] if include else []),
                    str(source),'-o',str(binary)],capture_output=True,text=True)
                self.assertEqual(result.returncode,0,result.stderr)
                env=dict(os.environ,ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',UBSAN_OPTIONS='halt_on_error=1')
                if '-fsanitize=address,undefined' in flags and os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                    env['LD_PRELOAD']=os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
                result=subprocess.run([str(binary)],env=env,capture_output=True,text=True,timeout=20)
                self.assertEqual(result.returncode,0,result.stdout+result.stderr)

    def test_c3_listening_remains_continuous_during_playback(self):
        app=(self.source/'main/application.cc').read_text()
        methods='\n'.join(defaults.method(app,s) for s in [
            'ListeningMode Application::GetDefaultListeningMode()', 'void Application::StartListeningAudio()'])
        program=(FIRMWARE/'tests/c3_voice_application_host.cc').read_text().replace('// PRODUCTION_METHODS',methods)
        for enabled in (0,1):self.run_host(program,enabled)

    def test_new_config_is_opt_in_and_startup_initializes_before_ota_confirmation(self):
        kconfig=(self.source/'main/Kconfig.projbuild').read_text()
        self.assertIn('config XMINI_C3_VOICE_BARGE',kconfig)
        block=kconfig.split('config XMINI_C3_VOICE_BARGE',1)[1].split('\nconfig ',1)[0]
        self.assertIn('default n',block);self.assertIn('depends on XMINI_C3_BUTTON_ENTRY',block)
        service=(self.source/'main/audio/audio_service.cc').read_text()
        initialize=defaults.method(service,'void AudioService::Initialize(')
        self.assertIn('InitializeAudioEngine()',initialize)
        activation=defaults.method((self.source/'main/application.cc').read_text(),'void Application::CheckNewVersion()')
        self.assertLess(activation.index('ReferenceFaulted()'),activation.index('MarkCurrentVersionValid()'))

    def test_owner_uses_sdk_with_epoch_safe_no_raw_fallback(self):
        header=FIRMWARE/'overlays/main/audio/c3_lite_aec.h'
        self.assertTrue(header.is_file(),'standalone C3 Lite AEC owner is missing')
        self.run_host((FIRMWARE/'tests/c3_lite_aec_host.cc').read_text(),1,FIRMWARE/'tests/c3_sdk_stubs')

    def test_ordered_tts_start_keeps_authorized_c3_uplink(self):
        for enabled in (0,1):
            self.run_host((FIRMWARE/'tests/playback_websocket_test.cc').read_text(),enabled)

    def test_actual_lite_engine_publishes_only_current_aec_output_during_playback(self):
        self.run_host((FIRMWARE/'tests/c3_lite_uplink_host.cc').read_text(),1,FIRMWARE/'tests/c3_sdk_stubs')

    def test_capture_drops_immediately_on_close_and_reopens_only_when_requested(self):
        service=(self.source/'main/audio/audio_service.cc').read_text()
        methods='\n'.join(defaults.method(service,s) for s in ['bool AudioService::CaptureRequested()',
            'void AudioService::UpdateCaptureActive()', 'bool AudioService::OpenC3Uplink(',
            'void AudioService::SetExperimentUplink('])
        program=(FIRMWARE/'tests/c3_voice_capture_host.cc').read_text().replace('// PRODUCTION_METHODS',methods)
        for enabled in (0,1):self.run_host(program,enabled)

    def test_joint_resampling_validates_capacity_result_and_stale_generations(self):
        service=(self.source/'main/audio/audio_service.cc').read_text()
        program=(FIRMWARE/'tests/c3_voice_resample_host.cc').read_text().replace('// PRODUCTION_METHOD',
            defaults.method(service,'bool AudioService::ReadAudioData('))
        self.run_host(program,1)

if __name__=='__main__':unittest.main()
