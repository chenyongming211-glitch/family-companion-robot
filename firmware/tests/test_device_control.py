"""Execute the shared S3 command/parser state on the cloud, not a model copy."""
from pathlib import Path
import hashlib
import os
import subprocess
import tempfile
import unittest
import urllib.request

class DeviceControl(unittest.TestCase):
    def test_actual_worker_preserves_json_across_three_sdk_post_cycles(self):
        from test_onboarding_lifecycle import function
        root=Path(__file__).resolve().parents[1]
        # Pin the actual release SDK implementation, including its destructive
        # null-body behavior; never replace set_post_field with a no-op fake.
        sdk=Path(os.environ['IDF_PATH'])/'components/esp_http_client/esp_http_client.c' if os.environ.get('IDF_PATH') else root/'.cache/control-esp_http_client.c'
        digest='b28179259684e4cadbcaff00127ac50ea5c4477536cca1b4df86590694acdf24'
        if not sdk.exists():
            import json
            lock=json.loads((root/'upstream.lock.json').read_text())
            url='https://raw.githubusercontent.com/espressif/esp-idf/'+lock['idf_commit']+'/components/esp_http_client/esp_http_client.c'
            with urllib.request.urlopen(url,timeout=30) as response:
                data=response.read(512*1024+1)
            self.assertEqual(hashlib.sha256(data).hexdigest(),digest)
            sdk.parent.mkdir(parents=True,exist_ok=True);sdk.write_bytes(data)
        self.assertEqual(hashlib.sha256(sdk.read_bytes()).hexdigest(),digest)
        getter_source=Path(os.environ['IDF_PATH'])/'components/esp_http_client/lib/http_header.c' if os.environ.get('IDF_PATH') else root/'.cache/control-http_header.c'
        getter_digest='23056c41505aa38885cf210e22117e1fa57d964080ff537a36a5c9d99d76701e'
        if not getter_source.exists():
            import json
            lock=json.loads((root/'upstream.lock.json').read_text())
            url='https://raw.githubusercontent.com/espressif/esp-idf/'+lock['idf_commit']+'/components/esp_http_client/lib/http_header.c'
            with urllib.request.urlopen(url,timeout=30) as response:
                data=response.read(512*1024+1)
            self.assertEqual(hashlib.sha256(data).hexdigest(),getter_digest)
            getter_source.parent.mkdir(parents=True,exist_ok=True);getter_source.write_bytes(data)
        self.assertEqual(hashlib.sha256(getter_source.read_bytes()).hexdigest(),getter_digest)
        getter=function(getter_source.read_text(),'esp_err_t http_header_get(')
        setter=function(sdk.read_text(),'esp_err_t esp_http_client_set_post_field(')
        worker=function((root/'overlays/onboarding/family_control.cc').read_text(),'void Worker(void*) {')
        harness=r'''
#include <atomic>
#include <cassert>
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <map>
#include <mutex>
#include <string>
using esp_err_t=int;constexpr int ESP_OK=0,ESP_ERR_NOT_FOUND=-1,HTTP_METHOD_POST=1;
#define ESP_LOGD(...) ((void)0)
#define ESP_LOGE(...) ((void)0)
#define pdMS_TO_TICKS(value) (value)
struct Response{std::string body;bool overflow=false;};
struct Client{char* post_data=nullptr;int post_len=0;std::map<std::string,std::string> headers;};
using esp_http_client_handle_t=Client*;Client client;int requests=0,clears=0;
int esp_http_client_set_header(Client* c,const char* key,const char* value){if(value)c->headers[key]=value;else{c->headers.erase(key);++clears;}return ESP_OK;}
using http_header_handle_t=Client*;struct HeaderItem{char* value;};using http_header_item_handle_t=HeaderItem*;
HeaderItem* http_header_get_item(Client* c,const char* key){auto entry=c->headers.find(key);if(entry==c->headers.end())return nullptr;static HeaderItem item;item.value=const_cast<char*>(entry->second.c_str());return &item;}
'''+getter+r'''
int esp_http_client_get_header(Client* c,const char* key,char** value){return http_header_get(c,key,value);}
'''+setter+r'''
struct esp_http_client_config_t{const char* url;void(*crt_bundle_attach)();int timeout_ms;bool disable_auto_redirect;int buffer_size,buffer_size_tx;int(*event_handler)(void*);void* user_data;};
void esp_crt_bundle_attach(){} int Event(void*){return 0;}
Client* esp_http_client_init(esp_http_client_config_t*){return &client;}
void esp_http_client_set_method(Client*,int){}void esp_http_client_close(Client*){}
int esp_http_client_perform(Client* c){++requests;std::fprintf(stderr,"HTTP round %d Content-Type=%s\n",requests,(c->headers.count("Content-Type")?c->headers["Content-Type"].c_str():"<missing>"));assert(c->headers["Content-Type"]=="application/json");assert(c->post_data&&c->post_len>0);assert(std::string(c->post_data,c->post_len)=="{\"seq\":"+std::to_string(requests)+"}");return ESP_OK;}
int esp_http_client_get_status_code(Client*){return 200;}
struct Done{};void vTaskDelay(int){if(requests==3){assert(!client.post_data&&client.post_len==0&&clears==3);throw Done{};}}void vTaskDelete(void*){}
void esp_fill_random(uint8_t* data,size_t length){std::memset(data,1,length);}
namespace FamilyOnboarding{std::string Key(){return "synthetic";}bool Activated(){return true;}}
namespace SystemInfo{std::string GetMacAddress(){return "02:00:00:00:00:01";}}
struct Command{std::string id;};struct State{void Offer(Command,uint64_t){}};State state;
std::mutex mutex;std::atomic<bool> online{true},started{true};int volume=25,brightness=50;
constexpr const char* Endpoint="https://fixture.invalid/api/device/state";
uint64_t Now(){return 100;}bool Snapshot(){return true;}
std::string Report(const std::string&,uint64_t seq,int,int,State&){return "{\"seq\":"+std::to_string(seq)+"}";}
bool ParseResponse(const std::string&,Command&){return true;}
'''+worker+r'''
int main(){try{Worker(nullptr);}catch(Done&){assert(requests==3);return 0;}return 1;}
'''
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)/'worker.cc';path.write_text(harness)
            binary=Path(directory)/'worker'
            compiled=subprocess.run(['c++','-std=c++17','-Wall','-Wextra','-Werror',str(path),'-o',str(binary)],capture_output=True,text=True)
            self.assertEqual(compiled.returncode,0,compiled.stderr)
            result=subprocess.run([str(binary)],capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stderr)
            self.assertEqual(result.stderr.count('Content-Type=application/json'),3)

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
