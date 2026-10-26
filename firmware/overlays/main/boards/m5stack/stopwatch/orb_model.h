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
    static constexpr int64_t kUnavailableHoldUs=200000;
    OrbPresentation Update(const OrbFacts& f) {
        if (!f.session || f.system_page || !f.screen_on) {
            active_=false; gate_confirmed_=false; return {};
        }
        if (!active_) { active_=true; since_=f.now_us; }
        if (f.error || f.connecting) {
            gate_confirmed_=false;since_=f.now_us;
            return {f.error?OrbPhase::Error:OrbPhase::Connecting,0,false};
        }
        const auto& a=f.audio;
        const auto& o=a.output;
        const bool draining=o.draining;
        bool gate_open=a.gate_open;
        if(a.snapshot_available) {
            gate_confirmed_=a.gate_open;gate_us_=f.now_us;
            capture_=a.capture;epoch_=a.epoch;connection_=o.connection;
        } else {
            gate_open=gate_confirmed_ && a.capture==capture_ && a.epoch==epoch_ &&
                o.connection==connection_ && f.now_us>=gate_us_ &&
                f.now_us-gate_us_<=kUnavailableHoldUs;
            if(!gate_open) gate_confirmed_=false;
        }
        if (!draining && o.active && o.valid && o.identity.playback &&
            o.connection==o.identity.connection && Fresh(o.stamp_us,f.now_us))
            return {OrbPhase::Speaking,o.level,false};
        if (!gate_open) return {OrbPhase::Preparing,0,draining};
        const auto& i=a.input;
        const bool fresh=a.snapshot_available && i.capture==a.capture && i.epoch==a.epoch && Fresh(i.stamp_us,f.now_us);
        return {OrbPhase::Listening,static_cast<uint8_t>(fresh?i.level:0),draining};
    }
private:
    bool Fresh(int64_t stamp,int64_t now) const {
        return stamp>0 && stamp>=since_ && stamp<=now && now-stamp<=kFreshUs;
    }
    int64_t since_=0;
    int64_t gate_us_=0;
    uint32_t capture_=0,epoch_=0,connection_=0;
    bool active_=false,gate_confirmed_=false;
};
} // namespace family_robot
