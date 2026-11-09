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
        body=r'''
#include <cassert>
#include <string>
#include <vector>
#include <utility>
#include <cstdint>
#include "family_setup_state.h"
using FamilyOnboarding::SetupPhase;
constexpr int ESP_OK=0,pdPASS=1,NETWORK_PROV_SECURITY_2=2,NETWORK_PROV_EVENT_HANDLER_NONE=0,network_prov_scheme_ble=1;
struct network_prov_mgr_config_t{int scheme,scheme_event_handler;struct{int wifi_conn_attempts;}network_prov_wifi_conn_cfg;};
struct{const char* salt;size_t salt_len;const char* verifier;size_t verifier_len;}security;
std::vector<uint8_t> salt(16),verifier(384);
bool initialized=true,running=false,worker_active=false,manager_ready=false,endpoint_created=false;
int expiry_timer=1,stops=0,timer_stops=0,fail_stage=0,calls=0,screen_errors=0;
long long deadline;constexpr long long WindowUs=180000000;
std::vector<std::pair<std::string,int>> logs;
constexpr uint32_t MALLOC_CAP_INTERNAL=1,MALLOC_CAP_8BIT=2;
size_t heap_caps_get_free_size(uint32_t caps){assert(caps==2||caps==3);return caps==3?100:1000;}
size_t heap_caps_get_largest_free_block(uint32_t caps){assert(caps==3);return 50;}
void LogInfo(const char* tag,const char* format,const char* stage,int result,unsigned internal,unsigned largest,unsigned free8){
 assert(std::string(tag)=="FamilySetup");assert(std::string(format)=="configure stage=%s err=%d internal=%u largest=%u free8=%u");
 assert(internal==100&&largest==50&&free8==1000);logs.emplace_back(stage,result);
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
int esp_timer_start_once(int,long long){return 0;}void esp_timer_stop(int){++timer_stops;}
long long esp_timer_get_time(){return 1;}
void Worker(void*){}void Claim(){}
int xTaskCreate(void(*)(void*),const char*,int,void*,int,void*){return result()? -1:pdPASS;}
struct SystemInfo{static std::string GetMacAddress(){return "PRIVATE_MAC";}};
'''+function(source,'void LogConfigure(')+'\n'+function(source,'void Configure() {')+r'''
int main(){
 const char* failed[]={"manager_init","endpoint_create","auto_stop","provision_start","endpoint_register","worker_create"};
 for(int i=1;i<=6;++i){
  running=worker_active=manager_ready=endpoint_created=false;logs.clear();calls=stops=timer_stops=screen_errors=0;fail_stage=i;
  Configure();assert(screen_errors==1&&!running&&!worker_active);
  assert(!logs.empty()&&logs.back().first==failed[i-1]&&logs.back().second!=0);
  assert(stops==(i>=4?1:0));assert(timer_stops==(i==6?1:0));
  for(const auto& entry:logs)assert(entry.first.find("PRIVATE")==std::string::npos);
 }
 running=worker_active=manager_ready=endpoint_created=false;logs.clear();calls=screen_errors=0;fail_stage=0;Configure();
 assert(running&&worker_active&&screen_errors==0&&logs.back().first=="worker_create"&&logs.back().second==0);
 const std::vector<std::string> expected={"begin","screen","manager_init","endpoint_create","auto_stop","provision_start","endpoint_register","worker_create"};
 assert(logs.size()==expected.size());for(size_t i=0;i<logs.size();++i)assert(logs[i].first==expected[i]);
}
'''
        with tempfile.TemporaryDirectory() as tmp:
            tmp=Path(tmp);(tmp/'test.cc').write_text(body)
            result=subprocess.run(['c++','-std=c++17','-Wall','-Wextra','-Werror','-I',str(SOURCE.parent),str(tmp/'test.cc'),'-o',str(tmp/'test')],capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stderr)
            result=subprocess.run([str(tmp/'test')],capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stderr)
