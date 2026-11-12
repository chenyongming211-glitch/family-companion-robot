// Real Initialize/SetFont/Update methods with deterministic LVGL geometry.
// Distinct LVGL enums deliberately retain the target C++20 warning surface.
#include <array>
#include <algorithm>
#include <atomic>
#include <cassert>
#include <cstdio>
#include <cstring>
#include <cstdlib>
#include <string>
#include <vector>
#include "face_layout.h"
#include "playback_progress.h"
#include <functional>
#include <ctime>
enum lv_obj_flag_t {LV_OBJ_FLAG_HIDDEN=1,LV_OBJ_FLAG_SCROLLABLE=2,LV_OBJ_FLAG_OVERFLOW_VISIBLE=4,LV_OBJ_FLAG_CLICKABLE=8};
enum lv_align_t {LV_ALIGN_TOP_MID,LV_ALIGN_CENTER};
enum lv_part_t {LV_PART_MAIN=0};
enum lv_state_t {LV_STATE_DEFAULT=0};
using lv_style_selector_t=uint32_t;
struct lv_font_t {int line_height;};
#if LV_FONT_MONTSERRAT_48
static const lv_font_t lv_font_montserrat_48{48};
#endif
static const lv_font_t family_robot_desktop_clock_digits_font{48};
static const lv_font_t family_robot_desktop_clock_font{44};
struct lv_point_t {int32_t x,y;};
constexpr int LV_TEXT_FLAG_NONE=0;
static void lv_text_get_size(lv_point_t* size,const char* text,const lv_font_t* font,int,int,int,int) {
    size->x=std::strlen(text)*font->line_height*3/5;size->y=font->line_height;
}
struct lv_obj_t {
    lv_obj_t* parent=nullptr;std::vector<lv_obj_t*> children;
    int x=0,y=0,w=128,h=128,transform_scale=256;uint32_t scale=256,writes=0;
    const lv_font_t* font=nullptr;bool label=false;unsigned flags=0;std::string text;
};
struct lv_timer_t {void* user=nullptr;bool paused=false;uint32_t period=67;};
struct lv_image_dsc_t {
    struct {int magic=0,cf=0,w=0,h=0,stride=0;} header;
    size_t data_size=0;const uint8_t* data=nullptr;
};
constexpr int LV_IMAGE_HEADER_MAGIC=0x19,LV_COLOR_FORMAT_RGB565=1,LV_TEXT_ALIGN_CENTER=1;
constexpr int LV_LABEL_LONG_WRAP=0,LV_RADIUS_CIRCLE=1000,MALLOC_CAP_SPIRAM=1,MALLOC_CAP_8BIT=2;
static lv_obj_t screen;
static lv_obj_t* lv_screen_active(){return &screen;}
static lv_obj_t* lv_obj_create(lv_obj_t* parent){auto* p=new lv_obj_t;p->parent=parent;if(parent)parent->children.push_back(p);return p;}
static lv_obj_t* lv_label_create(lv_obj_t* p){auto* o=lv_obj_create(p);o->label=true;return o;}
static lv_obj_t* lv_image_create(lv_obj_t* p){return lv_obj_create(p);}
static void lv_obj_delete(lv_obj_t* p){for(auto* c:p->children)lv_obj_delete(c);delete p;}
static void lv_obj_set_size(lv_obj_t* p,int w,int h){p->w=w;p->h=h;}
static void lv_obj_set_width(lv_obj_t* p,int w){p->w=w;}
static void lv_obj_set_pos(lv_obj_t* p,int x,int y){p->x=x;p->y=y;}
static void lv_obj_set_style_text_font(lv_obj_t* p,const lv_font_t* f,lv_style_selector_t){p->font=f;p->h=f->line_height;}
static void lv_obj_align(lv_obj_t* p,lv_align_t a,int x,int y){
    p->x=(p->parent->w-p->w)/2+x;p->y=(a==LV_ALIGN_CENTER?(p->parent->h-p->h)/2:0)+y;
}
static void lv_obj_set_style_radius(lv_obj_t*,int,lv_style_selector_t){}
static void lv_obj_set_style_border_width(lv_obj_t*,int,lv_style_selector_t){}
static void lv_obj_set_style_pad_all(lv_obj_t*,int,lv_style_selector_t){}
static void lv_obj_set_style_bg_color(lv_obj_t*,uint32_t,lv_style_selector_t){}
static void lv_obj_set_style_border_color(lv_obj_t*,uint32_t,lv_style_selector_t){}
static void lv_obj_set_style_text_color(lv_obj_t*,uint32_t,lv_style_selector_t){}
static void lv_obj_set_style_text_align(lv_obj_t*,int,lv_style_selector_t){}
static void lv_obj_set_style_transform_scale_x(lv_obj_t* p,int s,lv_style_selector_t){p->transform_scale=s;}
static void lv_obj_set_style_transform_scale_y(lv_obj_t*,int,lv_style_selector_t){}
static void lv_obj_set_style_transform_pivot_x(lv_obj_t*,int,lv_style_selector_t){}
static void lv_obj_set_style_transform_pivot_y(lv_obj_t*,int,lv_style_selector_t){}
static uint32_t lv_color_hex(uint32_t color){return color;}
static void lv_obj_add_flag(lv_obj_t* p,lv_obj_flag_t f){p->flags|=f;}
static void lv_obj_remove_flag(lv_obj_t* p,lv_obj_flag_t f){p->flags&=~f;}
static void lv_obj_move_to_index(lv_obj_t*,int){}
static void lv_label_set_long_mode(lv_obj_t*,int){}
static void lv_label_set_text(lv_obj_t* p,const char* text){p->text=text;++p->writes;}
static void lv_image_set_src(lv_obj_t* p,lv_image_dsc_t* d){p->w=d->header.w;p->h=d->header.h;}
static void lv_image_set_scale(lv_obj_t* p,uint32_t scale){p->scale=scale;}
static void lv_image_set_antialias(lv_obj_t*,bool){}
static void lv_obj_invalidate(lv_obj_t*){}
static lv_timer_t* lv_timer_create(void (*)(lv_timer_t*),uint32_t period,void* user){return new lv_timer_t{user,false,period};}
static void* lv_timer_get_user_data(lv_timer_t* p){return p->user;}
static void lv_timer_delete(lv_timer_t* p){delete p;}
static void lv_timer_pause(lv_timer_t* p){p->paused=true;}
static void lv_timer_resume(lv_timer_t* p){p->paused=false;}
static void lv_timer_set_period(lv_timer_t* p,uint32_t v){p->period=v;}
static bool allocation_fails=false;
static void* heap_caps_malloc(size_t n,int){return allocation_fails?nullptr:std::malloc(n);}
static void heap_caps_free(void* p){std::free(p);}
static uint32_t draw_us=1000;
static int64_t esp_timer_get_time(){static int64_t now=1000000;return now+=draw_us;}

