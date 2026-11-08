#include "playback_progress.h"
#if CONFIG_CHICK_FACE_UI
#include "playback_expression_event.h"
#include "chick_face_model.h"
#endif
#include <cassert>
#include <cmath>
#include <cstring>
#include <cstdlib>
#include <functional>
#include <algorithm>
#include <memory>
#include <mutex>
#include <string>
#include <vector>
#include <atomic>
#include <thread>
using namespace family_robot;
// cJSON/transport are host test doubles. Protocol methods themselves are the
// exact production include; hardware state uses the real PlaybackProgress core.
struct cJSON {
    std::vector<std::pair<std::string,std::unique_ptr<cJSON>>> children;
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
    // Real cJSON preserves ordered duplicate keys and returns the first one.
    for(const auto& child:n->children)if(child.first==key)return child.second.get();
    return nullptr;
}
static bool cJSON_IsObject(const cJSON* n){return n&&n->kind==cJSON::Object;}
static bool cJSON_IsString(const cJSON* n){return n&&n->kind==cJSON::String;}
static bool cJSON_IsNumber(const cJSON* n){return n&&n->kind==cJSON::Number;}
static bool cJSON_IsTrue(const cJSON* n){return n&&n->kind==cJSON::Bool&&n->boolean;}
static void cJSON_AddStringToObject(cJSON* n,const char* k,const char* v){
    auto x=std::make_unique<cJSON>();x->kind=cJSON::String;x->string=v;x->valuestring=x->string.c_str();n->children.emplace_back(k,std::move(x));
}
static void cJSON_AddNumberToObject(cJSON* n,const char* k,double v){
    auto x=std::make_unique<cJSON>();x->kind=cJSON::Number;x->valuedouble=v;x->valueint=static_cast<int>(v);n->children.emplace_back(k,std::move(x));
}
static void cJSON_AddBoolToObject(cJSON* n,const char* k,bool v){
    auto x=std::make_unique<cJSON>();x->kind=cJSON::Bool;x->boolean=v;n->children.emplace_back(k,std::move(x));
}
static void cJSON_DeleteItemFromObject(cJSON* n,const char* k){
    const auto it=std::find_if(n->children.begin(),n->children.end(),[&](const auto& child){return child.first==k;});
    if(it!=n->children.end())n->children.erase(it); // cJSON deletes only the first match
}
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
#if CONFIG_CHICK_FACE_UI
    void MarkOrbDraining(PlaybackIdentity id) {core.MarkOrbDraining(id);}
#endif
    uint32_t PlaybackGeneration(){return generation;}
    bool PlaybackSupported(){return true;}
};
static AudioCodec codec;
enum class PowerSaveLevel {LOW_POWER,PERFORMANCE};
struct TestDisplay {
    void SetChatMessage(const char*,const char*){}
#if CONFIG_CHICK_FACE_UI
    struct Expression {uint32_t connection,playback;std::string name;int64_t arrival;};
    std::vector<Expression> expressions;std::vector<std::string> legacy_emotions;int clears=0;
    FaceModel model;
    void SetEmotion(const char* name){legacy_emotions.emplace_back(name);model.SetEmotion(name,1000000);}
    void SetLegacyEmotion(uint32_t connection,const char* name){legacy_emotions.emplace_back(name);model.SetLegacyEmotion(connection,name,1000000);}
    void SetPlaybackEmotion(uint32_t connection,uint32_t playback,const char* name,int64_t arrival) {
        expressions.push_back({connection,playback,name,arrival});
        model.SetPlaybackEmotion({connection,playback},name,arrival);
    }
    void ClearPlaybackEmotions(){++clears;model.ClearPlaybackEmotions();}
#endif
};
class Board {public:PowerSaveLevel power=PowerSaveLevel::PERFORMANCE;TestDisplay display;
    static Board& GetInstance(){static Board b;return b;}AudioCodec* GetAudioCodec(){return &codec;}
    void SetPowerSaveLevel(PowerSaveLevel p){power=p;}TestDisplay* GetDisplay(){return &display;}};
