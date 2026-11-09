"""Cloud host execution of actual Configure startup boundaries; no real identity."""
from pathlib import Path
import subprocess
import tempfile
import unittest
from test_onboarding_lifecycle import function
SOURCE=Path(__file__).resolve().parents[1]/'overlays/onboarding/family_onboarding.cc'
class ConfigureDiagnostics(unittest.TestCase):
    def test_each_failure_reports_exact_stage_and_keeps_cleanup(self):
        source=SOURCE.read_text()
        snapshot=source[source.index('struct ConfigureStartupSnapshot'):source.index('// Only fixed Configure')] if 'struct ConfigureStartupSnapshot' in source else ''
        replay=function(source,'void ReportConfigureStartup() {') if 'void ReportConfigureStartup() {' in source else 'void ReportConfigureStartup() {}'
        body=r'''
#include <cassert>
#include <string>
#include <vector>
#include <utility>
#include <cstdint>
#include "family_setup_state.h"
using FamilyOnboarding::SetupPhase;
constexpr int ESP_OK=0,pdPASS=1,NETWORK_PROV_SECURITY_2=2,NETWORK_PROV_EVENT_HANDLER_NONE=0,network_prov_scheme_ble=1,eSetValueWithOverwrite=0;
using TaskHandle_t=void*;
struct network_prov_mgr_config_t{int scheme,scheme_event_handler;struct{int wifi_conn_attempts;}network_prov_wifi_conn_cfg;};
struct{const char* salt;size_t salt_len;const char* verifier;size_t verifier_len;}security;
std::vector<uint8_t> salt(16),verifier(384);
bool initialized=true,running=false,worker_active=false,manager_ready=false,endpoint_created=false;
int expiry_timer=1,stops=0,timer_stops=0,fail_stage=0,calls=0,screen_errors=0;
long long deadline;constexpr long long WindowUs=180000000;
std::vector<std::pair<std::string,int>> logs;
constexpr uint32_t MALLOC_CAP_INTERNAL=1,MALLOC_CAP_8BIT=2;
unsigned live_heap=100;int critical_depth=0;
using portMUX_TYPE=int;
#define portMUX_INITIALIZER_UNLOCKED 0
void portENTER_CRITICAL(portMUX_TYPE*){assert(critical_depth++==0);}
void portEXIT_CRITICAL(portMUX_TYPE*){assert(--critical_depth==0);}
size_t heap_caps_get_free_size(uint32_t caps){assert(!critical_depth);assert(caps==2||caps==3);return caps==3?live_heap:1000;}
size_t heap_caps_get_largest_free_block(uint32_t caps){assert(!critical_depth);assert(caps==3);return 50;}
void LogInfo(const char* tag,const char* format,const char* stage,int result,unsigned internal,unsigned largest,unsigned free8,int reclaimed,unsigned resource_stage,unsigned prior_internal){
 assert(!critical_depth);assert(std::string(tag)=="FamilySetup");assert(std::string(format)=="configure stage=%s err=%d internal=%u largest=%u free8=%u reclaimed=%d resource_stage=%u prior_internal=%u"||std::string(format)=="configure replay stage=%s err=%d internal=%u largest=%u free8=%u reclaimed=%d resource_stage=%u prior_internal=%u");
 assert(largest==50&&free8==1000);if(std::string(stage)=="voice_bt_cleanup"){assert(reclaimed==12345&&resource_stage==3&&prior_internal==100&&internal==12445);}else{assert(internal==100&&reclaimed==0&&resource_stage==0&&prior_internal==0);}logs.emplace_back(stage,result);
}
#define ESP_LOGI LogInfo
bool Activated(){return false;}
void BeginSetup(){}void ClearClaim(){}void ShowSetup(SetupPhase p){assert(p==SetupPhase::StartError);++screen_errors;}
int result(){return ++calls==fail_stage?123:0;}
int network_prov_mgr_init(network_prov_mgr_config_t){return result();}
int network_prov_mgr_endpoint_create(const char*){return result();}
int network_prov_mgr_disable_auto_stop(int){return result();}
int network_prov_mgr_start_provisioning(int,void*,const char*,void*){return result();}
int network_prov_mgr_endpoint_register(const char*,void(*)(void),void*){return result();}
void network_prov_mgr_stop_provisioning(){++stops;}
int esp_timer_start_once(int,long long){return result();}void esp_timer_stop(int){++timer_stops;}
long long esp_timer_get_time(){return 1;}
void Worker(void*){}void Claim(){}
int xTaskCreate(void(*)(void*),const char*,int,void*,int,TaskHandle_t* task){if(result())return -1;*task=reinterpret_cast<void*>(1);return pdPASS;}
int xTaskNotify(TaskHandle_t task,int command,int){assert(task==reinterpret_cast<void*>(1));if(command==2)worker_active=false;else assert(command==1);return pdPASS;}
struct SystemInfo{static std::string GetMacAddress(){return "PRIVATE_MAC";}};
'''+snapshot+'\n'+function(source,'void LogConfigure(')+'\n'+replay+'\n'+function(source,'void Configure() {')+r'''
int main(){
 ReportConfigureStartup();assert(logs.empty());
 const char* failed[]={"manager_init","endpoint_create","auto_stop","worker_create","provision_start","endpoint_register","expiry_start"};
 for(int i=1;i<=7;++i){
  running=worker_active=manager_ready=endpoint_created=false;logs.clear();calls=stops=timer_stops=screen_errors=0;fail_stage=i;
  Configure();assert(screen_errors==1&&!running&&!worker_active);
  assert(!logs.empty()&&logs.back().first==failed[i-1]&&logs.back().second!=0);
  assert(stops==(i>=5?1:0));assert(timer_stops==0);
  for(const auto& entry:logs)assert(entry.first.find("PRIVATE")==std::string::npos);
 }
 running=worker_active=manager_ready=endpoint_created=false;logs.clear();calls=screen_errors=0;fail_stage=0;Configure();
 assert(running&&worker_active&&screen_errors==0&&logs.back().first=="expiry_start"&&logs.back().second==0);
 const std::vector<std::string> expected={"begin","screen","manager_init","endpoint_create","auto_stop","worker_create","provision_start","endpoint_register","expiry_start"};
 assert(logs.size()==expected.size());for(size_t i=0;i<logs.size();++i)assert(logs[i].first==expected[i]);
 // Late console sees exactly the latest stage, with captured rather than live allocator facts.
 logs.clear();live_heap=999;ReportConfigureStartup();
 assert(logs.size()==1&&logs.back().first=="expiry_start"&&logs.back().second==0);
 logs.clear();ReportConfigureStartup();assert(logs.empty());
 live_heap=100;LogConfigure("manager_init",123);LogConfigure("endpoint_create",456);
 logs.clear();live_heap=999;ReportConfigureStartup();
 assert(logs.size()==1&&logs.back().first=="endpoint_create"&&logs.back().second==456);
 logs.clear();ReportConfigureStartup();assert(logs.empty());
 live_heap=100;LogConfigure("voice_bt_cleanup",0,12345,3,100);logs.clear();live_heap=999;
 ReportConfigureStartup();assert(logs.size()==1&&logs.back().first=="voice_bt_cleanup");
 logs.clear();ReportConfigureStartup();assert(logs.size()==1&&logs.back().first=="voice_bt_cleanup");
}
'''
        with tempfile.TemporaryDirectory() as tmp:
            tmp=Path(tmp);(tmp/'test.cc').write_text(body)
            result=subprocess.run(['c++','-std=c++17','-Wall','-Wextra','-Werror','-I',str(SOURCE.parent),str(tmp/'test.cc'),'-o',str(tmp/'test')],capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stderr)
            result=subprocess.run([str(tmp/'test')],capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stderr)

    def test_isolated_prepare_adds_replay_to_existing_heap_telemetry(self):
        import importlib.util
        root=SOURCE.parents[2]
        spec=importlib.util.spec_from_file_location('prepare',root/'tools/prepare_onboarding.py')
        module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
        self.assertTrue(hasattr(module,'prepare_configure_replay'),'missing existing-telemetry replay hook')
        with tempfile.TemporaryDirectory() as tmp:
            tree=Path(tmp);(tree/'main').mkdir();path=tree/'main/system_info.cc'
            original='#include "system_info.h"\nvoid SystemInfo::PrintHeapStats() {\n    ExistingHeapStats();\n}\n'
            path.write_text(original);module.prepare_configure_replay(tree)
            result=path.read_text()
            self.assertIn('#include "family_onboarding.h"',result)
            self.assertIn('void SystemInfo::PrintHeapStats() {\n    FamilyOnboarding::ReportConfigureStartup();',result)
            self.assertIn('ExistingHeapStats();',result)
