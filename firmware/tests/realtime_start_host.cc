#include <atomic>
#include <cassert>
#include <cstdint>
#include <functional>
#include <string>
#include <vector>
#define ESP_LOGI(...) ((void)0)

enum AecMode {kAecOff, kAecOnDeviceSide, kAecOnServerSide};
enum ListeningMode {kListeningModeAutoStop, kListeningModeRealtime, kListeningModeManualStop};
enum DeviceState {kDeviceStateIdle, kDeviceStateListening, kDeviceStateSpeaking};
static int64_t now_us = 1000;
static int64_t esp_timer_get_time() { return now_us; }
struct Codec {
    bool software = true, eligible = false, valid = false, faulted = false;
    uint32_t capture = 7;
    std::function<void()> after_eligible_check;
    bool SoftwareReference() const { return software; }
    bool RealtimeEligible() const {
        const bool result=eligible && !faulted;
        if (after_eligible_check) after_eligible_check();
        return result;
    }
    bool ReferenceValid() const { return valid; }
    bool ReferenceFaulted() const { return faulted; }
    uint32_t CaptureGeneration() const { return capture; }
};
struct Display {void ShowNotification(const char*){}};
struct Board {
    Codec codec;
    Display display;
    static Board& GetInstance() { static Board board; return board; }
    Codec* GetAudioCodec() { return &codec; }
    Display* GetDisplay() { return &display; }
};
namespace Lang {
namespace Sounds {constexpr int OGG_POPUP=1;}
namespace Strings {const char* RTC_MODE_OFF="off"; const char* RTC_MODE_ON="on";}
}
struct AudioEngine {
    uint32_t generation=7;
    uint32_t OutputCaptureGeneration() const {return generation;}
};
struct AudioServiceCallbacks {std::function<void(uint32_t)> on_capture_ready;};
struct AudioService {
    Codec* codec_=&Board::GetInstance().codec;
    AudioEngine engine;
    AudioEngine* audio_engine_=&engine;
    AudioServiceCallbacks callbacks_;
    std::atomic<bool> capture_ready_waiting_{false};
    bool uplink=false, voice=false, observer=false, voice_enable_succeeds=true;
    int popups=0, configured=0;
    void SetExperimentUplink(bool enabled, bool tail=false){uplink=enabled; observer=tail;}
    void EnableVoiceProcessing(bool enabled){voice=enabled && voice_enable_succeeds;}
    bool IsAudioProcessorRunning()const{return voice;}
    void EnableWakeWordDetection(bool){}
    void EnableDeviceAec(bool){}
    void WaitForCaptureReady(bool enabled){capture_ready_waiting_.store(enabled);}
    void PlaySound(int){++popups;}
    void NotifyCaptureReady();
};
struct Protocol {
    bool opened=true, start_ok=true;
    uint32_t epoch=41;
    int closes=0;
    std::vector<ListeningMode> starts;
    AudioService* audio=nullptr;
    bool IsAudioChannelOpened()const{return opened;}
    uint32_t AudioChannelEpoch()const{return epoch;}
    void CloseAudioChannel(){opened=false; ++closes;}
    bool SendStartListening(ListeningMode mode){assert(!audio->uplink); starts.push_back(mode); return start_ok;}
};
struct StateMachine {
    DeviceState state=kDeviceStateListening;
    bool TransitionTo(DeviceState next){state=next; return true;}
};
struct Application {
    StateMachine state_machine_;
    AecMode aec_mode_=kAecOnDeviceSide;
    ListeningMode listening_mode_=kListeningModeAutoStop;
    AudioService audio_service_;
    Protocol protocol;
    Protocol* protocol_=&protocol;
    bool play_popup_on_listening_=false, pending_realtime_start_=false, skip_realtime_wait_once_=false;
    int64_t realtime_start_deadline_us_=0;
    uint32_t realtime_start_channel_epoch_=0;
    std::atomic<uint32_t> capture_ready_generation_{UINT32_MAX};
    int failures=0;
    std::vector<std::function<void()>> scheduled;
    Application(){
        protocol.audio=&audio_service_;
        audio_service_.callbacks_.on_capture_ready=[this](uint32_t generation){capture_ready_generation_=generation;};
    }
    DeviceState GetDeviceState()const{return state_machine_.state;}
    bool SetDeviceState(DeviceState);
    void CancelPendingRealtimeStart();
    void ContinueRealtimeListeningStart();
    void StartListeningAudio();
    ListeningMode GetDefaultListeningMode()const;
    void SetAecMode(AecMode);
    void ConfigureWakeWordForListening(){++audio_service_.configured;}
    void RecoverListeningFailure(){++failures; CancelPendingRealtimeStart(); audio_service_.SetExperimentUplink(false); protocol.CloseAudioChannel(); SetDeviceState(kDeviceStateIdle);}
    void Schedule(std::function<void()> function){scheduled.push_back(std::move(function));}
    void Drain(){while(!scheduled.empty()){auto function=std::move(scheduled.front()); scheduled.erase(scheduled.begin()); function();}}
};

// PRODUCTION_METHODS

