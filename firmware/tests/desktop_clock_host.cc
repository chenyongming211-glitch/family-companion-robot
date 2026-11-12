// Linked to the real ChickFaceView through the existing LVGL boundary stubs.
#include "desktop_clock.h"
#include <limits>
static void ClockFormatting() {
    using namespace family_robot;
    for(time_t invalid:{time_t(-1),time_t(0),time_t(1735689599),time_t(4102444800),std::numeric_limits<time_t>::max()}) {
        const auto c=FormatDesktopClock(invalid);
        assert(!c.valid && !std::strcmp(c.time,"--:--") && !c.date[0]);
    }
    const auto first=FormatDesktopClock(1735689600); // already-local2025-01-01
    assert(first.valid && !std::strcmp(first.time,"00:00"));
    assert(!std::strcmp(first.date,"1月1日 星期三"));
    assert(!std::strcmp(FormatDesktopClock(1709251199).time,"--:--")); // 2024 outside supported range
    assert(!std::strcmp(FormatDesktopClock(1835395140).date,"2月28日 星期一"));
    assert(!std::strcmp(FormatDesktopClock(1835395200).date,"2月29日 星期二"));
    assert(!std::strcmp(FormatDesktopClock(1835481600).date,"3月1日 星期三"));
    assert(!std::strcmp(FormatDesktopClock(1794488400).time,"13:00")); // no second timezone offset
}
static bool Hidden(const lv_obj_t* p){return p->flags&LV_OBJ_FLAG_HIDDEN;}
static void ClockView() {
    using namespace family_robot;
    for(const int width:{240,466,480})for(const int line:{20,30})for(const bool fail:{false,true}) {
        allocation_fails=fail;lv_font_t font{line};ChickFaceView view;
        wall_seconds=1735689600;wall_samples=0;
        view.Initialize(&font,width,width==240?320:width,[](void*){},nullptr);
        assert(view.clock_font_==&family_robot_desktop_clock_digits_font);
        assert(view.clock_time_->font==view.clock_font_ && view.clock_font_->line_height==48);
        FaceFacts f;f.now_us=1000000;assert(view.Update(f));
        assert(view.clock_mode_==DesktopClockMode::Desktop);
        assert(!Hidden(view.clock_time_) && !Hidden(view.clock_date_));
        assert(view.clock_time_->text=="00:00" && view.clock_date_->text=="1月1日 星期三");
        const auto writes=view.clock_time_->writes,dates=view.clock_date_->writes;
        for(int i=1;i<=9;++i){f.now_us+=100000;assert(view.Update(f));}
        assert(wall_samples==1 && view.clock_time_->writes==writes && view.clock_date_->writes==dates);
        wall_seconds+=59;f.now_us+=100000;assert(view.Update(f));
        assert(wall_samples==2 && view.clock_time_->writes==writes);
        wall_seconds++;f.now_us+=1000000;assert(view.Update(f));
        assert(view.clock_time_->text=="00:01" && view.clock_time_->writes==writes+1);
        assert(view.clock_date_->writes==dates);
        // Ordinary stock notices suppress only time/date: real face/caption and
        // quiet-idle raster policy remain unchanged, including fallback shapes.
        for(const auto mode:{DesktopClockMode::Desktop,DesktopClockMode::Compact,DesktopClockMode::Quiet}) {
            f.state=mode==DesktopClockMode::Compact?FaceState::Thinking:FaceState::Idle;
            f.quiet=mode==DesktopClockMode::Quiet;f.now_us+=1000000;assert(view.Update(f));
            const auto state=view.state_;const auto caption=view.caption_->text;
            const auto notice_samples=wall_samples;
            view.SetClockNotice(true);f.now_us+=1;assert(view.Update(f));
            assert(Hidden(view.clock_time_) && Hidden(view.clock_date_));
            assert(view.state_==state && view.caption_->text==caption && !Hidden(view.root_));
            const auto notice_frames=rendered_frames.load();
            wall_seconds+=60;f.now_us+=2000000;assert(view.Update(f));
            assert(wall_samples==notice_samples);
            if(mode==DesktopClockMode::Quiet) {
                assert(view.timer_->period==1000 && rendered_frames.load()==notice_frames);
                if(view.image_)assert(Hidden(view.image_));else assert(Hidden(view.fallback_left_));
            }
            view.SetClockNotice(false);f.now_us+=1;assert(view.Update(f));
            assert(!Hidden(view.clock_time_) && Hidden(view.clock_date_)==(mode==DesktopClockMode::Compact));
            assert(view.clock_time_->text==FormatDesktopClock(wall_seconds).time);
            assert(wall_samples==notice_samples+1 && view.state_==state && view.caption_->text==caption);
        }
        f.state=FaceState::Idle;f.quiet=false;wall_seconds=1735689660;f.now_us+=1000000;assert(view.Update(f));
        const auto small=view.image_?view.image_->scale:0;
        const int safe=width/10;
        auto bounds=[&] {
            const auto* p=view.clock_time_;
            assert(p->x>=safe && p->x+p->w*p->transform_scale/256<=width-safe);
            assert(p->y>=0 && p->y+p->h*p->transform_scale/256<=view.caption_->y);
        };
        bounds();
        assert(view.clock_date_->x>=safe && view.clock_date_->x+
            view.clock_date_->w*view.clock_date_->transform_scale/256<=width-safe);
        assert(view.clock_date_->font==&family_robot_desktop_clock_font);
        lv_point_t date_size{};lv_text_get_size(&date_size,view.clock_date_->text.c_str(),view.clock_date_->font,0,0,INT32_MAX,LV_TEXT_FLAG_NONE);
        assert(view.clock_date_->w>=date_size.x);
        const auto transition_samples=wall_samples;
        f.state=FaceState::Thinking;f.now_us+=1;assert(view.Update(f));
        assert(wall_samples==transition_samples);
        assert(view.clock_mode_==DesktopClockMode::Compact && Hidden(view.clock_date_));
        if(view.image_)assert(view.image_->scale>small);
        bounds();
        for(auto state:{FaceState::Boot,FaceState::WifiConfig,FaceState::Notifying,FaceState::Error,FaceState::Upgrading}) {
            f.state=state;assert(view.Update(f));assert(Hidden(view.clock_time_) && Hidden(view.clock_date_));
        }
        f.state=FaceState::Idle;view.SetMessage("网络异常");assert(view.Update(f));assert(Hidden(view.clock_time_));
        view.SetMessage("");assert(view.Update(f));assert(!Hidden(view.clock_time_));
        f.quiet=true;f.now_us+=1000000;const auto frames=rendered_frames.load();assert(view.Update(f));
        assert(view.clock_mode_==DesktopClockMode::Quiet && view.timer_->period==1000);
        if(view.image_)assert(Hidden(view.image_));else assert(Hidden(view.fallback_left_));
        f.now_us+=1000000;assert(view.Update(f));assert(rendered_frames.load()==frames);
        view.SetScreenOn(false);assert(!view.Update(f));assert(view.timer_->paused && Hidden(view.root_));
        const auto samples=wall_samples;wall_seconds=1735776000;
        f.now_us+=10000000;assert(!view.Update(f));assert(wall_samples==samples);
        view.SetScreenOn(true);assert(view.Update(f));assert(!view.timer_->paused);
        assert(view.clock_time_->text=="00:00" && view.clock_date_->text=="1月2日 星期四");
        wall_seconds=0;f.now_us+=1000000;assert(view.Update(f));
        assert(view.clock_time_->text=="--:--" && view.clock_date_->text.empty());
        wall_seconds=1735689600;f.now_us+=1000000;assert(view.Update(f));assert(view.clock_time_->text=="00:00");
        f.system_page=true;assert(!view.Update(f));assert(Hidden(view.root_));
        wall_seconds=1735689720;f.system_page=false;assert(view.Update(f));assert(view.clock_time_->text=="00:02");
        wall_seconds-=60;f.now_us+=1000000;assert(view.Update(f));assert(view.clock_time_->text=="00:01");
        wall_seconds=1735775940;f.now_us+=1000000;assert(view.Update(f));assert(view.clock_time_->text=="23:59");
        wall_seconds+=60;f.now_us+=1000000;assert(view.Update(f));
        assert(view.clock_time_->text=="00:00" && view.clock_date_->text=="1月2日 星期四");
    }
    allocation_fails=false;lv_font_t font{30};ChickFaceView view;
    view.Initialize(&font,480,480,[](void*){},nullptr);
    FaceFacts f;f.now_us=10000000;f.quiet=true;f.audio.output.connection=8;view.Update(f);
    auto& o=f.audio.output;o.identity={8,1};o.active=o.valid=true;o.level=120;
    f.now_us+=10000;o.stamp_us=f.now_us;o.playbacks[0]={{8,1},f.now_us,f.now_us,true,false,false,false};
    view.SetClockNotice(true);
    view.SetPlaybackEmotion({8,1},"happy",f.now_us);assert(view.Update(f));
    assert(view.state_==FaceState::Speaking && view.emotion_==FaceEmotion::Happy);
    assert(view.clock_mode_==DesktopClockMode::Compact && !Hidden(view.image_));
    assert(o.identity==OrbOutputActivity::Identity({8,1}) && o.level==120);
    assert(Hidden(view.clock_time_) && Hidden(view.clock_date_));
    view.SetClockNotice(false);f.now_us+=1;assert(view.Update(f));
    assert(view.clock_mode_==DesktopClockMode::Compact && !Hidden(view.clock_time_) && Hidden(view.clock_date_));
}
int main(){ClockFormatting();ClockView();std::puts("Desktop clock formatting, real view modes/cache/layout/lifecycle passed");}
