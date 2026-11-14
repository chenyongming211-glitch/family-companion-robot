"""Actual Box codec methods serialize controls with exclusive hardware reopen."""
import os,subprocess,unittest
import test_realtime_default as defaults
class MusicBoxTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):defaults.RealtimeDefaultTests.setUpClass.__func__(cls)
    def test_volume_serializes_with_profile_and_voice_rates_stay_fixed(self):
        body=(self.source/'main/audio/codecs/box_audio_codec.cc').read_text()
        methods='\n'.join(defaults.method(body,s) for s in ['bool BoxAudioCodec::LockDuplex(', 'bool BoxAudioCodec::ReopenDuplexForMusic(', 'void BoxAudioCodec::SetOutputVolume('])
        source=self.work/'music-box.cc';binary=self.work/'music-box'
        source.write_text((defaults.FIRMWARE/'tests/music_box_host.cc').read_text().replace('// PRODUCTION_BOX_METHODS',methods))
        result=subprocess.run(['c++','-std=c++20','-pthread','-Wall','-Wextra','-Werror',
            '-fsanitize=address,undefined','-fno-sanitize-recover=all',str(source),'-o',str(binary)],capture_output=True,text=True)
        self.assertEqual(result.returncode,0,result.stderr)
        env=dict(os.environ,ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
        if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):env['LD_PRELOAD']=os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
        result=subprocess.run([str(binary)],env=env,capture_output=True,text=True,timeout=15)
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)
