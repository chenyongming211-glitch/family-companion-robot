#pragma once
#include "../audio/orb_activity.h"
#include <cmath>
#include <cstring>

namespace family_robot {
enum class FaceState : uint8_t { Boot, WifiConfig, Connecting, Idle, Listening, Thinking,
    Speaking, Notifying, Error, Upgrading, LowBattery, Charging, Hidden };
enum class FaceEmotion : uint8_t { Neutral, Happy, Loving, Embarrassed, Confident, Delicious,
    Sad, Crying, Sleepy, Silly, Angry, Surprised, Winking, Relaxed, Confused, Thinking };
enum class EyeShape : uint8_t { Open, Wide, Half, Squint, Closed, Happy, Sad, Heart, Star, Spiral };
enum class MouthShape : uint8_t { None, Flat, Smile, Sad, Wavy, O, Open, Grin, Tongue, Smirk };
enum FaceEffect : uint16_t { FxNone=0, FxZzz=1, FxQuestion=2, FxExclaim=4, FxTears=8,
    FxSweat=16, FxSparkle=32, FxDots=64, FxBars=128, FxRing=256, FxBattery=512 };
struct EyeGeometry { EyeShape shape=EyeShape::Open; float open=1,pupil_x=0,pupil_y=0; };
struct MouthGeometry { MouthShape shape=MouthShape::Smile; float width=1,open=0; };
struct BrowGeometry { bool visible=false; float y=0,angle=0; bool split=false; };
struct FacePreset {
    EyeGeometry left{},right{}; MouthGeometry mouth{}; BrowGeometry brow{};
    float blush=0; uint16_t effects=FxNone;
    uint32_t background=0x14181f,eye_color=0xffd8a8;
};
inline constexpr unsigned kStatePresetCount=12,kEmotionPresetCount=15;

// Geometry is transcribed from the adopted CHICK-01 HTML's const L0/L1 data.
// Thinking is a protocol alias for the L0 Thinking geometry, not a 28th preset.
inline FacePreset StatePreset(FaceState state) {
    FacePreset p;
    switch(state) {
    case FaceState::Boot:p.left={EyeShape::Wide,1,0,0};p.mouth.shape=MouthShape::O;p.effects=FxRing;break;
    case FaceState::WifiConfig:p.left={EyeShape::Open,1,-0.7f,0.5f};p.mouth.shape=MouthShape::Flat;p.effects=FxBars;break;
    case FaceState::Connecting:p.left={EyeShape::Open,0.9f,0,-0.7f};p.mouth.shape=MouthShape::Flat;p.effects=FxDots;break;
    case FaceState::Idle:p.left={EyeShape::Half,0.75f,0,0};p.mouth.width=0.6f;break;
    case FaceState::Listening:p.left={EyeShape::Wide,1,0,-0.15f};break;
    case FaceState::Thinking:p.left={EyeShape::Open,0.85f,0.6f,-0.6f};p.mouth.shape=MouthShape::Flat;p.effects=FxDots;break;
    case FaceState::Speaking:p.mouth={MouthShape::Open,1,0.5f};p.blush=0.2f;break;
    case FaceState::Notifying:p.left.shape=EyeShape::Wide;p.mouth.shape=MouthShape::O;p.brow={true,-6,10,false};p.effects=FxExclaim;break;
    case FaceState::Error:p.left.shape=EyeShape::Sad;p.mouth={MouthShape::Sad,0.8f,0};p.brow={true,6,14,false};p.background=0x1e1416;break;
    case FaceState::Upgrading:p.left={EyeShape::Squint,0.6f,0,0};p.mouth.shape=MouthShape::Flat;p.effects=FxRing;break;
    case FaceState::LowBattery:p.left={EyeShape::Half,0.5f,0,0};p.mouth={MouthShape::Sad,0.7f,0};p.effects=FxBattery;break;
    case FaceState::Charging:p.left.shape=EyeShape::Closed;p.mouth.width=0.7f;p.blush=0.3f;break;
    default:break;
    }
    p.right=p.left; return p;
}
inline FacePreset EmotionPreset(FaceEmotion emotion) {
    FacePreset p;
    switch(emotion) {
    case FaceEmotion::Happy:p.left.shape=EyeShape::Happy;p.mouth.shape=MouthShape::Grin;p.blush=0.5f;break;
    case FaceEmotion::Loving:p.left.shape=EyeShape::Heart;p.mouth.width=0.8f;p.blush=0.7f;break;
    case FaceEmotion::Embarrassed:p.left={EyeShape::Open,0.6f,0.8f,0.6f};p.mouth.shape=MouthShape::Wavy;p.blush=1;p.effects=FxSweat;break;
    case FaceEmotion::Confident:p.left={EyeShape::Half,0.6f,0,0};p.mouth={MouthShape::Smirk,0.8f,0};p.brow={true,-4,12,true};break;
    case FaceEmotion::Delicious:p.left.shape=EyeShape::Happy;p.mouth.shape=MouthShape::Tongue;p.blush=0.6f;break;
    case FaceEmotion::Sad:p.left.shape=EyeShape::Sad;p.mouth.shape=MouthShape::Sad;p.brow={true,5,10,false};break;
    case FaceEmotion::Crying:p.left={EyeShape::Sad,0.7f,0,0};p.mouth={MouthShape::Sad,0.8f,0};p.brow={true,6,12,false};p.effects=FxTears;p.blush=0.4f;break;
    case FaceEmotion::Sleepy:p.left.shape=EyeShape::Closed;p.mouth.shape=MouthShape::O;p.effects=FxZzz;break;
    case FaceEmotion::Silly:p.left.shape=EyeShape::Wide;p.mouth={MouthShape::Wavy,0.9f,0};break;
    case FaceEmotion::Angry:p.left={EyeShape::Open,0.7f,0,0};p.mouth={MouthShape::Flat,0.75f,0};p.brow={true,2,-16,false};p.background=0x1c1416;break;
    case FaceEmotion::Surprised:p.left.shape=EyeShape::Wide;p.mouth.shape=MouthShape::O;p.brow={true,-8,8,false};p.effects=FxSparkle;break;
    case FaceEmotion::Winking:p.mouth={MouthShape::Smirk,0.7f,0};p.blush=0.4f;break;
    case FaceEmotion::Relaxed:p.left.shape=EyeShape::Closed;p.mouth.width=0.75f;p.blush=0.25f;break;
    case FaceEmotion::Confused:p.left={EyeShape::Open,0.9f,0.5f,-0.5f};p.mouth.shape=MouthShape::Wavy;p.brow={true,-2,0,true};p.effects=FxQuestion;break;
    case FaceEmotion::Thinking:return StatePreset(FaceState::Thinking);
    default:break;
    }
    p.right=p.left;
    if(emotion==FaceEmotion::Silly)p.right={EyeShape::Squint,0.5f,0,0};
    if(emotion==FaceEmotion::Winking)p.right.shape=EyeShape::Closed;
    return p;
}
inline FaceEmotion ParseEmotion(const char* text) {
    if(!text)return FaceEmotion::Neutral;
    struct Mapping {const char* name;FaceEmotion emotion;};
    static constexpr Mapping map[]={
        {"neutral",FaceEmotion::Neutral},{"happy",FaceEmotion::Happy},
        {"laughing",FaceEmotion::Happy},{"funny",FaceEmotion::Happy},
        {"loving",FaceEmotion::Loving},{"embarrassed",FaceEmotion::Embarrassed},
        {"confident",FaceEmotion::Confident},{"delicious",FaceEmotion::Delicious},
        {"sad",FaceEmotion::Sad},{"crying",FaceEmotion::Crying},{"sleepy",FaceEmotion::Sleepy},
        {"silly",FaceEmotion::Silly},{"angry",FaceEmotion::Angry},{"surprised",FaceEmotion::Surprised},
        {"shocked",FaceEmotion::Surprised},{"thinking",FaceEmotion::Thinking},
        {"winking",FaceEmotion::Winking},{"relaxed",FaceEmotion::Relaxed},{"confused",FaceEmotion::Confused}};
    for(const auto& item:map)if(!std::strcmp(text,item.name))return item.emotion;
    return FaceEmotion::Neutral;
}
struct FaceFacts {
    int64_t now_us=0; FaceState state=FaceState::Idle;
    bool screen_on=true,quiet=false,system_page=false,low_battery=false,charging=false,model_thinking=false;
    OrbAudioActivity audio{};
};
struct FaceFrame {
    FaceState state=FaceState::Hidden; FaceEmotion emotion=FaceEmotion::Neutral; FacePreset preset{};
    uint8_t mouth_level=0; float blink=0,gaze_x=0,gaze_y=0,breath=1,tilt=0,bob_y=0;
    uint32_t motion_ms=0; bool shown=false,draining=false,preparing=false;
    // L0 describes the device's actual activity. L1 moods and L2 motion never
    // become labels, even when a mood temporarily changes the face's shape.
    const char* Caption() const {
        // Pending hardware tail can exist before a first DMA completion.
        // It is truthful drain metadata, not evidence for a speaking mouth.
        if(draining && (state==FaceState::Speaking || state==FaceState::Listening ||
                        state==FaceState::Connecting || state==FaceState::Thinking))return "正在停播";
        if(preparing)return "正在准备语音";
        switch(state) {
        case FaceState::Boot:return "小满启动中";
        case FaceState::WifiConfig:return "等待配网";
        case FaceState::Connecting:return "正在连接";
        case FaceState::Idle:return "待机中";
        case FaceState::Listening:return "正在聆听";
        case FaceState::Thinking:return "正在思考";
        case FaceState::Speaking:return "小满正在说话";
        case FaceState::Notifying:return "小满有个提示";
        case FaceState::Error:return "设备异常，请重试";
        case FaceState::Upgrading:return "正在升级";
        case FaceState::LowBattery:return "电量低，请充电";
        case FaceState::Charging:return "正在充电";
        default:return "";
        }
    }
};
class FaceModel {
public:
    static constexpr int64_t kFreshUs=350000,kUnavailableHoldUs=200000;
    void SetEmotion(const char* name,int64_t now_us) {
        const auto next=ParseEmotion(name);
        shocked_=name && !std::strcmp(name,"shocked");
        if(next==emotion_ && now_us>=emotion_us_ && now_us-emotion_us_<800000)return;
        emotion_=next;emotion_us_=now_us;
        post_speech_until_=0;
        if(next!=FaceEmotion::Neutral && last_state_==FaceState::Idle)
            idle_since_=now_us;
    }
    FaceFrame Update(const FaceFacts& f) {
        FaceFrame frame;
        if(!f.screen_on || f.system_page || f.state==FaceState::Hidden) {
            if(was_speaking_)post_speech_until_=f.now_us+2000000;
            visible_=false;gate_confirmed_=false;mouth_=0;was_speaking_=false;presentation_=false;return frame;
        }
        if(!visible_ || f.now_us<last_us_) {
            visible_=true;since_=f.now_us;state_us_=f.now_us;last_us_=f.now_us;
            next_blink_=f.now_us+RandomMs(3000,6000);next_gaze_=f.now_us+RandomMs(2000,8000);
            next_action_=f.now_us+RandomMs(30000,90000);blink_us_=action_us_=-1;
            gaze_x_=gaze_y_=0;gate_confirmed_=false;
        }
        const auto& a=f.audio; const auto& o=a.output;
        bool gate=a.gate_open;
        if(a.snapshot_available) {
            gate_confirmed_=gate;gate_us_=f.now_us;capture_=a.capture;epoch_=a.epoch;connection_=o.connection;
        } else {
            gate=gate_confirmed_ && capture_==a.capture && epoch_==a.epoch && connection_==o.connection &&
                f.now_us>=gate_us_ && f.now_us-gate_us_<=kUnavailableHoldUs;
            if(!gate)gate_confirmed_=false;
        }
        const bool output=o.active && o.valid && o.identity.playback &&
            o.connection==o.identity.connection && Fresh(o.stamp_us,f.now_us);
        // Expire before choosing state: an L1 thinking string is finite mood
        // data. Only explicit generation facts may hold the L0 Thinking state.
        if(emotion_!=FaceEmotion::Neutral && (f.now_us<emotion_us_ || f.now_us-emotion_us_>=5000000 ||
            (post_speech_until_>0 && f.now_us>=post_speech_until_)))emotion_=FaceEmotion::Neutral;
        const bool fixed=f.state==FaceState::Error || f.state==FaceState::Upgrading ||
            f.state==FaceState::Notifying || f.state==FaceState::Boot || f.state==FaceState::WifiConfig;
        frame.state=f.state;frame.shown=true;frame.draining=o.draining;
        if(!fixed) {
            if(output)frame.state=FaceState::Speaking;
            else if(f.model_thinking || f.state==FaceState::Thinking)
                frame.state=FaceState::Thinking;
            else if(f.state==FaceState::Listening || f.state==FaceState::Speaking) {
                frame.state=gate?FaceState::Listening:FaceState::Connecting;frame.preparing=!gate;
            } else if(f.state==FaceState::Idle || f.state==FaceState::LowBattery || f.state==FaceState::Charging) {
                frame.state=(f.charging || f.state==FaceState::Charging)?FaceState::Charging:
                    ((f.low_battery || f.state==FaceState::LowBattery)?FaceState::LowBattery:FaceState::Idle);
            }
        }
        if(frame.state!=last_state_) {
            state_us_=f.now_us;
            if(frame.state==FaceState::Idle)idle_since_=f.now_us;
        }
        const bool speaking=frame.state==FaceState::Speaking && output;
        if(was_speaking_ && !speaking) {
            post_speech_until_=f.now_us+2000000;
            if(frame.state==FaceState::Listening && (was_draining_ || o.draining))
                interrupted_until_=f.now_us+250000;
        }
        if(speaking)post_speech_until_=0;
        if(emotion_!=FaceEmotion::Neutral && frame.state==FaceState::Idle && post_speech_until_==0 &&
            f.now_us>=idle_since_ && f.now_us-idle_since_>=2000000)
            emotion_=FaceEmotion::Neutral;
        frame.preset=StatePreset(frame.state);
        if(!fixed && (frame.state==FaceState::Idle || frame.state==FaceState::Speaking) &&
            emotion_!=FaceEmotion::Neutral) {
            frame.emotion=emotion_;frame.preset=EmotionPreset(emotion_);
        } else if(frame.state==FaceState::Thinking)frame.emotion=FaceEmotion::Thinking;
        if(frame.state==FaceState::Listening && f.now_us<interrupted_until_) {
            frame.emotion=FaceEmotion::Surprised;frame.preset=EmotionPreset(FaceEmotion::Surprised);
        }
        const int64_t animation_us=frame.emotion!=FaceEmotion::Neutral?emotion_us_:state_us_;
        frame.motion_ms=static_cast<uint32_t>((f.now_us>=animation_us?f.now_us-animation_us:0)/1000);
        if(frame.state==FaceState::Speaking && output) {
            const int64_t dt=f.now_us>=last_us_?f.now_us-last_us_:0;
            // Envelope observations are compressed PCM levels, never queued PCM.
            // A first valid block appears immediately; subsequent blocks smooth over 60ms.
            if(!mouth_ || dt>=60000)mouth_=o.level;
            else mouth_+=static_cast<int>((static_cast<int>(o.level)-mouth_)*dt/60000);
            frame.mouth_level=static_cast<uint8_t>(mouth_);
        } else mouth_=0; // no cached output survives invalid/stale physical evidence
        Present(frame,f.now_us);
        const bool interruption=frame.state==FaceState::Listening && f.now_us<interrupted_until_;
        const bool closed_mouth=frame.state==FaceState::Listening || frame.state==FaceState::Connecting ||
            frame.state==FaceState::Thinking || (frame.state==FaceState::Idle && post_speech_until_>0);
        if(!speaking && closed_mouth && !interruption) {
            // Shape crossovers must not prolong the last physical speech mouth.
            // The deliberate250ms interruption surprise keeps its static O.
            auto& mouth=frame.preset.mouth;
            if(mouth.shape==MouthShape::Open || mouth.shape==MouthShape::Grin ||
                mouth.shape==MouthShape::Tongue || mouth.shape==MouthShape::O)
                mouth.shape=StatePreset(frame.state).mouth.shape;
            mouth.open=0;
        }
        Animate(f,frame);
        was_speaking_=speaking;was_draining_=o.draining;
        last_state_=frame.state;last_us_=f.now_us;return frame;
    }
private:
    uint32_t Next() {rng_^=rng_<<13;rng_^=rng_>>17;rng_^=rng_<<5;return rng_;}
    int64_t RandomMs(uint32_t lo,uint32_t hi) {return int64_t(lo+Next()%(hi-lo+1))*1000;}
    bool Fresh(int64_t stamp,int64_t now)const {
        return stamp>0 && stamp>=since_ && stamp<=now && now-stamp<=kFreshUs;
    }
    static float Mix(float a,float b,float t){return a+(b-a)*t;}
    static uint32_t MixColor(uint32_t a,uint32_t b,float t) {
        uint32_t result=0;
        for(int shift=0;shift<=16;shift+=8)result|=static_cast<uint32_t>(Mix((a>>shift)&255,(b>>shift)&255,t)+.5f)<<shift;
        return result;
    }
    static bool SameEye(const EyeGeometry& a,const EyeGeometry& b) {
        return a.shape==b.shape && a.open==b.open && a.pupil_x==b.pupil_x && a.pupil_y==b.pupil_y;
    }
    static bool Same(const FacePreset& a,const FacePreset& b) {
        return SameEye(a.left,b.left) && SameEye(a.right,b.right) && a.mouth.shape==b.mouth.shape &&
            a.mouth.width==b.mouth.width && a.mouth.open==b.mouth.open && a.brow.visible==b.brow.visible &&
            a.brow.y==b.brow.y && a.brow.angle==b.brow.angle && a.brow.split==b.brow.split &&
            a.blush==b.blush && a.effects==b.effects && a.background==b.background && a.eye_color==b.eye_color;
    }
    static EyeGeometry MixEye(const EyeGeometry& a,const EyeGeometry& b,float t) {
        return {t<.5f?a.shape:b.shape,Mix(a.open,b.open,t),Mix(a.pupil_x,b.pupil_x,t),Mix(a.pupil_y,b.pupil_y,t)};
    }
    FacePreset CurrentPreset(int64_t now)const {
        if(now>=transition_us_+150000)return target_;
        const float t=now<=transition_us_?0:static_cast<float>(now-transition_us_)/150000;
        FacePreset p=t<.5f?from_:target_;
        p.left=MixEye(from_.left,target_.left,t);p.right=MixEye(from_.right,target_.right,t);
        p.mouth.width=Mix(from_.mouth.width,target_.mouth.width,t);p.mouth.open=Mix(from_.mouth.open,target_.mouth.open,t);
        p.brow.y=Mix(from_.brow.y,target_.brow.y,t);p.brow.angle=Mix(from_.brow.angle,target_.brow.angle,t);
        p.blush=Mix(from_.blush,target_.blush,t);p.background=MixColor(from_.background,target_.background,t);
        p.eye_color=MixColor(from_.eye_color,target_.eye_color,t);return p;
    }
    void Present(FaceFrame& frame,int64_t now) {
        if(!presentation_){from_=target_=frame.preset;transition_us_=now;presentation_=true;}
        else if(!Same(frame.preset,target_)){from_=CurrentPreset(now);target_=frame.preset;transition_us_=now;}
        frame.preset=CurrentPreset(now);
    }
    static float BlinkAt(int64_t delta) {
        if(delta<0 || delta>=160000)return 0;
        if(delta<60000)return static_cast<float>(delta)/60000;
        if(delta<80000)return 1;
        return static_cast<float>(160000-delta)/80000;
    }
    void Animate(const FaceFacts& f,FaceFrame& frame) {
        if(frame.state==FaceState::Error || frame.state==FaceState::Upgrading)return;
        const int64_t now=f.now_us;
        if(now>=next_blink_) {
            blink_us_=now;double_blink_=Next()%10==0;
            next_blink_=now+(frame.state==FaceState::LowBattery?10000000:RandomMs(3000,6000));
        }
        if(blink_us_>=0){frame.blink=BlinkAt(now-blink_us_);
            if(double_blink_ && now-blink_us_>=260000)frame.blink=BlinkAt(now-blink_us_-260000);}
        const float phase=static_cast<float>((now>=0?now:0)%4000000)/4000000*6.283185307f;
        frame.breath=1+(1-std::cos(phase))*(frame.state==FaceState::Speaking?0.00375f:0.0075f);
        if(frame.state==FaceState::Idle && frame.emotion!=FaceEmotion::Thinking) {
            if(now>=next_gaze_) {gaze_x_=static_cast<int>(Next()%201)-100;gaze_x_/=100;
                gaze_y_=static_cast<int>(Next()%201)-100;gaze_y_/=100;
                next_gaze_=now+RandomMs(2000,8000);}
            frame.gaze_x=gaze_x_;frame.gaze_y=gaze_y_; // saccade, never a slow scan
            if(now>=next_action_) {action_us_=now;action_sign_=Next()%2?1:-1;next_action_=now+RandomMs(30000,90000);}
            if(action_us_>=0 && now-action_us_<1200000)
                frame.tilt=action_sign_*6*std::sin(static_cast<float>(now-action_us_)/1200000*3.141592654f);
        } else if(frame.state==FaceState::Thinking || frame.emotion==FaceEmotion::Thinking) {
            frame.gaze_x=0.08f*std::sin(phase);frame.gaze_y=0.06f*std::cos(phase);
        }
        if(frame.emotion==FaceEmotion::Happy){frame.tilt=0;frame.bob_y=2*std::sin(phase*2);}
        if(frame.emotion==FaceEmotion::Loving)frame.breath+=0.008f*(1-std::cos(phase*2));
        if(frame.emotion==FaceEmotion::Delicious && frame.motion_ms<400)
            frame.tilt=3*std::sin(static_cast<float>(frame.motion_ms)/400*3.141592654f);
        if(shocked_ && frame.emotion==FaceEmotion::Surprised && frame.motion_ms<400)
            frame.tilt=(frame.motion_ms/40)%2?1.5f:-1.5f;
        if(frame.state==FaceState::Boot) {
            const int64_t age=now-state_us_;
            if(age<300000)frame.blink=1-static_cast<float>(age)/300000;
            else if(age<480000)frame.blink=BlinkAt(age-320000);
        }
    }
    FaceEmotion emotion_=FaceEmotion::Neutral; FaceState last_state_=FaceState::Hidden;
    int64_t emotion_us_=0,idle_since_=0,state_us_=0,last_us_=0,since_=0,gate_us_=0;
    int64_t post_speech_until_=0,interrupted_until_=0,transition_us_=0;
    int64_t next_blink_=0,blink_us_=-1,next_gaze_=0,next_action_=0,action_us_=-1;
    uint32_t capture_=0,epoch_=0,connection_=0,rng_=0x43484943;
    float gaze_x_=0,gaze_y_=0;int action_sign_=1,mouth_=0;
    FacePreset from_{},target_{};
    bool visible_=false,gate_confirmed_=false,double_blink_=false,shocked_=false;
    bool was_speaking_=false,was_draining_=false,presentation_=false;
};
} // namespace family_robot
