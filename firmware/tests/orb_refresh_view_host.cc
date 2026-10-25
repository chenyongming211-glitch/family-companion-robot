// Platform plumbing is fake; console parsing and the whole Update method are
// extracted unchanged from production, using the real class and policy headers.
#include <array>
#include <atomic>
#include <cassert>
#include <cstdio>
#include <cstring>
#include <cstdint>
struct lv_obj_t {bool hidden=false;const char* text="";uint32_t scale=256,invalidations=0;};
struct lv_timer_t {bool paused=true;uint32_t period=67;};
struct lv_font_t {uint32_t line_height=24;};
struct lv_image_dsc_t {};
constexpr int LV_OBJ_FLAG_HIDDEN=1;
static void lv_obj_add_flag(lv_obj_t* p,int){p->hidden=true;}
static void lv_obj_remove_flag(lv_obj_t* p,int){p->hidden=false;}
static void lv_timer_pause(lv_timer_t* p){p->paused=true;}
static void lv_timer_resume(lv_timer_t* p){p->paused=false;}
static void lv_timer_set_period(lv_timer_t* p,uint32_t period){p->period=period;}
static void lv_label_set_text_static(lv_obj_t* p,const char* text){p->text=text;}
static void lv_image_set_scale(lv_obj_t* p,uint32_t scale){p->scale=scale;}
static void lv_obj_invalidate(lv_obj_t* p){++p->invalidations;}
static int64_t clock_us=1000000;
static uint32_t draw_us=10000;
static int64_t esp_timer_get_time(){clock_us+=draw_us;return clock_us;}
#define private public
#include "xiaoman_orb_view.h"
#undef private
namespace family_robot {
namespace {
std::atomic<uint32_t> motion_request{2};
std::atomic<int> effective_mode{2};
std::atomic<uint32_t> frame_count{0},max_draw_us{0},buffer_bytes{32768},slow_frames{0};
void Hidden(lv_obj_t* p,bool hidden){if(hidden)lv_obj_add_flag(p,1);else lv_obj_remove_flag(p,1);}
}
XiaomanOrbView::~XiaomanOrbView()=default; // stack-owned fake platform objects
// PRODUCTION_METHODS
}
int main(){
    using namespace family_robot;
    XiaomanOrbView view;
    lv_obj_t root,caption,hint,image;lv_timer_t timer;
    std::array<uint16_t,OrbRenderer::kSide*OrbRenderer::kSide> pixels{};
    view.root_=&root;view.caption_=&caption;view.hint_=&hint;view.image_=&image;
    view.timer_=&timer;view.pixels_=pixels.data();
    OrbFacts facts{};facts.session=true;facts.audio.gate_open=true;facts.now_us=1000000;
    auto update=[&](uint32_t cost){facts.now_us+=200000;draw_us=cost;return view.Update(facts);};
    // Observed cold spike lowers the actual timer without changing request2.
    assert(update(138822));
    assert(effective_mode.load()==1 && (motion_request.load()&3u)==2 && timer.period==200);
    for(int i=0;i<9;++i){assert(update(20000));assert(effective_mode.load()==1);}
    assert(update(20000));assert(effective_mode.load()==2 && timer.period==67);
    assert(XiaomanOrbConsole("orb_ui low"));
    for(int i=0;i<20;++i) assert(update(5000));
    assert(effective_mode.load()==1 && timer.period==200);
    assert(XiaomanOrbConsole("orb_ui off"));assert(update(5000));
    const auto static_frames=frame_count.load();
    for(int i=0;i<20;++i) assert(update(5000));
    assert(effective_mode.load()==0 && frame_count.load()==static_frames);
    assert(XiaomanOrbConsole("orb_ui on"));assert(update(90000));assert(update(90000));
    assert(effective_mode.load()==0 && (motion_request.load()&3u)==2);
    assert(XiaomanOrbConsole("orb_stats"));
    const auto old_request=motion_request.load();
    // Same value explicit retry must be seen by the real Update adapter too.
    assert(XiaomanOrbConsole("orb_ui on"));assert(motion_request.load()!=old_request);
    assert(update(5000));assert(effective_mode.load()==2 && timer.period==67);
    assert(update(30000));assert(effective_mode.load()==1);
    // A yielded or sleeping UI cannot manufacture recovery frames.
    facts.system_page=true;const auto yielded_frames=frame_count.load();
    for(int i=0;i<20;++i) assert(!update(5000));
    assert(frame_count.load()==yielded_frames && effective_mode.load()==1);
    facts.system_page=false;
    for(int i=0;i<9;++i){assert(update(5000));assert(effective_mode.load()==1);}
    assert(update(5000));assert(effective_mode.load()==2);
    facts.session=false;const auto inactive_frames=frame_count.load();
    for(int i=0;i<20;++i) assert(!update(5000));
    assert(timer.paused && frame_count.load()==inactive_frames);
    facts.session=true;view.SetScreenOn(false);
    for(int i=0;i<20;++i) assert(!update(5000));
    assert(timer.paused && frame_count.load()==inactive_frames);
    puts("production view adaptive timer, request retry, static and inactive paths passed");
}
