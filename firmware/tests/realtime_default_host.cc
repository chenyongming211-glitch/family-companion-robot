#include <cassert>
#include <cstdint>
#include <functional>
#include <string>
#include <vector>

enum AecMode {kAecOff, kAecOnDeviceSide, kAecOnServerSide};
enum ListeningMode {kListeningModeAutoStop, kListeningModeRealtime};
constexpr int MAIN_EVENT_CLOCK_TICK = 1;
constexpr int ESP_TIMER_TASK = 0;
static int xEventGroupCreate() { return 1; }
static void xEventGroupSetBits(int, int) {}
struct esp_timer_create_args_t {
    void (*callback)(void*);
    void* arg;
    int dispatch_method;
    const char* name;
    bool skip_unhandled_events;
};
static int esp_timer_create(const esp_timer_create_args_t*, int* handle) { *handle = 1; return 0; }
struct Codec {
    bool software = CONFIG_STOPWATCH_SOFTWARE_AEC;
    bool valid = false, eligible = false, faulted = false;
    bool SoftwareReference() const { return software; }
    bool ManagedCapture() const { return software || CONFIG_WAVESHARE_XIAOMAN_REALTIME; }
    bool RealtimeEligible() const { return eligible; }
    bool ReferenceValid() const { return valid; }
    bool ReferenceFaulted() const { return faulted; }
};
struct Display { void ShowNotification(const char*) {} };
struct Board {
    Codec codec;
    Display display;
    static Board& GetInstance() { static Board board; return board; }
    Codec* GetAudioCodec() { return &codec; }
    Display* GetDisplay() { return &display; }
};
namespace Lang { namespace Strings { const char* RTC_MODE_OFF = "off"; const char* RTC_MODE_ON = "on"; } }
struct AudioService {
    bool uplink = false, device_aec = false;
    void SetExperimentUplink(bool value) { uplink = value; }
    void EnableDeviceAec(bool value) { device_aec = value; }
};
struct NotifyPlayer { explicit NotifyPlayer(AudioService&) {} };
struct Protocol {
    bool opened = false;
    int closes = 0;
    bool IsAudioChannelOpened() const { return opened; }
    void CloseAudioChannel() { opened = false; ++closes; }
};
struct Application {
    AudioService audio_service_;
    NotifyPlayer notify_player_;
    int event_group_ = 0, clock_timer_handle_ = 0;
    AecMode aec_mode_ = kAecOff;
    Protocol protocol;
    Protocol* protocol_ = &protocol;
    std::vector<std::function<void()>> scheduled;
    Application();
    ListeningMode GetDefaultListeningMode() const;
    void SetAecMode(AecMode);
    void CancelPendingRealtimeStart() {}
    void Schedule(std::function<void()> function) { scheduled.push_back(std::move(function)); }
    void Drain() {
        while (!scheduled.empty()) {
            auto function = std::move(scheduled.front());
            scheduled.erase(scheduled.begin());
            function();
        }
    }
};

// PRODUCTION_METHODS

int main() {
    auto& codec = Board::GetInstance().codec;
    Application app;
#if CONFIG_WAVESHARE_XIAOMAN_REALTIME
    assert(!codec.SoftwareReference() && codec.ManagedCapture() && app.aec_mode_==kAecOnDeviceSide);
    assert(app.GetDefaultListeningMode()==kListeningModeAutoStop);
    codec.valid=codec.eligible=true;
    assert(app.GetDefaultListeningMode()==kListeningModeRealtime);
    app.SetAecMode(kAecOff);app.Drain();
    assert(app.GetDefaultListeningMode()==kListeningModeAutoStop);
    codec.valid=codec.eligible=false;app.SetAecMode(kAecOnDeviceSide);
    assert(app.aec_mode_==kAecOff);
    codec.valid=true;app.protocol.opened=true;
    app.SetAecMode(kAecOnDeviceSide);app.Drain();
    assert(app.aec_mode_==kAecOnDeviceSide && app.audio_service_.device_aec);
    assert(!app.protocol.opened && app.protocol.closes==1);
    assert(app.GetDefaultListeningMode()==kListeningModeAutoStop);
    codec.eligible=true;assert(app.GetDefaultListeningMode()==kListeningModeRealtime);
    app.SetAecMode(kAecOff);app.Drain();codec.faulted=true;
    app.SetAecMode(kAecOnDeviceSide);assert(app.aec_mode_==kAecOff);
#elif CONFIG_STOPWATCH_SOFTWARE_AEC
    assert(app.aec_mode_ == (CONFIG_STOPWATCH_DEFAULT_REALTIME ? kAecOnDeviceSide : kAecOff));
    codec.eligible = false;
    assert(app.GetDefaultListeningMode() == kListeningModeAutoStop);
    codec.valid = codec.eligible = true;
    assert(app.GetDefaultListeningMode() == (CONFIG_STOPWATCH_DEFAULT_REALTIME ?
                                             kListeningModeRealtime : kListeningModeAutoStop));
    // A runtime request may enable a valid reference, irrespective of boot default.
    app.SetAecMode(kAecOnDeviceSide);
    app.Drain();
    assert(app.GetDefaultListeningMode() == kListeningModeRealtime);
    // Ordinary-output resource fallback on this software-AEC board has no AEC reference.
    codec.software = false;
    assert(app.GetDefaultListeningMode() == (CONFIG_STOPWATCH_DEFAULT_REALTIME ?
                                             kListeningModeAutoStop : kListeningModeRealtime));
    codec.software = true;
    // Reference readiness can be revoked before the deferred fault->Off callback.
    codec.valid = codec.eligible = false;
    // A pause revokes readiness; only the new variant waits for a fresh generation.
    assert(app.GetDefaultListeningMode() == (CONFIG_STOPWATCH_DEFAULT_REALTIME ?
                                             kListeningModeAutoStop : kListeningModeRealtime));
    app.SetAecMode(kAecOff);
    app.Drain();
    assert(app.aec_mode_ == kAecOff && app.GetDefaultListeningMode() == kListeningModeAutoStop);
    app.SetAecMode(kAecOnDeviceSide);
    assert(app.aec_mode_ == kAecOff && app.scheduled.empty());
    codec.valid = codec.eligible = true;
    assert(app.GetDefaultListeningMode() == kListeningModeAutoStop);
    // Manual Off remains authoritative even after readiness comes back.
    app.SetAecMode(kAecOnDeviceSide);
    app.Drain();
    app.SetAecMode(kAecOff);
    app.Drain();
    assert(app.aec_mode_ == kAecOff && app.GetDefaultListeningMode() == kListeningModeAutoStop);
#else
    assert(app.aec_mode_ == (CONFIG_USE_DEVICE_AEC ? kAecOnDeviceSide :
                            CONFIG_USE_SERVER_AEC ? kAecOnServerSide : kAecOff));
    codec.eligible = false;
    assert(app.GetDefaultListeningMode() == (app.aec_mode_ == kAecOff ?
                                             kListeningModeAutoStop : kListeningModeRealtime));
#endif
    return 0;
}