#define ESP_LOGW(...) ((void)0)
static unsigned stack_samples=0;
static unsigned uxTaskGetStackHighWaterMark(void*){++stack_samples;return 4096;}
static const char* pcTaskGetName(void*){return "lvgl";}
static std::string profile_log;
template<class... Args> static void HostLog(const char* format,Args... args) {
    if(std::strstr(format,"frames=")) {
        std::array<char,256> line{};std::snprintf(line.data(),line.size(),format,args...);
        profile_log=line.data();
    }
}
#define ESP_LOGI(tag,...) HostLog(__VA_ARGS__)
#define private public
#include "chick_face_view.h"
#undef private
// PRODUCTION_VIEW
// Only task/transport boundaries are faked; the scheduled stop body below is
// extracted from the production Application include.
enum {kDeviceStateSpeaking,kDeviceStateListening,kDeviceStateIdle,kListeningModeAutoStop,kListeningModeManualStop};
struct StopProtocol {
    bool PlaybackEventCurrent(family_robot::PlaybackEvent event)const{return bool(event);}
};
class Application {
public:
    StopProtocol owner,*protocol_=&owner;
    struct Audio {void ResetDecoder(){}} audio_service_;
    int state=kDeviceStateSpeaking,listening_mode_=kListeningModeAutoStop;
    std::vector<std::function<void()>> scheduled;
    void Schedule(std::function<void()> fn){scheduled.push_back(std::move(fn));}
    int GetDeviceState()const{return state;}void SetDeviceState(int next){state=next;}
    void SchedulePlaybackStop(family_robot::PlaybackEvent,bool);
    void Run(){auto work=std::move(scheduled);scheduled.clear();for(auto& fn:work)fn();}
};
// PRODUCTION_PLAYBACK_STOP
static void SealedPendingView() {
    using namespace family_robot;
    for(int mode=0;mode<4;++mode) {
        allocation_fails=true;lv_font_t font{30};ChickFaceView view;
        view.Initialize(&font,480,480,[](void*){},nullptr);Application app;
        PlaybackProgress ledger;ledger.Connect(50);const PlaybackIdentity id{50,1};
        assert(ledger.Start(id));assert(ledger.Admit(id,0,1440));
        std::array<int16_t,240> pcm{};pcm.fill(2000);
        for(size_t i=0;i<6;++i)assert(ledger.Submit(id,i*240,pcm.data(),240,180)==PlaybackProgress::SubmitResult::Accepted);
        ledger.Release(id);
        FaceFacts f;f.now_us=1000000;f.state=FaceState::Speaking;f.audio.output=ledger.OrbActivity();
        f.audio.gate_open=true;assert(view.Update(f));
        if(mode!=3)assert(view.SetPlaybackEmotion({50,1},"happy",f.now_us));
        if(mode==1)ledger.MarkOrbDraining(id);
        ledger.Seal(id,mode==2);
        app.SchedulePlaybackStop({id,1},mode==2);app.Run();assert(app.state==kDeviceStateListening);
        f.state=FaceState::Listening;f.audio.output=ledger.OrbActivity();assert(view.Update(f));
        assert(view.emotion_==FaceEmotion::Neutral && view.state_!=FaceState::Speaking);
        if(mode==3)assert(view.SetPlaybackEmotion({50,1},"happy",f.now_us)); // label arrives after logical stop
        std::array<std::array<int16_t,240>,6> dma{};
        for(size_t i=0;i<12;++i) {
            f.now_us+=10000;ledger.Complete(dma[i%6].data(),240,f.now_us);
            f.audio.output=ledger.OrbActivity();assert(view.Update(f));
            if(i<6 || mode==2)assert(view.state_!=FaceState::Speaking && view.emotion_==FaceEmotion::Neutral);
            else if(i==6)assert(view.state_==FaceState::Speaking && view.emotion_==((mode==0 || mode==3)?FaceEmotion::Happy:FaceEmotion::Neutral));
        }
        // A real subsequent input takeover or hard cancellation cannot revive it.
        f.now_us+=10000;f.state=FaceState::Listening;f.audio.output=ledger.OrbActivity();view.Update(f);
        f.now_us+=2000001;f.state=FaceState::Idle;view.Update(f);assert(view.emotion_==FaceEmotion::Neutral);
    }
}
int main() {
    using namespace family_robot;
    SealedPendingView();
    for(const int size:{240,466,480})for(const bool fail:{false,true}) {
        allocation_fails=fail;lv_font_t font{30};
        ChickFaceView view;
        view.Initialize(&font,size,size==240?320:size,[](void*){},nullptr);
        assert(view.Ready() && (view.pixels_==nullptr)==fail);
        assert(view.caption_->font==&font);
        FaceFacts facts;facts.now_us=1000000;
        assert(view.Update(facts));assert(view.caption_->text=="待机中");
        view.SetEmotion("happy",facts.now_us);facts.now_us+=100000;
        assert(view.Update(facts));assert(view.caption_->text=="待机中");
        view.SetThinking(true);facts.now_us+=100000;assert(view.Update(facts));
        assert(view.caption_->text=="正在思考");
        view.SetThinking(false);view.SetMessage("请检查网络连接");
        assert(view.Update(facts));assert(view.caption_->text=="请检查网络连接");
        view.SetMessage("");assert(view.Update(facts));assert(view.caption_->text=="待机中");
        facts.state=FaceState::Connecting;assert(view.Update(facts));
        assert(view.caption_->text=="正在连接");
        facts.state=FaceState::Listening;assert(view.Update(facts));
        assert(view.caption_->text=="正在准备语音");
        facts.audio.output.draining=true;assert(view.Update(facts));
        assert(view.caption_->text=="正在停播");assert(view.state_!=FaceState::Speaking);
        facts.state=FaceState::Error;assert(view.Update(facts));
        assert(view.caption_->text=="设备异常，请重试");
        facts.state=FaceState::Upgrading;assert(view.Update(facts));assert(view.caption_->text=="正在升级");
        facts.audio.output.draining=false;facts.state=FaceState::Idle;
        facts.quiet=true;facts.now_us+=200000;assert(view.Update(facts));
        assert(view.timer_->period==1000u); // quiet standby is a static clock
        view.SetScreenOn(false);assert(!view.Update(facts));
        assert(view.timer_->paused && (view.root_->flags&LV_OBJ_FLAG_HIDDEN));
        view.SetScreenOn(true);facts.system_page=true;assert(!view.Update(facts));
        assert(view.root_->flags&LV_OBJ_FLAG_HIDDEN);
        facts.system_page=false;assert(view.Update(facts));
        assert(!(view.root_->flags&LV_OBJ_FLAG_HIDDEN));
        if(!fail) {
            assert(ChickFaceConsole("face_ui static"));facts.now_us+=200000;assert(view.Update(facts));
            const auto count=rendered_frames.load();
            facts.now_us+=200000;assert(view.Update(facts));assert(rendered_frames.load()==count);
            assert(ChickFaceConsole("face_ui on"));facts.quiet=false;facts.now_us+=200000;
            assert(view.Update(facts));assert(rendered_frames.load()>count);
        }
    }
    // Production forwarding uses immutable receive time; pending does not mean speech.
    {
        allocation_fails=true;lv_font_t font{30};ChickFaceView view;
        view.Initialize(&font,480,480,[](void*){},nullptr);
        FaceFacts f;f.now_us=10000000;f.audio.output.connection=8;
        assert(view.Update(f));assert(view.SetPlaybackEmotion({8,1},"happy",f.now_us));
        assert(view.Update(f) && view.emotion_==FaceEmotion::Neutral);
        auto& o=f.audio.output;f.now_us+=10000;
        o.identity={8,1};o.active=o.valid=true;o.level=120;o.stamp_us=f.now_us;
        o.playbacks[0]={{8,1},f.now_us,f.now_us,true,false,false,false};
        assert(view.Update(f) && view.emotion_==FaceEmotion::Happy && view.state_==FaceState::Speaking);
        assert(view.caption_->text=="正在说话");
        // A second early label cannot change the actual old tail.
        assert(view.SetPlaybackEmotion({8,2},"sad",f.now_us));
        o.draining=true;o.playbacks[0].draining=true;f.now_us+=10000;
        o.stamp_us=o.playbacks[0].last_us=f.now_us;
        assert(view.Update(f) && view.emotion_==FaceEmotion::Happy);
        o.playbacks[0].terminal=true;o.draining=false;o.identity={8,2};
        f.now_us+=10000;o.stamp_us=f.now_us;
        o.playbacks[1]={{8,2},f.now_us,f.now_us,true,false,false,false};
        assert(view.Update(f) && view.emotion_==FaceEmotion::Sad);
        view.SetScreenOn(false);assert(!view.Update(f));
        f.now_us+=10000;o.active=false;o.playbacks[1].terminal=true;
        o.playbacks[1].last_us=f.now_us;assert(!view.Update(f));
        f.now_us+=2000001;view.SetScreenOn(true);
        assert(view.Update(f) && view.emotion_==FaceEmotion::Neutral);
        // Queue delay must not turn a receive-time-eligible late label into a new late arrival.
        o={};o.connection=9;f.now_us=20000000;assert(view.Update(f));
        const int64_t first=f.now_us,arrival=first+1999999;
        o.identity={9,1};o.active=o.valid=true;o.level=80;o.stamp_us=f.now_us;
        o.playbacks[0]={{9,1},first,f.now_us,true,false,false,false};assert(view.Update(f));
        f.now_us=first+2100000;o.stamp_us=o.playbacks[0].last_us=f.now_us;
        assert(view.Update(f)); // a timer frame may run before the queued event
        assert(view.SetPlaybackEmotion({9,1},"loving",arrival));
        assert(view.Update(f) && view.emotion_==FaceEmotion::Loving);
        view.ClearPlaybackEmotions();assert(view.Update(f) && view.emotion_==FaceEmotion::Neutral);
        // An early receive can be dequeued after the first DMA was observed.
        o={};o.connection=10;f.now_us=25000000;assert(view.Update(f));
        const auto early_arrival=f.now_us;f.now_us+=10000;
        o.identity={10,1};o.active=o.valid=true;o.level=80;o.stamp_us=f.now_us;
        o.playbacks[0]={{10,1},f.now_us,f.now_us,true,false,false,false};assert(view.Update(f));
        assert(view.SetPlaybackEmotion({10,1},"delicious",early_arrival));
        const auto early=view.model_.Update(f);
        assert(early.emotion==FaceEmotion::Delicious && early.motion_ms==0);
    }
    {
        allocation_fails=true;lv_font_t font{30};ChickFaceView view;
        view.Initialize(&font,480,480,[](void*){},nullptr);
        FaceFacts f;f.now_us=28000000;f.audio.output.connection=40;
        view.SetLegacyEmotion(40,"shocked",f.now_us); // first event before first observation
        assert(view.Update(f) && view.emotion_==FaceEmotion::Surprised);
        f.now_us+=100000;f.audio.output.connection=41;
        assert(view.Update(f) && view.emotion_==FaceEmotion::Neutral);
        view.SetLegacyEmotion(41,"loving",f.now_us);
        assert(view.Update(f) && view.emotion_==FaceEmotion::Loving);
        view.ClearPlaybackEmotions();assert(view.Update(f) && view.emotion_==FaceEmotion::Neutral);
        view.SetEmotion("sleepy",f.now_us);f.now_us+=100000;f.audio.output.connection=42;
        assert(view.Update(f) && view.emotion_==FaceEmotion::Sleepy);
    }
    {
        // Sample the current LVGL owner once at the existing profiling point.
        allocation_fails=false;lv_font_t font{30};ChickFaceView view;
        view.Initialize(&font,480,480,[](void*){},nullptr);
        FaceFacts f;f.now_us=30000000;view.frames_=149;
        assert(stack_samples==0);assert(view.Update(f));assert(stack_samples==1);
        assert(profile_log.find("stack_min_free_bytes=4096")!=std::string::npos);
        f.now_us+=100000;assert(view.Update(f));assert(stack_samples==1);
    }
    std::puts("CHICK real view captions, lifecycle, layout and fallback passed");
}
