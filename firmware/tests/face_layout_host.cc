#include "face_layout.h"
#include <cassert>
using namespace family_robot;
int main() {
    for(const auto size:{240,466,480})for(const int line:{20,30,40})for(bool notice:{false,true}) {
        const int height=size==240?320:size;
        auto l=ComputeFaceLayout(size,height,line,notice);
        assert(l.caption_x>=0 && l.caption_y>=0 && l.caption_x+l.caption_width<=size);
        assert(l.caption_y+l.caption_height<=height && l.scale>0);
        assert(320*l.scale/256<=height-line-8-l.caption_height-height/9-11);
        if(size!=240) {
            // Caption box corners, including multiline notices, fit round M5.
            const int dx=l.caption_width/2+1;
            for(int y:{l.caption_y,l.caption_y+l.caption_height}) {
                const int dy=y-size/2;
                assert(dx*dx+dy*dy<=(size/2)*(size/2));
            }
        }
    }
}
