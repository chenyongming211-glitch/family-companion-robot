#include "chick_face_model.h"
#include <cstdio>
#include <cassert>
#include <cstring>
#include <memory>
#include <string>
#include <mutex>
#include <atomic>
#include <thread>
#include <future>
#include "experiment_gate.h"

static int mutations=0,audio_reads=0;
// Keep the distinct LVGL enum types: C++20+ rejects their implicit bitwise
// combination under the target's deprecated-enum-enum-conversion warning.
enum lv_state_t {LV_STATE_DEFAULT=0,LV_STATE_CHECKED=0x0001,LV_STATE_USER_4=0x8000};
enum lv_part_t {LV_PART_MAIN=0,LV_PART_SCROLLBAR=0x010000};
using lv_style_selector_t=uint32_t;
using lv_color_t=uint32_t;
constexpr int LV_OPA_TRANSP=0,LV_OPA_50=127;
struct lv_obj_t {
    bool hidden=false;
    lv_state_t state=LV_STATE_DEFAULT;
    int bg_opa=LV_OPA_50,orb_bg_opa=-1;
    lv_color_t text_color=0,orb_text_color=0;
    bool has_orb_text=false;
};
static inline lv_color_t lv_color_hex(uint32_t color){return color;}
static inline lv_state_t lv_obj_get_state(lv_obj_t* p){return p->state;}
static inline void lv_obj_add_state(lv_obj_t* p,lv_state_t state){++mutations;p->state=static_cast<lv_state_t>(p->state|state);}
static inline void lv_obj_remove_state(lv_obj_t* p,lv_state_t state){++mutations;p->state=static_cast<lv_state_t>(p->state&~state);}
static inline void lv_obj_set_style_bg_opa(lv_obj_t* p,int opacity,lv_style_selector_t selector){
    ++mutations;
    if(selector==0)p->bg_opa=opacity;
    else {assert(selector==LV_STATE_USER_4);p->orb_bg_opa=opacity;}
}
static inline void lv_obj_set_style_text_color(lv_obj_t* p,lv_color_t color,lv_style_selector_t selector){
    ++mutations;
    if(selector==0)p->text_color=color;
    else {assert(selector==LV_STATE_USER_4);p->orb_text_color=color;p->has_orb_text=true;}
}
// Matches LVGL's state-specific selector precedence over default local style.
static int lv_obj_get_style_bg_opa(lv_obj_t* p,lv_part_t){
    return p->state&LV_STATE_USER_4 && p->orb_bg_opa>=0?p->orb_bg_opa:p->bg_opa;
}
static lv_color_t lv_obj_get_style_text_color(lv_obj_t* p,lv_part_t){
    return p->state&LV_STATE_USER_4 && p->has_orb_text?p->orb_text_color:p->text_color;
}
constexpr int LV_OBJ_FLAG_HIDDEN=1;
static bool lv_obj_has_flag(lv_obj_t* p,int){assert(p);return p->hidden;}
static void lv_obj_add_flag(lv_obj_t* p,int){assert(p);++mutations;p->hidden=true;}
static void lv_obj_remove_flag(lv_obj_t* p,int){assert(p);++mutations;p->hidden=false;}
static int64_t now=1000000;
static int64_t esp_timer_get_time(){return now;}
struct DisplayLockGuard {template<class T>explicit DisplayLockGuard(T*){} explicit operator bool()const{return true;}};
struct LvglImage {};
struct Font {int* font(){return nullptr;}};
struct Theme {uint32_t text_color=0;std::shared_ptr<Font> GetTextFont(){return std::make_shared<Font>();}};
class SpiLcdDisplay {
public:
    virtual ~SpiLcdDisplay()=default;
    bool setup=false;int chat_writes=0;Theme theme;Theme* current_theme_=&theme;
    lv_obj_t emoji,status,bottom,preview{true},battery{true},notification{true};
    lv_obj_t top,network,mute,battery_icon;
    lv_obj_t *top_bar_=nullptr,*network_label_=nullptr,*mute_label_=nullptr,*battery_label_=nullptr;
    lv_obj_t *emoji_box_=nullptr,*status_label_=nullptr,*bottom_bar_=nullptr,*preview_image_=nullptr,*low_battery_popup_=nullptr,*notification_label_=nullptr;
    bool IsSetupUICalled()const{return setup;}
    virtual void SetupUI(){setup=true;emoji_box_=&emoji;status_label_=&status;bottom_bar_=&bottom;preview_image_=&preview;low_battery_popup_=&battery;notification_label_=&notification;
        top_bar_=&top;network_label_=&network;mute_label_=&mute;battery_label_=&battery_icon;
    }
    virtual void SetStatus(const char*){if(status_label_)lv_obj_remove_flag(status_label_,1);if(notification_label_)lv_obj_add_flag(notification_label_,1);}
    virtual void SetEmotion(const char*){if(emoji_box_)lv_obj_remove_flag(emoji_box_,1);}
    virtual void SetThinking(bool){}
    virtual void SetChatMessage(const char*,const char* text){++chat_writes;bottom.hidden=!text||!*text;}
    virtual void ClearChatMessages(){bottom.hidden=true;}
    virtual void SetPowerSaveMode(bool){}
    virtual void ShowNotification(const char*,int=3000){notification.hidden=false;status.hidden=true;}
    virtual void ShowNotification(const std::string&,int=3000){}
    virtual void SetPreviewImage(std::unique_ptr<LvglImage> p){
        preview.hidden=!p;
        emoji.hidden=bool(p); // matches pinned LcdDisplay: preview owns emoji visibility
    }
    virtual void SetTheme(Theme* p){current_theme_=p;
        // Pinned LcdDisplay::SetTheme rewrites default selector0, not USER state.
        lv_obj_set_style_bg_opa(top_bar_,LV_OPA_50,0);
        for(auto* icon:{network_label_,mute_label_,battery_label_,notification_label_})
            lv_obj_set_style_text_color(icon,p->text_color,0);
    }
};

