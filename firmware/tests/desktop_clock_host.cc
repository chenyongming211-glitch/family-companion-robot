// Linked to the real ChickFaceView through the existing LVGL boundary stubs.
#include "desktop_clock.h"
#include <limits>
static void ClockFormatting() {
    using namespace family_robot;
    for(time_t invalid:{time_t(-1),time_t(0),time_t(1735689599),time_t(4102444800),std::numeric_limits<time_t>::max()}) {
        const auto c=FormatDesktopClock(invalid);
        assert(!c.valid && !std::strcmp(c.time,"--:--") && !c.date[0]);
    }
    const auto first=FormatDesktopClock(1735689600); // UTC0 fixture: 2025-01-01
    assert(first.valid && !std::strcmp(first.time,"00:00"));
    assert(!std::strcmp(first.date,"1月1日 星期三"));
    assert(!std::strcmp(FormatDesktopClock(1709251199).time,"--:--")); // 2024 outside supported range
    assert(!std::strcmp(FormatDesktopClock(1835395140).date,"2月28日 星期一"));
    assert(!std::strcmp(FormatDesktopClock(1835395200).date,"2月29日 星期二"));
    assert(!std::strcmp(FormatDesktopClock(1835481600).date,"3月1日 星期三"));
    assert(!std::strcmp(FormatDesktopClock(1794488400).time,"13:00")); // UTC0 layout fixture
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

static void CheckFaceStatsConsole() {
    const auto calls=lvgl_accesses.load();
    const auto lines=clock_logs.size();
    std::thread console([]{console_thread=true;assert(family_robot::ChickFaceConsole("face_stats"));console_thread=false;});
    console.join();
    assert(lvgl_accesses.load()==calls && clock_logs.size()==lines);
}
static void ExpectDiagnosticLabel(const std::string& line,const char* name,const lv_obj_t* label,
                                  const lv_font_t* expected,size_t bound) {
    auto has=[&](const std::string& value){assert(line.find(value)!=std::string::npos);};
    has(std::string("label=")+name+" ");
    has("hidden="+std::to_string(Hidden(label)));
    has(" x="+std::to_string(label->x)+" y="+std::to_string(label->y)+
        " w="+std::to_string(label->w)+" h="+std::to_string(label->h));
    has(" font_line="+std::to_string(label->font?label->font->line_height:0)+
        " font_expected="+std::to_string(label->font==expected));
    has(" text_opa="+std::to_string(label->text_opa)+" overall_opa="+
        std::to_string(lv_obj_get_style_opa_recursive(label,LV_PART_MAIN)));
    has(" effective_text_opa="+std::to_string(label->text_opa*lv_obj_get_style_opa_recursive(label,LV_PART_MAIN)/255));
    has(" scale_x="+std::to_string(label->transform_scale)+" scale_y="+std::to_string(label->transform_scale_y));
    has(" text="+label->text.substr(0,bound));
    assert(line.size()<512);
}
static void ClockDiagnostic() {
    using namespace family_robot;
    allocation_fails=false;lv_font_t font{30},wrong_font{19};
    clock_logs.clear();
    {
        ChickFaceView view;wall_seconds=1735689600;
        view.Initialize(&font,480,480,[](void*){},nullptr);
        FaceFacts f;f.now_us=1000000;f.state=FaceState::Boot;
        const auto layouts=layout_updates;assert(view.Update(f));
        assert(clock_logs.empty() && layout_updates==layouts); // Hidden never consumes a mode.
        f.state=FaceState::Idle;f.now_us+=1;assert(view.Update(f));
        assert(clock_logs.size()==3); // RED: supported Update must emit without any console request.
        assert(layout_updates==layouts+1);
        assert(clock_logs[0].find("mode=1 notice=0 root_hidden=0 screen_on=1 quiet=0 valid=1")!=std::string::npos);
        assert(clock_logs[0].find(" time=00:00 date=1月1日 星期三")!=std::string::npos);
        ExpectDiagnosticLabel(clock_logs[1],"time",view.clock_time_,&family_robot_desktop_clock_digits_font,5);
        ExpectDiagnosticLabel(clock_logs[2],"date",view.clock_date_,&family_robot_desktop_clock_font,39);
        CheckFaceStatsConsole();f.now_us+=1;assert(view.Update(f));
        assert(clock_logs.size()==3 && layout_updates==layouts+1);
        f.system_page=true;assert(!view.Update(f));assert(clock_logs.size()==3 && layout_updates==layouts+1);
        f.system_page=false;f.state=FaceState::Thinking;f.now_us+=1;assert(view.Update(f));
        assert(clock_logs.size()==6 && layout_updates==layouts+2);
        assert(clock_logs[3].find("mode=2 notice=0 root_hidden=0 screen_on=1 quiet=0 valid=1")!=std::string::npos);
        assert(!Hidden(view.clock_time_) && Hidden(view.clock_date_));
        ExpectDiagnosticLabel(clock_logs[4],"time",view.clock_time_,&family_robot_desktop_clock_digits_font,5);
        ExpectDiagnosticLabel(clock_logs[5],"date",view.clock_date_,&family_robot_desktop_clock_font,39);
        f.now_us+=1;assert(view.Update(f));assert(clock_logs.size()==6 && layout_updates==layouts+2);
        f.state=FaceState::Idle;f.quiet=true;wall_seconds=0;f.now_us+=1000000;assert(view.Update(f));
        assert(clock_logs.size()==9 && layout_updates==layouts+3);
        assert(clock_logs[6].find("mode=3 notice=0 root_hidden=0 screen_on=1 quiet=1 valid=0")!=std::string::npos);
        assert(clock_logs[6].find(" time=--:-- date=")!=std::string::npos);
        ExpectDiagnosticLabel(clock_logs[7],"time",view.clock_time_,&family_robot_desktop_clock_digits_font,5);
        ExpectDiagnosticLabel(clock_logs[8],"date",view.clock_date_,&family_robot_desktop_clock_font,39);
        // Revisiting each visible mode, notices and console requests cannot exceed nine lines.
        for(int i=0;i<3;++i) {
            f.quiet=false;f.state=FaceState::Idle;f.now_us+=1;assert(view.Update(f));
            f.state=FaceState::Thinking;assert(view.Update(f));
            view.SetClockNotice(true);assert(view.Update(f));view.SetClockNotice(false);
            f.state=FaceState::Idle;f.quiet=true;assert(view.Update(f));
            CheckFaceStatsConsole();assert(view.Update(f));
            assert(clock_logs.size()==9 && layout_updates==layouts+3);
        }
    }
    {
        // Per-view budget resets. Off and Hidden must leave all three visible modes available.
        const auto begin=clock_logs.size();const auto layouts=layout_updates;
        ChickFaceView view;view.Initialize(&font,480,480,[](void*){},nullptr);
        view.SetScreenOn(false);FaceFacts f;f.now_us=20000000;f.quiet=true;
        const auto samples=wall_samples;assert(!view.Update(f));assert(!view.Update(f));
        assert(clock_logs.size()==begin && layout_updates==layouts && wall_samples==samples && view.timer_->paused);
        view.SetScreenOn(true);f.system_page=true;assert(!view.Update(f));
        assert(clock_logs.size()==begin && layout_updates==layouts);
        f.system_page=false;f.quiet=false;f.state=FaceState::Thinking;assert(view.Update(f));
        assert(clock_logs.size()==begin+3 && layout_updates==layouts+1 && !view.timer_->paused);
        assert(clock_logs[begin].find("mode=2 notice=0 root_hidden=0 screen_on=1 quiet=0")!=std::string::npos);
        assert(view.Update(f));assert(clock_logs.size()==begin+3 && layout_updates==layouts+1);
        f.state=FaceState::Idle;view.SetClockNotice(true);assert(view.Update(f));
        assert(clock_logs.size()==begin+6 && layout_updates==layouts+2);
        assert(clock_logs[begin+3].find("mode=1 notice=1 root_hidden=0")!=std::string::npos);
        assert(Hidden(view.clock_time_) && Hidden(view.clock_date_));
        ExpectDiagnosticLabel(clock_logs[begin+4],"time",view.clock_time_,&family_robot_desktop_clock_digits_font,5);
        ExpectDiagnosticLabel(clock_logs[begin+5],"date",view.clock_date_,&family_robot_desktop_clock_font,39);
        view.SetClockNotice(false);assert(view.Update(f));assert(clock_logs.size()==begin+6);
        view.SetScreenOn(false);f.quiet=true;assert(!view.Update(f));
        assert(clock_logs.size()==begin+6 && layout_updates==layouts+2 && view.timer_->paused);
        view.SetScreenOn(true);assert(view.Update(f));
        assert(clock_logs.size()==begin+9 && layout_updates==layouts+3);
        assert(clock_logs[begin+6].find("mode=3 notice=0 root_hidden=0 screen_on=1 quiet=1")!=std::string::npos);
        assert(view.Update(f));assert(clock_logs.size()==begin+9 && layout_updates==layouts+3);
    }
    {
        // Deliberately differ the actual fields from cached/layout expectations before
        // the first sample; bypass a mode transition so Layout does not overwrite them.
        const auto begin=clock_logs.size();ChickFaceView view;
        view.Initialize(&font,480,480,[](void*){},nullptr);view.clock_mode_=DesktopClockMode::Desktop;
        view.clock_time_->text="19:45longer";view.clock_time_->font=&wrong_font;
        view.clock_time_->x=-17;view.clock_time_->y=511;view.clock_time_->w=0;view.clock_time_->h=7;
        view.clock_time_->text_opa=120;view.clock_time_->opa=64;view.root_->opa=128;
        view.clock_time_->transform_scale=77;view.clock_time_->transform_scale_y=99;
        wall_seconds=0;FaceFacts f;f.now_us=30000000;assert(view.Update(f));
        assert(clock_logs.size()==begin+3);
        assert(clock_logs[begin].find(" time=--:-- ")!=std::string::npos);
        ExpectDiagnosticLabel(clock_logs[begin+1],"time",view.clock_time_,&family_robot_desktop_clock_digits_font,5);
        assert(clock_logs[begin+1].find("longer")==std::string::npos && clock_logs[begin+1].find("cache_match=0")!=std::string::npos);
        assert(clock_logs[1].find("cache_match=1")!=std::string::npos);
        view.SetMessage("private user speech");assert(view.Update(f));assert(clock_logs.size()==begin+3);
        view.SetMessage("");assert(view.Update(f));assert(clock_logs.size()==begin+3);
    }
    for(const auto& log:clock_logs)assert(log.find("private user speech")==std::string::npos && log.find("待机中")==std::string::npos);
    std::puts("Automatic clock diagnostic per-view mode budget, actual metadata, console isolation and off/Hidden deferral passed");
}

int main(){ClockFormatting();ClockView();ClockDiagnostic();std::puts("Desktop clock formatting, real view modes/cache/layout/lifecycle passed");}
