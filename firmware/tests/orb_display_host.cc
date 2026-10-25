#include "orb_model.h"
#include <cassert>
#include <cstring>
#include <memory>
#include <string>
#include <mutex>
#include <atomic>
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
        for(auto* icon:{network_label_,mute_label_,battery_label_})
            lv_obj_set_style_text_color(icon,p->text_color,0);
    }
};
namespace family_robot {
class XiaomanOrbView {
public:
    OrbModel model;bool ready=false,screen=true,save=false,shown=false;int fonts=0;OrbFacts last{};
    bool Ready()const{return ready;}
    void Initialize(int*,void (*)(void*),void*){ready=true;}
    void SetFont(int*){++fonts;}
    void SetScreenOn(bool on){screen=on;}
    void SetPowerSave(bool on){save=on;}
    bool Update(OrbFacts f){f.screen_on=screen&&!save;last=f;return shown=model.Update(f).phase!=OrbPhase::Hidden;}
};
}
enum DeviceState{kDeviceStateIdle,kDeviceStateConnecting,kDeviceStateListening,kDeviceStateSpeaking,kDeviceStateNotifying,kDeviceStateWifiConfiguring};
struct AudioServiceFake {family_robot::OrbAudioActivity facts{};family_robot::OrbAudioActivity GetOrbAudioActivity(){++audio_reads;return facts;}};
struct Application {
    DeviceState state=kDeviceStateIdle;AudioServiceFake audio;
    static Application& GetInstance(){static Application value;return value;}
    DeviceState GetDeviceState(){return state;}AudioServiceFake& GetAudioService(){return audio;}
};
class RoundLcdDisplay : public SpiLcdDisplay {
public:
    void SetupUI() override {SpiLcdDisplay::SetupUI();SetupOrbUI();}
#include "xiaoman_orb_display.inc"
public:
    bool OrbShown()const{return orb_.shown;}
    bool OrbScreenOn()const{return orb_.last.screen_on;}
};

class Codec {
public:
    uint32_t capture=4;bool fault=false,software=true;
    uint32_t CaptureGeneration(){return capture;}
    bool SoftwareReference(){return software;}
    bool ReferenceFaulted(){return fault;}
    uint32_t ReferenceIntegrity(){return 7;}
    family_robot::OrbOutputActivity GetOrbOutputActivity(){return {};}
};
class AudioService {
public:
    std::mutex audio_queue_mutex_;Codec* codec_=nullptr;family_robot::ExperimentGate experiment_gate_;
    family_robot::UplinkAuthorization experiment_authorization_;
    std::atomic<uint32_t> capture_generation_{4};family_robot::OrbInputActivity orb_input_activity_{};
    bool processor=false;bool IsAudioProcessorRunning(){return processor;}
    family_robot::OrbAudioActivity GetOrbAudioActivity();
};
#include "orb_audio_service.inc"
int main(){
    RoundLcdDisplay display;
    // Board RestoreBrightness invokes this before SetupUI or audio Initialize.
    display.SetOrbScreenOn(true);display.SetOrbScreenOn(false);
    assert(mutations==0 && audio_reads==0);
    display.SetupUI();display.SetOrbScreenOn(true);
    display.top.state=display.network.state=LV_STATE_CHECKED;
    auto chrome=[&](bool active){
        assert(lv_obj_get_style_bg_opa(&display.top,LV_PART_MAIN)==(active?LV_OPA_TRANSP:LV_OPA_50));
        assert(display.top.bg_opa==LV_OPA_50); // orb never rewrites default theme
        assert((lv_obj_get_state(&display.top)&LV_STATE_CHECKED)!=0);
        for(auto* icon:{&display.network,&display.mute,&display.battery_icon}) {
            assert(!icon->hidden);
            assert(lv_obj_get_style_text_color(icon,LV_PART_MAIN)==(active?0xc5ccd8:display.current_theme_->text_color));
            assert(icon->text_color==display.current_theme_->text_color);
        }
        assert((lv_obj_get_state(&display.network)&LV_STATE_CHECKED)!=0);
    };
    chrome(false);
    auto& app=Application::GetInstance();app.state=kDeviceStateListening;app.audio.facts.gate_open=true;
    display.SetStatus("listening");assert(display.OrbShown()&&display.emoji.hidden&&display.status.hidden);
    chrome(true);
    Theme dark;dark.text_color=0xffffff;
    display.SetTheme(&dark);chrome(true); // a live theme update cannot erase orb contrast
    display.SetTheme(&display.theme);chrome(true);
    const auto writes=display.chat_writes;
    display.SetChatMessage("user","private transcript");display.SetChatMessage("assistant","private reply");
    assert(display.chat_writes==writes);
    display.SetChatMessage("system","network error");assert(!display.OrbShown()&&!display.bottom.hidden);chrome(false);
    display.ClearChatMessages();assert(display.OrbShown());chrome(true);
    display.ShowNotification("low battery");assert(!display.OrbShown()&&!display.notification.hidden);chrome(false);
    display.SetStatus("listening");assert(display.OrbShown()&&display.status.hidden);
    display.SetOrbScreenOn(false);assert(!display.OrbShown()&&!display.OrbScreenOn());chrome(false);
    display.SetOrbScreenOn(true);assert(display.OrbShown());chrome(true);
    display.SetPowerSaveMode(true);assert(!display.OrbShown());
    display.SetPowerSaveMode(false);assert(display.OrbShown());
    display.battery.hidden=false;display.SetEmotion("happy");assert(!display.OrbShown());
    display.battery.hidden=true;display.SetEmotion("happy");assert(display.OrbShown());
    display.SetPreviewImage(std::make_unique<LvglImage>());
    assert(!display.OrbShown() && display.emoji.hidden);chrome(false);
    display.SetStatus("preview active");
    assert(!display.OrbShown() && display.emoji.hidden);chrome(false);
    display.SetPreviewImage(nullptr);assert(display.OrbShown());
    app.state=kDeviceStateNotifying;display.SetStatus("notifying");assert(!display.OrbShown());
    const auto notification_writes=display.chat_writes;
    display.SetChatMessage("assistant","existing notification text");
    assert(display.chat_writes==notification_writes+1 && !display.bottom.hidden);
    app.state=kDeviceStateIdle;display.SetChatMessage("assistant","stale conversation reply");
    assert(display.chat_writes==notification_writes+1);
    app.state=kDeviceStateWifiConfiguring;display.SetStatus("setup");assert(!display.OrbShown());chrome(false);
    app.state=kDeviceStateIdle;display.SetStatus("clock");assert(!display.OrbShown()&&!display.status.hidden);chrome(false);
    // Observer getter uses actual gate and current capture, and fails closed on
    // contention/processor stop rather than obtaining a second audio lock.
    Codec codec;AudioService service;service.codec_=&codec;service.processor=true;
    service.experiment_authorization_.Request(true,7);service.experiment_gate_.Set(true,4);
    assert(service.GetOrbAudioActivity().gate_open);
    service.capture_generation_=3;assert(!service.GetOrbAudioActivity().gate_open);
    service.capture_generation_=4;service.processor=false;assert(!service.GetOrbAudioActivity().gate_open);
    service.processor=true;service.experiment_gate_.Set(false,4);assert(!service.GetOrbAudioActivity().gate_open);
    puts("production orb boot/visibility/system priority/power/caption/gate integration passed");
}
