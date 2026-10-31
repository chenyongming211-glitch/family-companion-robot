#include "button_uplink.h"
#include <cassert>
#include <cstdint>
#include <functional>
enum { kDeviceStateIdle, kDeviceStateListening, kDeviceStateSpeaking };
enum { kListeningModeAutoStop, kListeningModeManualStop, kListeningModeRealtime };
namespace Lang { namespace Sounds { constexpr int OGG_POPUP = 1; } }
struct AudioService {
    family_robot::ButtonUplink gate;
    bool idle = true, enabled = false;
    family_robot::ButtonUplink& C3Uplink() { return gate; }
    bool IsPlaybackIdle() const { return idle; }
    void SetExperimentUplink(bool enabled, bool = false) { if (!enabled) gate.Close([] {}); }
    bool OpenC3Uplink(uint32_t revision) { return gate.Open(revision, [] {}); }
    void EnableVoiceProcessing(bool value) { enabled = value; }
    void PlaySound(int) {}
};
struct Protocol {
    bool opened = true;
    bool receiving = false;
    int starts = 0;
    std::function<void()> during_send;
    bool IsAudioChannelOpened() const { return opened; }
    bool PlaybackAllowsListening() const { return opened && !receiving; }
    bool SendStartListening(int) { ++starts; if (during_send) during_send(); return true; }
};
class Application {
public:
    int state = kDeviceStateListening, listening_mode_ = kListeningModeAutoStop;
    Protocol transport, *protocol_ = &transport;
    AudioService audio_service_;
    bool pending_listening_start_ = false, play_popup_on_listening_ = false;
    int GetDeviceState() const { return state; }
    void RecoverListeningFailure() { assert(false); }
    void ConfigureWakeWordForListening() {}
    void StartListeningAudio();
};
// PRODUCTION_LISTENING_METHOD
int main() {
    Application normal;
    normal.StartListeningAudio();
    assert(normal.audio_service_.gate.Capture() && normal.audio_service_.enabled);
    Application tts;
    tts.transport.during_send = [&] { tts.audio_service_.SetExperimentUplink(false); };
    tts.StartListeningAudio();
    assert(!tts.audio_service_.gate.Capture() && !tts.audio_service_.enabled);
    Application disconnect;
    disconnect.transport.during_send = [&] {
        disconnect.transport.opened = false;
        disconnect.audio_service_.SetExperimentUplink(false);
    };
    disconnect.StartListeningAudio();
    assert(!disconnect.audio_service_.gate.Capture() && !disconnect.audio_service_.enabled);
    Application receiving;
    receiving.transport.receiving = true;
    receiving.StartListeningAudio();
    assert(!receiving.audio_service_.gate.Capture() && receiving.transport.starts == 0);
    Application dma_tail;
    dma_tail.audio_service_.idle = false;
    dma_tail.StartListeningAudio();
    assert(!dma_tail.audio_service_.gate.Capture() && dma_tail.transport.starts == 0);
    dma_tail.audio_service_.idle = true;
    dma_tail.StartListeningAudio();
    assert(dma_tail.audio_service_.gate.Capture() && dma_tail.audio_service_.enabled);
    Application closed;
    closed.transport.opened = false;
    closed.StartListeningAudio();
    assert(!closed.audio_service_.gate.Capture() && closed.transport.starts == 0);
}
