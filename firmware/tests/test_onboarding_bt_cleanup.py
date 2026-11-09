"""Cloud common-S3 activated-only irreversible Bluetooth reclamation checks."""
from pathlib import Path
import subprocess
import tempfile
import unittest
from test_onboarding_lifecycle import function
ROOT=Path(__file__).resolve().parents[1]
class ActivatedBluetooth(unittest.TestCase):
    def test_same_helper_and_activated_boot_order_on_both_s3_profiles(self):
        source=(ROOT/'overlays/onboarding/family_onboarding.cc').read_text()
        ready=function(source,'bool VoiceResourcesReady() {') if 'bool VoiceResourcesReady() {' in source else 'bool VoiceResourcesReady(){return true;}'
        cleanup=function(source,'bool PrepareActivatedVoice() {') if 'bool PrepareActivatedVoice() {' in source else 'bool PrepareActivatedVoice(){return true;}'
        boot=function(source,'bool PrepareVoiceOnBoot() {') if 'bool PrepareVoiceOnBoot() {' in source else 'bool PrepareVoiceOnBoot(){return true;}'
        start=function(source,'void Start(std::function<void(bool)> connection) {')
        worker=function(source,'void Worker(void*) {')
        recovery=function(start,'xTaskCreate([](void*) {').replace('xTaskCreate([](void*)','void Recovery(void*)',1)
        start=start[:start.index('    err = esp_event_loop_create_default();')]+'}\n'
        body=r'''
#include <atomic>
#include <algorithm>
#include <cassert>
#include <functional>
#include <string>
#include <vector>
#include <cstdint>
#include "family_setup_state.h"
using namespace FamilyOnboarding;
constexpr int ESP_OK=0,ESP_FAIL=-1,ESP_ERR_INVALID_STATE=259,ESP_BT_MODE_BLE=1,ESP_BT_CONTROLLER_STATUS_IDLE=0,MALLOC_CAP_INTERNAL=1,MALLOC_CAP_8BIT=2,NVS_READONLY=0,ESP_BLUEDROID_STATUS_UNINITIALIZED=0;
std::atomic<bool> voice_resources_ready{false};
bool factory_present=true;bool Present(){return factory_present;}
bool activated=false,manager_ready=false,endpoint_created=false,initialized=false;
int host_status=0,controller_status=0,failure=0,deinits=0,releases=0,wifi=0,errors=0,lock=0;
std::vector<int> order;std::function<void()> during_deinit;
bool Activated(){return activated;}
size_t heap_caps_get_free_size(unsigned){return releases&&failure!=3?200:100;}
size_t heap_caps_get_largest_free_block(unsigned){return 80;}
int network_prov_mgr_deinit(){++deinits;order.push_back(1);if(during_deinit)during_deinit();if(failure==1)return ESP_FAIL;controller_status=0;if(failure!=4)host_status=0;return 0;}
int esp_bt_controller_get_status(){return controller_status;}
int esp_bluedroid_get_status(){return host_status;}
int esp_bt_mem_release(int mode){assert(mode==ESP_BT_MODE_BLE&&controller_status==0);++releases;order.push_back(2);return failure==3?ESP_FAIL:ESP_OK;}
void LogInfo(const char* tag,const char* format,unsigned stage,int,int reclaimed,unsigned internal,unsigned largest){
 assert(std::string(tag)=="FamilySetup");assert(std::string(format)=="voice_bt_cleanup stage=%u err=%d reclaimed=%d internal=%u largest=%u");
 assert(stage>=1&&stage<=3&&reclaimed>=0&&internal>=100&&largest==80);
}
void LogConfigure(const char* stage,int,int reclaimed=0,unsigned resource_stage=0,unsigned prior_internal=0){
 assert(std::string(stage)=="voice_bt_cleanup"&&resource_stage>=1&&resource_stage<=3&&prior_internal==100&&reclaimed>=0);
}
#define ESP_LOGI LogInfo
std::vector<uint8_t> salt,verifier,screen_code;bool screen_code_valid=false;
using nvs_handle_t=int;
int nvs_open(const char*,int,int* p){*p=1;return 0;}void nvs_close(int){}
bool LoadBlob(int,const char*,std::vector<uint8_t>& data,size_t size){data.resize(size);return true;}
std::function<void(bool)> callback;
std::string Key(){return std::string(43,'a');}
int commits=0;
struct Settings{bool pending=false;Settings(const char*,bool=false){}~Settings(){if(pending){activated=true;++commits;}}void SetInt(const char*,int value){assert(value==1);pending=true;}std::string GetString(const char* key){return std::string(key)=="mac"?"synthetic":"dev_"+std::string(32,'a');}};
struct SystemInfo{static std::string GetMacAddress(){return "synthetic";}};
int xSemaphoreCreateMutex(){return 1;}void BeginSetup(){}void ShowSetup(SetupPhase phase,uint32_t=0){if(phase==SetupPhase::StartError)++errors;}
int esp_netif_init(){assert(voice_resources_ready);++wifi;order.push_back(3);return ESP_OK;}
'''+ready+'\n'+cleanup+'\n'+boot+'\n'+start+r'''
constexpr int pdTRUE=1;constexpr unsigned portMAX_DELAY=~0u;
#define pdMS_TO_TICKS(x) (x)
std::atomic<bool> running{true},worker_active{true},connected{true};std::atomic<unsigned> network_generation{0};
long long deadline=1000000;std::string claim_id="synthetic",challenge="synthetic";int expiry_timer=1,notifications=0,deletes=0;
unsigned ulTaskNotifyTake(int,unsigned){return 1;}unsigned CurrentSetup(){return 1;}
long long esp_timer_get_time(){return 1;}bool SecureClock(){return true;}bool Activate(const std::string&,const std::string&){return true;}
void xSemaphoreTake(int,unsigned){}void xSemaphoreGive(int){}void ClearClaim(){}void esp_timer_stop(int){}
void vTaskDelay(int){}void vTaskDelete(void*){++deletes;}void network_prov_mgr_stop_provisioning(){}
bool PostConnected(unsigned){assert(activated&&VoiceResourcesReady());++notifications;return true;}void ScheduleClock(){}
void esp_wifi_disconnect(){assert(false);}void esp_wifi_stop(){assert(false);}void Configure(){assert(false);}
'''+worker+'\n'+recovery+r'''
void reset(){activated=manager_ready=endpoint_created=initialized=false;voice_resources_ready=false;host_status=controller_status=failure=deinits=releases=wifi=errors=0;order.clear();during_deinit=nullptr;commits=notifications=deletes=0;running=worker_active=connected=true;}
int main(){
 reset();manager_ready=endpoint_created=true;
 assert(!PrepareActivatedVoice()&&deinits==0&&releases==0); // unbound expiry/retry must retain BLE
 activated=true;controller_status=2;during_deinit=[](){assert(!VoiceResourcesReady());};
 assert(PrepareActivatedVoice()&&VoiceResourcesReady());assert((order==std::vector<int>{1,2}));
 assert(!manager_ready&&!endpoint_created&&deinits==1&&releases==1);
 assert(PrepareActivatedVoice()&&releases==1&&deinits==1); // irreversible API called once
 for(int fail=1;fail<=4;++fail){
  reset();activated=true;failure=fail;manager_ready=fail==1||fail==4;host_status=fail==4?1:0;controller_status=fail==2?2:0;
  assert(!PrepareActivatedVoice()&&!VoiceResourcesReady());
  assert(releases==(fail==3?1:0));
 }
 reset();activated=true;assert(PrepareVoiceOnBoot()&&releases==1&&VoiceResourcesReady());Start(nullptr);assert(wifi==1&&releases==1&&deinits==0);assert((order==std::vector<int>{2,3}));
 reset();activated=true;failure=3;Start(nullptr);assert(wifi==0&&!VoiceResourcesReady()&&errors==1);
 for(auto operation:{Worker,Recovery}){
  reset();manager_ready=endpoint_created=true;controller_status=2;
  during_deinit=[](){assert(activated&&commits==1&&!VoiceResourcesReady()&&notifications==0);};
  operation(nullptr);assert(commits==1&&releases==1&&notifications==1&&deletes==1&&!worker_active);
  reset();manager_ready=true;failure=1;operation(nullptr);
  assert(activated&&commits==1&&!VoiceResourcesReady()&&notifications==0&&deletes==1&&errors==1&&!worker_active);
 }
}
'''
        with tempfile.TemporaryDirectory() as d:
            d=Path(d);(d/'test.cc').write_text(body)
            for board in ['M5STACK_STOPWATCH','WAVESHARE_ESP32_S3_TOUCH_AMOLED_2_16']:
                result=subprocess.run(['c++','-std=c++17','-Wall','-Wextra','-Werror','-Wno-unused-variable',f'-DCONFIG_BOARD_TYPE_{board}=1','-I',str(ROOT/'overlays/onboarding'),str(d/'test.cc'),'-o',str(d/'test')],capture_output=True,text=True)
                self.assertEqual(result.returncode,0,result.stderr)
                result=subprocess.run([str(d/'test')],capture_output=True,text=True)
                self.assertEqual(result.returncode,0,result.stderr)

    def test_common_application_hook_precedes_audio_initialization(self):
        import importlib.util
        spec=importlib.util.spec_from_file_location('prepare',ROOT/'tools/prepare_onboarding.py')
        prepare=importlib.util.module_from_spec(spec);spec.loader.exec_module(prepare)
        self.assertTrue(hasattr(prepare,'prepare_voice_boot'),'missing shared activated boot audio gate')
        with tempfile.TemporaryDirectory() as d:
            tree=Path(d);(tree/'main').mkdir();path=tree/'main/application.cc'
            path.write_text('#include "application.h"\nvoid Application::Initialize() {\n    // Setup the audio service\n    auto codec = board.GetAudioCodec();\n    audio_service_.Initialize(codec);\n}\n')
            prepare.prepare_voice_boot(tree);text=path.read_text()
            self.assertIn('#include "family_onboarding.h"',text)
            self.assertLess(text.index('PrepareVoiceOnBoot()'),text.index('board.GetAudioCodec()'))
            self.assertIn('SystemInfo::PrintHeapStats();',text)
            (tree/'main/application.h').write_text(r'''
#include <cassert>
struct Halted{};bool cleanup_ok=false;int audio=0,runs=0,halts=0,codecs=0;
namespace FamilyOnboarding{bool PrepareVoiceOnBoot(){return cleanup_ok;}}
struct SystemInfo{static void PrintHeapStats(){}}
;
void vTaskDelete(void* current){assert(current==nullptr);++halts;throw Halted{};}
struct Board{int GetAudioCodec(){++codecs;return 0;}}board;
struct Audio{void Initialize(int){++audio;}};
struct Application{Audio audio_service_;void Initialize();void Run(){++runs;}};
''')
            (tree/'main/family_onboarding.h').write_text('')
            program=text+r'''
int main(){Application app;try{app.Initialize();app.Run();}catch(const Halted&){}
assert(audio==0&&codecs==0&&runs==0&&halts==1);
cleanup_ok=true;halts=0;app.Initialize();app.Run();assert(audio==1&&codecs==1&&runs==1&&halts==0);}
'''
            path.write_text(program)
            result=subprocess.run(['c++','-std=c++17','-Wall','-Wextra','-Werror',str(path),'-o',str(tree/'test')],capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stderr)
            result=subprocess.run([str(tree/'test')],capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stderr)
