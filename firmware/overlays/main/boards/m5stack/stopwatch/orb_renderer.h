#pragma once
#include <cstddef>
#include <cstdint>

namespace family_robot {
// A single caller-owned RGB565 PSRAM buffer. No allocation, trig, sqrt, blur,
// full-screen pass or per-frame retained state. Host snapshots use this code.
class OrbRenderer {
public:
    static constexpr size_t kSide=128,kBytes=kSide*kSide*sizeof(uint16_t);
    static constexpr uint16_t kBackground=0x0000; // RGB565 near-black #020305
    static void Render(uint16_t* pixels,uint32_t time_ms,uint8_t envelope,bool dim) {
        if (!pixels) return;
        const int t=(time_ms/48)%256;
        const int amp=envelope;
        const int radius=49+amp/40+Sin(t)/180;
        const int radius2=radius*radius;
        int bx[5],by[5];
        for (int i=0;i<5;++i) { bx[i]=Sin(t/3+i*51)*31/256; by[i]=Sin(t/4+i*51+64)*32/256; }
        constexpr int color[5][3]{{34,166,240},{136,81,237},{237,116,193},{69,232,225},{159,196,255}};
        const int deform1=Sin(t),deform2=Sin(t+79),deform3=Sin(t+153);
        for (int y=-64;y<64;++y) for (int x=-64;x<64;++x) {
            const int rr=x*x+y*y;
            const int contour=radius2+(deform1*(x*x-y*y)+deform2*2*x*y)/5200+
                deform3*(x*x*x-3*x*y*y)/270000;
            const int edge=contour-rr;
            int r=2,g=3,b=5;
            if (edge> -650) {
                if (edge>=-110) {
                    r=12;g=22;b=52;
                    for (int i=0;i<5;++i) {
                        const int dx=x-bx[i],dy=y-by[i];
                        int w=2000-dx*dx-dy*dy;
                        if (w>0) {
                            w=(w*w)>>14;
                            if(i==4) w/=3; // keep the pale highlight subordinate to the four hues
                            r+=((255-r)*color[i][0]*w)>>16;
                            g+=((255-g)*color[i][1]*w)>>16;
                            b+=((255-b)*color[i][2]*w)>>16;
                        }
                    }
                    const int shade=230-y/2-rr/80;
                    r=r*shade/255; g=g*shade/255; b=b*shade/255;
                    // One soft antialiased edge; no saturated outer ring.
                    int mask=(edge+110)*255/220;
                    if(mask>255) mask=255;
                    r=2+(r-2)*mask/255;g=3+(g-3)*mask/255;b=5+(b-5)*mask/255;
                } else {
                    const int glow=(edge+650)/32;
                    r+=glow/3;g+=glow/2;b+=glow;
                }
            }
            if (dim) {r=r/3;g=g/3;b=b/3;}
            pixels[(y+64)*kSide+x+64]=Rgb(r,g,b);
        }
    }
private:
    static uint16_t Rgb(int r,int g,int b) {
        return static_cast<uint16_t>(((r>>3)<<11)|((g>>2)<<5)|(b>>3));
    }
    static int Sin(int phase) {
        // Bhaskara integer approximation, only ~20 evaluations per frame.
        phase&=255;
        const bool negative=phase>=128; const int x=phase&127;
        const int product=x*(128-x);
        const int value=4096*product/(5*16384-4*product);
        return negative?-value:value;
    }
};
} // namespace family_robot
