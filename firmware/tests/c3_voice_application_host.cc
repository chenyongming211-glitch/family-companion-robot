#include "button_uplink.h"
#include <atomic>
#include <cassert>
#include <cstdint>
#include <functional>
enum { kDeviceStateIdle, kDeviceStateListening, kDeviceStateSpeaking };
enum ListeningMode { kListeningModeAutoStop, kListeningModeManualStop, kListeningModeRealtime };
enum { kAecOff, kAecOnDeviceSide };
namespace Lang { namespace Sounds { constexpr int OGG_POPUP=1; } }
struct AudioService {
    family_robot::ButtonUplink gate;
    bool idle=false, enabled=false;
    family_robot::ButtonUplink& C3Uplink() { return gate; }
    bool IsPlaybackIdle() const { return idle; }
    void SetExperimentUplink(bool value,bool=false) { if(!value)gate.Close([]{}); }
    bool OpenC3Uplink(uint32_t revision) { return gate.Open(revision,[]{}); }
    void EnableVoiceProcessing(bool value) { enabled=value; }
    void PlaySound(int) {}
};
struct Protocol {
    bool opened=true, receiving=true; int starts=0;
    bool IsAudioChannelOpened() const { return opened; }
    bool PlaybackAllowsListening() const { return opened&&!receiving; }
    bool SendStartListening(int) { ++starts;return true; }
};
class Application { public:
    int state=kDeviceStateListening,aec_mode_=kAecOff;
    ListeningMode listening_mode_=kListeningModeRealtime;
    Protocol transport,*protocol_=&transport;AudioService audio_service_;
    bool pending_listening_start_=false,play_popup_on_listening_=false;
    int GetDeviceState() const { return state; }
    void RecoverListeningFailure() { assert(false); }
    void ConfigureWakeWordForListening() {}
    ListeningMode GetDefaultListeningMode() const;
    void StartListeningAudio();
};
// PRODUCTION_METHODS
int main() {
    Application a;
    assert(a.GetDefaultListeningMode()==(CONFIG_XMINI_C3_VOICE_BARGE?kListeningModeRealtime:kListeningModeAutoStop));
    a.StartListeningAudio();
    if(CONFIG_XMINI_C3_VOICE_BARGE) {
        assert(a.audio_service_.gate.Capture()&&a.audio_service_.enabled&&a.transport.starts==1);
        const auto epoch=a.audio_service_.gate.Capture();
        a.StartListeningAudio();
        assert(a.audio_service_.gate.Capture()==epoch&&a.transport.starts==1);
        a.audio_service_.SetExperimentUplink(false);
        a.transport.opened=false;a.StartListeningAudio();
        assert(!a.audio_service_.gate.Capture()&&a.transport.starts==1);
    } else assert(!a.audio_service_.gate.Capture()&&!a.transport.starts);
}
