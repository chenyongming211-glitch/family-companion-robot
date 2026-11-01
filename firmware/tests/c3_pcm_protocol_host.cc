#include "playback_progress.h"
#include "c3_pcm_buffer.h"
#include "button_uplink.h"
#include <cassert>
#include <atomic>
#include <cmath>
#include <cstring>
#include <cstdlib>
#include <functional>
#include <map>
#include <memory>
#include <mutex>
#include <string>
#include <vector>
using namespace family_robot;
// cJSON/transport are host test doubles. Protocol methods themselves are the
// exact production include; hardware state uses the real PlaybackProgress core.
struct cJSON {
    std::map<std::string,std::unique_ptr<cJSON>> children;
    std::string string;
    const char* valuestring=nullptr;
    double valuedouble=0;int valueint=0;
    enum {Object,String,Number,Bool} kind=Object;
    bool boolean=false;
};
static cJSON* cJSON_CreateObject(){return new cJSON;}
static cJSON* cJSON_GetObjectItem(const cJSON* n,const char* key) {
    if (!n) {
        return nullptr;
    }
    auto it = n->children.find(key);
    return it == n->children.end() ? nullptr : it->second.get();
}
static bool cJSON_IsObject(const cJSON* n){return n&&n->kind==cJSON::Object;}
static bool cJSON_IsString(const cJSON* n){return n&&n->kind==cJSON::String;}
static bool cJSON_IsNumber(const cJSON* n){return n&&n->kind==cJSON::Number;}
static bool cJSON_IsTrue(const cJSON* n){return n&&n->kind==cJSON::Bool&&n->boolean;}
static void cJSON_AddStringToObject(cJSON* n,const char* k,const char* v){
    auto x=std::make_unique<cJSON>();x->kind=cJSON::String;x->string=v;x->valuestring=x->string.c_str();n->children[k]=std::move(x);
}
static void cJSON_AddNumberToObject(cJSON* n,const char* k,double v){
    auto x=std::make_unique<cJSON>();x->kind=cJSON::Number;x->valuedouble=v;x->valueint=static_cast<int>(v);n->children[k]=std::move(x);
}
static void cJSON_AddBoolToObject(cJSON* n,const char* k,bool v){
    auto x=std::make_unique<cJSON>();x->kind=cJSON::Bool;x->boolean=v;n->children[k]=std::move(x);
}
static void cJSON_DeleteItemFromObject(cJSON* n,const char* k){n->children.erase(k);}
class AudioCodec {
public:
    enum class PlaybackOp {Connect,Start,Admit,Release,Seal,Interrupt,Invalidate,Reset};
    PlaybackProgress core;uint32_t generation=0;
    bool PlaybackControl(PlaybackOp op,PlaybackIdentity id={},uint64_t first=0,size_t n=0){
        switch(op){
            case PlaybackOp::Connect:core.Connect(id.connection);break;
            case PlaybackOp::Start:return core.Start(id);
            case PlaybackOp::Admit:return core.Admit(id,first,n);
            case PlaybackOp::Release:core.Release(id);break;
            case PlaybackOp::Seal:core.Seal(id,false);break;
            case PlaybackOp::Interrupt:core.Seal(id,true);break;
            case PlaybackOp::Invalidate:core.Invalidate(id);break;
            case PlaybackOp::Reset:core.InterruptAll();++generation;break;
        }return true;
    }
#if CONFIG_XIAOMAN_ORB_UI
    void MarkOrbDraining(PlaybackIdentity id) {core.MarkOrbDraining(id);}
#endif
    uint32_t PlaybackGeneration(){return generation;}
    bool PlaybackSupported(){return true;}
};
static AudioCodec codec;
enum class PowerSaveLevel {LOW_POWER,PERFORMANCE};
struct TestDisplay {void SetChatMessage(const char*,const char*){}};
class Board {public:PowerSaveLevel power=PowerSaveLevel::PERFORMANCE;TestDisplay display;
    static Board& GetInstance(){static Board b;return b;}AudioCodec* GetAudioCodec(){return &codec;}
    void SetPowerSaveLevel(PowerSaveLevel p){power=p;}TestDisplay* GetDisplay(){return &display;}};
