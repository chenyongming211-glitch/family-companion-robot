#pragma once
#include "orb_activity.h"

namespace family_robot {
enum class OrbPhase : uint8_t { Hidden, Connecting, Preparing, Listening, Speaking, Error };
struct OrbFacts {
    int64_t now_us=0;
    bool session=false,connecting=false,system_page=false,screen_on=true,error=false;
    OrbAudioActivity audio{};
};
struct OrbPresentation {
    OrbPhase phase=OrbPhase::Hidden;
    uint8_t level=0;
    bool draining=false;
    const char* Caption() const {
        switch(phase) {
        case OrbPhase::Connecting:return "正在连接";
        case OrbPhase::Preparing:return "正在准备语音";
        case OrbPhase::Listening:return "正在聆听";
        case OrbPhase::Speaking:return "小满正在说话";
        case OrbPhase::Error:return "连接异常";
        default:return "";
        }
    }
};
// No timers infer server turn-end or response progress. Freshness only prevents
// stale audio levels from looking like continuous physical speech.
class OrbModel {
public:
    static constexpr int64_t kFreshUs=350000;
    OrbPresentation Update(const OrbFacts& f) {
        if (!f.session || f.system_page || !f.screen_on) {
            active_=false; return {};
        }
        if (!active_) { active_=true; since_=f.now_us; }
        if (f.error) return {OrbPhase::Error,0,false};
        if (f.connecting) return {OrbPhase::Connecting,0,false};
        const auto& a=f.audio;
        const auto& o=a.output;
        const bool draining=o.draining;
        if (!draining && o.active && o.valid && o.identity.playback &&
            o.connection==o.identity.connection && Fresh(o.stamp_us,f.now_us))
            return {OrbPhase::Speaking,o.level,false};
        if (!a.gate_open) return {OrbPhase::Preparing,0,draining};
        const auto& i=a.input;
        const bool fresh=i.capture==a.capture && i.epoch==a.epoch && Fresh(i.stamp_us,f.now_us);
        return {OrbPhase::Listening,static_cast<uint8_t>(fresh?i.level:0),draining};
    }
private:
    bool Fresh(int64_t stamp,int64_t now) const {
        return stamp>0 && stamp>=since_ && stamp<=now && now-stamp<=kFreshUs;
    }
    int64_t since_=0;
    bool active_=false;
};
} // namespace family_robot
