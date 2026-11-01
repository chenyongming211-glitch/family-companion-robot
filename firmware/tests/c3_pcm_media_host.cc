#include "c3_pcm_audio.h"
#include "playback_progress.h"
#include "button_uplink.h"
#include "c3_echo_stats.h"
#include "family_c3_pcm_cues_generated.h"
#include <openssl/sha.h>
#include <algorithm>
#include <cassert>
#include <chrono>
#include <condition_variable>
#include <cstring>
#include <fstream>
#include <functional>
#include <future>
#include <memory>
#include <mutex>
#include <string>
#include <thread>
#include <vector>
using namespace family_robot;
static int mbedtls_sha256(const unsigned char* p,size_t n,unsigned char* out,int){return SHA256(p,n,out)?0:-1;}
constexpr int ESP_OK=0,ESP_PARTITION_TYPE_DATA=1,ESP_PARTITION_SUBTYPE_ANY=0,ESP_PARTITION_MMAP_DATA=0;
struct esp_partition_t {size_t address=0x600000,size=0x200000;};static esp_partition_t partition;
static std::vector<uint8_t> pack_bytes;static int maps=0,unmaps=0;
static const esp_partition_t* esp_partition_find_first(int,int,const char*){return &partition;}
static int esp_partition_mmap(const esp_partition_t*,size_t offset,size_t n,int,const void** bytes,unsigned* handle){
 assert(offset==0x50000&&n==pack_bytes.size());++maps;*handle=maps;*bytes=pack_bytes.data();return 0;}
static void esp_partition_munmap(unsigned){++unmaps;}
static unsigned scheduler_depth=0,resource_task_reads=0,heap_reads=0,resource_logs=0;
template<class... Args> static void Log(const char*,const char* format,Args... args){assert(scheduler_depth==0);char text[1024];if constexpr(sizeof...(Args)) {assert(std::snprintf(text,sizeof(text),format,args...)>0);}else {assert(std::snprintf(text,sizeof(text),"%s",format)>0);}++resource_logs;}
#define ESP_LOGE(...) Log(__VA_ARGS__)
#define ESP_LOGI(...) Log(__VA_ARGS__)
#define TAG "AudioService"
constexpr unsigned AS_EVENT_AUDIO_TESTING_RUNNING=1,AS_EVENT_WAKE_WORD_RUNNING=2,
 AS_EVENT_AUDIO_PROCESSOR_RUNNING=4,AS_EVENT_AUDIO_INPUT_STOP_REQUEST=8,
 AS_C3_INPUT_EXIT=32,AS_C3_OUTPUT_EXIT=64,AS_C3_TASKS_GO=128;
using EventBits_t=unsigned;
struct Event {std::mutex mutex;std::condition_variable cv;unsigned bits=0;};
using EventGroupHandle_t=Event*;
static std::atomic<bool> force_wait_timeout{false};
static unsigned xEventGroupSetBits(Event* e,unsigned bits){std::lock_guard<std::mutex> l(e->mutex);e->bits|=bits;e->cv.notify_all();return e->bits;}
static unsigned xEventGroupClearBits(Event* e,unsigned bits){std::lock_guard<std::mutex> l(e->mutex);const auto old=e->bits;e->bits&=~bits;return old;}
static unsigned xEventGroupWaitBits(Event* e,unsigned bits,bool clear,bool all,unsigned timeout){
 std::unique_lock<std::mutex> l(e->mutex);auto ready=[&]{return all?(e->bits&bits)==bits:(e->bits&bits)!=0;};
 if(force_wait_timeout.load()&&timeout!=UINT32_MAX)return e->bits;
 if(timeout==UINT32_MAX)e->cv.wait(l,ready);else e->cv.wait_for(l,std::chrono::milliseconds(timeout),ready);
 auto value=e->bits;if(clear&&ready())e->bits&=~bits;return value;}