class AudioService {public:bool preserve=false,uplink=true;int resets=0;void SetExperimentUplink(bool b){uplink=b;}void PreserveServerPlayback(bool b){preserve=b;}void ResetDecoder(){++resets;codec.PlaybackControl(AudioCodec::PlaybackOp::Reset);}};
enum AbortReason {kAbortReasonNone};
class Protocol {public:virtual ~Protocol()=default;virtual void SendAbortSpeaking(AbortReason) {}
    virtual bool PlaybackNegotiated()const{return false;}
    virtual bool PlaybackDrainNegotiated()const{return false;}
    virtual uint32_t AudioChannelEpoch()const{return 0;}
    virtual bool PlaybackEventCurrent(PlaybackEvent)const{return true;}
#if CONFIG_CHICK_FACE_UI
    virtual bool PlaybackExpressionCurrent(PlaybackExpressionEvent)const{return false;}
    virtual bool FaceExpressionNegotiated()const{return false;}
    virtual bool LegacyEmotionCurrent(uint32_t)const{return false;}
#endif
};
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
#if CONFIG_CHICK_FACE_UI
    void SchedulePlaybackExpression(PlaybackExpressionEvent);
    void ScheduleLegacyEmotion(uint32_t,std::string);
    void IncomingLegacyForTest(const cJSON*);
#endif
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
    void ParseServerHello(const cJSON*,uint32_t);
    int sent=0;std::string last_text;void SendText(const std::string& s){++sent;last_text=s;}
    std::function<void(uint32_t)> on_audio_channel_closed_;
    void NotifyPlaybackClosed(uint32_t);uint32_t AudioChannelEpoch()const override;
#if CONFIG_CHICK_FACE_UI
    uint32_t FaceStateEpoch(const char*)const;
    void DispatchExpressionForTest(cJSON*,uint32_t,int64_t);
    std::function<void(const cJSON*)> on_incoming_json_;
    bool playback_expression_negotiated_=false,playback_expression_blocked_=true;
    bool FaceExpressionNegotiated()const override;
    bool LegacyEmotionCurrent(uint32_t)const override;
    bool HandlePlaybackExpression(const cJSON*,uint32_t,int64_t);
    bool PlaybackExpressionCurrent(PlaybackExpressionEvent)const override;
