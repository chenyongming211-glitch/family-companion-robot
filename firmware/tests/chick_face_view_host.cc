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
enum lv_obj_flag_t {LV_OBJ_FLAG_HIDDEN=1,LV_OBJ_FLAG_SCROLLABLE=2,LV_OBJ_FLAG_OVERFLOW_VISIBLE=4,LV_OBJ_FLAG_CLICKABLE=8};
enum lv_align_t {LV_ALIGN_TOP_MID,LV_ALIGN_CENTER};
enum lv_part_t {LV_PART_MAIN=0};
enum lv_state_t {LV_STATE_DEFAULT=0};
using lv_style_selector_t=uint32_t;
struct lv_font_t {int line_height;};
struct lv_obj_t {
    lv_obj_t* parent=nullptr;std::vector<lv_obj_t*> children;
    int x=0,y=0,w=128,h=128;uint32_t scale=256;
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
static uint32_t lv_color_hex(uint32_t color){return color;}
static void lv_obj_add_flag(lv_obj_t* p,lv_obj_flag_t f){p->flags|=f;}
static void lv_obj_remove_flag(lv_obj_t* p,lv_obj_flag_t f){p->flags&=~f;}
static void lv_obj_move_to_index(lv_obj_t*,int){}
static void lv_label_set_long_mode(lv_obj_t*,int){}
static void lv_label_set_text(lv_obj_t* p,const char* text){p->text=text;}
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
#define ESP_LOGI(...) ((void)0)
#define private public
#include "chick_face_view.h"
#undef private
// PRODUCTION_VIEW
int main() {
    using namespace family_robot;
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
        assert(view.timer_->period==(fail?1000u:200u));
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
    std::puts("CHICK real view captions, lifecycle, layout and fallback passed");
}
