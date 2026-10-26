"""Prepared production integration contracts; no SDK or device runs here."""
import json
import os
import subprocess
import unittest

import test_realtime_default as defaults


class WaveshareIntegrationTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        defaults.RealtimeDefaultTests.setUpClass.__func__(cls)

    def test_hardware_capture_is_explicit_and_keeps_the_physical_afe(self):
        codec = (self.source/'main/audio/audio_codec.h').read_text()
        self.assertIn('virtual bool ManagedCapture() const', codec)
        engine = (self.source/'main/audio/engines/afe_audio_engine.cc').read_text()
        controls = defaults.method(engine, 'void AfeAudioEngine::ApplyAfeControls(')
        self.assertIn('codec_->SoftwareReference() || (bits & kWakeWordEnabled)', controls)
        self.assertIn('codec_->ManagedCapture()', defaults.method(engine, 'void AfeAudioEngine::FeedCaptured('))
        self.assertIn('codec_->ManagedCapture()', defaults.method(engine, 'void AfeAudioEngine::ResetWakeWordCapture('))
        self.assertIn('codec_->ManagedCapture()', defaults.method(engine, 'void AfeAudioEngine::EncodeWakeWordData('))
        custom = (self.source/'main/audio/wake_words/custom_wake_word.cc').read_text()
        self.assertIn('!codec_->ManagedCapture()', defaults.method(custom, 'void CustomWakeWord::FeedSamples('))
        service = (self.source/'main/audio/audio_service.cc').read_text()
        for signature in ['void AudioService::SetExperimentUplink(', 'void AudioService::OpusCodecTask(',
                          'void AudioService::PushTaskToEncodeQueue(',
                          'std::unique_ptr<AudioStreamPacket> AudioService::PopPacketFromSendQueue(']:
            self.assertIn('codec_->ManagedCapture()', defaults.method(service, signature))

    def test_ci_maps_four_isolated_targets_to_the_correct_board(self):
        workflow = (defaults.FIRMWARE.parent/'.github/workflows/firmware-experiment.yml').read_text()
        for variant, board in [('m5stack-stopwatch', 'm5stack/stopwatch'),
                ('m5stack-stopwatch-software-aec', 'm5stack/stopwatch'),
                ('m5stack-stopwatch-xiaoman', 'm5stack/stopwatch'),
                ('esp32-s3-touch-amoled-2.16-xiaoman', 'waveshare/esp32-s3-touch-amoled-2.16')]:
            self.assertIn(f'- variant: {variant}\n            board: {board}', workflow)
        self.assertIn('scripts/build.py "$BOARD" --name "$VARIANT"', workflow)
        self.assertNotIn('esptool', workflow)

    def test_dedicated_variant_and_shared_view_are_connected(self):
        board = self.source/'main/boards/waveshare/esp32-s3-touch-amoled-2.16'
        configs = json.loads((board/'config.json').read_text())['builds']
        xiaoman = next((c for c in configs if c['name']=='esp32-s3-touch-amoled-2.16-xiaoman'), None)
        self.assertIsNotNone(xiaoman, 'dedicated Waveshare target is absent')
        options = xiaoman['sdkconfig_append']
        for option in ['CONFIG_WAVESHARE_XIAOMAN_REALTIME=y', 'CONFIG_STOPWATCH_SOFTWARE_AEC=n',
                       'CONFIG_USE_DEVICE_AEC=y', 'CONFIG_XIAOMAN_ORB_UI=y',
                       'CONFIG_CUSTOM_WAKE_WORD="xiao man"', 'CONFIG_CUSTOM_WAKE_WORD_THRESHOLD=20',
                       'CONFIG_PARTITION_TABLE_CUSTOM_FILENAME="partitions/waveshare-xiaoman-16m.csv"']:
            self.assertIn(option, options)
        source = (board/'esp32-s3-touch-amoled-2.16.cc').read_text()
        self.assertIn('static XiaomanBoxCodec audio_codec(', source)
        self.assertIn('using OrbDisplayOwner=CustomLcdDisplay;', source)
        self.assertIn('#include "xiaoman_orb_display.inc"', source)
        self.assertIn('SetOrbScreenOn(brightness>0)', source)
        cmake = (self.source/'main/CMakeLists.txt').read_text()
        self.assertIn('if(CONFIG_XIAOMAN_ORB_UI AND NOT CONFIG_BOARD_TYPE_M5STACK_STOPWATCH)', cmake)

    def test_checked_read_is_opt_in_and_ordinary_writes_cannot_touch_refill_dma(self):
        box = (self.source/'main/audio/codecs/box_audio_codec.cc').read_text()
        self.assertIn('auto_clear_after_cb = !callback_refill_', box)
        self.assertIn('callback_refill_ && result != ESP_CODEC_DEV_OK', defaults.method(box, 'int BoxAudioCodec::Read('))
        self.assertIn('if (callback_refill_) return 0;', defaults.method(box, 'int BoxAudioCodec::Write('))

    def test_real_box_read_failure_and_refill_write_ownership(self):
        box = (self.source/'main/audio/codecs/box_audio_codec.cc').read_text()
        methods = '\n'.join(defaults.method(box, signature) for signature in [
            'int BoxAudioCodec::Read(', 'int BoxAudioCodec::Write('])
        program = '''
#include <cassert>
#include <cstdint>
#include <cstddef>
constexpr int ESP_CODEC_DEV_OK=0;
static int read_status=0,writes=0;
static int esp_codec_dev_read(void*,void*,size_t){return read_status;}
static int esp_codec_dev_write(void*,void*,size_t){++writes;return 0;}
#define ESP_ERROR_CHECK_WITHOUT_ABORT(x) ((void)(x))
class BoxAudioCodec {
public:
 bool input_enabled_=true,output_enabled_=true,callback_refill_=false;
 void *input_dev_=nullptr,*output_dev_=nullptr;
 int Read(int16_t*,int);int Write(const int16_t*,int);
};
'''+methods+'''
int main(){BoxAudioCodec codec;int16_t samples[240]{};
 read_status=-1;assert(codec.Read(samples,240)==240);
 codec.Write(samples,240);assert(writes==1);
 codec.callback_refill_=true;assert(codec.Read(samples,240)==0);
 codec.Write(samples,240);assert(writes==1);
 read_status=0;assert(codec.Read(samples,240)==240);
}
'''
        source = self.work/'checked-box.cc'
        binary = source.with_suffix('')
        source.write_text(program)
        result = subprocess.run(['c++', '-std=c++17', '-Wall', '-Wextra', '-Werror', '-pedantic',
            '-fsanitize=address,undefined', '-fno-sanitize-recover=all', str(source), '-o', str(binary)],
            capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        environment = dict(os.environ, ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                           UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
        if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
            environment['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
        result = subprocess.run([str(binary)], env=environment, capture_output=True, text=True, timeout=20)
        self.assertEqual(result.returncode, 0, result.stdout+result.stderr)


if __name__ == '__main__':
    unittest.main()
