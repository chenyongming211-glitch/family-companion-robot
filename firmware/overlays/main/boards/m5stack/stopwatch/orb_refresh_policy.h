#pragma once
#include <cstdint>

namespace family_robot {
// UI computation budget only. Caller observes rendered frames; there is no
// clock, probing, audio control, or work scheduled by this bounded policy.
class OrbRefreshPolicy {
public:
    static constexpr uint32_t kFastFrameUs=25000,kCriticalFrameUs=80000;
    static constexpr uint8_t kRecoveryFrames=10,kCriticalFrames=2;
    // Every explicit request, including the same mode, rearms the requested
    // rate. The adapter supplies a revision so repeated "on" is observable.
    void Request(int mode) {
        requested_=effective_=(mode>=0 && mode<=2)?mode:0;
        fast_=critical_=0;
    }
    void ObserveFrame(uint32_t elapsed_us) {
        // Static fallback never renders extra probe frames to recover itself.
        if(requested_==0 || effective_==0) return;
        if(elapsed_us<=kFastFrameUs) {
            critical_=0;
            if(requested_==2 && effective_==1 && ++fast_>=kRecoveryFrames) {
                effective_=2;fast_=0;
            }
            return;
        }
        fast_=0;
        if(elapsed_us>kCriticalFrameUs) {
            if(critical_<kCriticalFrames) ++critical_;
        } else critical_=0;
        if(effective_==2) effective_=1;
        if(critical_>=kCriticalFrames) effective_=0;
    }
    int requested() const {return requested_;}
    int effective() const {return effective_;}
private:
    int requested_=2,effective_=2;
    uint8_t fast_=0,critical_=0;
};
} // namespace family_robot
