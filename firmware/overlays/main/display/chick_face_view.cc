#include "chick_face_view.h"
#include "face_layout.h"
#include "desktop_clock_font.h"
#include "desktop_clock_digits_font.h"
#if CONFIG_CHICK_FACE_UI
#include <algorithm>
#include <atomic>
#include <cstdio>
#include <cstring>
#include <ctime>
#include <esp_heap_caps.h>
#include <esp_log.h>
#include <esp_timer.h>
#include <freertos/FreeRTOS.h>
#include <freertos/task.h>

namespace family_robot {
namespace {
std::atomic<uint32_t> requested_mode{2};
std::atomic<uint32_t> rendered_frames{0},maximum_draw_us{0};
void Hide(lv_obj_t* o,bool hide) {
    if(!o)return;
    if(hide)lv_obj_add_flag(o,LV_OBJ_FLAG_HIDDEN);
    else lv_obj_remove_flag(o,LV_OBJ_FLAG_HIDDEN);
}
}
bool ChickFaceConsole(const char* line) {
    if(!line)return false;
    int mode=-1;
    if(!std::strcmp(line,"face_ui static"))mode=0;
    else if(!std::strcmp(line,"face_ui low"))mode=1;
    else if(!std::strcmp(line,"face_ui on"))mode=2;
    else if(!std::strcmp(line,"face_stats")) {
        std::printf("face_stats frames=%lu max_draw_us=%lu requested=%lu\n",
            static_cast<unsigned long>(rendered_frames.load()),
            static_cast<unsigned long>(maximum_draw_us.load()),
            static_cast<unsigned long>(requested_mode.load()&3));
        return true;
    } else return false;
    const auto revision=(requested_mode.load()+4u)&~uint32_t{3};
    requested_mode.store(revision|static_cast<uint32_t>(mode));
    return true;
}
ChickFaceView::~ChickFaceView() {
    if(timer_)lv_timer_delete(timer_);
    if(root_)lv_obj_delete(root_);
    if(pixels_)heap_caps_free(pixels_);
}
void ChickFaceView::Initialize(const lv_font_t* font,int width,int height,
                              void (*refresh)(void*),void* context) {
    if(root_)return;
    width_=width;height_=height;font_=font;refresh_=refresh;context_=context;
    root_=lv_obj_create(lv_screen_active());
    lv_obj_set_size(root_,width,height);
    lv_obj_set_pos(root_,0,0);
    lv_obj_set_style_border_width(root_,0,0);
    lv_obj_set_style_radius(root_,0,0);
    lv_obj_set_style_pad_all(root_,0,0);
    lv_obj_set_style_bg_color(root_,lv_color_hex(0x14181f),0);
    lv_obj_remove_flag(root_,LV_OBJ_FLAG_SCROLLABLE);
    lv_obj_remove_flag(root_,LV_OBJ_FLAG_CLICKABLE);
    lv_obj_move_to_index(root_,1);
    pixels_=static_cast<uint16_t*>(heap_caps_malloc(FaceRenderer::kBytes,
                                                   MALLOC_CAP_SPIRAM|MALLOC_CAP_8BIT));
    if(pixels_) {
        descriptor_.header.magic=LV_IMAGE_HEADER_MAGIC;
        descriptor_.header.cf=LV_COLOR_FORMAT_RGB565;
        descriptor_.header.w=FaceRenderer::kWidth;
        descriptor_.header.h=FaceRenderer::kHeight;
        descriptor_.header.stride=FaceRenderer::kWidth*sizeof(uint16_t);
        descriptor_.data_size=FaceRenderer::kBytes;
        descriptor_.data=reinterpret_cast<const uint8_t*>(pixels_);
        image_=lv_image_create(root_);
        lv_image_set_src(image_,&descriptor_);
        lv_image_set_antialias(image_,false);
    } else {
        auto shape=[this](int w,int h) {
            auto* object=lv_obj_create(root_);
            lv_obj_set_size(object,w,h);
            lv_obj_set_style_radius(object,LV_RADIUS_CIRCLE,0);
            lv_obj_set_style_border_width(object,0,0);
            lv_obj_set_style_bg_color(object,lv_color_hex(0xffd8a8),0);
            lv_obj_remove_flag(object,LV_OBJ_FLAG_SCROLLABLE);
            return object;
        };
        fallback_left_=shape(32,48);fallback_right_=shape(32,48);fallback_mouth_=shape(36,6);
        ESP_LOGW("ChickFace","PSRAM unavailable; using static face");
    }
    caption_=lv_label_create(root_);
    lv_obj_set_style_text_color(caption_,lv_color_hex(0xffd8a8),0);
    lv_obj_set_style_text_align(caption_,LV_TEXT_ALIGN_CENTER,0);
    lv_label_set_long_mode(caption_,LV_LABEL_LONG_WRAP);
    clock_time_=lv_label_create(root_);clock_date_=lv_label_create(root_);
    for(auto* label:{clock_time_,clock_date_}) {
        lv_obj_set_style_text_color(label,lv_color_hex(0xffd8a8),0);
        lv_obj_set_style_text_align(label,LV_TEXT_ALIGN_CENTER,0);
        Hide(label,true);
    }
    lv_label_set_text(clock_time_,clock_text_.time);
    lv_label_set_text(clock_date_,clock_text_.date);
    SetFont(font);
    Hide(root_,true);
    timer_=lv_timer_create([](lv_timer_t* timer) {
        auto* self=static_cast<ChickFaceView*>(lv_timer_get_user_data(timer));
        self->refresh_(self->context_);
    },period_ms_,this);
    lv_timer_pause(timer_);
    ESP_LOGI("ChickFace","ready canvas=%dx%d psram=%u screen=%dx%d",
        FaceRenderer::kWidth,FaceRenderer::kHeight,pixels_?unsigned(FaceRenderer::kBytes):0u,width,height);
}
void ChickFaceView::SetFont(const lv_font_t* font) {
    font_=font;
    if(caption_ && font_)lv_obj_set_style_text_font(caption_,font_,0);
    clock_font_=&family_robot_desktop_clock_digits_font;
    if(clock_time_ && clock_font_)lv_obj_set_style_text_font(clock_time_,clock_font_,0);
    if(clock_date_)lv_obj_set_style_text_font(clock_date_,&family_robot_desktop_clock_font,0);
    Layout();
}
void ChickFaceView::Layout() {
    if(!root_ || !caption_ || !font_)return;
    // Both square and round panels keep the L0 text in the lower safe area.
    const auto layout=ComputeFaceLayout(width_,height_,font_->line_height,!message_.empty());
    if(image_) {
        lv_obj_align(image_,LV_ALIGN_CENTER,0,layout.image_offset_y);
        lv_image_set_scale(image_,static_cast<uint32_t>(layout.scale));
    }
    if(fallback_left_) {
        lv_obj_set_pos(fallback_left_,width_/2-58,height_/2-48);
        lv_obj_set_pos(fallback_right_,width_/2+26,height_/2-48);
        lv_obj_set_pos(fallback_mouth_,width_/2-18,height_/2+36);
    }
    lv_obj_set_size(caption_,layout.caption_width,layout.caption_height);
    lv_obj_set_pos(caption_,layout.caption_x,layout.caption_y);
    if(clock_time_ && clock_font_) {
        // Measure all digit combinations plus the invalid placeholder. Pivot at
        // the top-left so transformed visual bounds have an explicit position.
        lv_point_t size{},sample{};
        for(const char* text:{"00:00","11:11","22:22","33:33","44:44","55:55",
                              "66:66","77:77","88:88","99:99","--:--"}) {
            lv_text_get_size(&sample,text,clock_font_,0,0,width_,LV_TEXT_FLAG_NONE);
            size.x=std::max(size.x,sample.x);size.y=std::max(size.y,sample.y);
        }
        lv_point_t date_size{};
        lv_text_get_size(&date_size,"12月31日 星期三",&family_robot_desktop_clock_font,0,0,INT32_MAX,LV_TEXT_FLAG_NONE);
        const int date_scale=std::max(1,std::min(256,(width_-2*(width_/10))*256/std::max(1,int(date_size.x))));
        const auto clock=ComputeDesktopClockLayout(width_,height_,font_->line_height,(date_size.y+4)*date_scale/256,
                                                   size.x,size.y,clock_mode_);
        lv_obj_set_size(clock_time_,size.x,size.y);
        lv_obj_set_style_transform_pivot_x(clock_time_,0,0);
        lv_obj_set_style_transform_pivot_y(clock_time_,0,0);
        lv_obj_set_style_transform_scale_x(clock_time_,clock.scale,0);
        lv_obj_set_style_transform_scale_y(clock_time_,clock.scale,0);
        lv_obj_set_pos(clock_time_,clock.x,clock.y);
        lv_obj_set_size(clock_date_,date_size.x,date_size.y+4);
        lv_obj_set_style_transform_pivot_x(clock_date_,0,0);
        lv_obj_set_style_transform_pivot_y(clock_date_,0,0);
        lv_obj_set_style_transform_scale_x(clock_date_,date_scale,0);
        lv_obj_set_style_transform_scale_y(clock_date_,date_scale,0);
        lv_obj_set_pos(clock_date_,(width_-date_size.x*date_scale/256)/2,clock.date_y);
        const bool desktop=clock_mode_==DesktopClockMode::Desktop || clock_mode_==DesktopClockMode::Quiet;
        if(desktop && image_) {
            lv_image_set_scale(image_,clock.face_scale);
            const int center=clock.face_top+FaceRenderer::kHeight*clock.face_scale/512;
            lv_obj_align(image_,LV_ALIGN_CENTER,0,center-height_/2);
        }
        if(desktop && fallback_left_) {
            const int unit=std::max(1,clock.face_scale);
            auto place=[&](lv_obj_t* object,int x,int y,int w,int h) {
                lv_obj_set_size(object,std::max(1,w*unit/256),std::max(1,h*unit/256));
                lv_obj_set_pos(object,width_/2+x*unit/256,clock.face_top+y*unit/256);
            };
            place(fallback_left_,-58,112,32,48);place(fallback_right_,26,112,32,48);
            place(fallback_mouth_,-18,196,36,6);
        } else if(fallback_left_) {
            lv_obj_set_size(fallback_left_,32,48);lv_obj_set_size(fallback_right_,32,48);
            lv_obj_set_size(fallback_mouth_,36,6);
        }
    }
    painted_=false;
}
void ChickFaceView::UpdateClock(int64_t now_us) {
    if(clock_notice_ || clock_mode_==DesktopClockMode::Hidden){clock_sampled_=false;return;}
    if(clock_sampled_ && now_us>=clock_sample_us_ && now_us-clock_sample_us_<1000000)return;
    clock_sample_us_=now_us;clock_sampled_=true;
    const auto next=FormatDesktopClock(time(nullptr));
    if(std::strcmp(clock_text_.time,next.time))lv_label_set_text(clock_time_,next.time);
    if(std::strcmp(clock_text_.date,next.date))lv_label_set_text(clock_date_,next.date);
    clock_text_=next;
}
void ChickFaceView::SetEmotion(const char* emotion,int64_t now) {
    if(!emotion)return;
    model_.SetEmotion(emotion,now);
}
void ChickFaceView::SetLegacyEmotion(uint32_t connection,const char* emotion,int64_t now_us) {
    model_.SetLegacyEmotion(connection,emotion,now_us);
}
bool ChickFaceView::SetPlaybackEmotion(OrbOutputActivity::Identity id,const char* emotion,int64_t arrival_us) {
    return model_.SetPlaybackEmotion(id,emotion,arrival_us);
}
void ChickFaceView::ClearPlaybackEmotions() {
    model_.ClearPlaybackEmotions();
}
void ChickFaceView::SetMessage(const char* text) {
    std::string next=text?text:"";
    if(next.size()>256) {
        size_t end=256;
        while(end && (static_cast<unsigned char>(next[end])&0xc0)==0x80)--end;
        next.resize(end);
    }
    if(next==message_)return;
    message_=std::move(next);Layout();
}
void ChickFaceView::SetClockNotice(bool on) {
    if(clock_notice_==on)return;
    clock_notice_=on;
    // Stock transient banners own only clock visibility, never FaceModel facts.
    Hide(clock_time_,on || clock_mode_==DesktopClockMode::Hidden);
    Hide(clock_date_,on || (clock_mode_!=DesktopClockMode::Desktop && clock_mode_!=DesktopClockMode::Quiet));
    if(on)clock_sampled_=false;
}
bool ChickFaceView::Update(FaceFacts facts) {
    if(!Ready())return false;
    facts.screen_on=facts.screen_on && screen_on_;
    facts.quiet=facts.quiet || quiet_;
    facts.model_thinking=facts.model_thinking || thinking_;
    auto frame=model_.Update(facts);
    const auto clock_mode=SelectDesktopClockMode(frame,facts.quiet,!message_.empty());
    const bool clock_changed=clock_mode_!=clock_mode;
    if(clock_changed) {
        clock_mode_=clock_mode;Layout();
        Hide(clock_time_,clock_notice_ || clock_mode==DesktopClockMode::Hidden);
        Hide(clock_date_,clock_notice_ || (clock_mode!=DesktopClockMode::Desktop && clock_mode!=DesktopClockMode::Quiet));
        const bool hide_face=clock_mode==DesktopClockMode::Quiet;
        for(auto* object:{image_,fallback_left_,fallback_right_,fallback_mouth_})Hide(object,hide_face);
    }
    // First real DMA activity proves generation advanced beyond waiting;
    // later draining cannot resurrect the thinking hint.
    if(frame.state==FaceState::Speaking)thinking_=false;
    const bool changed=state_!=frame.state || emotion_!=frame.emotion ||
        draining_!=frame.draining || preparing_!=frame.preparing || shown_!=frame.shown;
    if(shown_!=frame.shown)Hide(root_,!frame.shown);
    shown_=frame.shown;state_=frame.state;emotion_=frame.emotion;draining_=frame.draining;preparing_=frame.preparing;
    if(!facts.screen_on) {
        if(timer_running_){lv_timer_pause(timer_);timer_running_=false;}
        painted_=false;last_frame_us_=0;
        clock_sampled_=false;
        return false;
    }
    if(!timer_running_){lv_timer_resume(timer_);timer_running_=true;}
    UpdateClock(facts.now_us);
    const auto request=requested_mode.load();
    if(request!=applied_request_) {
        applied_request_=request;refresh_policy_.Request(int(request&3));
        painted_=false;
    }
    mode_=refresh_policy_.effective();
    uint32_t period=(!pixels_ || mode_==0 || clock_mode_==DesktopClockMode::Quiet)?1000u:((facts.quiet || mode_==1 || !frame.shown)?200u:
                                      (frame.state==FaceState::Speaking?33u:67u));
    if(period_ms_!=period){lv_timer_set_period(timer_,period);period_ms_=period;}
    if(!frame.shown){painted_=false;return false;}
    if(changed || !painted_) {
        const char* caption=message_.empty()?frame.Caption():message_.c_str();
        lv_label_set_text(caption_,caption);
    }
    if(clock_mode_==DesktopClockMode::Quiet){painted_=true;return true;}
    if(!pixels_){painted_=true;return true;}
    if(painted_ && !changed && (mode_==0 ||
       (facts.now_us>=last_frame_us_ && facts.now_us-last_frame_us_<int64_t(period_ms_)*1000)))return true;
    if(mode_==0) {
        frame.blink=0;frame.gaze_x=frame.gaze_y=frame.tilt=frame.bob_y=0;
        frame.breath=1;frame.motion_ms=0;frame.mouth_level=0;
    }
    const auto before=esp_timer_get_time();
    FaceRenderer::Render(pixels_,frame);
    const auto elapsed=static_cast<uint32_t>(esp_timer_get_time()-before);
    max_draw_us_=std::max(max_draw_us_,elapsed);maximum_draw_us.store(max_draw_us_);
    rendered_frames.fetch_add(1);++frames_;
    refresh_policy_.ObserveFrame(elapsed);mode_=refresh_policy_.effective();
    lv_obj_invalidate(image_);
    painted_=true;last_frame_us_=facts.now_us;
    if(!profile_reported_ && frames_>=150) {
        // ESP-IDF's pinned task.h defines this high-water value in bytes.
        // Read only at the existing profiling point, never on every frame.
        const auto stack_min_free_bytes=uxTaskGetStackHighWaterMark(nullptr);
        ESP_LOGI("ChickFace","frames=%lu max_draw_us=%lu mode=%d stack_min_free_bytes=%lu task=%s",
            static_cast<unsigned long>(frames_),static_cast<unsigned long>(max_draw_us_),mode_,
            static_cast<unsigned long>(stack_min_free_bytes),pcTaskGetName(nullptr));
        profile_reported_=true;
    }
    return true;
}
} // namespace family_robot
#endif