class AudioService {public:
 void WakeC3PcmOwner(){}
 family_robot::ButtonUplink gate;family_robot::C3PcmOutputPool output;bool receiving=false;unsigned faults=0;
 family_robot::ButtonUplink& C3Uplink(){return gate;}
 void RequestC3PcmFault(uint32_t token){gate.Revoke(token);++faults;}
 void SetC3ServerReceiving(bool b){receiving=b;}
 bool PushC3PcmOutput(const uint8_t* p,size_t n,family_robot::C3PcmOutputMetadata m){return output.Push(p,n,m)==family_robot::C3PcmResult::Ok;}
bool preserve=false,uplink=true;int resets=0;void SetExperimentUplink(bool b){uplink=b;}void PreserveServerPlayback(bool b){preserve=b;}void ResetDecoder(){++resets;codec.PlaybackControl(AudioCodec::PlaybackOp::Reset);}};
enum AbortReason {kAbortReasonNone};
class Protocol {public:virtual ~Protocol()=default;virtual void SendAbortSpeaking(AbortReason) {}
    virtual bool PlaybackNegotiated()const{return false;}
    virtual bool PlaybackDrainNegotiated()const{return false;}
    virtual uint32_t AudioChannelEpoch()const{return 0;}
    virtual bool PlaybackEventCurrent(PlaybackEvent)const{return true;}};
enum {kDeviceStateIdle,kDeviceStateSpeaking,kDeviceStateListening,kListeningModeManualStop,kListeningModeAutoStop};
class Application {public:AudioService audio_service_;Protocol* protocol_=nullptr;
    bool aborted_=false;int state=kDeviceStateListening,listening_mode_=kListeningModeAutoStop;
    std::vector<std::function<void()>> scheduled;
    static Application& GetInstance(){static Application a;return a;}
    AudioService& GetAudioService(){return audio_service_;}
    void Schedule(std::function<void()> fn){scheduled.push_back(std::move(fn));}
    void RunScheduled(){auto work=std::move(scheduled);scheduled.clear();for(auto& fn:work)fn();}
    int GetDeviceState(){return state;}void SetDeviceState(int s){state=s;}
    void ScheduleAudioChannelClosed(uint32_t);
    void SchedulePlaybackStart(PlaybackEvent);void SchedulePlaybackStop(PlaybackEvent,bool);
    void AbortSpeaking(AbortReason);
};
struct AudioStreamPacket {int sample_rate=24000,frame_duration=60;PlaybackIdentity receipt_id{};uint32_t receipt_first=0,output_generation=0;};

struct TestSocket {std::function<void()> disconnect;~TestSocket(){if(disconnect)disconnect();}};
class WebsocketProtocol:public Protocol {
public:
    std::unique_ptr<TestSocket> websocket_;
    void CloseAudioChannel(bool send_goodbye=true);
    mutable std::mutex playback_mutex_;
    uint32_t playback_connection_=0,socket_connection_=0,playback_event_lease_=0;
    PlaybackIdentity playback_id_{};
    uint64_t playback_first_=0;
    bool playback_negotiated_=false,playback_receiving_=false,playback_tracking_=false,playback_drain_negotiated_=false;
    std::string session_id_;
    std::function<void(std::unique_ptr<AudioStreamPacket>)> on_incoming_audio_;
    int server_sample_rate_=24000,server_frame_duration_=60,event_group_handle_=0;
    void DeliverC3PcmAudio(const uint8_t*,size_t,uint32_t);
    std::atomic<uint32_t> c3_current_connection_{0},c3_pending_connection_{0};
    void RequestC3PcmDisconnect(uint32_t);void PollC3PcmDisconnect();
    void ParseServerHello(const cJSON*,uint32_t);
    int sent=0;std::string last_text;void SendText(const std::string& s){++sent;last_text=s;}
    std::function<void(uint32_t)> on_audio_channel_closed_;
    void NotifyPlaybackClosed(uint32_t);uint32_t AudioChannelEpoch()const override;
    bool ResetPlaybackConnection(uint32_t expected=0,uint32_t* closed_epoch=nullptr);bool PlaybackNegotiated()const;
#if CONFIG_XMINI_C3_BUTTON_ENTRY
    bool PlaybackAllowsListening()const;
#endif
    bool PlaybackDrainNegotiated()const override {std::lock_guard<std::mutex> lock(playback_mutex_);return playback_drain_negotiated_;}
    PlaybackEvent PlaybackEventIdentity(const cJSON*)const;bool PlaybackEventCurrent(PlaybackEvent)const override;
    bool HandlePlaybackTts(cJSON*,uint32_t);void DeliverAudio(std::unique_ptr<AudioStreamPacket>,uint32_t);
    void SendPlayback(const PlaybackSnapshot&);void SendAbortSpeaking(AbortReason);
};
#define CONFIG_STOPWATCH_SOFTWARE_AEC 1
#define ESP_LOGE(...) (void)0
#define ESP_LOGI(...) (void)0
#define WEBSOCKET_PROTOCOL_SERVER_HELLO_EVENT 1
static void xEventGroupSetBits(int,int) {}
#include "playback_websocket.inc"
#include "playback_application.inc"
#include "c3_pcm_protocol.inc"
static std::unique_ptr<cJSON> tts(const char* state,double id=1,const char* session="s"){
    auto r=std::unique_ptr<cJSON>(cJSON_CreateObject());
    cJSON_AddStringToObject(r.get(),"state",state);cJSON_AddStringToObject(r.get(),"session_id",session);cJSON_AddNumberToObject(r.get(),"playback_id",id);return r;
}

