#pragma once
#include "sdkconfig.h"
#if CONFIG_XIAOMAN_ORB_UI
#include "orb_model.h"
#include "orb_renderer.h"
#include "orb_refresh_policy.h"
#include <lvgl.h>

namespace family_robot {
// Commands use the existing USB diagnostics task, never the audio callback.
bool XiaomanOrbConsole(const char* line);
class XiaomanOrbView {
public:
    ~XiaomanOrbView();
    void Initialize(const lv_font_t* font,void (*refresh)(void*),void* context);
    bool Update(OrbFacts facts);
    void SetFont(const lv_font_t* font);
    bool Ready() const { return root_!=nullptr && timer_!=nullptr; }
    void SetScreenOn(bool on) { screen_on_=on; }
    void SetPowerSave(bool on) { power_save_=on; }
private:
#if CONFIG_WAVESHARE_XIAOMAN_REALTIME
    static constexpr int kScreenSide=480;
#else
    static constexpr int kScreenSide=466;
#endif
    OrbModel model_;
    OrbRefreshPolicy refresh_policy_;
    uint32_t applied_request_=UINT32_MAX;
    void (*refresh_)(void*)=nullptr;
    void* context_=nullptr;
    lv_obj_t *root_=nullptr,*graphic_=nullptr,*name_=nullptr,*image_=nullptr,*caption_=nullptr,*hint_=nullptr;
    lv_timer_t* timer_=nullptr;
    uint16_t* pixels_=nullptr;
    lv_image_dsc_t descriptor_{};
    bool screen_on_=true,power_save_=false,shown_=false,painted_=false;
    uint8_t smoothed_=0;
    OrbPhase phase_=OrbPhase::Hidden;
    bool draining_=false;
    int mode_=-1;
    uint64_t motion_ms_=0;
    uint32_t max_scale_=544;
    uint32_t image_scale_=UINT32_MAX,timer_period_=67;
    bool timer_running_=false;
    int64_t last_frame_us_=0,entrance_us_=0;
};
} // namespace family_robot
#endif
