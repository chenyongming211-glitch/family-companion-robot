// Host dependencies for verbatim production application/protocol methods.
#include <cassert>
#include <cstdint>
#include <condition_variable>
#include <mutex>
#include <thread>
#include <functional>
#include <iostream>
#include <memory>
#include <string>
#include <vector>
#define ESP_LOGI(...) ((void)0)
enum DeviceState {kDeviceStateIdle, kDeviceStateConnecting, kDeviceStateListening, kDeviceStateSpeaking, kDeviceStateActivating, kDeviceStateNotifying};
enum PowerSaveLevel {PERFORMANCE, LOW_POWER};
enum AbortReason {kAbortReasonWakeWordDetected};
enum AecMode {kAecOff};
enum ListeningMode {kListeningModeRealtime, kListeningModeAutoStop, kListeningModeManualStop};
namespace Lang { namespace Sounds { const int OGG_POPUP = 1; } }
struct Display {void SetChatMessage(const char*, const char*){}};
struct Board {static Board& GetInstance(){static Board b; return b;} void SetPowerSaveLevel(PowerSaveLevel){} Display* GetDisplay(){static Display d; return &d;}};
struct Packet {int index;};
struct Protocol {
    bool opened=true, open_result=true;
    int open_calls=0, close_calls=0, sends=0, fail_at=-1;
    bool detect_ok=true, start_ok=true;
    std::function<void(uint32_t)> on_close;
    uint32_t epoch=1;
    uint32_t AudioChannelEpoch(){return epoch;}
    std::string session_id_="test";
    std::vector<std::string> controls;
    bool IsAudioChannelOpened(){return opened;}
    bool OpenAudioChannel(){++open_calls; ++epoch; opened=open_result; return open_result;}
    void CloseAudioChannel(){++close_calls; opened=false; if(on_close)on_close(epoch);}
    bool SendAudio(std::unique_ptr<Packet>){return sends++ != fail_at;}
    bool SendText(const std::string& text){controls.push_back(text); return text.find("detect")!=std::string::npos ? detect_ok : start_ok;}
    // PROTOCOL_DECLARATIONS
};
struct AudioService {
    int available=3, popped=0, voices_started=0, popups=0;
    bool valid=true, wake=false, voice=false, uplink=false;
    int invalidate_at=-1;
    bool producer_active=false, terminal_seen=false;
    bool asynchronous=false, stop=false, requested=false, premature_wake=false;
    std::mutex mutex;
    std::condition_variable condition;
    std::thread producer;
    int produced=0;
    ~AudioService(){
        {std::lock_guard<std::mutex> lock(mutex); stop=true; condition.notify_all();}
        if(producer.joinable())producer.join();
    }
    std::string GetLastWakeWord(){return "synthetic-wake";}
    std::unique_ptr<Packet> PopPacketFromSendQueue(){return nullptr;}
    void ResetDecoder(){}
    void EncodeWakeWord(){
        if(producer.joinable()){
            {std::lock_guard<std::mutex> lock(mutex); stop=true; condition.notify_all();}
            producer.join(); stop=false;
        }
        popped=0; produced=0; wake=false; producer_active=true; terminal_seen=false;
        if(asynchronous)producer=std::thread([this]{
            std::unique_lock<std::mutex> lock(mutex);
            while(!stop){
                condition.wait(lock,[this]{return stop || requested;});
                if(stop)return;
                premature_wake |= wake;
                ++produced; requested=false;
                condition.notify_all();
            }
        });
    }
    std::unique_ptr<Packet> PopWakeWordPacket(){
        std::unique_lock<std::mutex> lock(mutex);
        if(asynchronous){
            const int previous=produced; requested=true; condition.notify_all();
            condition.wait(lock,[this,previous]{return produced>previous;});
        }
        if(popped==available){producer_active=false; terminal_seen=true; return nullptr;}
        return std::make_unique<Packet>(Packet{popped++});
    }
    bool WakeAudioBatchValid(){return valid && (invalidate_at<0 || popped<invalidate_at);}
    void EnableWakeWordDetection(bool enable){
        std::lock_guard<std::mutex> lock(mutex);
        if(enable && asynchronous && producer_active)premature_wake=true;
        wake=enable;
    }
    void SetExperimentUplink(bool enable, bool=false){uplink=enable;}
    void EnableVoiceProcessing(bool enable){voice=enable; if(enable)++voices_started;}
    void PlaySound(int){++popups;}
};
struct Application {
    DeviceState state=kDeviceStateIdle;
    Protocol p; Protocol* protocol_=&p; AudioService audio_service_;
    ListeningMode listening_mode_=kListeningModeRealtime;
    bool play_popup_on_listening_=false, pending_listening_start_=false;
    int aec_off=0;
    std::vector<std::function<void()>> scheduled;
    Application(){p.on_close=[this](uint32_t epoch){ScheduleAudioChannelClosed(epoch);};}
    DeviceState GetDeviceState(){return state;}
    bool SetDeviceState(DeviceState s){state=s; return true;}
    void SetAecMode(AecMode){++aec_off;}
    ListeningMode GetDefaultListeningMode(){return kListeningModeRealtime;}
    void Schedule(std::function<void()> f){scheduled.push_back(std::move(f));}
    void Drain(){while(!scheduled.empty()){auto f=std::move(scheduled.front()); scheduled.erase(scheduled.begin()); f();}}
    void ConfigureWakeWordForListening(){audio_service_.EnableWakeWordDetection(false);}
    void AbortSpeaking(AbortReason){}
    void StopNotification(){state=kDeviceStateIdle;}
    void HandleWakeWordDetectedEvent();
    void ScheduleAudioChannelClosed(uint32_t);
    void BeginWakeWordInvoke(const std::string&);
    void ContinueWakeWordInvoke(const std::string&);
    void StartListeningAudio();
    void SetListeningMode(ListeningMode);
    void RecoverListeningFailure();
};
// PRODUCTION_METHODS
void recoverable(Application& a){
    a.Drain();
    assert(a.state==kDeviceStateIdle);
    assert(!a.p.opened);
    assert(a.audio_service_.wake);
    assert(!a.audio_service_.uplink && !a.audio_service_.voice);
    assert(!a.play_popup_on_listening_ && !a.pending_listening_start_);
}
void invoke(Application& a){a.BeginWakeWordInvoke("synthetic-wake"); a.Drain();}
void success(Application& a){
    assert(a.state==kDeviceStateListening);
    a.StartListeningAudio();
    assert(a.audio_service_.voice && a.audio_service_.uplink);
    assert(a.p.controls.back().find("\"state\":\"start\"")!=std::string::npos);
}
int main(int argc, char** argv){
    if(argc>1 && std::string(argv[1])=="close-order"){
        Application a; a.p.detect_ok=false;
#if CONFIG_SEND_WAKE_WORD_DATA
        a.BeginWakeWordInvoke("synthetic-wake");
#else
        invoke(a); a.p.start_ok=false; a.StartListeningAudio();
#endif
        // Run handles WAKE before SCHEDULE. A new invocation must not be ready
        // while the close callback for the failed one is still queued.
        assert(!a.audio_service_.wake);
        a.HandleWakeWordDetectedEvent();
        a.Drain(); recoverable(a);
        a.p.detect_ok=true; a.p.start_ok=true; invoke(a); success(a);
        return 0;
    }

#if CONFIG_SEND_WAKE_WORD_DATA
    for(int fail=0; fail<3; ++fail){
        Application a; a.audio_service_.asynchronous=true; a.p.fail_at=fail; invoke(a);
        assert(a.p.sends==fail+1 && a.p.controls.empty());
        assert(a.audio_service_.voices_started==0);
        assert(a.audio_service_.terminal_seen && !a.audio_service_.producer_active); recoverable(a);
        assert(!a.audio_service_.premature_wake);
        a.p.fail_at=-1; invoke(a); success(a);
        assert(a.p.open_calls==1);
    }
    {Application a; a.p.fail_at=0; a.audio_service_.invalidate_at=2; invoke(a);
     assert(a.p.sends==1 && a.p.controls.empty() && a.aec_off==1); recoverable(a);}
    {Application a; a.p.detect_ok=false; invoke(a);
     assert(a.p.sends==3 && a.p.controls.size()==1); recoverable(a);
     a.p.detect_ok=true; invoke(a); success(a);}
    {Application a; a.audio_service_.valid=false; invoke(a);
     assert(a.p.sends==0 && a.p.controls.empty()); recoverable(a);
     a.audio_service_.valid=true; invoke(a); success(a);}
#endif
    {Application a; invoke(a); a.p.start_ok=false; a.pending_listening_start_=true;
     a.StartListeningAudio(); recoverable(a); assert(a.audio_service_.voices_started==0 && a.audio_service_.popups==0);
     a.p.start_ok=true; invoke(a); success(a);}
    {Application a; a.state=kDeviceStateListening; a.p.start_ok=false;
     a.HandleWakeWordDetectedEvent(); recoverable(a); assert(a.audio_service_.popups==0);}
    {Application a; invoke(a); success(a); const auto old_epoch=a.p.epoch;
     a.p.start_ok=false; a.StartListeningAudio(); recoverable(a);
     a.p.start_ok=true; invoke(a); success(a);
     a.ScheduleAudioChannelClosed(old_epoch); a.Drain();
     assert(a.state==kDeviceStateListening && a.p.opened);}
    {Application a; a.p.opened=false; a.p.open_result=false; invoke(a); recoverable(a);
     assert(a.p.controls.empty() && a.p.sends==0);
     a.p.open_result=true; invoke(a); success(a);}
    {Application a; invoke(a); success(a);
#if CONFIG_SEND_WAKE_WORD_DATA
     assert(a.p.sends==3 && a.p.controls.size()==2 && a.audio_service_.popups==0);
#else
     assert(a.p.sends==0 && a.p.controls.size()==1 && a.audio_service_.popups==1);
#endif
    }
    {Application a; a.state=kDeviceStateIdle; a.ContinueWakeWordInvoke("ignored");
     assert(a.p.sends==0 && a.p.controls.empty() && a.p.close_calls==0);}
    {Application a; a.audio_service_.available=0; invoke(a); success(a);}
    // Serialization and return propagation use the actual production methods.
    for(auto mode : {kListeningModeRealtime,kListeningModeAutoStop,kListeningModeManualStop}){
        Protocol p; p.SendStartListening(mode);
        const char* expected=mode==kListeningModeRealtime?"realtime":mode==kListeningModeAutoStop?"auto":"manual";
        assert(p.controls.back().find(std::string("\"mode\":\"")+expected+"\"")!=std::string::npos);
    }
    std::cout << "wake recovery cases passed\n";
}
