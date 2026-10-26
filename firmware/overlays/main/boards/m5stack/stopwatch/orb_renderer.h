#pragma once
#include <array>
#include <cstddef>
#include <cstdint>

namespace family_robot {
namespace orb_detail {
// Flash-resident, bounded lookup tables: 514+2050bytes, no PSRAM or heap.
constexpr auto Sines() {
    std::array<int16_t,257> values{};
    for(int i=0;i<=256;++i) {
        const int x=i&127,product=x*(128-x);
        const int value=4096*product/(5*16384-4*product);
        values[i]=static_cast<int16_t>(i&128?-value:value);
    }
    return values;
}
constexpr auto Reciprocals() {
    std::array<uint16_t,1025> values{};
    for(size_t i=1;i<values.size();++i) values[i]=static_cast<uint16_t>(65535/i);
    return values;
}
inline constexpr auto kSines=Sines();
inline constexpr auto kReciprocals=Reciprocals();
}
// One caller-owned RGB565 buffer. The lightweight domain warp is computed in
// two128-entry row/column arrays, not a fluid simulation or a screen blur.
class OrbRenderer {
public:
    static constexpr size_t kSide=128,kBytes=kSide*kSide*sizeof(uint16_t);
    static constexpr size_t kTableBytes=sizeof(orb_detail::kSines)+sizeof(orb_detail::kReciprocals);
    static constexpr uint16_t kBackground=0x0000;
    static void Render(uint16_t* pixels,uint64_t time_ms,uint8_t envelope,bool dim) {
        if(!pixels) return;
        // Exactly48..50px, with subpixel audio response instead of jumps at
        // envelope100/200. Squaring happens once per frame, not per pixel.
        const int radius_q8=48*256+int(envelope)*512/255;
        const int radius2=(radius_q8*radius_q8+32768)>>16;
        const int deform1=Sin(Phase(time_ms,8700));
        const int deform2=Sin(Phase(time_ms,11300)+20224);
        const int deform3=Sin(Phase(time_ms,15700)+39168);
        const int flow=Phase(time_ms,14900),cross=Phase(time_ms,19300);
        const int ribbon_phase=Phase(time_ms,12700);
        int16_t row_warp[128],column_warp[128];
        for(int i=0;i<128;++i) {
            // Q4 positions keep slow flow below one source pixel continuous.
            row_warp[i]=static_cast<int16_t>(Sin(flow+i*448)*9/16);
            column_warp[i]=static_cast<int16_t>(Sin(cross+i*384)*9/16);
        }
        int bx[4],by[4];
        for(int i=0;i<4;++i) {
            bx[i]=Sin(Phase(time_ms,15100+i*1300)+i*16384)*26/16;
            by[i]=Sin(Phase(time_ms,17300+i*1700)+i*16384+16384)*25/16;
        }
        constexpr int color[4][3]{{34,166,240},{136,81,237},{237,116,193},{69,232,225}};
        for(int y=-64;y<64;++y) for(int x=-64;x<64;++x) {
            const int rr=x*x+y*y;
            const int contour=radius2+(deform1*(x*x-y*y)+deform2*2*x*y)/3600+
                deform3*(x*x*x-3*x*y*y)/200000;
            const int edge=contour-rr;
            int r=2,g=3,b=5;
            if(edge>=-64) {
                const int wx=x*16+row_warp[y+64],wy=y*16+column_warp[x+64];
                // A softly curved moving ribbon bends the hue field. There is
                // one interpolated lookup per painted pixel, no pixel trig.
                const int ribbon=Sin(ribbon_phase+wx*24+wy*16)/2;
                int sum=0,red=0,green=0,blue=0;
                for(int i=0;i<4;++i) {
                    const int dx=wx-bx[i],dy=wy-by[i];
                    int w=3072-((dx*dx+dy*dy)>>8);
                    if(w<=0) continue;
                    w>>=4;w=(w*w)>>8;
                    if(i==0)w=w*(256+ribbon)>>8;
                    else if(i==2)w=w*(256-ribbon)>>8;
                    sum+=w;red+=color[i][0]*w;green+=color[i][1]*w;blue+=color[i][2]*w;
                }
                if(sum) {
                    const int reciprocal=orb_detail::kReciprocals[sum];
                    r=(red*reciprocal)>>16;g=(green*reciprocal)>>16;b=(blue*reciprocal)>>16;
                } else {r=12;g=30;b=69;}
                // Normalized hue blending never accumulates toward white.
                int shade=238-y/5-rr/55+ribbon/5;if(shade>255)shade=255;
                r=r*shade/255;g=g*shade/255;b=b*shade/255;
                int mask=(edge+64)*255/128;if(mask>255)mask=255;
                r=2+(r-2)*mask/255;g=3+(g-3)*mask/255;b=5+(b-5)*mask/255;
            } else if(edge> -260) {
                const int glow=(edge+260)/32;r+=glow/3;g+=glow/2;b+=glow;
            }
            if(dim) {r/=3;g/=3;b/=3;}
            pixels[(y+64)*kSide+x+64]=Rgb(r,g,b);
        }
    }
private:
    static int Phase(uint64_t time_ms,uint32_t period_ms) {
        return static_cast<int>((time_ms%period_ms)*65536/period_ms);
    }
    static int Sin(int phase) {
        const uint32_t p=static_cast<uint32_t>(phase)&65535;
        const int index=p>>8,fraction=p&255;
        return orb_detail::kSines[index]+(orb_detail::kSines[index+1]-orb_detail::kSines[index])*fraction/256;
    }
    static uint16_t Rgb(int r,int g,int b) {
        return static_cast<uint16_t>(((r>>3)<<11)|((g>>2)<<5)|(b>>3));
    }
};
} // namespace family_robot