static std::unique_ptr<cJSON> Hello(){
 auto hello=std::unique_ptr<cJSON>(cJSON_CreateObject());cJSON_AddStringToObject(hello.get(),"transport","websocket");
 cJSON_AddStringToObject(hello.get(),"session_id","s");cJSON_AddNumberToObject(hello.get(),"version",1);
 auto params=std::unique_ptr<cJSON>(cJSON_CreateObject());cJSON_AddStringToObject(params.get(),"format","pcm_s16le");
 cJSON_AddNumberToObject(params.get(),"sample_rate",24000);cJSON_AddNumberToObject(params.get(),"channels",1);cJSON_AddNumberToObject(params.get(),"frame_duration",60);hello->children["audio_params"]=std::move(params);
 auto features=std::unique_ptr<cJSON>(cJSON_CreateObject());for(const char* key:{"pcm_s16le_v1","playback_progress_v1","playback_drain_v1"})cJSON_AddBoolToObject(features.get(),key,true);hello->children["features"]=std::move(features);return hello;
}
int main(){
 auto& audio=Application::GetInstance().audio_service_;
 for(unsigned test=0;test<9;++test){WebsocketProtocol ws;ws.ResetPlaybackConnection();auto hello=Hello();
  auto* params=cJSON_GetObjectItem(hello.get(),"audio_params");auto* features=cJSON_GetObjectItem(hello.get(),"features");
  switch(test){case 0:cJSON_AddNumberToObject(hello.get(),"version",2);break;case 1:cJSON_AddStringToObject(params,"format","opus");break;
   case 2:cJSON_AddNumberToObject(params,"sample_rate",16000);break;case 3:cJSON_AddNumberToObject(params,"channels",2);break;
   case 4:cJSON_AddNumberToObject(params,"frame_duration",60.5);break;case 5:cJSON_AddBoolToObject(features,"pcm_s16le_v1",false);break;
   case 6:cJSON_AddBoolToObject(features,"playback_progress_v1",false);break;case 7:cJSON_AddBoolToObject(features,"playback_drain_v1",false);break;default:break;}
  ws.ParseServerHello(hello.get(),ws.playback_connection_);assert(ws.PlaybackNegotiated()==(test==8));
 }
 WebsocketProtocol ws;ws.ResetPlaybackConnection();auto hello=Hello();ws.ParseServerHello(hello.get(),ws.playback_connection_);assert(ws.PlaybackNegotiated());
 auto start=tts("start");assert(ws.HandlePlaybackTts(start.get(),ws.playback_connection_)&&audio.receiving);
 assert(audio.gate.Open(0,[]{}));uint8_t frame[2880]{};frame[0]=0x23;frame[1]=0x81;
 const auto faults=audio.faults;ws.DeliverC3PcmAudio(frame,2879,ws.playback_connection_);assert(audio.faults==faults+1&&audio.output.occupied()==0&&ws.playback_first_==0);
 ws.DeliverC3PcmAudio(frame,2880,ws.playback_connection_-1);assert(audio.output.occupied()==0);
 ws.DeliverC3PcmAudio(frame,2880,ws.playback_connection_);assert(audio.output.occupied()==1&&ws.playback_first_==1440);
 family_robot::C3PcmOutputPool::Lease lease;assert(audio.output.Acquire(lease)==family_robot::C3PcmResult::Ok);
 assert(lease.samples[0]==-32477&&lease.metadata.connection==ws.playback_connection_&&lease.metadata.playback==1&&lease.metadata.first==0&&lease.metadata.output_generation==codec.generation);
 audio.output.Release(lease);auto stop=tts("stop");cJSON_AddNumberToObject(stop.get(),"sent_samples",1440);assert(ws.HandlePlaybackTts(stop.get(),ws.playback_connection_)&&!audio.receiving);
 auto next=tts("start",2);const auto resets=audio.resets;assert(ws.HandlePlaybackTts(next.get(),ws.playback_connection_)&&audio.resets==resets&&audio.receiving);
}
