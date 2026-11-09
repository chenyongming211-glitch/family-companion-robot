"""Execute actual native factory event/clock functions with bounded host platform fakes."""
from pathlib import Path
import subprocess
import tempfile
import unittest
SOURCE = Path(__file__).resolve().parents[1] / 'overlays/onboarding/family_onboarding.cc'
def function(text, signature):
    begin=text.index(signature); opening=text.index('{',begin); depth=1; end=opening+1
    while depth:
        depth += (text[end]=='{')-(text[end]=='}'); end+=1
    return text[begin:end]
class NativeLifecycle(unittest.TestCase):
    def test_actual_retry_clock_and_connection_gate(self):
        source=SOURCE.read_text()
        fake=r'''
#include <atomic>
#include <cassert>
#include <functional>
using esp_event_base_t=const char*;
const char *NETWORK_PROV_EVENT="prov", *WIFI_EVENT="wifi", *IP_EVENT="ip";
constexpr int NETWORK_PROV_WIFI_CRED_FAIL=1,WIFI_EVENT_STA_START=2,WIFI_EVENT_STA_DISCONNECTED=3,IP_EVENT_STA_GOT_IP=4,ESP_OK=0,pdPASS=1;
#define pdMS_TO_TICKS(x) (x)
#define ESP_LOGE(...) ((void)0)
std::atomic<bool> running{false},connected{false},clock_worker{false},clock_synced{false},connection_reported{false};
std::atomic<unsigned> network_generation{0};
std::function<void(bool)> callback;
bool activated=false; int resets=0,notices=0,sntp_starts=0,waits=0,sync_result=ESP_OK,connects=0,queued=0;
void (*task)(void*)=nullptr; std::function<void()> on_wait;
bool Activated(){return activated;}
int network_prov_mgr_reset_wifi_sm_state_on_failure(){++resets;return ESP_OK;}
int esp_netif_sntp_start(){++sntp_starts;return ESP_OK;}
int esp_netif_sntp_sync_wait(int ms){assert(ms==15000);++waits;if(on_wait)on_wait();return sync_result;}
int esp_wifi_connect(){++connects;return ESP_OK;}
int xTaskCreate(void (*fn)(void*),const char*,int,void*,int,void*){++queued;task=fn;return pdPASS;}
void vTaskDelete(void*){}
struct Display {void ShowNotification(const char*){++notices;}};
struct Board {static Board& GetInstance(){static Board b;return b;} Display* GetDisplay(){static Display d;return &d;}};
'''
        body=fake+'\n'+function(source,'bool SecureClock() {')+'\n'+function(source,'void ScheduleClock() {')+'\n'+function(source,'void TimeSynchronized(')+'\n'+function(source,'void Event(')+r'''
int main(){
 running=true; Event(nullptr,NETWORK_PROV_EVENT,NETWORK_PROV_WIFI_CRED_FAIL,nullptr); assert(resets==1&&notices==1);
 running=false; Event(nullptr,NETWORK_PROV_EVENT,NETWORK_PROV_WIFI_CRED_FAIL,nullptr); assert(resets==1);
 assert(SecureClock()); assert(SecureClock()); assert(waits==1); // sync semaphore is consumed once
 clock_synced=false; activated=true; sync_result=-1; int accepted=0; callback=[&](bool c){if(c)++accepted;};
 Event(nullptr,IP_EVENT,IP_EVENT_STA_GOT_IP,nullptr); Event(nullptr,IP_EVENT,IP_EVENT_STA_GOT_IP,nullptr);
 assert(sntp_starts==2&&queued==1&&accepted==0); task(nullptr); assert(accepted==0&&!clock_worker);
 // Late real SNTP success on the SAME association must recover without GOT_IP.
 int before=waits; TimeSynchronized(nullptr); assert(queued==2); task(nullptr); assert(accepted==1&&clock_synced&&waits==before);
 TimeSynchronized(nullptr); Event(nullptr,IP_EVENT,IP_EVENT_STA_GOT_IP,nullptr); assert(queued==2&&accepted==1);
 Event(nullptr,WIFI_EVENT,WIFI_EVENT_STA_DISCONNECTED,nullptr); assert(!connected&&connects==1&&!connection_reported);
 TimeSynchronized(nullptr); assert(queued==2); // never connect while link is down
 Event(nullptr,IP_EVENT,IP_EVENT_STA_GOT_IP,nullptr); assert(queued==3); task(nullptr); assert(accepted==2&&waits==before);
 Event(nullptr,WIFI_EVENT,WIFI_EVENT_STA_DISCONNECTED,nullptr); clock_synced=false; sync_result=-1;
 Event(nullptr,IP_EVENT,IP_EVENT_STA_GOT_IP,nullptr); assert(queued==4);
 on_wait=[](){TimeSynchronized(nullptr);}; task(nullptr); on_wait=nullptr;
 assert(queued==5&&accepted==2); task(nullptr); assert(accepted==3); // sync at timeout cleanup cannot be lost
}
'''
        with tempfile.TemporaryDirectory() as tmp:
            tmp=Path(tmp);(tmp/'test.cc').write_text(body)
            built=subprocess.run(['c++','-std=c++20','-Wall','-Wextra','-Werror',str(tmp/'test.cc'),'-o',str(tmp/'test')],capture_output=True,text=True)
            self.assertEqual(built.returncode,0,built.stderr)
            ran=subprocess.run([str(tmp/'test')],capture_output=True,text=True)
            self.assertEqual(ran.returncode,0,ran.stdout+ran.stderr)