constexpr bool pdFALSE=false,pdTRUE=true;constexpr int pdPASS=1,pdFAIL=0;constexpr unsigned portMAX_DELAY=UINT32_MAX;
#define pdMS_TO_TICKS(x) (x)
struct Task {std::thread thread;std::atomic<bool> finished{false};unsigned stack=0;};
using TaskHandle_t=Task*;static std::vector<std::unique_ptr<Task>> tasks;static int create_calls=0,fail_create=0;
static std::function<void()> create_observer;
static int xTaskCreate(void(*fn)(void*),const char* name,unsigned stack,void* arg,unsigned,Task** handle){
 if(create_observer)create_observer();
 ++create_calls;if(create_calls==fail_create){*handle=nullptr;return pdFAIL;}
 assert((!std::strcmp(name,"audio_input")&&stack==8192)||(!std::strcmp(name,"audio_output")&&stack==4096));
 auto task=std::make_unique<Task>();auto* raw=task.get();raw->stack=stack;*handle=raw;tasks.push_back(std::move(task));
 raw->thread=std::thread([=]{fn(arg);raw->finished=true;});return pdPASS;}
static void vTaskDelete(void*){}
static void vTaskSuspendAll(){assert(!scheduler_depth);++scheduler_depth;}
static void xTaskResumeAll(){assert(scheduler_depth==1);--scheduler_depth;}
static unsigned uxTaskGetStackHighWaterMark(Task* task){assert(scheduler_depth==1&&task&&!task->finished);++resource_task_reads;return task->stack/2;}
constexpr unsigned MALLOC_CAP_INTERNAL=1,MALLOC_CAP_8BIT=2;
static size_t heap_caps_get_free_size(unsigned){assert(!scheduler_depth);++heap_reads;return 40000;}
static size_t heap_caps_get_largest_free_block(unsigned){assert(!scheduler_depth);++heap_reads;return 20000;}
static size_t heap_caps_get_minimum_free_size(unsigned){assert(!scheduler_depth);++heap_reads;return 15000;}
struct LiteAudioEngine {C3EchoStatistics C3Statistics(){assert(!scheduler_depth);return {};}};
static void JoinTasks(){for(auto& task:tasks)if(task->thread.joinable())task->thread.join();tasks.clear();}
static void esp_timer_stop(int){}static void esp_timer_start_periodic(int,int){}
constexpr int AUDIO_POWER_CHECK_INTERVAL_MS=1000;
struct AudioStreamPacket {int sample_rate=0,frame_duration=0;uint32_t uplink_epoch=0;std::vector<uint8_t> payload;};
struct AudioCodec {
 enum class PlaybackOp{Connect,Start,Admit,Release,Seal,Interrupt,Invalidate,Reset};
 std::atomic<uint32_t> generation{1};std::atomic<bool> busy{false},fault{false},enabled{true};
 std::atomic<int> releases{0},resets{0},writes{0};std::function<void()> output_hook,enable_hook;
 std::mutex recorded_mutex;std::vector<std::vector<int16_t>> recorded;
 uint32_t PlaybackGeneration()const{return generation.load();}bool PlaybackBusy()const{return busy.load();}
 bool PlaybackControl(PlaybackOp op,PlaybackIdentity={},uint64_t=0,size_t=0){
  if(op==PlaybackOp::Reset){++generation;++resets;busy=false;}if(op==PlaybackOp::Release)++releases;return true;}
 bool output_enabled()const{return enabled.load();}void EnableOutput(bool b){if(b&&!enabled.load()){++generation;if(enable_hook)enable_hook();}enabled=b;}
 bool ReferenceFaulted()const{return fault.load();}
 void ReportReferenceFailure(){fault=true;}
 void OutputTaggedSpan(const int16_t* pcm,size_t count,PlaybackIdentity,uint64_t,uint32_t gen){
  if(output_hook)output_hook();
  if(gen!=generation.load())return;
  std::lock_guard<std::mutex> lock(recorded_mutex);recorded.emplace_back(pcm,pcm+count);++writes;}
};
struct Callbacks{std::function<void()> on_send_queue_available,on_playback_drained;};
struct DebugStatistics{unsigned playback_count=0;};
class AudioService {
public:
 C3PcmAudioState c3_pcm_;ButtonUplink button_uplink_;AudioCodec* codec_;Callbacks callbacks_;
 std::unique_ptr<LiteAudioEngine> audio_engine_=std::make_unique<LiteAudioEngine>();
 unsigned c3_cue_mmap_=0;std::mutex c3_service_lifecycle_;bool c3_lifecycle_failed_=false;
 mutable std::mutex audio_queue_mutex_;std::condition_variable audio_queue_cv_;
 std::atomic<bool> service_stopped_{true};Event event;Event* event_group_=&event;
 Task* audio_input_task_handle_=nullptr,*audio_output_task_handle_=nullptr,*opus_codec_task_handle_=nullptr;
 bool output_in_flight_=false,playback_drained_notified_=true;unsigned output_activity_=0,playback_generation_=0;
 int audio_power_timer_=0;std::chrono::steady_clock::time_point last_output_time_;DebugStatistics debug_statistics_;
 explicit AudioService(AudioCodec& c):codec_(&c){}
 void UpdateCaptureActive(){}
 void ResetDecoder(){std::lock_guard<std::mutex> lock(audio_queue_mutex_);codec_->PlaybackControl(AudioCodec::PlaybackOp::Reset);ResetC3PcmOutputLocked();}
 void ClearC3UplinkQueues(){std::lock_guard<std::mutex> l(audio_queue_mutex_);c3_pcm_.input.Reset();}
 void SetExperimentUplink(bool enabled){if(!enabled)button_uplink_.Close([&]{ClearC3UplinkQueues();});}
 void AudioInputTask(){while(!service_stopped_.load())std::this_thread::yield();}
 bool MarkPlaybackDrainedLocked(){if(!C3PcmPlaybackDrainedLocked()||playback_drained_notified_)return false;playback_drained_notified_=true;return true;}
 void PrintC3Resources()const;
 void WakeC3PcmOwner();
 bool OpenC3Uplink(uint32_t);
 bool InitializeC3CuePack();void RequestC3PcmFault(uint32_t);bool FinishC3PcmFault();
 bool FeedC3PcmSpan(const int16_t*,size_t,uint32_t);std::unique_ptr<AudioStreamPacket> PopC3PcmPacket();
 bool PushC3PcmOutput(const uint8_t*,size_t,C3PcmOutputMetadata);void CancelC3PcmCuesLocked();
 void SetC3ServerReceiving(bool);void ResetC3PcmOutputLocked();void PlayC3PcmCue(const std::string_view&);
 bool C3PcmPlaybackDrainedLocked()const;void C3PcmOutputTask();void StartC3Pcm();void StopC3Pcm();
};
#include "c3_pcm_audio.inc"
// PRODUCTION_RESOURCES_METHOD
// PRODUCTION_OPEN_METHOD
static std::vector<uint8_t> Read(const std::string& path){std::ifstream f(path,std::ios::binary);assert(f);return {std::istreambuf_iterator<char>(f),{}};}
template<class Check> static void Eventually(Check check){for(int i=0;i<1000&&!check();++i)std::this_thread::sleep_for(std::chrono::milliseconds(1));assert(check());}
static void Input(){
 AudioCodec codec;AudioService audio(codec);audio.service_stopped_=false;assert(audio.button_uplink_.Open(0,[]{}));auto token=audio.button_uplink_.Capture();
 int16_t samples[512];for(int i=0;i<512;++i)samples[i]=i;
 assert(audio.FeedC3PcmSpan(samples,512,token));assert(audio.FeedC3PcmSpan(samples,512,token));auto packet=audio.PopC3PcmPacket();
 assert(packet&&packet->payload.size()==1920&&packet->uplink_epoch==token);assert(packet->payload[1026]==1&&packet->payload[1027]==0);
 audio.SetExperimentUplink(false);assert(audio.button_uplink_.Open(audio.button_uplink_.Revision(),[&]{audio.ClearC3UplinkQueues();}));
 assert(!audio.FeedC3PcmSpan(samples,512,token));assert(audio.c3_pcm_.input.occupied()==0);
 const auto newer=audio.button_uplink_.Capture();audio.RequestC3PcmFault(token);assert(!audio.FinishC3PcmFault());assert(audio.button_uplink_.Allows(newer));
}
static void BusyFull(){
 AudioCodec codec;AudioService audio(codec);audio.service_stopped_=false;assert(audio.button_uplink_.Open(0,[]{}));const auto token=audio.button_uplink_.Capture();
 std::promise<void> entered,release;auto release_future=release.get_future().share();
 auto send=std::async(std::launch::async,[&]{return audio.button_uplink_.Send(token,[&]{entered.set_value();release_future.wait();return true;});});entered.get_future().wait();
 int16_t pcm[512]{};assert(audio.FeedC3PcmSpan(pcm,512,token));assert(audio.FeedC3PcmSpan(pcm,512,token));assert(audio.FeedC3PcmSpan(pcm,512,token));
 auto fault=std::async(std::launch::async,[&]{return audio.FeedC3PcmSpan(pcm,512,token);});assert(fault.wait_for(std::chrono::milliseconds(100))==std::future_status::ready);assert(!fault.get());
 assert(!audio.button_uplink_.Allows(token));release.set_value();assert(send.get());assert(audio.FinishC3PcmFault());assert(!codec.fault&&audio.c3_pcm_.input.occupied()==0&&audio.c3_pcm_.input_faults==1&&audio.c3_pcm_.media_faults==1&&audio.c3_pcm_.reference_faults==0);
 assert(audio.OpenC3Uplink(audio.button_uplink_.Revision()));const auto newer=audio.button_uplink_.Capture();
 assert(newer>token&&audio.FeedC3PcmSpan(pcm,512,newer));audio.RequestC3PcmFault(token);assert(!audio.FinishC3PcmFault()&&audio.button_uplink_.Allows(newer));
 codec.fault=true;audio.RequestC3PcmFault(newer);assert(audio.FinishC3PcmFault()&&codec.fault&&audio.c3_pcm_.media_faults==1&&audio.c3_pcm_.reference_faults==1);
 assert(!audio.OpenC3Uplink(audio.button_uplink_.Revision()));
}
static void Mapping(){
 AudioCodec codec;AudioService audio(codec);assert(audio.InitializeC3CuePack());assert(maps==1&&unmaps==0&&audio.c3_pcm_.pack.count()==21);
 int16_t frame[1440];C3PcmCuePack::Descriptor d{0,0};
 const auto* entry=pack_bytes.data()+64;uint32_t samples;std::memcpy(&samples,entry+36,4);d.samples=samples;
 assert(audio.c3_pcm_.pack.CopyFrame(d,0,frame,1440)==C3PcmCuePack::CopyResult::Ok);
 AudioService broken(codec);pack_bytes.back()^=1;assert(!broken.InitializeC3CuePack());assert(unmaps==1);pack_bytes.back()^=1;
 partition.address=0x610000;AudioService wrong(codec);assert(!wrong.InitializeC3CuePack());assert(maps==2);
}
static void OutputReset(){
 AudioCodec codec;AudioService audio(codec);audio.StartC3Pcm();assert(!audio.service_stopped_);assert(audio.button_uplink_.Open(0,[]{}));
 uint8_t frame[2880]{};std::promise<void> entered,release;auto wait=release.get_future().share();std::atomic<bool> first{true};
 codec.output_hook=[&]{if(first.exchange(false)){entered.set_value();wait.wait();}};
 assert(audio.PushC3PcmOutput(frame,2880,{1,1,0,1}));entered.get_future().wait();
 for(unsigned i=1;i<5;++i)assert(audio.PushC3PcmOutput(frame,2880,{1,1,i*1440,1}));
 {std::lock_guard<std::mutex> l(audio.audio_queue_mutex_);assert(audio.c3_pcm_.output.occupied()==5);audio.ResetC3PcmOutputLocked();assert(audio.c3_pcm_.output.occupied()==1);}
 assert(codec.releases==4);for(unsigned i=0;i<4;++i)assert(audio.PushC3PcmOutput(frame,2880,{1,2,i*1440,1}));
 assert(!audio.PushC3PcmOutput(frame,2880,{1,2,5760,1}));assert(!audio.button_uplink_.Capture());release.set_value();
 Eventually([&]{std::lock_guard<std::mutex> l(audio.audio_queue_mutex_);return audio.c3_pcm_.output.occupied()==0;});audio.StopC3Pcm();JoinTasks();assert(codec.releases==10);
}
static void Cues(const std::string& source){
 AudioCodec codec;AudioService audio(codec);assert(audio.InitializeC3CuePack());audio.StartC3Pcm();codec.enabled=false;audio.SetC3ServerReceiving(true);
 const auto cue1=Read(source+"/main/assets/common/popup.ogg"),cue2=Read(source+"/main/assets/common/vibration.ogg");
 audio.PlayC3PcmCue({reinterpret_cast<const char*>(cue1.data()),cue1.size()});audio.PlayC3PcmCue({reinterpret_cast<const char*>(cue2.data()),cue2.size()});
 {std::lock_guard<std::mutex> l(audio.audio_queue_mutex_);assert(audio.c3_pcm_.cue_count==2&&!audio.C3PcmPlaybackDrainedLocked());}
 std::this_thread::sleep_for(std::chrono::milliseconds(25));assert(codec.writes==0);codec.busy=true;audio.SetC3ServerReceiving(false);
 std::this_thread::sleep_for(std::chrono::milliseconds(25));assert(codec.writes==0);codec.busy=false;audio.audio_queue_cv_.notify_all();
 Eventually([&]{std::lock_guard<std::mutex> l(audio.audio_queue_mutex_);return audio.C3PcmPlaybackDrainedLocked();});assert(codec.writes>2);
 // Verify every output frame against CopyFrame, including descriptor ordering.
 C3PcmCuePack::Descriptor descriptors[2];uint8_t digest[32];C3PcmSha256(cue1.data(),cue1.size(),digest);assert(audio.c3_pcm_.pack.Find(digest,descriptors[0]));
 C3PcmSha256(cue2.data(),cue2.size(),digest);assert(audio.c3_pcm_.pack.Find(digest,descriptors[1]));
 size_t index=0;int16_t expected[1440];for(auto descriptor:descriptors)for(uint32_t frame=0;frame<descriptor.samples/1440;++frame){
  assert(audio.c3_pcm_.pack.CopyFrame(descriptor,frame,expected,1440)==C3PcmCuePack::CopyResult::Ok);assert(codec.recorded[index++]==std::vector<int16_t>(expected,expected+1440));}
 assert(index==codec.recorded.size());
 const auto generation=codec.generation.load();audio.c3_pcm_.cue_hardware=false;codec.busy=true;audio.SetC3ServerReceiving(true);assert(codec.generation==generation);codec.busy=false;
 audio.SetC3ServerReceiving(false);std::promise<void> entered,release;auto wait=release.get_future().share();std::atomic<bool> first{true};
 codec.output_hook=[&]{if(first.exchange(false)){entered.set_value();wait.wait();}};audio.PlayC3PcmCue({reinterpret_cast<const char*>(cue1.data()),cue1.size()});entered.get_future().wait();
 audio.SetC3ServerReceiving(true);assert(audio.c3_pcm_.cue_count==0&&codec.generation>generation);release.set_value();
 Eventually([&]{std::lock_guard<std::mutex> l(audio.audio_queue_mutex_);return !audio.output_in_flight_;});audio.SetC3ServerReceiving(false);
 std::this_thread::sleep_for(std::chrono::milliseconds(25));assert(codec.recorded.size()==index);
 // Stage cancellation while OUT power-up is in progress, before the first ticket.
 codec.output_hook={};codec.enabled=false;std::promise<void> powering,power_release;auto power_wait=power_release.get_future().share();
 codec.enable_hook=[&]{powering.set_value();power_wait.wait();};
 audio.PlayC3PcmCue({reinterpret_cast<const char*>(cue1.data()),cue1.size()});powering.get_future().wait();
 audio.SetC3ServerReceiving(true);uint8_t server[2880]{};server[0]=42;
 assert(audio.PushC3PcmOutput(server,sizeof(server),{1,9,0,codec.PlaybackGeneration()}));power_release.set_value();
 Eventually([&]{std::lock_guard<std::mutex> l(audio.audio_queue_mutex_);return !audio.output_in_flight_&&!audio.c3_pcm_.output.occupied();});
 audio.SetC3ServerReceiving(false);std::this_thread::sleep_for(std::chrono::milliseconds(25));
 assert(codec.recorded.size()==index+1&&codec.recorded.back()[0]==42);audio.StopC3Pcm();JoinTasks();
}
static void Lifecycle(){
 AudioCodec codec;AudioService audio(codec);audio.StartC3Pcm();auto* input=audio.audio_input_task_handle_;auto* output=audio.audio_output_task_handle_;
 assert(input&&output);audio.StartC3Pcm();assert(create_calls==2);audio.StopC3Pcm();JoinTasks();assert(!audio.audio_input_task_handle_&&!audio.audio_output_task_handle_);
 audio.StartC3Pcm();assert(create_calls==4);audio.StopC3Pcm();JoinTasks();
 fail_create=create_calls+2;AudioService failure(codec);failure.StartC3Pcm();assert(failure.service_stopped_&&failure.c3_lifecycle_failed_&&!failure.audio_input_task_handle_&&!failure.audio_output_task_handle_);JoinTasks();
 const auto count=create_calls;failure.StartC3Pcm();assert(create_calls==count);
}
static void StopBarrier(){
 AudioCodec codec;AudioService audio(codec);audio.StartC3Pcm();std::promise<void> entered,release;auto wait=release.get_future().share();codec.output_hook=[&]{entered.set_value();wait.wait();};
 uint8_t frame[2880]{};audio.PushC3PcmOutput(frame,2880,{1,1,0,1});entered.get_future().wait();
 auto stopping=std::async(std::launch::async,[&]{audio.StopC3Pcm();});assert(stopping.wait_for(std::chrono::milliseconds(30))==std::future_status::timeout);
 release.set_value();stopping.get();JoinTasks();assert(!audio.c3_lifecycle_failed_&&audio.service_stopped_);
}
static void Timeout(){
 AudioCodec codec;AudioService audio(codec);audio.StartC3Pcm();std::promise<void> entered,release;auto wait=release.get_future().share();codec.output_hook=[&]{entered.set_value();wait.wait();};
 uint8_t frame[2880]{};audio.PushC3PcmOutput(frame,2880,{1,1,0,1});entered.get_future().wait();force_wait_timeout=true;audio.StopC3Pcm();assert(audio.c3_lifecycle_failed_&&codec.fault);
 auto count=create_calls;audio.StartC3Pcm();assert(create_calls==count);force_wait_timeout=false;release.set_value();JoinTasks();
}
static void Resources(){
 AudioCodec codec;AudioService audio(codec);audio.audio_engine_.reset();audio.PrintC3Resources();assert(resource_task_reads==0&&heap_reads==3&&resource_logs==3);audio.audio_engine_=std::make_unique<LiteAudioEngine>();
 create_observer=[&]{auto before=resource_task_reads;audio.PrintC3Resources();assert(resource_task_reads==before);};
 audio.StartC3Pcm();create_observer={};audio.PrintC3Resources();assert(resource_task_reads==2);
 audio.StopC3Pcm();JoinTasks();audio.audio_input_task_handle_=reinterpret_cast<Task*>(uintptr_t(0x10));audio.audio_output_task_handle_=reinterpret_cast<Task*>(uintptr_t(0x20));
 audio.PrintC3Resources();assert(resource_task_reads==2&&!scheduler_depth);
}
int main(int argc,char** argv){assert(argc==4);pack_bytes=Read(argv[2]);const std::string test=argv[1];
 if(test=="resources")Resources();else if(test=="input")Input();else if(test=="busy-full")BusyFull();else if(test=="mapping")Mapping();else if(test=="output-reset")OutputReset();
 else if(test=="cues")Cues(argv[3]);else if(test=="lifecycle")Lifecycle();else if(test=="barrier")StopBarrier();else if(test=="timeout")Timeout();else assert(false);
}
