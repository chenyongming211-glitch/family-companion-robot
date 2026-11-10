// Application transition is extracted verbatim; DMA/reference and capture
// authorization use production helpers. Only task/board/service calls are fakes.
#include "experiment_gate.h"
#include "duplex_reference.h"
#include <cassert>
#include <cstdio>
#include <functional>
#include <memory>
#include <string>
#include <vector>
#define ESP_LOGI(...) ((void)0)

enum DeviceState { kDeviceStateUnknown, kDeviceStateIdle, kDeviceStateConnecting,
                   kDeviceStateListening, kDeviceStateSpeaking,
                   kDeviceStateNotifying, kDeviceStateWifiConfiguring };
enum ListeningMode { kListeningModeAutoStop, kListeningModeRealtime, kListeningModeManualStop };
enum PowerSaveLevel { PERFORMANCE };
enum AecMode { kAecOff };
namespace Lang { namespace Sounds { const int OGG_POPUP = 1; }
namespace Strings { constexpr auto STANDBY="idle", CONNECTING="connect", LISTENING="listen", SPEAKING="speak"; } }
struct Codec {
    family_robot::DuplexReference reference;
    family_robot::CaptureEpoch epochs;
    bool managed = true, active = false;
    int64_t sample = 0;
    Codec() { Reset(); }
    void Reset() {
        reference.Begin(1, 0); epochs = {}; active = false; sample = 0; managed = true;
        SetCaptureActive(true);
    }
    bool ManagedCapture() const { return managed; }
    void SetCaptureActive(bool enabled) {
        if (active == enabled) return;
        active = enabled;
        if (enabled) epochs.Resume(); else epochs.Pause();
        reference.Capture(enabled);
    }
    void Tick(bool drain) {
        int16_t pcm[240]{};
        const auto stamp = 1000000 + sample * 1000000 / 24000;
        reference.Tx(sample, stamp, pcm, 240);
        reference.Rx(sample, stamp, pcm, 240);
        sample += 240;
        if (drain && active) {
            int16_t mr[480];
            assert(reference.Take(mr, 240, stamp) ==
                   family_robot::DuplexReference::Result::Ready);
        }
    }
};
struct Board {
    struct Display {
        void SetStatus(const char*) {} void SetEmotion(const char*) {}
        void SetChatMessage(const char*, const char*) {} void ClearChatMessages() {}
    } display;
    struct Led { void OnStateChanged() {} } led;
    Codec codec;
    static Board& GetInstance() { static Board board; return board; }
    Codec* GetAudioCodec() { return &codec; }
    Display* GetDisplay() { return &display; }
    Led* GetLed() { return &led; }
    void SetPowerSaveLevel(PowerSaveLevel) {}
};
struct Packet {};
struct AudioService {
    family_robot::CaptureActivation activation;
    bool voice = false, wake = true, uplink = false;
    uint32_t batch = 0, frozen_batch = 0, frozen_wake = 0;
    int wake_packets = 0;
    void Refresh() {
        activation.Refresh([this] { return voice || wake; },
            [](bool active) { Board::GetInstance().codec.SetCaptureActive(active); });
    }
    void EnableVoiceProcessing(bool enabled) { voice = enabled; Refresh(); }
    void EnableWakeWordDetection(bool enabled) { wake = enabled; Refresh(); }
    void SetExperimentUplink(bool enabled) { uplink = enabled; }
    void EncodeWakeWord() {
        frozen_batch = ++batch;
        frozen_wake = Board::GetInstance().codec.epochs.wake();
        wake_packets = 3;
    }
    bool WakeAudioBatchValid() const {
        return family_robot::WakeBatchValid(frozen_batch, batch, frozen_wake,
                                            Board::GetInstance().codec.epochs.wake());
    }
    std::unique_ptr<Packet> PopWakeWordPacket() {
        return wake_packets-- > 0 ? std::make_unique<Packet>() : nullptr;
    }
    bool IsAudioProcessorRunning() const { return voice; }
    bool IsPlaybackIdle() const { return true; }
    bool IsAfeWakeWord() const { return false; }
    bool PreservingServerPlayback() const { return true; }
    void ResetDecoder() {} void PlaySound(int) {}
};
struct StateMachine {
    DeviceState state = kDeviceStateIdle;
    bool allow = true;
    bool TransitionTo(DeviceState next) {
        if (!allow) return false;
        state = next;
        return true;
    }
    DeviceState GetState() const { return state; }
};
struct Protocol {
    bool opened = false, open_result = true;
    int audio_sent = 0, detects = 0;
    bool IsAudioChannelOpened() const { return opened; }
    bool OpenAudioChannel() {
        for (int i = 0; i < 200; ++i) Board::GetInstance().codec.Tick(false);
        opened = open_result;
        return opened;
    }
    bool SendAudio(std::unique_ptr<Packet>) { ++audio_sent; return true; }
    bool SendWakeWordDetected(const std::string&) { ++detects; return true; }
};
struct Application {
    StateMachine state_machine_;
    AudioService audio_service_;
    ListeningMode listening_mode_ = kListeningModeRealtime;
    Protocol transport, *protocol_ = &transport;
    bool pending_listening_start_ = false, play_popup_on_listening_ = false;
    int clock_ticks_ = 0;
    std::string last_error_message_;
    std::vector<std::function<void()>> scheduled;
    int cancels = 0;
    void CancelPendingRealtimeStart() { ++cancels; }
    bool SetDeviceState(DeviceState state);
    DeviceState GetDeviceState() const { return state_machine_.state; }
    void HandleStateChangedEvent();
    void ContinueOpenAudioChannel(ListeningMode mode);
    void BeginWakeWordInvoke(const std::string&);
    void ContinueWakeWordInvoke(const std::string&);
    void SetListeningMode(ListeningMode);
    ListeningMode GetDefaultListeningMode() const { return kListeningModeRealtime; }
    void SetAecMode(AecMode) {}
    void Schedule(std::function<void()> fn) { scheduled.push_back(std::move(fn)); }
    void Drain() { while (!scheduled.empty()) { auto fn=std::move(scheduled.front()); scheduled.erase(scheduled.begin()); fn(); } }
    void RecoverListeningFailure() { SetDeviceState(kDeviceStateIdle); audio_service_.SetExperimentUplink(false); }
    void ConfigureWakeWordForListening() {}
    void StartListeningAudio() { audio_service_.EnableVoiceProcessing(true); }
};

