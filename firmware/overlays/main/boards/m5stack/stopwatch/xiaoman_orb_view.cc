#include "xiaoman_orb_view.h"
#if CONFIG_XIAOMAN_ORB_UI
#include <atomic>
#include <cstdio>
#include <cstring>
#include <initializer_list>
#include <esp_heap_caps.h>
#include <esp_timer.h>
#include <esp_log.h>

namespace family_robot {
namespace {
// Low two bits hold requested mode; the bounded revision makes even a repeated
// explicit "on" observable after an automatic static fallback. Console is the
// single writer. Effective degradation never changes this user request.
std::atomic<uint32_t> motion_request{2};
std::atomic<int> effective_mode{2}; // 0 static, 1 5fps, 2 target 15fps
std::atomic<uint32_t> frame_count{0},max_draw_us{0},buffer_bytes{0},slow_frames{0};
std::atomic<uint32_t> unavailable_reads{0},preparing_transitions{0},closed_reads{0};
void Hidden(lv_obj_t* obj,bool hidden) {
    if (!obj) return;
    if(hidden) lv_obj_add_flag(obj,LV_OBJ_FLAG_HIDDEN);
    else lv_obj_remove_flag(obj,LV_OBJ_FLAG_HIDDEN);
}
}
bool XiaomanOrbConsole(const char* line) {
    int requested=-1;
    if(std::strcmp(line,"orb_ui off")==0 || std::strcmp(line,"orb_ui static")==0) requested=0;
    else if(std::strcmp(line,"orb_ui low")==0) requested=1;
    else if(std::strcmp(line,"orb_ui on")==0) requested=2;
    else if(std::strcmp(line,"orb_stats")==0) {
        std::printf("orb_stats mode=%d frames=%lu max_draw_us=%lu slow=%lu psram=%lu requested=%u unavailable=%lu preparing=%lu closed=%lu\n",effective_mode.load(),
            static_cast<unsigned long>(frame_count.load()),static_cast<unsigned long>(max_draw_us.load()),
            static_cast<unsigned long>(slow_frames.load()),static_cast<unsigned long>(buffer_bytes.load()),
            static_cast<unsigned>(motion_request.load()&3u),static_cast<unsigned long>(unavailable_reads.load()),
            static_cast<unsigned long>(preparing_transitions.load()),static_cast<unsigned long>(closed_reads.load()));
        return true;
    } else return false;
    const auto revision=(motion_request.load()+4u)&~uint32_t{3};
    motion_request.store(revision|static_cast<uint32_t>(requested));
    std::printf("orb_ui mode=%d (0=static,1=5fps,2=15fps)\n",requested);
    return true;
}
XiaomanOrbView::~XiaomanOrbView() {
    if(timer_) lv_timer_delete(timer_);
    if(root_) lv_obj_delete(root_);
    if(pixels_) heap_caps_free(pixels_);
}
void XiaomanOrbView::Initialize(const lv_font_t* font,void (*refresh)(void*),void* context) {
    if(root_) return;
    root_=lv_obj_create(lv_screen_active());
    lv_obj_set_size(root_,466,466);
    lv_obj_set_pos(root_,0,0);
    lv_obj_set_style_radius(root_,0,0);
    lv_obj_set_style_border_width(root_,0,0);
    lv_obj_set_style_pad_all(root_,0,0);
    lv_obj_set_style_bg_color(root_,lv_color_hex(0x020305),0);
    lv_obj_remove_flag(root_,LV_OBJ_FLAG_SCROLLABLE);
    // Behind all existing icon/status/notification/low-battery layers. The
    // ordinary emoji and caption are hidden separately while this is visible.
    lv_obj_move_to_index(root_,1);
    auto label=[this,font](int cy,uint32_t color) {
        auto* obj=lv_label_create(root_);
        lv_obj_set_width(obj,340);
        lv_obj_set_style_text_align(obj,LV_TEXT_ALIGN_CENTER,0);
        lv_obj_set_style_text_font(obj,font,0);
        lv_obj_set_style_text_color(obj,lv_color_hex(color),0);
        lv_label_set_long_mode(obj,LV_LABEL_LONG_CLIP);
        lv_obj_align(obj,LV_ALIGN_TOP_MID,0,cy-font->line_height/2);
        return obj;
    };
    // Clip the opaque RGB565 image to its own region. Transform bounds include
    // the source's black corners, so image z-order alone is not a safe layout.
    graphic_=lv_obj_create(root_);
    lv_obj_set_style_radius(graphic_,0,0);
    lv_obj_set_style_border_width(graphic_,0,0);
    lv_obj_set_style_pad_all(graphic_,0,0);
    lv_obj_set_style_bg_color(graphic_,lv_color_hex(0x020305),0);
    lv_obj_remove_flag(graphic_,LV_OBJ_FLAG_SCROLLABLE);
    pixels_=static_cast<uint16_t*>(heap_caps_malloc(OrbRenderer::kBytes,MALLOC_CAP_SPIRAM|MALLOC_CAP_8BIT));
    if(pixels_) {
        buffer_bytes.store(OrbRenderer::kBytes);
        descriptor_.header.magic=LV_IMAGE_HEADER_MAGIC;
        descriptor_.header.cf=LV_COLOR_FORMAT_RGB565;
        descriptor_.header.w=OrbRenderer::kSide; descriptor_.header.h=OrbRenderer::kSide;
        descriptor_.header.stride=OrbRenderer::kSide*sizeof(uint16_t);
        descriptor_.data_size=OrbRenderer::kBytes;
        descriptor_.data=reinterpret_cast<const uint8_t*>(pixels_);
        image_=lv_image_create(graphic_);
        lv_image_set_src(image_,&descriptor_);
        // The soft contour is already in the 128px source. Nearest-neighbour
        // scaling avoids per-output-pixel bilinear work; text stays native.
        lv_image_set_antialias(image_,false);
    } else {
        // No internal-SRAM fallback: state remains readable without stealing
        // audio/DMA memory. A static LVGL disc costs no framebuffer.
        image_=lv_obj_create(graphic_);
        lv_obj_set_size(image_,220,220);
        lv_obj_set_style_radius(image_,LV_RADIUS_CIRCLE,0);
        lv_obj_set_style_bg_color(image_,lv_color_hex(0x244b87),0);
        lv_obj_set_style_border_color(image_,lv_color_hex(0x8851ed),0);
        lv_obj_set_style_border_width(image_,2,0);
        ESP_LOGW("XiaomanOrb","PSRAM unavailable: static disc and state text");
    }
    // Native labels are later siblings, above the graphic viewport at every
    // entrance scale and with the active theme's actual 24/32/40px font.
    name_=label(94,0xc5ccd8); lv_label_set_text_static(name_,"小满");
    caption_=label(381,0xd6dee8);lv_label_set_text_static(caption_,"");
    hint_=label(415,0x838d9d);lv_label_set_text_static(hint_,"");
    SetFont(font);
    Hidden(root_,true);
    refresh_=refresh; context_=context;
    timer_=lv_timer_create([](lv_timer_t* timer) {
        auto* self=static_cast<XiaomanOrbView*>(lv_timer_get_user_data(timer));
        self->refresh_(self->context_);
    },67,this);
    lv_timer_pause(timer_);
}
void XiaomanOrbView::SetFont(const lv_font_t* font) {
    if(!root_ || !font) return;
    for(auto* label:{name_,caption_,hint_}) lv_obj_set_style_text_font(label,font,0);
    lv_obj_align(name_,LV_ALIGN_TOP_MID,0,94-font->line_height/2);
    lv_obj_align(caption_,LV_ALIGN_TOP_MID,0,381-font->line_height/2);
    // Caption top rounds its half-height down; its bottom rounds up for odd
    // native font heights, so keep the full four-pixel hint gap in that case.
    lv_obj_align(hint_,LV_ALIGN_TOP_MID,0,381+(font->line_height+1)/2+4);
    const int top=94+font->line_height/2+8;
    const int height=381-font->line_height/2-8-top;
    lv_obj_set_pos(graphic_,93,top);
    lv_obj_set_size(graphic_,280,height);
    // Fit the complete painted artwork, including glow, drift and maximum
    // sound expansion; the stronger contour must not be cropped by this clip.
    const auto fitting=static_cast<uint32_t>((height-4)*256/(2*OrbRenderer::kArtworkRadius));
    const auto max_scale=fitting<544?fitting:544;
    // A changed viewport fit needs one new transform even in static mode.
    // Identical geometry retains the cached frame and its no-redraw behavior.
    if(max_scale_!=max_scale) painted_=false;
    max_scale_=max_scale;
    lv_obj_align(image_,LV_ALIGN_CENTER,0,0);
}
bool XiaomanOrbView::Update(OrbFacts facts) {
    if (!root_) return false;
    // Saturating UI-only observations, never authorization or playback proof.
    auto count=[](std::atomic<uint32_t>& counter) {
        const auto n=counter.load();if(n!=UINT32_MAX)counter.store(n+1);
    };
    if(facts.session) {
        if(!facts.audio.snapshot_available) count(unavailable_reads);
        else if(!facts.audio.gate_open) count(closed_reads);
    }
    facts.screen_on=facts.screen_on && screen_on_ && !power_save_;
    auto p=model_.Update(facts);
    if(p.phase==OrbPhase::Preparing && phase_!=p.phase)count(preparing_transitions);
    const bool visible=p.phase!=OrbPhase::Hidden;
    const bool entering=visible && !shown_;
    if(entering) entrance_us_=facts.now_us;
    const bool changed=phase_!=p.phase || draining_!=p.draining || shown_!=visible;
    if(shown_!=visible) Hidden(root_,!visible);
    shown_=visible;
    // Paused during Idle/system lifecycle and actual AMOLED brightness zero.
    // Temporary notifications keep a slow poll so their dismissal restores UI.
    if(!facts.session || !facts.screen_on) {
        if(timer_running_) {lv_timer_pause(timer_);timer_running_=false;}
        phase_=p.phase; painted_=false; smoothed_=0; last_frame_us_=0;
        return visible;
    }
    if(!timer_running_) {lv_timer_resume(timer_);timer_running_=true;}
    // Both LVGL calls wake its handler, even if their value is unchanged.
    auto period=[this](uint32_t value) {
        if(timer_period_!=value) {lv_timer_set_period(timer_,value);timer_period_=value;}
    };
    const auto request=motion_request.load();
    if(applied_request_!=request) {
        refresh_policy_.Request(static_cast<int>(request&3u));
        applied_request_=request;
    }
    const int mode=refresh_policy_.effective();
    effective_mode.store(mode);
    const bool mode_changed=mode_!=mode; mode_=mode;
    period((!visible || mode!=2)?200:67);
    if(!visible) {phase_=p.phase;painted_=false;return false;}
    if(changed) {
        lv_label_set_text_static(caption_,p.Caption());
        lv_label_set_text_static(hint_,p.draining?"正在停播":"");
    }
    phase_=p.phase;draining_=p.draining;
    if(!pixels_) return true;
    // Visual-only 300ms emergence, independent of audio/state readiness.
    const int64_t age=facts.now_us-entrance_us_;
    const int u=mode==0 || age>=300000 ? 256 : (age<=0?0:static_cast<int>(age*256/300000));
    const int inverse=256-u;
    const int ease=256-(inverse*inverse*inverse)/(256*256);
    const auto scale=static_cast<uint32_t>(54+(max_scale_-54)*ease/256);
    const bool dim=p.phase==OrbPhase::Error || p.phase==OrbPhase::Preparing;
    if(mode==0 && painted_ && !changed && !mode_changed) return true;
    // Status/emotion callbacks may refresh several times before the next
    // animation deadline. They can update text immediately without redrawing
    // the bitmap, mutating its transform or scheduling extra wakeups.
    if(mode!=0 && painted_ && !changed && !mode_changed && last_frame_us_ &&
       facts.now_us>=last_frame_us_ && facts.now_us-last_frame_us_<int64_t(timer_period_)*1000) return true;
    if(image_scale_!=scale) {lv_image_set_scale(image_,scale);image_scale_=scale;}
    const uint32_t dt=last_frame_us_ && facts.now_us>last_frame_us_ ?
        static_cast<uint32_t>((facts.now_us-last_frame_us_)/1000):67;
    last_frame_us_=facts.now_us;
    const uint32_t clamped=dt>300?300:dt;
    const int difference=int(p.level)-smoothed_;
    const int tau=difference>0?80:240;
    smoothed_=static_cast<uint8_t>(int(smoothed_)+difference*int(clamped)/(tau+int(clamped)));
    if(mode!=0 && !dim) motion_ms_+=clamped;
    const auto before=esp_timer_get_time();
    OrbRenderer::Render(pixels_,mode==0?0:motion_ms_,mode==0?0:smoothed_,dim);
    const auto elapsed=static_cast<uint32_t>(esp_timer_get_time()-before);
    frame_count.fetch_add(1);
    if(elapsed>max_draw_us.load()) max_draw_us.store(elapsed);
    // Budget observations affect only the next UI refresh period. Ten fast
    // rendered frames can recover a transient slowdown under requested mode2.
    if(elapsed>OrbRefreshPolicy::kFastFrameUs) slow_frames.fetch_add(1);
    refresh_policy_.ObserveFrame(elapsed);
    effective_mode.store(refresh_policy_.effective());
    period(refresh_policy_.effective()==2?67:200);
    lv_obj_invalidate(image_);
    painted_=true;
    return true;
}
} // namespace family_robot
#endif