#endif
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
static std::unique_ptr<cJSON> tts(const char* state,double id=1,const char* session="s"){
    auto r=std::unique_ptr<cJSON>(cJSON_CreateObject());
    cJSON_AddStringToObject(r.get(),"state",state);cJSON_AddStringToObject(r.get(),"session_id",session);cJSON_AddNumberToObject(r.get(),"playback_id",id);return r;
}
#if CONFIG_CHICK_FACE_UI
// Both blocks are extracted from the prepared native production callback.
void WebsocketProtocol::DispatchExpressionForTest(cJSON* root,uint32_t connection,int64_t arrival_us) {
    auto* type=cJSON_GetObjectItem(root,"type");
    // PRODUCTION_JSON_DISPATCH
}
void Application::IncomingLegacyForTest(const cJSON* root) {
    auto* display=Board::GetInstance().GetDisplay();
    // PRODUCTION_LEGACY_EMOTION
}
static std::unique_ptr<cJSON> expression(double id=1,const char* scope="expression-scope",const char* mood="happy") {
    auto r=tts("unused",id,scope);cJSON_AddStringToObject(r.get(),"type","llm");
    cJSON_AddStringToObject(r.get(),"emotion",mood);return r;
}
static void LegacyEpochProtocol() {
    auto& app=Application::GetInstance();auto& display=Board::GetInstance().display;
    for(int action=0;action<4;++action) {
        codec.core=PlaybackProgress{};WebsocketProtocol ws;ws.ResetPlaybackConnection();app.protocol_=&ws;
        display.legacy_emotions.clear();
        ws.on_incoming_json_=[&](const cJSON* root){app.IncomingLegacyForTest(root);};
        const auto connection=ws.AudioChannelEpoch();
        auto legacy=expression(1,"expression-scope","shocked");
        cJSON_DeleteItemFromObject(legacy.get(),"playback_id");
        ws.DispatchExpressionForTest(legacy.get(),connection,100);
        legacy.reset(); // cJSON and its string die before main consumes the event
        if(action==1)ws.NotifyPlaybackClosed(connection);
        if(action==2 || action==3) {
            ws.NotifyPlaybackClosed(connection);ws.ResetPlaybackConnection();
        }
        if(action==3) {
            auto h=std::unique_ptr<cJSON>(cJSON_CreateObject());
            cJSON_AddStringToObject(h.get(),"transport","websocket");
            cJSON_AddStringToObject(h.get(),"session_id","expression-scope");
            auto caps=std::unique_ptr<cJSON>(cJSON_CreateObject());
            cJSON_AddBoolToObject(caps.get(),"playback_progress_v1",true);
            cJSON_AddBoolToObject(caps.get(),"face_expression_v1",true);
            h->children.emplace_back("features",std::move(caps));
            ws.ParseServerHello(h.get(),ws.AudioChannelEpoch());
        }
        app.RunScheduled();
        assert(display.legacy_emotions.size()==size_t(action==0));
        if(action==0)assert(display.legacy_emotions.back()=="shocked");
    }
    // Negotiation can change on this same epoch while the old event waits.
    WebsocketProtocol ws;ws.ResetPlaybackConnection();app.protocol_=&ws;display.legacy_emotions.clear();
    ws.on_incoming_json_=[&](const cJSON* root){app.IncomingLegacyForTest(root);};
    auto legacy=expression();cJSON_DeleteItemFromObject(legacy.get(),"playback_id");
    ws.DispatchExpressionForTest(legacy.get(),ws.AudioChannelEpoch(),100);
    ws.playback_expression_negotiated_=true;
    app.RunScheduled();assert(display.legacy_emotions.empty());
    app.protocol_=nullptr;
}
static void LegacyLifecycleProtocol() {
    auto& app=Application::GetInstance();auto& display=Board::GetInstance().display;
    codec.core=PlaybackProgress{};WebsocketProtocol ws;ws.ResetPlaybackConnection();app.protocol_=&ws;
    display.model=FaceModel{};display.legacy_emotions.clear();
    ws.on_incoming_json_=[&](const cJSON* root){app.IncomingLegacyForTest(root);};
    ws.on_audio_channel_closed_=[&](uint32_t epoch){app.ScheduleAudioChannelClosed(epoch);};
    auto legacy=expression(1,"scope","loving");cJSON_DeleteItemFromObject(legacy.get(),"playback_id");
    ws.DispatchExpressionForTest(legacy.get(),ws.AudioChannelEpoch(),900000);app.RunScheduled();
    FaceFacts f;f.now_us=1000000;f.audio.output.connection=ws.AudioChannelEpoch();
    assert(display.model.Update(f).emotion==FaceEmotion::Loving);
    const auto old=ws.AudioChannelEpoch();ws.NotifyPlaybackClosed(old);ws.ResetPlaybackConnection();
    app.RunScheduled(); // stale close UI is skipped after the reopen
    f.now_us+=100000;f.audio.output.connection=ws.AudioChannelEpoch();
    assert(display.model.Update(f).emotion==FaceEmotion::Neutral);
    // A new legacy event and first-observed scope retain their ordinary mood.
    ws.DispatchExpressionForTest(legacy.get(),ws.AudioChannelEpoch(),1100000);app.RunScheduled();
    assert(display.model.Update(f).emotion==FaceEmotion::Loving);
    display.ClearPlaybackEmotions();assert(display.model.Update(f).emotion==FaceEmotion::Neutral);
    // Ordinary local/system mood is not a stale network label.
    display.model.SetEmotion("sleepy",f.now_us);f.now_us+=100000;++f.audio.output.connection;
    assert(display.model.Update(f).emotion==FaceEmotion::Sleepy);
    app.protocol_=nullptr;
}
static void DuplicateEmotionProtocol() {
    auto& app=Application::GetInstance();auto& display=Board::GetInstance().display;
    // Verify our double reproduces cJSON first-get / one-delete behavior.
    auto duplicate=expression(1,"expression-scope","shocked");
    cJSON_AddStringToObject(duplicate.get(),"emotion","happy");
    assert(!std::strcmp(cJSON_GetObjectItem(duplicate.get(),"emotion")->valuestring,"shocked"));
    cJSON_DeleteItemFromObject(duplicate.get(),"emotion");
    assert(!std::strcmp(cJSON_GetObjectItem(duplicate.get(),"emotion")->valuestring,"happy"));
    for(int mode=0;mode<4;++mode) {
        codec.core=PlaybackProgress{};WebsocketProtocol ws;ws.ResetPlaybackConnection();app.protocol_=&ws;
        display.expressions.clear();display.legacy_emotions.clear();int generic=0;
        ws.on_incoming_json_=[&](const cJSON* root){++generic;app.IncomingLegacyForTest(root);};
        ws.playback_negotiated_=true;ws.session_id_="expression-scope";
        ws.playback_expression_negotiated_=mode!=0;
        assert(ws.HandlePlaybackTts(tts("start",1,"expression-scope").get(),ws.AudioChannelEpoch()));
        // no ack bound, unknown canonical, unbound negotiated, valid bound
        auto message=expression(1,"expression-scope",mode==3?"loving":"shocked");
        if(mode==2)cJSON_DeleteItemFromObject(message.get(),"playback_id");
        cJSON_AddStringToObject(message.get(),"emotion","happy");
        ws.DispatchExpressionForTest(message.get(),ws.AudioChannelEpoch(),100);
        app.RunScheduled();assert(generic==0 && display.legacy_emotions.empty());
        assert(display.expressions.size()==size_t(mode==3));
        if(mode==3)assert(display.expressions.back().name=="loving");
        auto state=std::unique_ptr<cJSON>(cJSON_CreateObject());
        cJSON_AddStringToObject(state.get(),"type","llm");cJSON_AddStringToObject(state.get(),"state","thinking");
        ws.DispatchExpressionForTest(state.get(),ws.AudioChannelEpoch(),101);
        assert(generic==1); // state-only hints retain independent generic routing
    }
    app.protocol_=nullptr;
}
static void SealedPendingProtocol() {
    auto& app=Application::GetInstance();auto& display=Board::GetInstance().display;
    for(bool cancel:{false,true}) {
        codec.core=PlaybackProgress{};WebsocketProtocol ws;ws.ResetPlaybackConnection();app.protocol_=&ws;
        ws.playback_negotiated_=ws.playback_drain_negotiated_=ws.playback_expression_negotiated_=true;
        ws.session_id_="expression-scope";display.model=FaceModel{};
        const auto connection=ws.AudioChannelEpoch();
        auto start=tts("start",1,"expression-scope");assert(ws.HandlePlaybackTts(start.get(),connection));
        const auto id=ws.PlaybackEventIdentity(start.get()).identity;
        app.SchedulePlaybackStart(ws.PlaybackEventIdentity(start.get()));app.RunScheduled();
        assert(app.state==kDeviceStateSpeaking);
        std::array<int16_t,240> pcm{};pcm.fill(2000);
        ws.on_incoming_audio_=[&](std::unique_ptr<AudioStreamPacket> packet) {
            for(size_t i=0;i<6;++i)assert(codec.core.Submit(packet->receipt_id,i*240,pcm.data(),240,180)==PlaybackProgress::SubmitResult::Accepted);
            codec.core.Release(packet->receipt_id);
        };
        ws.DeliverAudio(std::make_unique<AudioStreamPacket>(),connection); // one legal 60ms/1440-sample frame
        assert(ws.HandlePlaybackExpression(expression().get(),connection,1000000));app.RunScheduled();
        FaceFacts f;f.now_us=1000000;f.state=FaceState::Speaking;f.audio.gate_open=true;
        f.audio.output=codec.core.OrbActivity();assert(display.model.Update(f).emotion==FaceEmotion::Neutral);
        if(cancel)app.AbortSpeaking(kAbortReasonNone); // retain queued tail, reject its unseen label
        auto stop=tts("stop",1,"expression-scope");cJSON_AddNumberToObject(stop.get(),"sent_samples",1440);
        if(cancel)cJSON_AddStringToObject(stop.get(),"reason","drain");
        assert(ws.HandlePlaybackTts(stop.get(),connection));
        app.SchedulePlaybackStop(ws.PlaybackEventIdentity(stop.get()),false);app.RunScheduled();
        assert(app.state==kDeviceStateListening);
        f.state=FaceState::Listening;f.audio.output=codec.core.OrbActivity();
        assert(display.model.Update(f).emotion==FaceEmotion::Neutral);
        std::array<std::array<int16_t,240>,6> dma{};
        for(size_t i=0;i<7;++i) {
            f.now_us=1010000+static_cast<int64_t>(i)*10000;
            codec.core.Complete(dma[i%6].data(),240,f.now_us);f.audio.output=codec.core.OrbActivity();
            const auto frame=display.model.Update(f);
            if(i<6)assert(frame.state!=FaceState::Speaking && frame.emotion==FaceEmotion::Neutral);
            else assert(frame.state==FaceState::Speaking && frame.emotion==(cancel?FaceEmotion::Neutral:FaceEmotion::Happy));
        }
        assert(codec.core.Snapshot(id).played_samples==240);
    }
    app.protocol_=nullptr;
}
static void ExpressionProtocol() {
    auto& app=Application::GetInstance();auto& display=Board::GetInstance().display;
    const auto hello=[](bool receipt,bool ack) {
        auto h=std::unique_ptr<cJSON>(cJSON_CreateObject());
        cJSON_AddStringToObject(h.get(),"transport","websocket");
        cJSON_AddStringToObject(h.get(),"session_id","expression-scope");
        auto caps=std::unique_ptr<cJSON>(cJSON_CreateObject());
        cJSON_AddBoolToObject(caps.get(),"playback_progress_v1",receipt);
        cJSON_AddBoolToObject(caps.get(),"playback_drain_v1",receipt);
        cJSON_AddBoolToObject(caps.get(),"face_expression_v1",ack);
        // No face_state_v1: expression negotiation is independent.
        h->children.emplace_back("features",std::move(caps));return h;
    };
    for(bool receipt:{false,true})for(bool ack:{false,true}) {
        codec.core=PlaybackProgress{};WebsocketProtocol ws;ws.ResetPlaybackConnection();
        app.protocol_=&ws;display.expressions.clear();
        ws.ParseServerHello(hello(receipt,ack).get(),ws.AudioChannelEpoch());
        assert(ws.FaceExpressionNegotiated()==(receipt&&ack));
        assert(ws.HandlePlaybackTts(tts("start",1,"expression-scope").get(),ws.AudioChannelEpoch()));
        assert(ws.HandlePlaybackExpression(expression().get(),ws.AudioChannelEpoch(),123456));
        app.RunScheduled();assert(display.expressions.size()==size_t(receipt&&ack));
        auto legacy=expression();cJSON_DeleteItemFromObject(legacy.get(),"playback_id");
        // Whether it is forwarded generically is covered through the native
        // dispatch fragment below; safe legacy routing also consumes it.
        ws.HandlePlaybackExpression(legacy.get(),ws.AudioChannelEpoch(),123457);
        app.RunScheduled();assert(display.expressions.size()==size_t(receipt&&ack));
        assert(ws.HandlePlaybackExpression(legacy.get(),ws.AudioChannelEpoch()+1,123458));
        app.RunScheduled();assert(display.expressions.size()==size_t(receipt&&ack));
    }
    static constexpr const char* canonical[]={"neutral","happy","loving","embarrassed","confident",
        "delicious","sad","crying","sleepy","silly","angry","surprised","winking","relaxed","confused"};
    for(const char* mood:canonical) {
        codec.core=PlaybackProgress{};WebsocketProtocol ws;ws.ResetPlaybackConnection();app.protocol_=&ws;
        ws.ParseServerHello(hello(true,true).get(),ws.AudioChannelEpoch());display.expressions.clear();
        const auto connection=ws.AudioChannelEpoch();
        assert(ws.HandlePlaybackExpression(expression(1,"expression-scope",mood).get(),connection,100));
        app.RunScheduled();assert(display.expressions.empty()); // future without start
        assert(ws.HandlePlaybackTts(tts("start",1,"expression-scope").get(),connection));
        assert(ws.HandlePlaybackExpression(expression(1,"expression-scope",mood).get(),connection,123456));
        app.RunScheduled();assert(display.expressions.size()==1);
        const auto& event=display.expressions.back();
        assert(event.connection==connection && event.playback==1 && event.name==mood && event.arrival==123456);
        for(double id:{0.0,-1.0,1.5,2.0,4294967296.0})
            assert(ws.HandlePlaybackExpression(expression(id).get(),connection,123457));
        for(const char* bad:{"thinking","shocked","laughing","HAPPY","unknown",""})
            assert(ws.HandlePlaybackExpression(expression(1,"expression-scope",bad).get(),connection,123457));
        assert(ws.HandlePlaybackExpression(expression(1,"wrong").get(),connection,123457));
        auto bad=expression();cJSON_DeleteItemFromObject(bad.get(),"playback_id");
        cJSON_AddStringToObject(bad.get(),"playback_id","1");
        assert(ws.HandlePlaybackExpression(bad.get(),connection,123457));
        assert(ws.HandlePlaybackExpression(expression().get(),connection+1,123457));
        app.RunScheduled();assert(display.expressions.size()==1);
    }
    // Normal server seal leaves real hardware playback eligible for late metadata.
    codec.core=PlaybackProgress{};WebsocketProtocol ws;ws.ResetPlaybackConnection();app.protocol_=&ws;
    ws.ParseServerHello(hello(true,true).get(),ws.AudioChannelEpoch());display.expressions.clear();
    const auto connection=ws.AudioChannelEpoch();
    assert(ws.HandlePlaybackTts(tts("start",1,"expression-scope").get(),connection));
    ws.on_incoming_audio_=[](std::unique_ptr<AudioStreamPacket>){};
    ws.DeliverAudio(std::make_unique<AudioStreamPacket>(),connection);
    auto seal=tts("stop",1,"expression-scope");cJSON_AddNumberToObject(seal.get(),"sent_samples",1440);
    assert(ws.HandlePlaybackTts(seal.get(),connection));assert(!ws.playback_receiving_);
    assert(ws.HandlePlaybackExpression(expression().get(),connection,200));app.RunScheduled();
    assert(display.expressions.size()==1);
    // Queued metadata cannot escape cancellation, drain, a replacement start or close/reopen.
    uint32_t next_id=2;
    for(int action=0;action<5;++action) {
        const double id=next_id++;
        assert(ws.HandlePlaybackTts(tts("start",id,"expression-scope").get(),ws.AudioChannelEpoch()));
        const auto current=ws.AudioChannelEpoch();
        assert(ws.HandlePlaybackExpression(expression(id).get(),current,300+action));
        if(action==0)ws.SendAbortSpeaking(kAbortReasonNone);
        if(action==1) {
            auto drain=tts("stop",id,"expression-scope");cJSON_AddNumberToObject(drain.get(),"sent_samples",0);
            cJSON_AddStringToObject(drain.get(),"reason","drain");assert(ws.HandlePlaybackTts(drain.get(),current));
        }
        if(action==2) {
            auto invalid=tts("stop",id,"expression-scope");cJSON_AddNumberToObject(invalid.get(),"sent_samples",1);
            assert(!ws.HandlePlaybackTts(invalid.get(),current));
        }
        if(action==3) {
            assert(ws.HandlePlaybackTts(tts("start",next_id++,"expression-scope").get(),current));
            assert(ws.HandlePlaybackExpression(expression(id).get(),current,400));
        }
        if(action==4) {ws.NotifyPlaybackClosed(current);ws.ResetPlaybackConnection();
            ws.ParseServerHello(hello(true,true).get(),ws.AudioChannelEpoch());
            assert(ws.HandlePlaybackTts(tts("start",id,"expression-scope").get(),ws.AudioChannelEpoch()));}
        assert(ws.HandlePlaybackExpression(expression(id).get(),current,500));
        app.RunScheduled();assert(display.expressions.size()==1);
    }
    app.protocol_=nullptr;display.expressions.clear();
}
#endif
int main(int argc,char** argv){
#if CONFIG_CHICK_FACE_UI
    if(argc>1 && !std::strcmp(argv[1],"legacy_epoch")){LegacyEpochProtocol();return 0;}
    if(argc>1 && !std::strcmp(argv[1],"duplicate_emotion")){DuplicateEmotionProtocol();return 0;}
    if(argc>1 && !std::strcmp(argv[1],"legacy_lifecycle")){LegacyLifecycleProtocol();return 0;}
    if(argc>1 && !std::strcmp(argv[1],"sealed_pending")){SealedPendingProtocol();return 0;}
    LegacyEpochProtocol();DuplicateEmotionProtocol();LegacyLifecycleProtocol();SealedPendingProtocol();ExpressionProtocol();
#else
    (void)argc;(void)argv;
#endif
#if CONFIG_CHICK_FACE_UI
    {
        WebsocketProtocol face;face.ResetPlaybackConnection();
        face.playback_negotiated_=true;face.session_id_="face-scope";
        const auto epoch=face.FaceStateEpoch("face-scope");assert(epoch);
        assert(!face.FaceStateEpoch(nullptr) && !face.FaceStateEpoch("wrong"));
        std::atomic<bool> started{false};
        std::thread reader([&] {
            started=true;
            for(int i=0;i<10000;++i) {
                const auto value=face.FaceStateEpoch("face-scope");
                assert(value==0 || value==epoch);
            }
        });
        while(!started.load())std::this_thread::yield();
        face.ResetPlaybackConnection();reader.join();
        assert(!face.FaceStateEpoch("face-scope") && face.AudioChannelEpoch()!=epoch);
    }
#endif
    WebsocketProtocol ws;ws.ResetPlaybackConnection();
    auto handle=[&](cJSON* r){return ws.HandlePlaybackTts(r,ws.playback_connection_);};
    // No mutual negotiation: retain ordinary JSON behavior and suppress receipts.
    assert(handle(tts("start").get()));
    ws.SendPlayback({{1,1},0,true,false,false});assert(ws.sent==0);
    auto hello=std::unique_ptr<cJSON>(cJSON_CreateObject());
    cJSON_AddStringToObject(hello.get(),"transport","websocket");cJSON_AddStringToObject(hello.get(),"session_id","s");
    ws.ParseServerHello(hello.get(),ws.playback_connection_);assert(!ws.PlaybackNegotiated());
    auto features=std::unique_ptr<cJSON>(cJSON_CreateObject());
    cJSON_AddBoolToObject(features.get(),"playback_progress_v1",true);hello->children.emplace_back("features",std::move(features));
    ws.ParseServerHello(hello.get(),ws.playback_connection_);assert(ws.PlaybackNegotiated());
    assert(Application::GetInstance().audio_service_.preserve);
    std::vector<AudioStreamPacket> frames;
    ws.on_incoming_audio_=[&](std::unique_ptr<AudioStreamPacket> p){frames.push_back(*p);};
    assert(!handle(tts("start",1.5).get()));
    assert(!handle(tts("start",1,"old").get()));
    Application::GetInstance().audio_service_.uplink=true;
    auto first_start=tts("start"); assert(handle(first_start.get()));
#if CONFIG_XMINI_C3_BUTTON_ENTRY
    assert(Application::GetInstance().audio_service_.uplink==bool(CONFIG_XMINI_C3_VOICE_BARGE));
#endif
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
#if CONFIG_XMINI_C3_BUTTON_ENTRY
    assert(!Application::GetInstance().audio_service_.uplink);
#endif
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
#if CONFIG_CHICK_FACE_UI
        const auto cleared=Board::GetInstance().display.clears;
#endif
        app.RunScheduled();assert(notifications==1 && app.state==kDeviceStateIdle);
#if CONFIG_CHICK_FACE_UI
        assert(Board::GetInstance().display.clears==cleared+1);
#endif
        assert(Board::GetInstance().power==PowerSaveLevel::LOW_POWER);
        // Remote callback first, then explicit close: exactly one notification.
        closed.ResetPlaybackConnection();closed.socket_connection_=closed.AudioChannelEpoch();
        const auto remote=closed.socket_connection_;closed.websocket_=std::make_unique<TestSocket>();
        closed.NotifyPlaybackClosed(remote);closed.CloseAudioChannel();assert(notifications==2);
        // Reopen before the old queued UI close: preserve new UI and power.
        closed.ResetPlaybackConnection();app.state=kDeviceStateSpeaking;
        Board::GetInstance().power=PowerSaveLevel::PERFORMANCE;app.RunScheduled();
        assert(app.state==kDeviceStateSpeaking && Board::GetInstance().power==PowerSaveLevel::PERFORMANCE);
#if CONFIG_CHICK_FACE_UI
        assert(Board::GetInstance().display.clears==cleared+1); // stale close cannot clear a reopened face
#endif
        closed.NotifyPlaybackClosed(remote);assert(notifications==2);
    }
    WebsocketProtocol exhausted;exhausted.playback_connection_=UINT32_MAX-1;
    assert(!exhausted.ResetPlaybackConnection()); // reserve final epoch for the last close
    int exhausted_notifications=0;
    exhausted.on_audio_channel_closed_=[&](uint32_t){++exhausted_notifications;};
    exhausted.NotifyPlaybackClosed(UINT32_MAX-1);exhausted.NotifyPlaybackClosed(UINT32_MAX-1);
    assert(exhausted_notifications==1&&!exhausted.ResetPlaybackConnection());
    // New mutually negotiated drain mode must preserve software and late network tails.
    {
        codec.core=PlaybackProgress{};codec.generation=0;
        WebsocketProtocol d;d.ResetPlaybackConnection();
        auto h=std::unique_ptr<cJSON>(cJSON_CreateObject());
        cJSON_AddStringToObject(h.get(),"transport","websocket");
        cJSON_AddStringToObject(h.get(),"session_id","drain-session");
        auto caps=std::unique_ptr<cJSON>(cJSON_CreateObject());
        cJSON_AddBoolToObject(caps.get(),"playback_progress_v1",true);
        cJSON_AddBoolToObject(caps.get(),"playback_drain_v1",true);
        h->children.emplace_back("features",std::move(caps));
        d.ParseServerHello(h.get(),d.playback_connection_);
        assert(d.playback_drain_negotiated_);
        auto start=tts("start",1,"drain-session");
        assert(d.HandlePlaybackTts(start.get(),d.playback_connection_));
        const auto old_event=d.PlaybackEventIdentity(start.get());
        std::vector<AudioStreamPacket> received;
        d.on_incoming_audio_=[&](std::unique_ptr<AudioStreamPacket> p){received.push_back(*p);};
        d.DeliverAudio(std::make_unique<AudioStreamPacket>(),d.playback_connection_);
        int16_t pcm[240]{};
        for(size_t i=0;i<6;++i)assert(codec.core.Submit(old_event.identity,i*240,pcm,240)==PlaybackProgress::SubmitResult::Accepted);
        const auto resets=app.audio_service_.resets; const auto generation=codec.generation;
        app.protocol_=&d;
#ifdef FAMILY_ROBOT_DRAIN_APPLICATION
        app.AbortSpeaking(kAbortReasonNone);
#else
        assert(false && "drain requires the actual production AbortSpeaking method");
#endif
        assert(app.aborted_&&app.audio_service_.resets==resets&&codec.generation==generation);
#if CONFIG_CHICK_FACE_UI
        assert(codec.core.OrbActivity().draining);
#endif
        assert(!d.PlaybackEventCurrent(old_event)); // retire only scheduled UI
        d.DeliverAudio(std::make_unique<AudioStreamPacket>(),d.playback_connection_);
        assert(received.size()==2&&received.back().receipt_first==1440);
        for(size_t i=6;i<12;++i)assert(codec.core.Submit(old_event.identity,i*240,pcm,240)==PlaybackProgress::SubmitResult::Accepted);
        codec.core.Release(old_event.identity);codec.core.Release(old_event.identity);
        auto seal=tts("stop",1,"drain-session");
        cJSON_AddStringToObject(seal.get(),"reason","drain");
        cJSON_AddNumberToObject(seal.get(),"sent_samples",2880);
        assert(d.HandlePlaybackTts(seal.get(),d.playback_connection_));
#if CONFIG_CHICK_FACE_UI
        assert(codec.core.OrbActivity().draining);
#endif
        d.DeliverAudio(std::make_unique<AudioStreamPacket>(),d.playback_connection_);
        assert(received.size()==2&&!codec.core.Snapshot(old_event.identity).finished);
        int16_t dma[6][240]{};
        for(int tick=0;tick<18;++tick)codec.core.Complete(dma[tick%6],240,(tick+1)*10000);
        auto final=codec.core.Snapshot(old_event.identity);
        assert(final.valid&&final.finished&&!final.interrupted&&final.played_samples==2880);
#if CONFIG_CHICK_FACE_UI
        assert(!codec.core.OrbActivity().draining && !codec.core.OrbActivity().active);
#endif
        // An advertised total must match the same ID's actually admitted frames.
        for(double wrong:{-1.0,0.0,1440.5,1441.0,4294967296.0}){
            d.ResetPlaybackConnection();d.ParseServerHello(h.get(),d.playback_connection_);
            auto begin=tts("start",1,"drain-session");assert(d.HandlePlaybackTts(begin.get(),d.playback_connection_));
            d.DeliverAudio(std::make_unique<AudioStreamPacket>(),d.playback_connection_);
            auto bad=tts("stop",1,"drain-session");cJSON_AddNumberToObject(bad.get(),"sent_samples",wrong);
            assert(!d.HandlePlaybackTts(bad.get(),d.playback_connection_));
        }
        d.ResetPlaybackConnection();d.ParseServerHello(h.get(),d.playback_connection_);
        assert(d.HandlePlaybackTts(tts("start",1,"drain-session").get(),d.playback_connection_));
        assert(!d.HandlePlaybackTts(tts("stop",1,"drain-session").get(),d.playback_connection_));
        // A transport close clears both negotiated capabilities.
        d.ResetPlaybackConnection();assert(!d.playback_drain_negotiated_);
    }
    puts("production WebSocket identity/stop/reconnect/fallback integration passed");
}