// PRODUCTION_TRANSITION

int main() {
    using Reference = family_robot::DuplexReference;
    auto& codec = Board::GetInstance().codec;
    {
        Application app;
        codec.Tick(true);
        const auto frozen_wake = codec.epochs.wake();
        const auto old_capture = codec.epochs.capture();
        assert(app.SetDeviceState(kDeviceStateConnecting));
        // An already-set SCHEDULE bit may immediately run network open before
        // STATE_CHANGED/UI is dispatched. No handler is called in this test.
        // Model 2s without scheduling a Take; real physical DMA keeps running.
        app.ContinueOpenAudioChannel(kListeningModeRealtime);
        if (codec.reference.fault() != Reference::Fault::None) {
            std::fputs("connection stalled capture while RX remained authorized\n", stderr);
            return 19;
        }
        assert(!codec.active && !app.audio_service_.wake && !app.audio_service_.voice);
        assert(codec.epochs.capture() != old_capture);
        assert(codec.epochs.wake() == frozen_wake); // frozen wake upload survives Pause
        assert(codec.reference.pending_blocks() == 0);
        app.audio_service_.EnableVoiceProcessing(true);
        assert(codec.active && codec.epochs.wake() != frozen_wake);
        codec.Tick(true);
        assert(codec.reference.eligible()); // existing fresh-AFE gate is tested separately
        app.audio_service_.uplink = true;
        assert(app.SetDeviceState(kDeviceStateSpeaking));
        assert(codec.active && app.audio_service_.uplink); // realtime barge-in stays live
    }
    for (bool already_open : {false, true}) {
        codec.Reset();
        Application app;
        codec.Tick(true);
        app.transport.opened = already_open;
        app.BeginWakeWordInvoke("synthetic-wake");
        app.Drain(); // continuation runs before STATE_CHANGED, including a queued-open case
        assert(codec.reference.fault() == Reference::Fault::None && !codec.active);
        assert(app.transport.audio_sent == 3 && app.transport.detects == 1);
        assert(app.audio_service_.WakeAudioBatchValid());
        app.audio_service_.EnableVoiceProcessing(true);
        assert(!app.audio_service_.WakeAudioBatchValid());
    }
    {
        codec.Reset();
        Application app;
        app.transport.open_result = false;
        app.SetDeviceState(kDeviceStateConnecting);
        app.ContinueOpenAudioChannel(kListeningModeRealtime);
        assert(app.GetDeviceState() == kDeviceStateIdle);
        app.HandleStateChangedEvent();
        assert(app.audio_service_.wake && !app.audio_service_.voice && !app.audio_service_.uplink);
        codec.Tick(true);
        assert(codec.reference.fault() == Reference::Fault::None);
    }
    {
        Application app;
        app.state_machine_.allow = false;
        assert(!app.SetDeviceState(kDeviceStateConnecting));
        assert(app.audio_service_.wake && codec.active); // rejected transition has no effect
    }
    {
        Application app;
        codec.reference.Fail(Reference::Fault::Overflow);
        assert(app.SetDeviceState(kDeviceStateConnecting));
        assert(codec.reference.fault() == Reference::Fault::Overflow); // never hide a fault
    }
    {
        Application app;
        codec.managed = false;
        assert(app.SetDeviceState(kDeviceStateConnecting));
        assert(app.audio_service_.wake); // ordinary stock codecs retain their behavior
    }
}
