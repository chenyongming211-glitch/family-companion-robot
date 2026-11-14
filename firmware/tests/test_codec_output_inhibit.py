"""Real pinned SDK enable/open/mute methods and actual Box output isolation."""
import hashlib
import os
from pathlib import Path
import subprocess
import unittest
import test_realtime_default as defaults

class CodecOutputInhibitTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        defaults.RealtimeDefaultTests.setUpClass.__func__(cls)

    def test_real_sdk_reopen_cannot_escape_pa_inhibit(self):
        prepared = self.source/'managed_components/espressif__esp_codec_dev'
        fallback = prepared if prepared.is_dir() else defaults.FIRMWARE/'.cache/codec-dev'
        component = Path(os.environ.get('FIRMWARE_CODEC_COMPONENT', str(fallback)))
        texts = []
        for relative, digest in [
            ('device/es8311/es8311.c','0da6374b8b3bb1dea81b2d2077c2f95683bfb39077086e162f5a1bc98749ca67'),
            ('esp_codec_dev.c','fc9fafbb591cb3509ac0b4b51445c18b75b90bf0063e3917a951983d28c6153b')]:
            path = component/relative
            self.assertTrue(path.is_file(), 'configure the existing pinned FIRMWARE_CODEC_COMPONENT fixture')
            raw = path.read_bytes()
            self.assertEqual(hashlib.sha256(raw).hexdigest(), digest)
            texts.append(raw.decode())
        body = (self.source/'main/audio/codecs/box_audio_codec.cc').read_text()
        constructor = defaults.method(body, 'BoxAudioCodec::BoxAudioCodec(')
        self.assertIn('if(callback_refill_)InitializeOutputInhibit(pa_pin);', constructor)
        self.assertLess(constructor.index('InitializeOutputInhibit'), constructor.index('CreateDuplexChannels'))
        signatures = ['bool BoxAudioCodec::MuteMusicOutput(', 'bool BoxAudioCodec::ReopenDuplexForMusic(']
        signatures += ['bool BoxAudioCodec::InitializeOutputInhibit(', 'bool BoxAudioCodec::SetOutputInhibit(']
        program = (defaults.FIRMWARE/'tests/codec_output_inhibit_host.cc').read_text()
        program = program.replace('// ACTUAL_ES8311_METHODS', '\n'.join(defaults.method(texts[0], s) for s in [
            'static int es8311_set_mute(', 'static void es8311_pa_power(', 'static int es8311_enable(']))
        program = program.replace('// ACTUAL_DEVICE_METHODS', '\n'.join(defaults.method(texts[1], s) for s in [
            'int esp_codec_dev_set_out_mute(', 'static void _update_codec_setting(', 'int esp_codec_dev_open(']))
        program = program.replace('// PRODUCTION_BOX_METHODS', '\n'.join(defaults.method(body, s) for s in signatures))
        source = self.work/'output-inhibit.cc';binary = self.work/'output-inhibit'
        source.write_text(program)
        compiled = subprocess.run(['c++','-std=c++20','-pthread','-Wall','-Wextra',
            '-Wno-unused-function','-Wno-unused-variable','-fsanitize=address,undefined',
            '-fno-sanitize-recover=all',str(source),'-o',str(binary)],capture_output=True,text=True)
        self.assertEqual(compiled.returncode,0,compiled.stderr)
        env = dict(os.environ,ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
        if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):env['LD_PRELOAD']=os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
        run = subprocess.run([str(binary)],env=env,capture_output=True,text=True,timeout=20)
        self.assertEqual(run.returncode,0,run.stdout+run.stderr)
        print(run.stdout.strip())
