#pragma once
#include "chick_face_model.h"
#include <algorithm>
#include <cstddef>
#include <cstdint>

namespace family_robot {
// A bounded, allocation-free RGB565 rasterizer. The caller owns the 150KiB
// PSRAM canvas; neither this class nor the audio observer allocates a buffer.
class FaceRenderer {
public:
    static constexpr int kWidth=240,kHeight=320;
    static constexpr size_t kBytes=size_t(kWidth)*kHeight*sizeof(uint16_t);
    static void Render(uint16_t* pixels,const FaceFrame& frame) {
        if(!pixels)return;
        Canvas c(pixels,frame);
        std::fill_n(pixels,size_t(kWidth)*kHeight,c.bg);
        if(!frame.shown)return;
        c.Effects();c.Blush();c.Eye(74,frame.preset.left);c.Eye(166,frame.preset.right);
        c.Brows();c.Mouth();
    }
private:
    struct Point {int x,y;};
    static int Clamp(int n,int lo,int hi){return n<lo?lo:(n>hi?hi:n);}
    static float Bound(float n,float lo,float hi) {
        if(!std::isfinite(n))return lo;
        return n<lo?lo:(n>hi?hi:n);
    }
    static uint16_t Color(uint32_t rgb) {
        return static_cast<uint16_t>(((rgb>>8)&0xf800)|((rgb>>5)&0x07e0)|((rgb>>3)&0x001f));
    }
    static uint32_t Blend(uint32_t rgb,uint32_t background,float opacity) {
        const int a=static_cast<int>(Bound(opacity,0,1)*256);
        const auto channel=[&](int shift){return ((((rgb>>shift)&255)*a+
            ((background>>shift)&255)*(256-a))>>8)<<shift;};
        return channel(16)|channel(8)|channel(0);
    }
    class Canvas {
    public:
        Canvas(uint16_t* p,const FaceFrame& frame):pixels(p),f(frame),bg(Color(f.preset.background)),
            eye(Color(f.preset.eye_color)) {
            scale=Bound(f.breath,0.9f,1.1f);
            const float angle=Bound(f.tilt,-15,15)*0.01745329252f;
            // Exactly two trigonometric evaluations per frame, never per pixel.
            cs=std::cos(angle)*scale;sn=std::sin(angle)*scale;
        }
        uint16_t* pixels;const FaceFrame& f;uint16_t bg,eye;
        float scale=1,cs=1,sn=0;
        Point Map(float x,float y)const {
            x=Bound(x,-400,640)-120;y=Bound(y,-400,720)-160+Bound(f.bob_y,-8,8);
            return {static_cast<int>(120+x*cs-y*sn),static_cast<int>(160+x*sn+y*cs)};
        }
        void Span(int y,int x0,int x1,uint16_t color) {
            if(y<0 || y>=kHeight || x1<0 || x0>=kWidth)return;
            x0=Clamp(x0,0,kWidth-1);x1=Clamp(x1,0,kWidth-1);
            if(x0<=x1)std::fill_n(pixels+size_t(y)*kWidth+x0,size_t(x1-x0+1),color);
        }
        void DiskRaw(int x,int y,int rx,int ry,uint16_t color) {
            rx=Clamp(rx,1,150);ry=Clamp(ry,1,150);
            // Integer ellipse spans: at most ry*rx simple comparisons, no sqrt.
            const int64_t rr=int64_t(rx)*rx*ry*ry;
            int dx=rx;
            for(int dy=0;dy<=ry;++dy) {
                while(dx>0 && int64_t(dx)*dx*ry*ry+int64_t(dy)*dy*rx*rx>rr)--dx;
                Span(y+dy,x-dx,x+dx,color);
                if(dy)Span(y-dy,x-dx,x+dx,color);
            }
        }
        void Ellipse(float x,float y,float rx,float ry,uint16_t color) {
            const auto p=Map(x,y);
            DiskRaw(p.x,p.y,static_cast<int>(Bound(rx,1,140)*scale),
                static_cast<int>(Bound(ry,1,140)*scale),color);
        }
        void Rect(float x,float y,float w,float h,uint16_t color) {
            const Point p[]={Map(x,y),Map(x+w,y),Map(x+w,y+h),Map(x,y+h)};
            PolygonRaw(p,4,color);
        }
        void PolygonRaw(const Point* p,int count,uint16_t color) {
            if(count<3 || count>48)return;
            int ymin=kHeight-1,ymax=0;
            for(int i=0;i<count;++i){ymin=std::min(ymin,p[i].y);ymax=std::max(ymax,p[i].y);}
            ymin=Clamp(ymin,0,kHeight-1);ymax=Clamp(ymax,0,kHeight-1);
            for(int y=ymin;y<=ymax;++y) {
                int crossings[48];int n=0;
                for(int i=0,j=count-1;i<count;j=i++) {
                    const auto a=p[j],b=p[i];
                    if((a.y<=y && b.y>y)||(b.y<=y && a.y>y))
                        crossings[n++]=a.x+(y-a.y)*(b.x-a.x)/(b.y-a.y);
                }
                // Tiny stack array; insertion sort avoids library scratch storage.
                for(int i=1;i<n;++i){int v=crossings[i],j=i;while(j>0 && crossings[j-1]>v){crossings[j]=crossings[j-1];--j;}crossings[j]=v;}
                for(int i=0;i+1<n;i+=2)Span(y,crossings[i],crossings[i+1],color);
            }
        }
        void Line(float x0,float y0,float x1,float y1,int width,uint16_t color) {
            auto a=Map(x0,y0);const auto b=Map(x1,y1);
            const int dx=std::abs(b.x-a.x),sx=a.x<b.x?1:-1;
            const int dy=-std::abs(b.y-a.y),sy=a.y<b.y?1:-1;
            int error=dx+dy;const int radius=Clamp(static_cast<int>(width*scale/2),1,10);
            for(int step=0;step<1600;++step) {
                DiskRaw(a.x,a.y,radius,radius,color);
                if(a.x==b.x && a.y==b.y)break;
                const int e2=2*error;if(e2>=dy){error+=dy;a.x+=sx;}if(e2<=dx){error+=dx;a.y+=sy;}
            }
        }
        void Quad(float x0,float y0,float cx,float cy,float x1,float y1,int width,uint16_t color) {
            float px=x0,py=y0;
            for(int i=1;i<=20;++i){const float t=i/20.0f,u=1-t;
                const float x=u*u*x0+2*u*t*cx+t*t*x1,y=u*u*y0+2*u*t*cy+t*t*y1;
                Line(px,py,x,y,width,color);px=x;py=y;}
        }
        void CubicPoints(Point* points,int& count,float x0,float y0,float x1,float y1,
                         float x2,float y2,float x3,float y3) {
            for(int i=0;i<=16;++i){const float t=i/16.0f,u=1-t;
                points[count++]=Map(u*u*u*x0+3*u*u*t*x1+3*u*t*t*x2+t*t*t*x3,
                    u*u*u*y0+3*u*u*t*y1+3*u*t*t*y2+t*t*t*y3);}
        }
        void QuadPoints(Point* points,int& count,float x0,float y0,float cx,float cy,float x1,float y1) {
            for(int i=0;i<=12;++i){const float t=i/12.0f,u=1-t;
                points[count++]=Map(u*u*x0+2*u*t*cx+t*t*x1,u*u*y0+2*u*t*cy+t*t*y1);}
        }
        void OutlineEllipse(float x,float y,float rx,float ry,int width,uint16_t color) {
            // Unit-circle vertices, avoiding trig in ellipse and effect paths.
            static constexpr float circle[16][2]={{1,0},{.924f,.383f},{.707f,.707f},{.383f,.924f},
                {0,1},{-.383f,.924f},{-.707f,.707f},{-.924f,.383f},{-1,0},
                {-.924f,-.383f},{-.707f,-.707f},{-.383f,-.924f},{0,-1},
                {.383f,-.924f},{.707f,-.707f},{.924f,-.383f}};
            for(int i=0;i<16;++i){const int j=(i+1)%16;Line(x+circle[i][0]*rx,y+circle[i][1]*ry,
                x+circle[j][0]*rx,y+circle[j][1]*ry,width,color);}
        }
        void Star(float x,float y,float radius,uint16_t color) {
            static constexpr float star[10][2]={{0,-1},{.259f,-.356f},{.951f,-.309f},
                {.419f,.136f},{.588f,.809f},{0,.441f},{-.588f,.809f},
                {-.419f,.136f},{-.951f,-.309f},{-.259f,-.356f}};
            Point p[10];for(int i=0;i<10;++i)p[i]=Map(x+star[i][0]*radius,y+star[i][1]*radius);
            PolygonRaw(p,10,color);
        }
        void Heart(float x,float y) {
            Point p[34];int n=0;
            CubicPoints(p,n,x,y+22,x-34,y-4,x-20,y-30,x,y-12);
            CubicPoints(p,n,x,y-12,x+20,y-30,x+34,y-4,x,y+22);
            PolygonRaw(p,n,eye);
        }
        void Eye(float x,const EyeGeometry& geometry) {
            const float blink=Bound(f.blink,0,1),openness=Bound(geometry.open,0,1.2f)*(1-blink);
            const float y=132;
            if(geometry.shape==EyeShape::Closed || blink>0.92f) {
                Quad(x-30,y,x,y+13,x+30,y,9,eye);return;
            }
            switch(geometry.shape) {
            case EyeShape::Happy:Quad(x-31,y+9,x,y-22*(1-blink),x+31,y+9,9,eye);return;
            case EyeShape::Sad:Quad(x-31,y-7,x,y+20*(1-blink),x+31,y-7,9,eye);return;
            case EyeShape::Heart:Heart(x,y);return;
            case EyeShape::Star:Star(x,y,34,eye);return;
            case EyeShape::Spiral:OutlineEllipse(x,y,33,33,8,eye);OutlineEllipse(x,y,16,16,7,eye);return;
            default:break;
            }
            const bool wide=geometry.shape==EyeShape::Wide;
            const float ry=42*openness*(wide?1.2f:1);
            Ellipse(x,y,32,ry,eye);
            if(geometry.shape!=EyeShape::Squint) {
                const float px=x+Bound(geometry.pupil_x,-1,1)*13+Bound(f.gaze_x,-1,1)*12;
                const float py=y+Bound(geometry.pupil_y,-1,1)*13+Bound(f.gaze_y,-1,1)*12;
                Ellipse(px,py,wide?13:12,std::min(ry,wide?13.0f:12.0f),Color(0x14181f));
                if(ry>12)Ellipse(px-4,py-5,4.5f,4.5f,Color(0xffffff));
            }
            if(geometry.shape==EyeShape::Half || geometry.shape==EyeShape::Squint) {
                Rect(x-36,y-ry-2,72,ry*.55f+2,bg);
                Line(x-34,y-ry*.44f,x+34,y-ry*.44f,7,eye);
            }
        }
        void Blush() {
            const float level=Bound(f.preset.blush,0,1);if(level<=0)return;
            const auto color=Color(Blend(0xff7a6b,f.preset.background,.28f+level*.45f));
            Ellipse(46,182,17,10,color);Ellipse(194,182,17,10,color);
        }
        void Brows() {
            const auto& b=f.preset.brow;if(!b.visible)return;
            const float y=76+Bound(b.y,-20,20),split=b.split?9:0;
            // The source angle range is +/-16 degrees. One tangent per face.
            const float t=std::tan(Bound(b.angle,-30,30)*.01745329252f)*50;
            Line(54,y-split,104,y-split-t,7,eye);Line(136,y+split-t,186,y+split,7,eye);
        }
        void Mouth() {
            const auto& m=f.preset.mouth;const float x=120,y=208,w=26*Bound(m.width,0.2f,1.5f);
            if(f.state==FaceState::Speaking) {
                if(f.mouth_level==0)Quad(x-w*.7f,y,x,y+5,x+w*.7f,y,6,eye);
                else {const float opening=f.mouth_level/255.0f;
                    Ellipse(x,y+4,w*.92f,3+opening*24,eye);
                    if(m.shape==MouthShape::Tongue && opening>.25f)
                        Ellipse(x,y+8+opening*12,9,4,Color(0xff8a7a));}
                return;
            }
            switch(m.shape) {
            case MouthShape::None:break;
            case MouthShape::Flat:Line(x-w,y,x+w,y,8,eye);break;
            case MouthShape::Smile:Quad(x-w,y-3,x,y+18,x+w,y-3,8,eye);break;
            case MouthShape::Sad:Quad(x-w,y+13,x,y-7,x+w,y+13,8,eye);break;
            case MouthShape::Wavy:Quad(x-w,y,x-w/2,y-11,x,y,7,eye);Quad(x,y,x+w/2,y+11,x+w,y,7,eye);break;
            case MouthShape::O:Ellipse(x,y+2,12,16,eye);break;
            case MouthShape::Open:Ellipse(x,y+4,w*.92f,7+Bound(m.open,0,1)*20,eye);break;
            case MouthShape::Grin:case MouthShape::Tongue: {
                Point p[39];int n=0;const float bottom=m.shape==MouthShape::Tongue?22:24;
                QuadPoints(p,n,x-w,y-6,x,y-3,x+w,y-6);
                QuadPoints(p,n,x+w,y-6,x+w-2,y+bottom,x,y+bottom);
                QuadPoints(p,n,x,y+bottom,x-w+2,y+bottom,x-w,y-6);
                PolygonRaw(p,n,eye);
                if(m.shape==MouthShape::Tongue) {
                    Point tongue[26];int count=0;
                    QuadPoints(tongue,count,x-9,y+14,x,y+26,x+9,y+14);
                    QuadPoints(tongue,count,x+9,y+14,x,y+21,x-9,y+14);
                    PolygonRaw(tongue,count,Color(0xff8a7a));
                }break;
            }
            case MouthShape::Smirk:Quad(x-w,y+3,x,y+15,x+w,y-3,8,eye);break;
            }
        }
        void Drop(float x,float y,int size) {
            Point p[34];int n=0;
            CubicPoints(p,n,x,y,x-size,y+size*2,x-size/2.0f,y+size*3,x,y+size*3.5f);
            CubicPoints(p,n,x,y+size*3.5f,x+size/2.0f,y+size*3,x+size,y+size*2,x,y);
            PolygonRaw(p,n,Color(0x8fd3ff));
        }
        void Z(float x,float y,int size,uint16_t color) {
            Line(x,y,x+size,y,3,color);Line(x+size,y,x,y+size,3,color);Line(x,y+size,x+size,y+size,3,color);
        }
        void Sparkle(float x,float y,int r) {
            const Point p[]={Map(x,y-r),Map(x+3,y-3),Map(x+r,y),Map(x+3,y+3),
                Map(x,y+r),Map(x-3,y+3),Map(x-r,y),Map(x-3,y-3)};
            PolygonRaw(p,8,eye);
        }
        void Effects() {
            const auto effects=f.preset.effects;const uint32_t t=f.motion_ms;
            if(effects&FxZzz) {
                const int y=static_cast<int>((t%2400)/400);
                Z(176,71-y,12,Color(Blend(f.preset.eye_color,f.preset.background,.85f)));
                Z(194,48-y,16,Color(Blend(f.preset.eye_color,f.preset.background,.65f)));
                Z(213,20-y,20,Color(Blend(f.preset.eye_color,f.preset.background,.45f)));
            }
            if(effects&FxQuestion) {
                const int y=static_cast<int>((t%1600)/400);
                Quad(194,43-y,193,25-y,209,29-y,6,eye);
                Quad(209,29-y,224,38-y,206,49-y,6,eye);
                Line(206,49-y,205,56-y,6,eye);Ellipse(205,68-y,3,3,eye);
            }
            if(effects&FxExclaim){Line(207,31,207,54,7,eye);Ellipse(207,68,4,4,eye);}
            if(effects&FxTears){const int y=static_cast<int>((t%1200)/200);Drop(62,170+y,7);Drop(178,170+y,7);}
            if(effects&FxSweat)Drop(196,96,7);
            if(effects&FxSparkle){Sparkle(44,98,14);Sparkle(200,206,10);}
            if(effects&FxDots) {
                for(int i=0;i<3;++i)Ellipse(96+i*24,272,6,6,
                    (t/350)%3==static_cast<unsigned>(i)?eye:Color(Blend(f.preset.eye_color,f.preset.background,.3f)));
            }
            if(effects&FxBars) {
                for(int i=0;i<3;++i){const int extent=10+i*6,y=92-i*12;
                    const auto color=(t/500)%3>=static_cast<unsigned>(i)?eye:Color(Blend(f.preset.eye_color,f.preset.background,.35f));
                    Quad(196-extent,y,196,y-extent*.7f,196+extent,y,5,color);}
            }
            if(effects&FxRing) {
                // Indeterminate bar, deliberately no invented OTA percentage.
                Line(64,286,176,286,3,eye);Line(64,298,176,298,3,eye);
                Quad(64,286,52,292,64,298,3,eye);Quad(176,286,188,292,176,298,3,eye);
                const int start=62+static_cast<int>((t%1800)*40/1800);
                Line(start,292,start+76,292,4,eye);
            }
            if(effects&FxBattery) {
                Line(171,262,199,262,3,eye);Line(171,278,199,278,3,eye);
                Line(168,265,168,275,3,eye);Line(202,265,202,275,3,eye);
                Rect(202,267,4,6,eye);Rect(172,266,8,8,Color(0xff7a6b));
            }
        }
    };
};
} // namespace family_robot
