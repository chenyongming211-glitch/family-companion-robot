"""Exercise the real idle timer with the prepared Waveshare board policy."""
import os
import re
import subprocess
import unittest

import test_realtime_default as defaults


class WavesharePowerTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        defaults.RealtimeDefaultTests.setUpClass.__func__(cls)

    def test_idle_keeps_screen_sleep_without_powering_off(self):
        board = self.source/'main/boards/waveshare/esp32-s3-touch-amoled-2.16/esp32-s3-touch-amoled-2.16.cc'
        policy = defaults.method(board.read_text(), 'void InitializePowerSaveTimer()')
        arguments = re.findall(r'new PowerSaveTimer\(([-0-9, ]+)\)', policy)
        self.assertEqual(len(arguments), 1, 'board idle policy must be unambiguous')
        common = self.source/'main/boards/common'
        work = self.work/'power'
        work.mkdir()
        (work/'esp_timer.h').write_text('''#pragma once
#include <cassert>
#include <cstdint>
struct esp_timer_create_args_t {
 void (*callback)(void*);void* arg;int dispatch_method;const char* name;bool skip_unhandled_events;
};
struct FakeTimer {esp_timer_create_args_t args;bool running=false;};
using esp_timer_handle_t=FakeTimer*;
inline FakeTimer* timer=nullptr;
constexpr int ESP_TIMER_TASK=1;
#define ESP_ERROR_CHECK(result) assert((result)==0)
inline int esp_timer_create(const esp_timer_create_args_t* a,esp_timer_handle_t* t){
 *t=new FakeTimer{*a};timer=*t;return 0;
}
inline int esp_timer_start_periodic(esp_timer_handle_t t,int64_t period){
 assert(period==1000000);t->running=true;return 0;
}
inline int esp_timer_stop(esp_timer_handle_t t){t->running=false;return 0;}
inline int esp_timer_delete(esp_timer_handle_t t){delete t;timer=nullptr;return 0;}
inline void tick(){assert(timer&&timer->running);timer->args.callback(timer->args.arg);}
''')
        (work/'esp_pm.h').write_text('''#pragma once
struct esp_pm_config_t {int max_freq_mhz;int min_freq_mhz;bool light_sleep_enable;};
inline int esp_pm_configure(const esp_pm_config_t*){return 0;}
inline void vTaskDelay(int){}
#define pdMS_TO_TICKS(value) (value)
''')
        (work/'esp_log.h').write_text('#pragma once\n#define ESP_LOGI(...) ((void)0)\n')
        (work/'settings.h').write_text('''#pragma once
struct Settings {Settings(const char*,bool){} bool GetBool(const char*,bool fallback){return fallback;}};
''')
        (work/'application.h').write_text('''#pragma once
struct Audio {bool IsWakeWordRunning(){return true;}void EnableWakeWordDetection(bool){}};
struct Codec {void EnableInput(bool){}};
struct Board {static Board& GetInstance(){static Board board;return board;}
 Codec* GetAudioCodec(){return &codec;}Codec codec;};
struct Application {static Application& GetInstance(){static Application app;return app;}
 bool CanEnterSleepMode(){return idle;}Audio& GetAudioService(){return audio;}bool idle=true;Audio audio;};
''')
        source = work/'power.cc'
        source.write_text('''#include <cassert>
#include <cstdio>
#include "power_save_timer.cc"
int main(){
 int slept=0,woke=0,shutdown=0;
 PowerSaveTimer policy('''+arguments[0]+''');
 policy.OnEnterSleepMode([&]{++slept;});
 policy.OnExitSleepMode([&]{++woke;});
 policy.OnShutdownRequest([&]{++shutdown;});
 policy.SetEnabled(true);
 Application::GetInstance().idle=false;
 for(int second=0;second<400;++second)tick();
 assert(slept==0&&shutdown==0);
 Application::GetInstance().idle=true;
 for(int second=0;second<59;++second)tick();
 assert(slept==0&&shutdown==0);
 tick();assert(slept==1);
 for(int second=60;second<900;++second)tick();
 assert(shutdown==0&&"Waveshare powered off during 15 minutes of idle");
 assert(slept==1);
 policy.WakeUp();assert(woke==1);
 for(int second=0;second<60;++second)tick();
 assert(slept==2&&shutdown==0);
 policy.SetEnabled(false);assert(!timer->running&&woke==2);
 puts("Waveshare: 60-second screen sleep retained, 15-minute idle never requests shutdown");
}
''')
        binary = work/'power'
        compiled = subprocess.run(['c++', '-std=c++20', '-Wall', '-Wextra', '-Werror',
            '-fsanitize=address,undefined', '-fno-sanitize-recover=all',
            '-I', str(work), '-I', str(common), str(source), '-o', str(binary)],
            capture_output=True, text=True)
        self.assertEqual(compiled.returncode, 0, compiled.stderr)
        environment = dict(os.environ, ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                           UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
        if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
            environment['LD_PRELOAD'] = os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
        result = subprocess.run([str(binary)], env=environment, capture_output=True,
                                text=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stdout+result.stderr)
        print(result.stdout.strip())


if __name__ == '__main__':
    unittest.main()