#define LV_HOR_RES 480
#define LV_VER_RES 480
namespace family_robot {
class ChickFaceView {
public:
 FaceModel model;bool ready=false,screen=true,quiet=false,thinking=false;FaceFacts last{};FaceFrame frame{};std::string message;
 bool Ready()const{return ready;}
 void Initialize(int*,int width,int height,void (*)(void*),void*){assert(width==480&&height==480);ready=true;}
 void SetFont(int*){}
 void SetScreenOn(bool on){screen=on;}void SetQuiet(bool on){quiet=on;}
 void SetThinking(bool on){thinking=on;}
 void SetEmotion(const char* name,int64_t time){model.SetEmotion(name,time);}
 void SetMessage(const char* text){message=text?text:"";}
 bool Update(FaceFacts f){f.screen_on=f.screen_on&&screen;f.quiet=f.quiet||quiet;f.model_thinking=f.model_thinking||thinking;last=f;frame=model.Update(f);return frame.shown;}
};
}
enum DeviceState{kDeviceStateUnknown,kDeviceStateStarting,kDeviceStateWifiConfiguring,kDeviceStateIdle,kDeviceStateConnecting,kDeviceStateListening,kDeviceStateSpeaking,kDeviceStateNotifying,kDeviceStateUpgrading,kDeviceStateActivating,kDeviceStateAudioTesting,kDeviceStateFatalError};
struct AudioServiceFake {family_robot::OrbAudioActivity facts{};family_robot::OrbAudioActivity GetOrbAudioActivity(){++audio_reads;return facts;}};
struct Application {DeviceState state=kDeviceStateIdle;AudioServiceFake audio;static Application& GetInstance(){static Application a;return a;}DeviceState GetDeviceState(){return state;}AudioServiceFake& GetAudioService(){return audio;}};
struct Board {int reads=0;static Board& GetInstance(){static Board b;return b;}bool GetBatteryLevel(int& level,bool& charging,bool& discharging){++reads;level=80;charging=false;discharging=true;return true;}};
class RoundLcdDisplay:public SpiLcdDisplay {
public:
 void SetupUI()override{SpiLcdDisplay::SetupUI();SetupFaceUI();}
#include "chick_face_display.inc"
public:
 auto& Face(){return chick_face_;}
};
int main(){
 using namespace family_robot;
 RoundLcdDisplay d;d.SetFaceScreenOn(true);assert(audio_reads==0&&mutations==0);
 d.SetupUI();d.SetStatus("ready");assert(d.Face().frame.state==FaceState::Idle&&d.Face().frame.shown);
 assert(d.emoji.hidden&&d.bottom.hidden&&d.status.hidden);assert(Board::GetInstance().reads==1);
 for(int i=0;i<10;++i){now+=33000;d.SetStatus("ready");}assert(Board::GetInstance().reads==1);
 d.SetPowerSaveMode(true);assert(d.Face().last.quiet&&d.Face().frame.state==FaceState::Idle);
 assert(d.Face().frame.emotion!=FaceEmotion::Sleepy);
 d.SetEmotion("sleepy");assert(d.Face().frame.emotion==FaceEmotion::Sleepy);
 d.SetPowerSaveMode(false);assert(!d.Face().last.quiet);d.SetEmotion("neutral");
 auto& app=Application::GetInstance();app.state=kDeviceStateListening;app.audio.facts.capture=7;app.audio.facts.epoch=9;
 d.SetStatus("listen");assert(d.Face().frame.state==FaceState::Connecting);
 app.audio.facts.gate_open=true;d.SetStatus("listen");assert(d.Face().frame.state==FaceState::Listening);
 d.SetThinking(true);assert(d.Face().frame.state==FaceState::Thinking);
 d.SetEmotion("happy");assert(d.Face().frame.state==FaceState::Thinking);
 d.SetThinking(false);assert(d.Face().frame.state==FaceState::Listening);
 d.SetPreviewImage(std::make_unique<LvglImage>());assert(!d.Face().frame.shown&&d.emoji.hidden);
 d.SetPreviewImage(nullptr);assert(d.Face().frame.shown&&d.emoji.hidden);
 d.SetThinking(true);assert(d.Face().frame.state==FaceState::Thinking);
 app.state=kDeviceStateIdle;d.SetChatMessage("system","connection trouble");
 assert(d.Face().frame.state==FaceState::Notifying&&d.Face().message=="connection trouble");
 assert(!d.Face().thinking);
 d.ClearChatMessages();assert(d.Face().frame.state==FaceState::Idle);
 d.ShowNotification("音量已调整");assert(!d.notification.hidden);
 assert(lv_obj_get_style_text_color(&d.notification,LV_PART_MAIN)==0xffd8a8);
 Theme light;light.text_color=0;d.SetTheme(&light);
 assert(lv_obj_get_style_text_color(&d.notification,LV_PART_MAIN)==0xffd8a8);
 d.SetPreviewImage(std::make_unique<LvglImage>());
 assert(lv_obj_get_style_text_color(&d.notification,LV_PART_MAIN)==0);
 d.SetPreviewImage(nullptr);
 assert(lv_obj_get_style_text_color(&d.notification,LV_PART_MAIN)==0xffd8a8);
 d.SetFaceScreenOn(false);assert(!d.Face().frame.shown&&d.emoji.hidden);
 d.SetFaceScreenOn(true);assert(d.Face().frame.shown);
 app.state=kDeviceStateFatalError;d.SetEmotion("happy");assert(d.Face().frame.state==FaceState::Error);
 app.state=kDeviceStateUpgrading;d.SetStatus("upgrade");assert(d.Face().frame.state==FaceState::Upgrading);
 std::puts("CHICK display lifecycle checks passed");
}
