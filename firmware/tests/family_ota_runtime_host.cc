#include "family_ota_protocol.h"
#include <cassert>
#include <atomic>
#include <functional>
#include <mutex>
#include <vector>
using esp_ota_img_states_t=int;constexpr int ESP_OTA_IMG_PENDING_VERIFY=1,ESP_OK=0,pdPASS=1;
static uint64_t now=100;static uint64_t Now(){return now;}
static int boot_state=0,marks=0,rollbacks=0,restarts=0,task_creates=0,mark_result=0,create_result=pdPASS;
static int esp_ota_get_state_partition(const void*,int* out){*out=boot_state;return 0;}
static const void* esp_ota_get_running_partition(){return reinterpret_cast<void*>(1);}
static int esp_ota_mark_app_valid_cancel_rollback(){++marks;return mark_result;}
static int esp_ota_mark_app_invalid_rollback_and_reboot(){++rollbacks;return 0;}
static void esp_restart(){++restarts;}
static std::function<void()> transient;
static int xTaskCreate(void(*fn)(void*),const char*,unsigned,void* arg,unsigned,void*){++task_creates;if(create_result!=pdPASS)return 0;transient=[=]{fn(arg);};return pdPASS;}
static void vTaskDelete(void*){}
static bool layout_ok=true;static bool CompatibleLayout(){return layout_ok;}
struct Codec{bool healthy=false,available=true,paused=false,resuming=false,pause_ok=true,resume_ok=true,finish_ok=true,fault=false;int pauses=0,resumes=0;uint64_t pause_elapsed=0;bool ReferenceFaulted(){return fault;}bool FirmwareHealthy(){return healthy;}bool FirmwareAvailable(){return available&&!fault;}bool PauseForFirmware(){++pauses;now+=pause_elapsed;paused=pause_ok;return pause_ok;}bool ResumeAfterFirmware(){++resumes;resuming=true;paused=false;return resume_ok;}bool FinishFirmwareResume(){resuming=false;return finish_ok&&healthy;}void ReportReferenceFailure(){fault=true;available=false;}}codec;
struct Board{static Board& GetInstance(){static Board b;return b;}Codec* GetAudioCodec(){return &codec;}};
struct Application{bool eligible=true,begun=false,quiet=true,wake=false;int finishes=0;std::vector<std::function<void()>> queued;static Application& GetInstance(){static Application a;return a;}bool FamilyFirmwareEligible(){return eligible;}bool BeginFamilyFirmware(){if(!eligible)return false;begun=true;wake=false;return true;}bool FamilyFirmwareQuiescent(){return quiet;}bool PrepareFamilyFirmwareBootAudio(){wake=true;return true;}void PauseFamilyFirmwareInput(){wake=false;}void ResumeFamilyFirmwareInput(){wake=true;}void FinishFamilyFirmware(){++finishes;begun=false;wake=true;}void Schedule(std::function<void()> fn){queued.push_back(fn);}void Drain(){auto q=std::move(queued);queued.clear();for(auto& fn:q)fn();}};
namespace FamilyControl{static bool held=false,busy=false;bool ReserveFirmware(){if(held||busy)return false;return held=true;}void ReleaseFirmware(){held=false;}}
static bool DownloadAvailable(){return true;}
static const char* download_error=nullptr;static const char* Download(const FamilyOta::Command&){return download_error;}
namespace FamilyOnboarding{static bool Present(){return true;}}
struct esp_app_desc_t{const char* version="2.5.3";uint8_t app_elf_sha256[32]{};};
static const esp_app_desc_t* esp_app_get_description(){static esp_app_desc_t d;return &d;}
#include "family_ota_runtime.inc"
static std::string Request(){return "{\"command\":{\"id\":\"ota_"+std::string(32,'a')+"\",\"releaseId\":\"fw_"+std::string(64,'a')+"\",\"version\":\"2.5.4\",\"url\":\"https://robot.songguoxue.com/api/device/firmware/ota_"+std::string(32,'a')+"\",\"size\":1024,\"sha256\":\""+std::string(64,'a')+"\",\"elfSha256\":\""+std::string(64,'b')+"\",\"ttlMs\":15000}}";}
int main(int argc,char** argv){
 FamilyOta::Progress(0);assert(argc==2);std::string scenario=argv[1];auto& app=Application::GetInstance();
 boot_state=scenario.rfind("pending",0)==0?ESP_OTA_IMG_PENDING_VERIFY:0;
 FamilyOta::InitializeBoot();assert(marks==0);
 if(boot_state==ESP_OTA_IMG_PENDING_VERIFY){
  now+=1000;FamilyOta::Tick();assert(marks==0&&rollbacks==0&&app.wake);
  if(scenario=="pending-timeout"){now+=30000;FamilyOta::Tick();assert(rollbacks==1&&marks==0);return 0;}
  codec.healthy=true;if(scenario.find("marker-error")!=std::string::npos)mark_result=-1;
  if(scenario=="pending-restore-error"||scenario=="pending-marker-error-restore-error")codec.resume_ok=false;
  FamilyOta::Tick();assert(marks==1&&codec.pauses==1&&codec.resumes==1);
  assert(rollbacks==0);
  if(scenario=="pending-marker-error-restore-error"){now+=30000;FamilyOta::Tick();assert(restarts==1&&!FamilyOta::Ready()&&marks==1);return 0;}
  if(scenario=="pending-marker-error-restore-timeout"){codec.healthy=false;now+=3000;FamilyOta::Tick();assert(!FamilyOta::Ready());now+=30000;FamilyOta::Tick();assert(restarts==1&&marks==1&&!FamilyOta::Ready());return 0;}
  if(mark_result!=0){assert(!FamilyOta::Ready());return 0;}
  if(scenario=="pending-restore-error"){now+=40000;FamilyOta::Tick();assert(rollbacks==0&&marks==1&&!FamilyOta::Ready());return 0;}
  FamilyOta::Tick();assert(FamilyOta::Ready()&&marks==1&&rollbacks==0);return 0;
 }
 codec.healthy=true;FamilyOta::Tick();assert(FamilyOta::Ready());codec.healthy=false;FamilyOta::Tick();assert(FamilyOta::Ready()&&"valid power idle must retain eligibility");
 if(scenario=="valid-fault"){codec.fault=true;now+=40000;FamilyOta::Tick();assert(!FamilyOta::Ready()&&!marks&&!rollbacks&&!restarts);return 0;}
 if(scenario=="busy")FamilyControl::busy=true;
 FamilyOta::Receive(Request(),now);FamilyOta::Receive(Request(),now);assert(app.queued.size()==1);if(scenario=="stale")now+=15000;
 app.Drain();
 if(scenario=="busy"||scenario=="stale"){assert(!FamilyOta::Reserved()&&task_creates==0);return 0;}
 assert(FamilyOta::Reserved()&&FamilyControl::held&&app.begun&&task_creates==0);
 if(scenario=="task-error")create_result=0;
 if(scenario=="expiry-during-pause")codec.pause_elapsed=15000;
 FamilyOta::Tick();if(scenario=="expiry-during-pause"){assert(task_creates==0&&codec.resuming);return 0;}assert(task_creates==1&&(codec.paused||scenario=="task-error"));
 if(scenario=="task-error"){assert(codec.resuming&&app.wake);codec.healthy=true;FamilyOta::Tick();assert(!FamilyOta::Reserved());return 0;}
 if(scenario=="download-error" || scenario=="retry")download_error="hash_mismatch";
 transient();app.Drain();
 if(scenario=="download-error"||scenario=="retry"){assert(codec.resuming&&app.wake&&FamilyOta::Reserved());codec.healthy=true;FamilyOta::Tick();assert(!FamilyOta::Reserved()&&!FamilyControl::held&&app.finishes==1&&!restarts);FamilyOta::Receive(Request(),now);assert(app.queued.empty()&&task_creates==1);
  if(scenario=="retry"){
   uint64_t revision=0;auto report=FamilyOta::Report(std::string(32,'a'),1,&revision);assert(report.find("hash_mismatch")!=std::string::npos);
   FamilyOta::Receive("{\"command\":null}",now,revision-1);assert(app.queued.empty());
   FamilyOta::Receive("{\"command\":null}",now,revision);app.Drain();FamilyOta::Tick();
   report=FamilyOta::Report(std::string(32,'a'),2);assert(report.find("\"jobId\":\"\"")!=std::string::npos&&FamilyOta::Ready());
   auto request=Request();size_t offset=0;while((offset=request.find("ota_",offset))!=std::string::npos){request[offset+4]='b';offset+=4;}
   FamilyOta::Receive(request,now);app.Drain();FamilyOta::Tick();assert(task_creates==2&&FamilyOta::Reserved());
  }
  return 0;}
 assert(FamilyOta::Reserved()&&codec.paused&&restarts==0);now+=6000;FamilyOta::Tick();assert(restarts==1&&app.finishes==0);
}
