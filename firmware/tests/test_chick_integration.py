"""Production target selects the face while retaining shipped audio behavior."""
import json
import unittest
import test_realtime_default as defaults


class ChickIntegrationTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        defaults.RealtimeDefaultTests.setUpClass.__func__(cls)

    def test_product_targets_select_face_without_reactivating_wake_experiment(self):
        for directory, variant in [
            ('waveshare/esp32-s3-touch-amoled-2.16','esp32-s3-touch-amoled-2.16-xiaoman'),
            ('m5stack/stopwatch','m5stack-stopwatch-xiaoman'),
        ]:
            builds=json.loads((self.source/'main/boards'/directory/'config.json').read_text())['builds']
            options=next(b['sdkconfig_append'] for b in builds if b['name']==variant)
            self.assertIn('CONFIG_CHICK_FACE_UI=y',options)
            self.assertNotIn('CONFIG_XIAOMAN_ORB_UI=y',options)
            self.assertNotIn('CONFIG_WAVESHARE_WAKE_SPEECH_WINDOW=y',options)
        config=(self.source/'main/Kconfig.projbuild').read_text()
        flag=config.split('config WAVESHARE_WAKE_SPEECH_WINDOW',1)[1].split('\nconfig ',1)[0]
        self.assertIn('default n',flag)

    def test_face_has_native_display_hook_and_retains_truthful_audio_observer(self):
        cmake=(self.source/'main/CMakeLists.txt').read_text()
        self.assertIn('display/chick_face_view.cc',cmake)
        self.assertNotIn('xiaoman_orb_view.cc',cmake)
        for relative in ['main/boards/m5stack/stopwatch/m5stack_stopwatch.cc',
                         'main/boards/waveshare/esp32-s3-touch-amoled-2.16/esp32-s3-touch-amoled-2.16.cc']:
            source=(self.source/relative).read_text()
            self.assertIn('chick_face_display.inc',source)
            self.assertIn('SetupFaceUI()',source)
            self.assertNotIn('xiaoman_orb_display.inc',source)
        observer=(self.source/'main/audio/orb_audio_service.inc').read_text()
        self.assertIn('std::try_to_lock',observer)
        self.assertIn('codec_->GetOrbOutputActivity()',observer)
        ws=(self.source/'main/protocols/websocket_protocol.cc').read_text()
        self.assertIn('"face_state_v1"',ws)
        self.assertIn('"face_expression_v1"',ws)
        receive=ws[ws.index('websocket_->OnData('):ws.index('websocket_->OnDisconnected(')]
        self.assertLess(receive.index('esp_timer_get_time()'),receive.index('cJSON_ParseWithLength'))
        self.assertIn('HandlePlaybackExpression(root, connection, arrival_us)',receive)
        protocol=(self.source/'main/protocols/protocol.h').read_text()
        self.assertIn('PlaybackExpressionCurrent',protocol)
        application=(self.source/'main/application.h').read_text()
        self.assertIn('SchedulePlaybackExpression',application.split('\nprivate:',1)[0])
        self.assertIn('ScheduleLegacyEmotion',application.split('\nprivate:',1)[0])
        self.assertIn('if (!handled && on_incoming_json_',receive)
        self.assertNotIn('cJSON_DeleteItemFromObject(root,"emotion")',receive)
        display=(self.source/'main/display/display.h').read_text()
        self.assertIn('SetPlaybackEmotion',display)
        self.assertIn('ClearPlaybackEmotions',display)
        self.assertIn('SetLegacyEmotion',display)
        self.assertNotIn('playback_progress.h',display)
        refresh=(self.source/'main/display/chick_face_display.inc').read_text()
        before=refresh[:refresh.index('facts.audio=app.GetAudioService().GetOrbAudioActivity();')]
        self.assertNotIn('if(facts.state==family_robot::FaceState::Listening',before)



if __name__=='__main__':unittest.main()
