"""Execute the shared S3 command/parser state on the cloud, not a model copy."""
from pathlib import Path
import hashlib
import os
import subprocess
import tempfile
import unittest
import urllib.request

class DeviceControl(unittest.TestCase):
    def test_main_thread_snapshot_queue_is_bounded_and_recovers(self):
        from test_onboarding_lifecycle import function
        root=Path(__file__).resolve().parents[1]
        source=(root/'overlays/onboarding/family_control.cc').read_text()
        harness=r'''
#include "family_control_state.h"
#include <cassert>
#include <functional>
#include <mutex>
#include <vector>
using namespace FamilyControl;
std::mutex mutex;State state;bool online=true,snapshot_queued=false;
uint64_t wanted=0,completed=0;int volume=-1,brightness=-1,signal=1;
bool notified=false;constexpr int pdTRUE=1;
#define pdMS_TO_TICKS(value) (value)
int xSemaphoreTake(int,int){bool old=notified;notified=false;return old?1:0;}
void xSemaphoreGive(int){notified=true;}
uint64_t Now(){return 100;}
struct Codec{void SetOutputVolume(int){}int output_volume(){return 60;}};
struct Light{void SetBrightness(int,bool){}int brightness(){return 40;}};
struct Board{static Board& GetInstance(){static Board b;return b;}Codec* GetAudioCodec(){static Codec c;return &c;}Light* GetBacklight(){static Light l;return &l;}};
struct Application{std::vector<std::function<void()>> queue;bool immediate=false;static Application& GetInstance(){static Application a;return a;}void Schedule(std::function<void()> fn){if(immediate)fn();else queue.push_back(fn);}};
'''+function(source,'bool Snapshot() {')+r'''
int main(){
 auto& app=Application::GetInstance();
 for(int i=0;i<100;++i)assert(!Snapshot());
 assert(app.queue.size()==1); // A blocked main task must not leak callbacks.
 app.queue[0]();app.queue.clear();app.immediate=true;
 assert(Snapshot());assert(volume==60&&brightness==40);
}
'''
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)/'snapshot.cc';path.write_text(harness)
            binary=Path(directory)/'snapshot'
            result=subprocess.run(['c++','-std=c++17','-Wall','-Wextra','-Werror','-I'+str(root/'overlays/onboarding'),str(path),'-o',str(binary)],capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stderr)
            result=subprocess.run([str(binary)],capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stderr)

    def test_shared_parser_command_deduplication_readback_and_deadlines(self):
        root=Path(__file__).resolve().parents[1]
        # The existing ESP-IDF component is cJSON 1.7.19. Host checks use the
        # exact same two upstream files, never a floating system-library ABI.
        cjson=Path(os.environ.get('FAMILY_CONTROL_CJSON_DIR',str(root/'.cache/control-cjson')))
        cjson.mkdir(parents=True,exist_ok=True)
        for name,digest in {'cJSON.c':'607e756460fa0de37d20a7a9181f2de29c97bfb7ce5a0e6c2f548243836cd852','cJSON.h':'25b0145150d500498e4d209cec69c18c42cf818bffcc54690be3b895a2a16dee'}.items():
            path=cjson/name
            if not path.exists():
                with urllib.request.urlopen('https://raw.githubusercontent.com/DaveGamble/cJSON/v1.7.19/'+name,timeout=30) as response:
                    data=response.read(256*1024+1)
                self.assertEqual(hashlib.sha256(data).hexdigest(),digest)
                path.write_bytes(data)
            self.assertEqual(hashlib.sha256(path.read_bytes()).hexdigest(),digest)
        with tempfile.TemporaryDirectory() as directory:
            binary=Path(directory)/'device-control'
            obj=Path(directory)/'cjson.o'
            subprocess.run(['cc','-std=c11','-I'+str(cjson),'-c',str(cjson/'cJSON.c'),'-o',str(obj)],check=True)
            compile=subprocess.run(['c++','-std=c++17','-Wall','-Wextra','-Werror',
                '-I'+str(root/'overlays/onboarding'),'-I'+str(cjson),
                str(root/'tests/device_control_host.cc'),str(obj),'-o',str(binary)],capture_output=True,text=True)
            self.assertEqual(compile.returncode,0,compile.stderr)
            result=subprocess.run([str(binary)],capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stderr)
            self.assertEqual(result.stdout.strip(),'ok')
