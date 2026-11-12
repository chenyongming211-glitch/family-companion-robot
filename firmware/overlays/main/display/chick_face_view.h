#pragma once
#include "sdkconfig.h"
#if CONFIG_CHICK_FACE_UI
#include "chick_face_model.h"
#include "chick_face_renderer.h"
#include "face_refresh_policy.h"
#include "desktop_clock.h"
#include <lvgl.h>
#include <initializer_list>
#include <string>

namespace family_robot {
bool ChickFaceConsole(const char* line);
class ChickFaceView {
public:
    ~ChickFaceView();
    void Initialize(const lv_font_t*,int width,int height,void (*refresh)(void*),void*);
    bool Update(FaceFacts);
    void SetFont(const lv_font_t*);
    void SetEmotion(const char*,int64_t);
    void SetLegacyEmotion(uint32_t,const char*,int64_t);
    bool SetPlaybackEmotion(OrbOutputActivity::Identity,const char*,int64_t);
    void ClearPlaybackEmotions();
    void SetThinking(bool on) { thinking_=on; }
    void SetMessage(const char*);
    void SetClockNotice(bool on);
    void SetScreenOn(bool on) { screen_on_=on; }
    void SetQuiet(bool on) { quiet_=on; }
    bool Ready() const { return root_ && timer_; }
private:
    void Layout();
    void UpdateClock(int64_t now_us);
    FaceModel model_;
    FaceRefreshPolicy refresh_policy_;
    lv_obj_t *root_=nullptr,*image_=nullptr,*caption_=nullptr;
    lv_obj_t *clock_time_=nullptr,*clock_date_=nullptr;
    lv_obj_t *fallback_left_=nullptr,*fallback_right_=nullptr,*fallback_mouth_=nullptr;
    lv_timer_t* timer_=nullptr;
    uint16_t* pixels_=nullptr;
    lv_image_dsc_t descriptor_{};
    const lv_font_t* font_=nullptr;
    const lv_font_t* clock_font_=nullptr;
    DesktopClockText clock_text_{};
    DesktopClockMode clock_mode_=DesktopClockMode::Hidden;
    uint8_t clock_diagnostic_modes_=0;
    int64_t clock_sample_us_=0;
    bool clock_sampled_=false,clock_notice_=false;
    void (*refresh_)(void*)=nullptr;
    void* context_=nullptr;
    std::string message_;
    int width_=0,height_=0;
    bool screen_on_=true,quiet_=false,shown_=false,painted_=false,thinking_=false;
    bool timer_running_=false,profile_reported_=false;
    int mode_=2;
    uint32_t applied_request_=UINT32_MAX,period_ms_=67,frames_=0,max_draw_us_=0;
    int64_t last_frame_us_=0;
    FaceState state_=FaceState::Hidden;
    FaceEmotion emotion_=FaceEmotion::Neutral;
    bool draining_=false,preparing_=false;
};
} // namespace family_robot
#endif
