#include "playback_progress.h"
#include <cassert>
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
    uint32_t PlaybackGeneration(){return generation;}
    bool PlaybackSupported(){return true;}
};
static AudioCodec codec;
enum class PowerSaveLevel {LOW_POWER,PERFORMANCE};
struct TestDisplay {void SetChatMessage(const char*,const char*){}};
class Board {public:PowerSaveLevel power=PowerSaveLevel::PERFORMANCE;TestDisplay display;
    static Board& GetInstance(){static Board b;return b;}AudioCodec* GetAudioCodec(){return &codec;}
    void SetPowerSaveLevel(PowerSaveLevel p){power=p;}TestDisplay* GetDisplay(){return &display;}};
class AudioService {public:bool preserve=false;int resets=0;void PreserveServerPlayback(bool b){preserve=b;}void ResetDecoder(){++resets;codec.PlaybackControl(AudioCodec::PlaybackOp::Reset);}};
enum AbortReason {kAbortReasonNone};
class Protocol {public:virtual ~Protocol()=default;void SendAbortSpeaking(AbortReason) {}
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
    bool playback_negotiated_=false,playback_receiving_=false,playback_tracking_=false;
    std::string session_id_;
    std::function<void(std::unique_ptr<AudioStreamPacket>)> on_incoming_audio_;
    int server_sample_rate_=24000,server_frame_duration_=60,event_group_handle_=0;
    void ParseServerHello(const cJSON*,uint32_t);
    int sent=0;std::string last_text;void SendText(const std::string& s){++sent;last_text=s;}
    std::function<void(uint32_t)> on_audio_channel_closed_;
    void NotifyPlaybackClosed(uint32_t);uint32_t AudioChannelEpoch()const override;
    bool ResetPlaybackConnection(uint32_t expected=0,uint32_t* closed_epoch=nullptr);bool PlaybackNegotiated()const;
    PlaybackEvent PlaybackEventIdentity(const cJSON*)const;bool PlaybackEventCurrent(PlaybackEvent)const override;
    bool HandlePlaybackTts(cJSON*,uint32_t);void DeliverAudio(std::unique_ptr<AudioStreamPacket>,uint32_t);
    void SendPlayback(const PlaybackSnapshot&);void SendAbortSpeaking(AbortReason);
};
#define CONFIG_STOPWATCH_SOFTWARE_AEC 1
#define ESP_LOGE(...) (void)0
#define WEBSOCKET_PROTOCOL_SERVER_HELLO_EVENT 1
static void xEventGroupSetBits(int,int) {}
#include "playback_websocket.inc"
#include "playback_application.inc"
static std::unique_ptr<cJSON> tts(const char* state,double id=1,const char* session="s"){
    auto r=std::unique_ptr<cJSON>(cJSON_CreateObject());
    cJSON_AddStringToObject(r.get(),"state",state);cJSON_AddStringToObject(r.get(),"session_id",session);cJSON_AddNumberToObject(r.get(),"playback_id",id);return r;
}
int main(){
    WebsocketProtocol ws;ws.ResetPlaybackConnection();
    auto handle=[&](cJSON* r){return ws.HandlePlaybackTts(r,ws.playback_connection_);};
    // No mutual negotiation: retain ordinary JSON behavior and suppress receipts.
    assert(handle(tts("start").get()));
    ws.SendPlayback({{1,1},0,true,false,false});assert(ws.sent==0);
    auto hello=std::unique_ptr<cJSON>(cJSON_CreateObject());
    cJSON_AddStringToObject(hello.get(),"transport","websocket");cJSON_AddStringToObject(hello.get(),"session_id","s");
    ws.ParseServerHello(hello.get(),ws.playback_connection_);assert(!ws.PlaybackNegotiated());
    auto features=std::unique_ptr<cJSON>(cJSON_CreateObject());
    cJSON_AddBoolToObject(features.get(),"playback_progress_v1",true);hello->children["features"]=std::move(features);
    ws.ParseServerHello(hello.get(),ws.playback_connection_);assert(ws.PlaybackNegotiated());
    assert(Application::GetInstance().audio_service_.preserve);
    std::vector<AudioStreamPacket> frames;
    ws.on_incoming_audio_=[&](std::unique_ptr<AudioStreamPacket> p){frames.push_back(*p);};
    assert(!handle(tts("start",1.5).get()));
    assert(!handle(tts("start",1,"old").get()));
    auto first_start=tts("start"); assert(handle(first_start.get()));
    const auto first_event=ws.PlaybackEventIdentity(first_start.get());const auto first=first_event.identity;const auto token=codec.generation;
    ws.DeliverAudio(std::make_unique<AudioStreamPacket>(),ws.playback_connection_);
    ws.DeliverAudio(std::make_unique<AudioStreamPacket>(),ws.playback_connection_);
    assert(frames.size()==2 && frames[0].receipt_id==first && frames[1].receipt_first==1440);
    assert(frames[0].output_generation==token); // before UI transition or dequeue
    assert(handle(tts("stop").get()));
    assert(!codec.core.Snapshot(first).finished); // decoder work still admitted
    assert(handle(tts("start",2).get()));
    assert(codec.generation==token); // normal consecutive blocks retain old tail
    assert(!ws.PlaybackEventCurrent(first_event));
    auto interrupt=tts("stop",2);cJSON_AddBoolToObject(interrupt.get(),"interrupted",true);
    assert(handle(interrupt.get()));assert(codec.generation!=token);
    assert(handle(tts("start",3).get()));
    ws.DeliverAudio(std::make_unique<AudioStreamPacket>(),ws.playback_connection_);
    assert(frames.back().receipt_id.playback==3);
    ws.SendPlayback({first,240,true,true,true});assert(ws.sent==1 && ws.last_text.find("\"playback_id\":1")!=std::string::npos && ws.last_text.find("\"played_samples\":240")!=std::string::npos && ws.last_text.find("\"state\":\"finished\"")!=std::string::npos && ws.last_text.find("\"valid\":true")!=std::string::npos);
    ws.SendAbortSpeaking(kAbortReasonNone);size_t count=frames.size();
    ws.DeliverAudio(std::make_unique<AudioStreamPacket>(),ws.playback_connection_);assert(frames.size()==count);
    ws.ResetPlaybackConnection();
    ws.playback_negotiated_=true;ws.session_id_="new";
    assert(!ws.HandlePlaybackTts(tts("start",99,"new").get(),first.connection));
    ws.DeliverAudio(std::make_unique<AudioStreamPacket>(),first.connection);
    assert(frames.size()==count);
    assert(!ws.ResetPlaybackConnection(first.connection));
    assert(ws.PlaybackEventIdentity(first_start.get()).identity==first); // scheduled event never relabelled
    ws.SendPlayback({first,480,true,true,true});assert(ws.sent==1);
    // Bounded overflow keeps ordinary audio but returns no new confirmed record.
    for(uint32_t i=1;i<=16;++i) assert(codec.core.Start({ws.playback_connection_,i}));
    assert(handle(tts("start",17,"new").get()));
    ws.DeliverAudio(std::make_unique<AudioStreamPacket>(),ws.playback_connection_);
    assert(!frames.back().receipt_id && frames.back().receipt_id.connection==ws.playback_connection_);
    ws.session_id_="quote\"slash\\";
    ws.SendPlayback({{ws.playback_connection_,UINT32_MAX},PlaybackProgress::kMaxSamples,true,true,false});
    assert(ws.last_text.find("quote\\\"slash\\\\")!=std::string::npos && ws.last_text.size()<512);
    assert(ws.last_text.find("4294967295")!=std::string::npos);
    assert(ws.last_text.find("9007199254740991")!=std::string::npos);
    ws.session_id_="new";auto pending=tts("start",18,"new");assert(handle(pending.get()));
    const auto pending_event=ws.PlaybackEventIdentity(pending.get());
    auto& app=Application::GetInstance();app.protocol_=&ws;app.state=kDeviceStateListening;
    app.SchedulePlaybackStart(pending_event); // actual application Schedule lambda
    app.aborted_=true;app.audio_service_.ResetDecoder();ws.SendAbortSpeaking(kAbortReasonNone);
    app.RunScheduled();assert(app.state==kDeviceStateListening && app.aborted_);
    auto stop=tts("stop",18,"new");assert(handle(stop.get()));
    app.state=kDeviceStateSpeaking;app.SchedulePlaybackStop(ws.PlaybackEventIdentity(stop.get()),false);
    app.RunScheduled();assert(app.state==kDeviceStateListening); // stop is not receiving-gated
    auto start19=tts("start",19,"new");assert(handle(start19.get()));
    app.SchedulePlaybackStart(ws.PlaybackEventIdentity(start19.get()));
    auto stop19=tts("stop",19,"new");assert(handle(stop19.get()));
    app.SchedulePlaybackStop(ws.PlaybackEventIdentity(stop19.get()),false);
    app.RunScheduled();assert(app.state==kDeviceStateListening); // normal sealed sequence remains valid
    for(bool negotiated:{false,true})for(bool deferred:{false,true}) {
        WebsocketProtocol closed;closed.ResetPlaybackConnection();closed.playback_negotiated_=negotiated;
        app.protocol_=&closed;app.state=kDeviceStateListening;Board::GetInstance().power=PowerSaveLevel::PERFORMANCE;
        int notifications=0;closed.on_audio_channel_closed_=[&](uint32_t epoch){++notifications;app.ScheduleAudioChannelClosed(epoch);};
        const auto old_connection=closed.socket_connection_=closed.AudioChannelEpoch();
        std::function<void()> callback=[&]{closed.NotifyPlaybackClosed(old_connection);};
        closed.websocket_=std::make_unique<TestSocket>();
        if(!deferred)closed.websocket_->disconnect=callback;
        closed.CloseAudioChannel();
        if (deferred) {
            callback();
        }
        callback();
        closed.CloseAudioChannel();
        app.RunScheduled();assert(notifications==1 && app.state==kDeviceStateIdle);
        assert(Board::GetInstance().power==PowerSaveLevel::LOW_POWER);
        // Remote callback first, then explicit close: exactly one notification.
        closed.ResetPlaybackConnection();closed.socket_connection_=closed.AudioChannelEpoch();
        const auto remote=closed.socket_connection_;closed.websocket_=std::make_unique<TestSocket>();
        closed.NotifyPlaybackClosed(remote);closed.CloseAudioChannel();assert(notifications==2);
        // Reopen before the old queued UI close: preserve new UI and power.
        closed.ResetPlaybackConnection();app.state=kDeviceStateSpeaking;
        Board::GetInstance().power=PowerSaveLevel::PERFORMANCE;app.RunScheduled();
        assert(app.state==kDeviceStateSpeaking && Board::GetInstance().power==PowerSaveLevel::PERFORMANCE);
        closed.NotifyPlaybackClosed(remote);assert(notifications==2);
    }
    WebsocketProtocol exhausted;exhausted.playback_connection_=UINT32_MAX-1;
    assert(!exhausted.ResetPlaybackConnection()); // reserve final epoch for the last close
    int exhausted_notifications=0;
    exhausted.on_audio_channel_closed_=[&](uint32_t){++exhausted_notifications;};
    exhausted.NotifyPlaybackClosed(UINT32_MAX-1);exhausted.NotifyPlaybackClosed(UINT32_MAX-1);
    assert(exhausted_notifications==1&&!exhausted.ResetPlaybackConnection());
    puts("production WebSocket identity/stop/reconnect/fallback integration passed");
}
