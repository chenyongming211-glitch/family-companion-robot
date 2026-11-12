#pragma once
#include "chick_face_model.h"
#include <algorithm>
#include <cstdio>
#include <ctime>

namespace family_robot {
// OTA has already applied timezone_offset before settimeofday. These seconds
// are local wall time, so gmtime_r deliberately performs no second TZ shift.
struct DesktopClockText {char time[6]="--:--",date[40]="";bool valid=false;};
inline DesktopClockText FormatDesktopClock(time_t seconds) {
    DesktopClockText result;std::tm local{};
    if(seconds<0 || !gmtime_r(&seconds,&local) || local.tm_year<125 || local.tm_year>199 ||
       local.tm_mon<0 || local.tm_mon>11 || local.tm_mday<1 || local.tm_mday>31 ||
       local.tm_wday<0 || local.tm_wday>6 || local.tm_hour<0 || local.tm_hour>23 ||
       local.tm_min<0 || local.tm_min>59 || local.tm_sec<0 || local.tm_sec>60)return result;
    static constexpr const char* weekdays[]={"日","一","二","三","四","五","六"};
    std::snprintf(result.time,sizeof(result.time),"%02d:%02d",local.tm_hour,local.tm_min);
    std::snprintf(result.date,sizeof(result.date),"%d月%d日 星期%s",local.tm_mon+1,local.tm_mday,weekdays[local.tm_wday]);
    result.valid=true;return result;
}
enum class DesktopClockMode {Hidden,Desktop,Compact,Quiet};
inline DesktopClockMode SelectDesktopClockMode(const FaceFrame& frame,bool quiet,bool message) {
    if(!frame.shown || message)return DesktopClockMode::Hidden;
    switch(frame.state) {
    case FaceState::Idle:case FaceState::Charging:case FaceState::LowBattery:
        return quiet?DesktopClockMode::Quiet:DesktopClockMode::Desktop;
    case FaceState::Connecting:case FaceState::Listening:case FaceState::Thinking:case FaceState::Speaking:
        return DesktopClockMode::Compact;
    default:return DesktopClockMode::Hidden;
    }
}
struct DesktopClockLayout {int x,y,scale,date_y,face_top,face_scale;};
inline DesktopClockLayout ComputeDesktopClockLayout(int width,int height,int line_height,int date_height,
                                                   int text_width,int text_height,DesktopClockMode mode) {
    const bool compact=mode==DesktopClockMode::Compact;
    const int safe=std::max(1,width/10),available=std::max(1,width-2*safe);
    const int target_height=compact?line_height:std::max(line_height,height/5);
    const int scale=std::max(1,std::min({1024,available*256/std::max(1,text_width),
                                      target_height*256/std::max(1,text_height)}));
    const int y=compact?8:std::max(line_height+8,height/7);
    const int date_y=y+text_height*scale/256+8;
    const int caption_top=height-line_height-8-std::max(8,height/9);
    const int face_top=date_y+date_height+8;
    const int face_height=std::max(1,std::min(height/3,caption_top-face_top-8));
    return {(width-text_width*scale/256)/2,y,scale,date_y,face_top,
        std::max(1,std::min(face_height*256/320,available*256/240))};
}
} // namespace family_robot
