#pragma once
#include <algorithm>

namespace family_robot {
struct FaceLayout {
    int caption_x,caption_y,caption_width,caption_height,image_offset_y,scale;
};
inline FaceLayout ComputeFaceLayout(int width,int height,int line_height,bool message) {
    const int caption_height=(message?3:1)*line_height+8;
    const int top=line_height+8,bottom=std::max(8,height/9);
    const int available=std::max(1,height-top-caption_height-bottom-12);
    const int caption_width=std::max(1,width*3/5);
    return {(width-caption_width)/2,height-caption_height-bottom,caption_width,caption_height,
        (top-caption_height-bottom-12)/2,
        std::max(1,std::min((width-24)*256/240,available*256/320))};
}
} // namespace family_robot
