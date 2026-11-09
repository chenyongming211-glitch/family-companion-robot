#include <atomic>
#include <algorithm>
#include <cassert>
#include <condition_variable>
#include <cstdint>
#include <functional>
#include <mutex>
#include <string>
#include <thread>
#include <vector>
#include "family_setup_state.h"
using FamilyOnboarding::SetupPhase;
constexpr int ESP_OK=0,pdPASS=1,pdTRUE=1,NETWORK_PROV_SECURITY_2=2,NETWORK_PROV_EVENT_HANDLER_NONE=0,network_prov_scheme_ble=1,eSetValueWithOverwrite=1;
constexpr unsigned portMAX_DELAY=~0u;
#define pdMS_TO_TICKS(x) (x)
struct network_prov_mgr_config_t{int scheme,scheme_event_handler;struct{int wifi_conn_attempts;}network_prov_wifi_conn_cfg;};
struct{const char* salt;size_t salt_len;const char* verifier;size_t verifier_len;}security;
std::vector<uint8_t> salt(16),verifier(384);
bool initialized=true,manager_ready=false,endpoint_created=false;
std::atomic<bool> running{false},worker_active{false},connected{true};
std::atomic<bool> service_ready{false},endpoint_ready{false},timer_ready{false};
std::atomic<int> stops{0},errors{0},tls{0},epoch_reads{0},allocated{0},deleted{0};
unsigned largest=34816;std::string fail_api;
int expiry_timer=1,lock=1;std::string claim_id,challenge;
std::atomic<unsigned> network_generation{0};
long long deadline;constexpr long long WindowUs=180000000;
struct Task {std::mutex mutex;std::condition_variable cv;unsigned notification=0;bool waiting=false;std::thread thread;};
using TaskHandle_t=Task*;Task* task=nullptr;thread_local Task* self=nullptr;
unsigned ulTaskNotifyTake(int clear,unsigned timeout){assert(clear==pdTRUE&&timeout==portMAX_DELAY);std::unique_lock guard(self->mutex);self->waiting=true;self->cv.notify_all();self->cv.wait(guard,[]{return self->notification!=0;});return self->notification;}
int xTaskNotify(TaskHandle_t t,unsigned value,int mode){assert(t&&mode==eSetValueWithOverwrite);std::lock_guard guard(t->mutex);assert(t->waiting);t->notification=value;t->cv.notify_all();return pdPASS;}
int xTaskCreate(void(*fn)(void*),const char*,int size,void*,int,TaskHandle_t* handle){
 assert(size==8192);if(largest<8192||fail_api=="worker_create")return -1;
 assert(!task);task=new Task;*handle=task;++allocated;
 task->thread=std::thread([=]{self=task;fn(nullptr);--allocated;});
 std::unique_lock guard(task->mutex);task->cv.wait(guard,[]{return task->waiting;});return pdPASS;
}
void join(){if(task){task->thread.join();delete task;task=nullptr;}}
void vTaskDelete(void*){++deleted;}void vTaskDelay(unsigned){}
void LogConfigure(const char*,int){}
bool Activated(){return false;}void BeginSetup(){}void ClearClaim(){}
void ShowSetup(SetupPhase p,uint32_t=0){if(p==SetupPhase::StartError)++errors;}
unsigned CurrentSetup(){assert(service_ready&&endpoint_ready&&timer_ready);++epoch_reads;return 1;}
bool SecureClock(){assert(service_ready&&endpoint_ready&&timer_ready);++tls;return false;}
bool Activate(const std::string&,const std::string&){assert(false);return false;}
bool PostConnected(unsigned){return true;}void ScheduleClock(){}
struct Settings{Settings(const char*,bool){}void SetInt(const char*,int){}};
void xSemaphoreTake(int,unsigned){}void xSemaphoreGive(int){}
long long esp_timer_get_time(){return 1;}
int esp_timer_start_once(int,long long){if(fail_api=="expiry_start")return 123;timer_ready=true;return ESP_OK;}
void esp_timer_stop(int){timer_ready=false;}
int network_prov_mgr_init(network_prov_mgr_config_t){return fail_api=="manager_init"?123:0;}
int network_prov_mgr_endpoint_create(const char*){return fail_api=="endpoint_create"?123:0;}
int network_prov_mgr_disable_auto_stop(int){return fail_api=="auto_stop"?123:0;}
int network_prov_mgr_start_provisioning(int,void*,const char*,void*){largest=7680;if(fail_api=="provision_start")return 123;service_ready=true;return 0;}
void Claim(){}
int network_prov_mgr_endpoint_register(const char*,void(*)(),void*){if(fail_api=="endpoint_register")return 123;endpoint_ready=true;return 0;}
void network_prov_mgr_stop_provisioning(){++stops;service_ready=false;}
struct SystemInfo{static std::string GetMacAddress(){return "SYNTHETIC";}};
// WORKER
// CONFIGURE
void reset(){assert(!task&&allocated==0);running=worker_active=manager_ready=endpoint_created=false;service_ready=endpoint_ready=timer_ready=false;largest=34816;stops=errors=tls=epoch_reads=deleted=0;}
int main(){
 // Observed device capacities: before BLE34816, after BLE7680, internal stack8192.
 reset();Configure();join();assert(errors==0&&tls==1&&epoch_reads==1&&allocated==0&&deleted==1&&!worker_active);assert(stops==1);
 for(const char* failure:{"manager_init","endpoint_create","auto_stop","worker_create","provision_start","endpoint_register","expiry_start"}){
  reset();fail_api=failure;Configure();join();
  assert(errors==1&&!running&&!worker_active&&allocated==0&&tls==0&&epoch_reads==0);
  bool reserved=fail_api=="provision_start"||fail_api=="endpoint_register"||fail_api=="expiry_start";
  assert(deleted==(reserved?1:0));assert(stops==(reserved?1:0));
  // Retry the same initialized manager/endpoint state after cancellation has exited.
  fail_api.clear();largest=34816;errors=stops=deleted=0;Configure();join();
  assert(errors==0&&tls==1&&allocated==0&&deleted==1&&!worker_active&&stops==1);
 }
}
