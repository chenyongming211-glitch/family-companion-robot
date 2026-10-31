#include "button_uplink.h"
#include <atomic>
#include <cassert>
#include <mutex>
enum { AS_EVENT_WAKE_WORD_RUNNING=1,AS_EVENT_AUDIO_PROCESSOR_RUNNING=2,AS_EVENT_AUDIO_TESTING_RUNNING=4 };
static int xEventGroupGetBits(int bits){return bits;}
struct Codec {
    bool active=false;int changes=0;
    void SetCaptureActive(bool value){if(value!=active)++changes;active=value;}
    bool ManagedCapture()const{return false;}
    bool ReferenceIntegrity()const{return true;}
};
struct CaptureActivation {template<class Requested,class Apply>void Refresh(Requested requested,Apply apply){apply(requested());}};
struct Authorization {void Request(bool,bool){}};
struct Tail {void Begin(bool){}};
class AudioService { public:
    int event_group_=AS_EVENT_AUDIO_PROCESSOR_RUNNING;
    std::atomic<bool> service_stopped_{false};
    family_robot::ButtonUplink button_uplink_;Codec codec,*codec_=&codec;
    CaptureActivation capture_activation_;Authorization experiment_authorization_;Tail observer_tail_;
    std::mutex audio_queue_mutex_;int clears=0;
    void ClearC3UplinkQueues(){++clears;}void UpdateExperimentGateLocked(){}
    bool CaptureRequested()const;void UpdateCaptureActive();bool OpenC3Uplink(uint32_t);void SetExperimentUplink(bool,bool=false);
};
// PRODUCTION_METHODS
int main(){
    AudioService a;a.UpdateCaptureActive();assert(a.codec.active==!bool(CONFIG_XMINI_C3_VOICE_BARGE));
    assert(a.OpenC3Uplink(a.button_uplink_.Revision()));a.UpdateCaptureActive();assert(a.codec.active);
    a.SetExperimentUplink(false);
    if(CONFIG_XMINI_C3_VOICE_BARGE)assert(!a.codec.active);
    a.event_group_=0;assert(a.OpenC3Uplink(a.button_uplink_.Revision()));a.UpdateCaptureActive();assert(!a.codec.active);
    a.event_group_=AS_EVENT_AUDIO_TESTING_RUNNING;a.UpdateCaptureActive();assert(a.codec.active);
    a.service_stopped_=true;a.UpdateCaptureActive();assert(!a.codec.active);
    // Initial Application state closure may run before AudioService::Initialize.
    a.codec_=nullptr;a.SetExperimentUplink(false);
}
