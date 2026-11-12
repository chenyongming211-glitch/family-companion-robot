"""Retained actual-DMA/queue observations and new display lifecycle regressions."""
import os
import re
from pathlib import Path
import subprocess
import tempfile
import unittest
import test_realtime_default as defaults

FIRMWARE=Path(__file__).resolve().parents[1]

class AudioUiActivityTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        defaults.RealtimeDefaultTests.setUpClass.__func__(cls)

    def test_production_observers_and_face_display(self):
        with tempfile.TemporaryDirectory() as directory:
            work=Path(directory)
            for name in ['sdkconfig.h','lvgl.h']:(work/name).write_text('')
            for name in ['playback_adapter_test.cc','playback_websocket_test.cc',
                         'audio_activity_host.cc','chick_face_display_host.cc','face_layout_host.cc',
                         'chick_face_view_host.cc']:
                binary=work/name.removesuffix('.cc')
                source=FIRMWARE/'tests'/name
                if name=='playback_websocket_test.cc':
                    native=(self.source/'main/protocols/websocket_protocol.cc').read_text()
                    tts=native.index('if (strcmp(type->valuestring, "tts")')
                    start=native.index('#if CONFIG_CHICK_FACE_UI',tts)
                    end=native.index('\n                }\n            } else',start)
                    dispatch=native[start:end]
                    app=(self.source/'main/application.cc').read_text()
                    llm=app.index('} else if (strcmp(type->valuestring, "llm")')
                    start=app.index('            auto emotion = cJSON_GetObjectItem(root, "emotion");',llm)
                    end=app.index('\n        } else if (strcmp(type->valuestring, "mcp")',start)
                    legacy=app[start:end]
                    source=work/name
                    source.write_text((FIRMWARE/'tests'/name).read_text().replace(
                        '// PRODUCTION_JSON_DISPATCH',dispatch).replace('// PRODUCTION_LEGACY_EMOTION',legacy))
                if name=='chick_face_display_host.cc':
                    native=(self.source/'main/display/lvgl_display/lvgl_display.cc').read_text()
                    def stock_method(signature):
                        body=defaults.method(native,signature)
                        return body.replace('void LvglDisplay::','virtual void ').replace('int duration_ms)', 'int duration_ms=3000)')
                    timer_start=native.index('    esp_timer_create_args_t notification_timer_args')
                    timer_end=native.index('\n',native.index('ESP_ERROR_CHECK(esp_timer_create(&notification_timer_args',timer_start))
                    source=work/name
                    source.write_text((FIRMWARE/'tests'/name).read_text().replace(
                        '// PRODUCTION_NOTIFICATION_TIMER',native[timer_start:timer_end]).replace(
                        '// PRODUCTION_SET_STATUS',stock_method('void LvglDisplay::SetStatus(')).replace(
                        '// PRODUCTION_SHOW_NOTIFICATION',
                        stock_method('void LvglDisplay::ShowNotification(const char*')+'\n'+
                        stock_method('void LvglDisplay::ShowNotification(const std::string&')))
                if name=='chick_face_view_host.cc':
                    view=(FIRMWARE/'overlays/main/display/chick_face_view.cc').read_text()
                    view=re.sub(r'^#include .*\n', '', view, flags=re.MULTILINE)
                    source=work/name
                    stop=defaults.method((FIRMWARE/'overlays/main/audio/playback_application.inc').read_text(),
                                         'void Application::SchedulePlaybackStop(')
                    source.write_text((FIRMWARE/'tests'/name).read_text().replace(
                        '// PRODUCTION_VIEW',view).replace('// PRODUCTION_PLAYBACK_STOP',stop))
                command=['c++','-std=c++20','-pthread','-Wall','-Wextra','-Werror','-pedantic',
                    '-Wno-unused-function','-Werror=deprecated-enum-enum-conversion',
                    '-fsanitize=address,undefined','-fno-sanitize-recover=all',
                    '-DCONFIG_CHICK_FACE_UI=1','-I',str(work),
                    '-I',str(FIRMWARE/'overlays/main/audio'),
                    '-I',str(FIRMWARE/'overlays/main/boards/m5stack/stopwatch'),
                    '-I',str(FIRMWARE/'overlays/main/display'),
                    str(source),'-o',str(binary)]
                built=subprocess.run(command,capture_output=True,text=True)
                self.assertEqual(built.returncode,0,built.stderr)
                environment=dict(os.environ,ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                                 UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
                if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                    environment['LD_PRELOAD']=os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
                cases=('legacy_epoch','duplicate_emotion','legacy_lifecycle','sealed_pending','all') if name=='playback_websocket_test.cc' else ('all',)
                for case in cases:
                    with self.subTest(host=name,case=case):
                        args=[str(binary),case] if name=='playback_websocket_test.cc' else [str(binary)]
                        ran=subprocess.run(args,capture_output=True,text=True,env=environment,timeout=30)
                        self.assertEqual(ran.returncode,0,ran.stdout+ran.stderr)

if __name__=='__main__':unittest.main()
