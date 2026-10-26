// Real Initialize/SetFont/Update methods with deterministic LVGL geometry.
// Distinct LVGL enums deliberately retain the target C++20 warning surface.
#include <array>
#include <algorithm>
#include <atomic>
#include <cassert>
#include <cstdio>
#include <cstring>
#include <cstdlib>
#include <vector>
enum lv_obj_flag_t {LV_OBJ_FLAG_HIDDEN=1,LV_OBJ_FLAG_SCROLLABLE=2,LV_OBJ_FLAG_OVERFLOW_VISIBLE=4};
enum lv_align_t {LV_ALIGN_TOP_MID,LV_ALIGN_CENTER};
enum lv_part_t {LV_PART_MAIN=0};
enum lv_state_t {LV_STATE_DEFAULT=0};
using lv_style_selector_t=uint32_t;
struct lv_font_t {int line_height;};
struct lv_obj_t {
    lv_obj_t* parent=nullptr;std::vector<lv_obj_t*> children;
    int x=0,y=0,w=128,h=128;uint32_t scale=256;
    const lv_font_t* font=nullptr;bool label=false;unsigned flags=0;const char* text="";
};
struct lv_timer_t {void* user=nullptr;bool paused=false;uint32_t period=67;};
struct lv_image_dsc_t {
    struct {int magic=0,cf=0,w=0,h=0,stride=0;} header;
    size_t data_size=0;const uint8_t* data=nullptr;
};
constexpr int LV_IMAGE_HEADER_MAGIC=0x19,LV_COLOR_FORMAT_RGB565=1,LV_TEXT_ALIGN_CENTER=1;
constexpr int LV_LABEL_LONG_CLIP=0,LV_RADIUS_CIRCLE=1000,MALLOC_CAP_SPIRAM=1,MALLOC_CAP_8BIT=2;
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
static void lv_label_set_text_static(lv_obj_t* p,const char* text){p->text=text;}
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
static int64_t esp_timer_get_time(){static int64_t now=1000000;return now+=1000;}
#define ESP_LOGW(...) ((void)0)
#define private public
#include "xiaoman_orb_view.h"
#undef private
namespace family_robot {
namespace {
std::atomic<uint32_t> motion_request{2};std::atomic<int> effective_mode{2};
std::atomic<uint32_t> frame_count{0},max_draw_us{0},buffer_bytes{0},slow_frames{0};
std::atomic<uint32_t> unavailable_reads{0},preparing_transitions{0},closed_reads{0};
void Hidden(lv_obj_t* p,bool hidden){if(hidden)lv_obj_add_flag(p,LV_OBJ_FLAG_HIDDEN);else lv_obj_remove_flag(p,LV_OBJ_FLAG_HIDDEN);}
}
// PRODUCTION_METHODS
}
static int GlobalY(lv_obj_t* p){int y=0;for(;p;p=p->parent)y+=p->y;return y;}
static bool IsAbove(lv_obj_t* image,lv_obj_t* text){
    auto* branch=image;while(branch->parent!=text->parent && branch->parent)branch=branch->parent;
    const auto& c=text->parent->children;
    auto position=[&](lv_obj_t* p){size_t i=0;while(i<c.size()&&c[i]!=p)++i;return i;};
    return position(text)>position(branch);
}
static void PaintedArtworkFits(family_robot::XiaomanOrbView& view) {
    auto* graphic=view.image_->parent;
    int left=graphic->w,right=0,top=graphic->h,bottom=0;
    for(uint64_t base:{uint64_t{0},uint64_t{1}<<32})
        for(uint64_t ms=0;ms<=16000;ms+=133)for(uint8_t level:{uint8_t{0},uint8_t{255}}) {
            family_robot::OrbRenderer::Render(view.pixels_,base+ms,level,false);
            for(int y=0;y<128;++y)for(int x=0;x<128;++x) {
                if(view.pixels_[y*128+x]==family_robot::OrbRenderer::kBackground)continue;
                // LVGL scales about the centered image pivot; reserve a pixel
                // for integer alignment/nearest-neighbour rounding on either side.
                const int px=graphic->w/2+(x-64)*int(view.image_->scale)/256;
                const int py=graphic->h/2+(y-64)*int(view.image_->scale)/256;
                left=std::min(left,px);right=std::max(right,px);
                top=std::min(top,py);bottom=std::max(bottom,py);
            }
        }
    std::fprintf(stderr,"font=%d painted bounds=%d,%d..%d,%d viewport=%d,%d scale=%u\n",
        view.name_->font->line_height,left,top,right,bottom,graphic->w,graphic->h,view.image_->scale);
    assert(left>=1 && top>=1 && right<graphic->w-1 && bottom<graphic->h-1);
}
static void StaticFontFitTruth() {
    family_robot::motion_request.store(0);
    lv_font_t small{27},large{44},same_geometry{44};
    family_robot::XiaomanOrbView view;
    view.Initialize(&small,[](void*){},nullptr);
    family_robot::OrbFacts f{};f.session=true;f.audio.gate_open=true;f.now_us=1000000;
    assert(view.Update(f));
    const auto small_scale=view.image_scale_;
    const auto initial_frames=family_robot::frame_count.load();
    assert(small_scale==view.max_scale_);
    view.SetFont(&small);f.now_us+=1000000;assert(view.Update(f));
    assert(family_robot::frame_count.load()==initial_frames);
    view.SetFont(&large);f.now_us+=1000000;assert(view.Update(f));
    std::fprintf(stderr,"static font27->44 applied_scale=%u fitting_scale=%u frames=%u->%u\n",
        view.image_scale_,view.max_scale_,initial_frames,family_robot::frame_count.load());
    assert(view.image_scale_==view.max_scale_ && view.image_->scale==view.max_scale_);
    assert(view.image_scale_<small_scale);
    assert(family_robot::frame_count.load()==initial_frames+1);
    // Both the same font and another font with identical geometry keep the
    // static frame cached, even when Update is called well after its deadline.
    for(const auto* font:{&large,&same_geometry}) {
        view.SetFont(font);f.now_us+=1000000;assert(view.Update(f));
        assert(family_robot::frame_count.load()==initial_frames+1);
    }
    family_robot::motion_request.store(2);
}
int main(){
    screen.w=family_robot::XiaomanOrbView::kScreenSide;screen.h=screen.w;
    StaticFontFitTruth();
    for(int height:{24,27,32,35,40,44}) {
        lv_font_t font{height};family_robot::XiaomanOrbView view;
        view.Initialize(&font,[](void*){},nullptr);
        assert(view.root_->w==screen.w && view.root_->h==screen.h);
        assert(IsAbove(view.image_,view.caption_)); // opaque bitmap must not cover labels
        auto* graphic=view.image_->parent;
        assert(graphic!=view.root_ && !(graphic->flags&LV_OBJ_FLAG_OVERFLOW_VISIBLE));
        assert(GlobalY(graphic)>=GlobalY(view.name_)+height+4);
        assert(GlobalY(graphic)+graphic->h+4<=GlobalY(view.caption_));
        assert(GlobalY(view.hint_)>=GlobalY(view.caption_)+height+4);
        assert(GlobalY(view.hint_)+height<=screen.h-14);
        // Entrance and maximum image transform remain clipped inside this area.
        family_robot::OrbFacts f{};f.session=true;f.audio.gate_open=true;
        for(int64_t age:{0,67000,150000,300000,3000000}) {
            f.now_us=1000000+age;assert(view.Update(f));
            assert(IsAbove(view.image_,view.caption_));
        }
        PaintedArtworkFits(view);
        f.screen_on=false;f.now_us+=67000;
        assert(!view.Update(f) && view.timer_->paused);
        assert(view.root_->flags&LV_OBJ_FLAG_HIDDEN);
        lv_font_t larger{40};view.SetFont(&larger);
        assert(GlobalY(graphic)>=GlobalY(view.name_)+40+4);
        assert(GlobalY(graphic)+graphic->h+4<=GlobalY(view.caption_));
        assert(GlobalY(view.hint_)>=GlobalY(view.caption_)+40+4);
    }
    allocation_fails=true;
    {
        family_robot::motion_request.store(0);
        lv_font_t font{44};family_robot::XiaomanOrbView view;
        view.Initialize(&font,[](void*){},nullptr);
        assert(!view.pixels_ && view.image_ && IsAbove(view.image_,view.caption_));
        assert(view.image_->h<=view.graphic_->h && view.image_->w<=view.graphic_->w);
        family_robot::OrbFacts f{};f.session=true;f.audio.gate_open=true;f.now_us=1000000;
        assert(view.Update(f) && !view.timer_->paused);
        assert(std::strcmp(view.caption_->text,"正在聆听")==0);
        const auto frames=family_robot::frame_count.load();
        lv_font_t smaller{27};view.SetFont(&smaller);f.now_us+=1000000;
        assert(view.Update(f) && !view.pixels_ && family_robot::frame_count.load()==frames);
        view.SetFont(&font);f.now_us+=1000000;
        assert(view.Update(f) && !view.pixels_ && family_robot::frame_count.load()==frames);
        f.screen_on=false;assert(!view.Update(f) && view.timer_->paused);
        family_robot::motion_request.store(2);
    }
    puts("production layout painted bounds, text layering, fonts, entrance, screen-off and low-memory passed");
}