static void reset_codec(){Board::GetInstance().codec=Codec{}; now_us=1000;}
[[maybe_unused]] static void arm(Application& app){
    app.StartListeningAudio();
    assert(app.pending_realtime_start_ && app.audio_service_.voice && !app.audio_service_.uplink);
    assert(app.protocol.starts.empty() && app.audio_service_.capture_ready_waiting_);
}
[[maybe_unused]] static void fresh_ready(Application& app){
    auto& codec=Board::GetInstance().codec;
    codec.valid=codec.eligible=true;
    app.audio_service_.engine.generation=codec.capture;
    app.audio_service_.NotifyCaptureReady();
    app.ContinueRealtimeListeningStart();
}
int main(){
#if CONFIG_STOPWATCH_DEFAULT_REALTIME
    reset_codec();
    {Application app; arm(app);
     auto& codec=Board::GetInstance().codec;
     app.capture_ready_generation_=codec.capture;
     ++codec.capture;
     codec.valid=codec.eligible=true;
     app.ContinueRealtimeListeningStart();
     assert(app.pending_realtime_start_ && app.protocol.starts.empty());
     // Even with the current reference ready, old output cannot relabel its source.
     app.audio_service_.NotifyCaptureReady();
     assert(app.capture_ready_generation_!=codec.capture);
     app.ContinueRealtimeListeningStart();
     assert(app.protocol.starts.empty());
     fresh_ready(app);
     assert(!app.pending_realtime_start_ && app.protocol.starts==std::vector<ListeningMode>{kListeningModeRealtime});
     assert(app.audio_service_.uplink && !app.audio_service_.capture_ready_waiting_);}
    reset_codec();
    {Application app; arm(app); app.protocol.epoch++; fresh_ready(app);
     assert(!app.pending_realtime_start_ && app.protocol.starts.empty() && !app.audio_service_.uplink);}
    reset_codec();
    {Application app; arm(app); app.protocol.opened=false; fresh_ready(app);
     assert(!app.pending_realtime_start_ && app.protocol.starts.empty());}
    reset_codec();
    {Application app; arm(app); app.SetDeviceState(kDeviceStateIdle); fresh_ready(app);
     assert(!app.pending_realtime_start_ && app.protocol.starts.empty());}
    reset_codec();
    {Application app; arm(app); app.SetAecMode(kAecOff); fresh_ready(app); app.Drain();
     assert(app.aec_mode_==kAecOff && !app.pending_realtime_start_ && app.protocol.starts.empty());}
    reset_codec();
    {Application app; arm(app); auto& codec=Board::GetInstance().codec; codec.faulted=true;
     app.ContinueRealtimeListeningStart(); app.Drain();
     assert(app.aec_mode_==kAecOff && !app.pending_realtime_start_ && app.protocol.starts.empty());}
    reset_codec();
    {Application app; arm(app); auto& codec=Board::GetInstance().codec;
     app.capture_ready_generation_=codec.capture; codec.valid=codec.eligible=true;
     codec.after_eligible_check=[&]{codec.faulted=true; codec.valid=false;};
     app.ContinueRealtimeListeningStart(); app.Drain();
     assert(app.aec_mode_==kAecOff && !app.pending_realtime_start_ && app.protocol.starts.empty());}
    reset_codec();
    {Application app; arm(app); now_us=app.realtime_start_deadline_us_;
     app.ContinueRealtimeListeningStart();
     assert(!app.pending_realtime_start_ && app.protocol.starts==std::vector<ListeningMode>{kListeningModeAutoStop});
     assert(app.audio_service_.uplink && app.audio_service_.observer);
     app.ContinueRealtimeListeningStart(); assert(app.protocol.starts.size()==1);}
    reset_codec();
    {Application app; app.audio_service_.voice_enable_succeeds=false; app.StartListeningAudio();
     assert(app.pending_realtime_start_); now_us=app.realtime_start_deadline_us_;
     app.ContinueRealtimeListeningStart(); assert(!app.pending_realtime_start_ && app.protocol.starts.size()==1);}
    reset_codec();
    {Application app; app.listening_mode_=kListeningModeManualStop; app.StartListeningAudio();
     assert(!app.pending_realtime_start_ && app.protocol.starts==std::vector<ListeningMode>{kListeningModeManualStop});}
    reset_codec();
    {Application app; arm(app); app.protocol.start_ok=false; fresh_ready(app);
     assert(app.failures==1 && !app.pending_realtime_start_ && !app.audio_service_.uplink);}
    reset_codec();
    {Application app; app.aec_mode_=kAecOff; app.StartListeningAudio();
     assert(!app.pending_realtime_start_ && app.protocol.starts==std::vector<ListeningMode>{kListeningModeAutoStop});}
#else
    reset_codec();
    {Application app; app.aec_mode_=kAecOff; app.StartListeningAudio();
     assert(!app.pending_realtime_start_ && app.protocol.starts==std::vector<ListeningMode>{kListeningModeAutoStop});}
    reset_codec();
    {Application app; app.aec_mode_=kAecOnDeviceSide;
     // Legacy explicit On keeps its selector/handshake semantics across wake pause.
     app.listening_mode_=app.GetDefaultListeningMode(); app.StartListeningAudio();
     assert(!app.pending_realtime_start_ && app.protocol.starts==std::vector<ListeningMode>{kListeningModeRealtime});}
#endif
    return 0;
}
