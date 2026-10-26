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
    // Includes the soft glow, smooth drift and maximum Q8 audio expansion.
    // The view uses this painted bound when fitting the native text viewport.
    static constexpr int kArtworkRadius=63;
    static constexpr uint16_t kBackground=0x0000;
    static void Render(uint16_t* pixels,uint64_t time_ms,uint8_t envelope,bool dim) {
        if(!pixels) return;
        // A continuously turning ellipse and cubic contour provide visible
        // silent motion. Audio adds only a small, continuous Q8 expansion.
        const int angle=Phase(time_ms,8100);
        const int deform1=Sin(angle),deform2=Sin(angle+16384);
        const int deform3=Sin(Phase(time_ms,6900)+21000);
        const int center_x=Sin(Phase(time_ms,9700))*3/16;
        const int center_y=Sin(Phase(time_ms,11300)+16384)*3/16;
        const int breathing_q8=44*256+Sin(Phase(time_ms,6300))*2;
        // Sound approaches A's existing 46px peak instead of adding beyond it.
        // At full level the Q8 radius moves halfway toward that bound; silence
        // retains the approved contour exactly, with no envelope thresholds.
        const int radius_q8=breathing_q8+(46*256-breathing_q8)*int(envelope)/(255*2);
        const int radius2=(radius_q8*radius_q8+32768)>>16;
        const int flow=Phase(time_ms,8300),cross=Phase(time_ms,10700);
        const int ribbon_phase=Phase(time_ms,6100);
        int16_t row_warp[128],column_warp[128];
        for(int i=0;i<128;++i) {
            // Q4 positions keep slow flow below one source pixel continuous.
            row_warp[i]=static_cast<int16_t>(Sin(flow+i*448)*12/16);
            column_warp[i]=static_cast<int16_t>(Sin(cross+i*384)*11/16);
        }
        int bx[4],by[4];
        for(int i=0;i<4;++i) {
            bx[i]=Sin(Phase(time_ms,7300+i*900)+i*16384)*28/16;
            by[i]=Sin(Phase(time_ms,9100+i*1100)+i*16384+16384)*28/16;
        }
        constexpr int color[4][3]{{12,178,250},{128,50,244},{239,64,165},{20,232,222}};
        for(int py=0;py<128;++py) for(int px=0;px<128;++px) {
            const int xq=(px-64)*16-center_x,yq=(py-64)*16-center_y;
            const int xx=(xq*xq)>>8,yy=(yq*yq)>>8,xy=(xq*yq)>>8;
            const int rr=xx+yy,cubic=xq*(xx-3*yy)/16;
            const int contour=radius2+(deform1*(xx-yy)+deform2*2*xy)/1350+
                deform3*cubic/90000;
            const int edge=contour-rr;
            int r=2,g=3,b=5;
            if(edge>=-64) {
                const int wx=xq+row_warp[py],wy=yq+column_warp[px];
                // A softly curved moving ribbon bends the hue field. There is
                // one interpolated lookup per painted pixel, no pixel trig.
                const int ribbon=Sin(ribbon_phase+wx*27+wy*18)/2;
                int sum=0,red=0,green=0,blue=0;
                for(int i=0;i<4;++i) {
                    const int dx=wx-bx[i],dy=wy-by[i];
                    int w=2500-((dx*dx+dy*dy)>>8);
                    if(w<=0) continue;
                    w>>=4;w=(w*w)>>8;
                    if(i==0)w=w*(256+ribbon)>>8;
                    else if(i==2)w=w*(256-ribbon)>>8;
                    sum+=w;red+=color[i][0]*w;green+=color[i][1]*w;blue+=color[i][2]*w;
                }
                if(sum) {
                    const int reciprocal=orb_detail::kReciprocals[sum];
                    r=(red*reciprocal)>>16;g=(green*reciprocal)>>16;b=(blue*reciprocal)>>16;
                } else {r=9;g=24;b=68;}
                // Normalized hue blending never accumulates toward white.
                int shade=245-(yq/16)/7-rr/85+ribbon/7;if(shade>255)shade=255;
                r=r*shade/255;g=g*shade/255;b=b*shade/255;
                int mask=(edge+64)*255/128;if(mask>255)mask=255;
                r=2+(r-2)*mask/255;g=3+(g-3)*mask/255;b=5+(b-5)*mask/255;
            } else if(edge> -200) {
                const int glow=(edge+200)/40;r+=glow/3;g+=glow/2;b+=glow;
            }
            if(dim) {r/=3;g/=3;b/=3;}
            pixels[py*kSide+px]=Rgb(r,g,b);
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
