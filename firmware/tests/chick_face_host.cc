#include "chick_face_renderer.h"
#include <array>
#include <cassert>
#include <cmath>
#include <cstdio>
#include <cstring>
#include <string>
#include <vector>

using namespace family_robot;

static FaceFacts Facts(int64_t now=1000000) {
    FaceFacts f; f.now_us=now; f.audio.capture=7; f.audio.epoch=3;
    f.audio.output.connection=9; return f;
}
static void Output(FaceFacts& f,uint8_t level=180) {
    auto& o=f.audio.output; o.connection=9; o.identity={9,4};
    o.valid=true; o.active=true; o.stamp_us=f.now_us; o.level=level;
}
static void Presets() {
    assert(kStatePresetCount==12 && kEmotionPresetCount==15);
    const char* names[]={"neutral","happy","laughing","funny","loving","embarrassed",
        "confident","delicious","sad","crying","sleepy","silly","angry","surprised",
        "shocked","thinking","winking","relaxed","confused"};
    const FaceEmotion mapped[]={FaceEmotion::Neutral,FaceEmotion::Happy,FaceEmotion::Happy,
        FaceEmotion::Happy,FaceEmotion::Loving,FaceEmotion::Embarrassed,FaceEmotion::Confident,
        FaceEmotion::Delicious,FaceEmotion::Sad,FaceEmotion::Crying,FaceEmotion::Sleepy,
        FaceEmotion::Silly,FaceEmotion::Angry,FaceEmotion::Surprised,FaceEmotion::Surprised,
        FaceEmotion::Thinking,FaceEmotion::Winking,FaceEmotion::Relaxed,FaceEmotion::Confused};
    for (size_t i=0;i<19;++i) assert(ParseEmotion(names[i])==mapped[i]);
    assert(ParseEmotion(nullptr)==FaceEmotion::Neutral);
    assert(ParseEmotion("new-cloud-emotion")==FaceEmotion::Neutral);
    assert(StatePreset(FaceState::Boot).left.shape==EyeShape::Wide);
    assert(StatePreset(FaceState::WifiConfig).left.pupil_x==-0.7f);
    assert(StatePreset(FaceState::Connecting).left.open==0.9f);
    assert(StatePreset(FaceState::Idle).left.open==0.75f);
    assert(StatePreset(FaceState::Listening).left.pupil_y==-0.15f);
    assert(StatePreset(FaceState::Thinking).left.pupil_x==0.6f);
    assert(StatePreset(FaceState::Speaking).mouth.open==0.5f);
    assert(StatePreset(FaceState::Notifying).brow.y==-6);
    assert(StatePreset(FaceState::Error).background==0x1e1416);
    assert(StatePreset(FaceState::Upgrading).left.shape==EyeShape::Squint);
    assert(StatePreset(FaceState::LowBattery).mouth.width==0.7f);
    assert(StatePreset(FaceState::Charging).blush==0.3f);
    assert(EmotionPreset(FaceEmotion::Neutral).left.open==1);
    assert(EmotionPreset(FaceEmotion::Happy).mouth.shape==MouthShape::Grin);
    assert(EmotionPreset(FaceEmotion::Loving).left.shape==EyeShape::Heart);
    assert(EmotionPreset(FaceEmotion::Embarrassed).blush==1);
    assert(EmotionPreset(FaceEmotion::Confident).brow.split);
    assert(EmotionPreset(FaceEmotion::Delicious).mouth.shape==MouthShape::Tongue);
    assert(EmotionPreset(FaceEmotion::Sad).left.shape==EyeShape::Sad);
    assert(EmotionPreset(FaceEmotion::Crying).effects&FxTears);
    assert(EmotionPreset(FaceEmotion::Sleepy).effects&FxZzz);
    assert(EmotionPreset(FaceEmotion::Silly).left.shape!=EmotionPreset(FaceEmotion::Silly).right.shape);
    assert(EmotionPreset(FaceEmotion::Angry).background==0x1c1416);
    assert(EmotionPreset(FaceEmotion::Surprised).effects&FxSparkle);
    assert(EmotionPreset(FaceEmotion::Winking).right.shape==EyeShape::Closed);
    assert(EmotionPreset(FaceEmotion::Relaxed).mouth.width==0.75f);
    assert(EmotionPreset(FaceEmotion::Confused).effects&FxQuestion);
    assert(EmotionPreset(FaceEmotion::Thinking).effects==StatePreset(FaceState::Thinking).effects);
}
static void StateAndAudio() {
    FaceModel m; auto f=Facts(); auto frame=m.Update(f);
    assert(frame.shown && frame.state==FaceState::Idle && frame.mouth_level==0);
    f.quiet=true; f.now_us+=120000000; frame=m.Update(f);
    assert(frame.state==FaceState::Idle && frame.emotion==FaceEmotion::Neutral);
    f.quiet=false; f.state=FaceState::Listening; frame=m.Update(f);
    assert(frame.state==FaceState::Connecting && !std::strcmp(frame.Caption(),"正在准备语音"));
    f.audio.gate_open=true; frame=m.Update(f); assert(frame.state==FaceState::Listening);
    f.now_us+=100000; f.audio.snapshot_available=false;
    assert(m.Update(f).state==FaceState::Listening);
    f.now_us+=100001; assert(m.Update(f).state==FaceState::Connecting);
    f.audio.snapshot_available=true; f.audio.gate_open=true; m.Update(f);
    f.audio.snapshot_available=false; ++f.audio.epoch;
    assert(m.Update(f).state==FaceState::Connecting);
    f.audio.snapshot_available=true; m.Update(f); f.audio.snapshot_available=false;
    ++f.audio.capture; assert(m.Update(f).state==FaceState::Connecting);
    f.audio.snapshot_available=true; m.Update(f); f.audio.snapshot_available=false;
    ++f.audio.output.connection; assert(m.Update(f).state==FaceState::Connecting);
    f.audio.snapshot_available=true; f.state=FaceState::Speaking;
    assert(m.Update(f).mouth_level==0);
    FaceModel boot; auto b=Facts(); b.state=FaceState::Boot;
    assert(boot.Update(b).blink==1);
    b.now_us+=300000; assert(boot.Update(b).blink==0);
    b.now_us+=80000; assert(boot.Update(b).blink>0.9f);
    b.now_us+=200000; assert(boot.Update(b).blink==0);
    Output(f); frame=m.Update(f); assert(frame.state==FaceState::Speaking && frame.mouth_level>0);
    f.audio.output.draining=true; f.now_us+=20000; Output(f);
    frame=m.Update(f); assert(frame.state==FaceState::Speaking && frame.draining && frame.mouth_level>0);
    f.audio.output.identity.connection=8; assert(m.Update(f).mouth_level==0);
    Output(f); f.audio.output.identity.playback=0; assert(m.Update(f).mouth_level==0);
    Output(f); f.audio.output.valid=false; assert(m.Update(f).mouth_level==0);
    Output(f); f.audio.output.active=false; assert(m.Update(f).mouth_level==0);
    Output(f); f.audio.output.stamp_us=f.now_us+1; assert(m.Update(f).mouth_level==0);
    Output(f); f.now_us+=350001; assert(m.Update(f).mouth_level==0);
    f.state=FaceState::Idle; f.model_thinking=true;
    assert(m.Update(f).state==FaceState::Thinking);
    f.model_thinking=false; f.low_battery=true;
    assert(m.Update(f).state==FaceState::LowBattery);
    f.charging=true; assert(m.Update(f).state==FaceState::Charging);
    f.state=FaceState::Listening; f.audio.gate_open=true;
    assert(m.Update(f).state==FaceState::Listening);
    Output(f); assert(m.Update(f).state==FaceState::Speaking);
    for (auto state:{FaceState::Error,FaceState::Upgrading,FaceState::Notifying}) {
        f.state=state; m.SetEmotion("happy",f.now_us); frame=m.Update(f);
        assert(frame.state==state && frame.mouth_level==0);
    }
    f.screen_on=false; assert(!m.Update(f).shown);
    f.screen_on=true; f.system_page=true; assert(m.Update(f).state==FaceState::Hidden);
    // Observations preceding the current presentation epoch cannot reopen mouth.
    f.system_page=false; f.state=FaceState::Idle; f.now_us+=100;
    assert(m.Update(f).mouth_level==0);
}
static void SystemCaptions() {
    FaceFrame frame;
    for(unsigned state=0;state<kStatePresetCount;++state) {
        frame.state=static_cast<FaceState>(state);
        const std::string caption=frame.Caption();
        assert(!caption.empty());
        // A mood and its animation may decorate L0, but must not rename it.
        for(int emotion=0;emotion<=static_cast<int>(FaceEmotion::Thinking);++emotion) {
            frame.emotion=static_cast<FaceEmotion>(emotion);
            frame.blink=1;frame.gaze_x=0.5f;frame.bob_y=2;
            assert(caption==frame.Caption());
        }
    }
    frame.state=FaceState::Hidden;assert(!*frame.Caption());
    frame.state=FaceState::Idle;assert(!std::strcmp(frame.Caption(),"待机中"));
    frame.state=FaceState::Thinking;assert(!std::strcmp(frame.Caption(),"正在思考"));
    frame.draining=true;frame.state=FaceState::Speaking;
    assert(!std::strcmp(frame.Caption(),"正在停播"));
    frame.state=FaceState::Connecting;frame.preparing=true;
    assert(!std::strcmp(frame.Caption(),"正在停播"));frame.preparing=false;
    frame.state=FaceState::Upgrading;assert(!std::strcmp(frame.Caption(),"正在升级"));
    frame.state=FaceState::Error;assert(!std::strcmp(frame.Caption(),"设备异常，请重试"));
}
static void EmotionsAndMotion() {
    FaceModel m; auto f=Facts(); m.Update(f); m.SetEmotion("happy",f.now_us);
    auto frame=m.Update(f); assert(frame.emotion==FaceEmotion::Happy);
    f.now_us+=100000; m.SetEmotion("laughing",f.now_us);
    frame=m.Update(f); assert(frame.motion_ms==100);
    f.now_us+=1700000; assert(m.Update(f).emotion==FaceEmotion::Happy);
    f.now_us+=300001; assert(m.Update(f).emotion==FaceEmotion::Neutral);
    m.SetEmotion("loving",f.now_us); Output(f); m.Update(f);
    f.now_us+=4999999; Output(f); assert(m.Update(f).emotion==FaceEmotion::Loving);
    ++f.now_us; Output(f); assert(m.Update(f).emotion==FaceEmotion::Neutral);
    m.SetEmotion("thinking",f.now_us); f.audio.output.active=false;
    assert(m.Update(f).state==FaceState::Idle && m.Update(f).emotion==FaceEmotion::Thinking);
    m.SetEmotion("neutral",f.now_us); assert(m.Update(f).state==FaceState::Idle);
    bool blink=false,gaze=false,tilt=false;
    for (int t=0;t<100000;t+=20) {
        f.now_us=20000000+int64_t(t)*1000; frame=m.Update(f);
        blink|=frame.blink>0; gaze|=std::abs(frame.gaze_x)>0.01f; tilt|=std::abs(frame.tilt)>0.1f;
        assert(frame.blink>=0 && frame.blink<=1);
        assert(frame.breath>=1 && frame.breath<=1.0151f);
        assert(std::abs(frame.gaze_x)<=1 && std::abs(frame.gaze_y)<=1);
        assert(frame.emotion!=FaceEmotion::Sleepy && frame.state==FaceState::Idle);
    }
    assert(blink && gaze && tilt);
}
static void TransitionRegressions() {
    // A temporary listening phase must not restart the two-second afterglow.
    FaceModel m; auto f=Facts();m.SetEmotion("happy",f.now_us);Output(f);m.Update(f);
    f.now_us+=100000;f.audio.output.active=false;f.audio.gate_open=true;f.state=FaceState::Listening;
    assert(m.Update(f).state==FaceState::Listening);
    f.now_us+=2200000;m.Update(f);f.state=FaceState::Idle;
    assert(m.Update(f).emotion==FaceEmotion::Neutral);
    for(bool system_page:{false,true}) {
        FaceModel hidden;f=Facts();hidden.SetEmotion("happy",f.now_us);Output(f);hidden.Update(f);
        f.now_us+=100000;f.system_page=system_page;f.screen_on=system_page;assert(!hidden.Update(f).shown);
        f.now_us+=2900000;f.system_page=false;f.screen_on=true;f.audio.output.active=false;
        assert(hidden.Update(f).emotion==FaceEmotion::Neutral);
    }
    // An emotional thinking alias is finite and cannot invent a generation state.
    FaceModel hint;f=Facts();hint.SetEmotion("thinking",f.now_us);
    auto frame=hint.Update(f);assert(frame.state==FaceState::Idle && frame.emotion==FaceEmotion::Thinking);
    f.now_us+=5000001;frame=hint.Update(f);
    assert(frame.state==FaceState::Idle && frame.emotion==FaceEmotion::Neutral);
    f.model_thinking=true;f.now_us+=9000000;assert(hint.Update(f).state==FaceState::Thinking);
    f.model_thinking=false;assert(hint.Update(f).state==FaceState::Idle);
    FaceModel mood_thinking;f=Facts();mood_thinking.Update(f);f.now_us+=100000000;
    mood_thinking.SetEmotion("thinking",f.now_us);frame=mood_thinking.Update(f);
    assert(frame.state==FaceState::Idle && frame.emotion==FaceEmotion::Thinking);
    assert(std::abs(frame.gaze_x-.08f)<.0001f && frame.tilt==0);
    f.now_us+=100000;frame=mood_thinking.Update(f);
    assert(std::abs(frame.gaze_x-.07901507f)<.0001f && frame.tilt==0);
    // Captions/state follow current facts while geometry crosses over in150ms.
    FaceModel blend;f=Facts();frame=blend.Update(f);assert(frame.preset.left.open==.75f);
    f.state=FaceState::Listening;f.audio.gate_open=true;frame=blend.Update(f);
    assert(frame.state==FaceState::Listening && !std::strcmp(frame.Caption(),"正在聆听"));
    assert(frame.preset.left.open==.75f);
    f.now_us+=75000;frame=blend.Update(f);assert(std::abs(frame.preset.left.open-.875f)<.001f);
    f.now_us+=75000;frame=blend.Update(f);assert(frame.preset.left.open==1);
    f.state=FaceState::Idle;blend.SetEmotion("happy",f.now_us);blend.Update(f);
    f.now_us+=150000;assert(blend.Update(f).preset.blush==.5f);
    blend.SetEmotion("embarrassed",f.now_us);blend.Update(f);f.now_us+=75000;
    frame=blend.Update(f);assert(std::abs(frame.preset.blush-.75f)<.001f && std::abs(frame.preset.left.open-.8f)<.001f);
    f.now_us+=75000;assert(blend.Update(f).preset.blush==1);
    Output(f);assert(blend.Update(f).mouth_level>0);
    f.audio.output.valid=false;assert(blend.Update(f).mouth_level==0);
    FaceModel mouth;f=Facts();mouth.SetEmotion("happy",f.now_us);Output(f);mouth.Update(f);
    f.state=FaceState::Speaking;f.audio.output.valid=false;f.audio.gate_open=true;
    for(int step=0;step<4;++step){frame=mouth.Update(f);
        assert(frame.state==FaceState::Listening && frame.mouth_level==0);
        assert(frame.preset.mouth.shape==MouthShape::Smile);
        f.now_us+=50000;}
    FaceModel colors;f=Facts();colors.Update(f);colors.SetEmotion("angry",f.now_us);colors.Update(f);
    f.now_us+=75000;frame=colors.Update(f);
    assert(frame.preset.background==0x18161b && frame.preset.brow.angle==-8 && frame.preset.brow.y==1);
    assert(std::abs(frame.preset.mouth.width-.675f)<.001f);
    for(auto state:{FaceState::Error,FaceState::Upgrading}) {
        FaceModel still;f=Facts();f.state=state;
        for(int t=0;t<10000;t+=20){f.now_us=1000000+int64_t(t)*1000;frame=still.Update(f);
            assert(frame.blink==0 && frame.gaze_x==0 && frame.gaze_y==0 && frame.tilt==0 && frame.breath==1);}
    }
    FaceModel happy;f=Facts(1125000);happy.SetEmotion("happy",f.now_us);frame=happy.Update(f);
    assert(frame.tilt==0); // happiness bounces vertically, without rocking the face
    // Positive prior/current drain evidence identifies an interrupted turn.
    // Neither flag means normal completion; it must not invent a surprise.
    for(bool prior:{false,true}) for(bool current:{false,true}) {
        FaceModel interrupted;f=Facts();Output(f);f.audio.output.draining=prior;interrupted.Update(f);
        f.now_us+=20000;f.audio.output.active=false;f.audio.output.draining=current;
        f.audio.gate_open=true;f.state=FaceState::Listening;frame=interrupted.Update(f);
        assert(frame.state==FaceState::Listening);
        assert((frame.emotion==FaceEmotion::Surprised)==(prior || current));
        f.now_us+=250000;assert(interrupted.Update(f).emotion!=FaceEmotion::Surprised);
    }
    FaceModel power;f=Facts();f.state=FaceState::LowBattery;
    assert(power.Update(f).state==FaceState::LowBattery);
    f.state=FaceState::Charging;assert(power.Update(f).state==FaceState::Charging);
}
static uint64_t RenderChecks() {
    constexpr size_t pixels=FaceRenderer::kWidth*FaceRenderer::kHeight;
    static_assert(FaceRenderer::kBytes==pixels*sizeof(uint16_t),"RGB565 byte count");
    std::vector<uint16_t> guarded(pixels+2,0xdead); uint64_t total=0;
    FaceFrame frame; frame.shown=true;
    for (int kind=0;kind<29;++kind) for (uint32_t t:{0u,80u,400u,1600u,15000u,90000u,0xffffffffu}) {
        frame.preset=kind<12?StatePreset(static_cast<FaceState>(kind)):
            EmotionPreset(static_cast<FaceEmotion>(kind-12));
        frame.state=kind<12?static_cast<FaceState>(kind):FaceState::Idle;
        frame.motion_ms=t; frame.blink=t==80?1:0; frame.tilt=t==400?6:0;
        frame.gaze_x=t==1600?1:-1; frame.gaze_y=0.7f; frame.breath=1.015f;
        FaceRenderer::Render(guarded.data()+1,frame);
        assert(guarded.front()==0xdead && guarded.back()==0xdead);
        size_t colored=0; for(size_t p=1;p<=pixels;++p){colored+=guarded[p]!=guarded[1];total+=guarded[p];}
        assert(colored>60);
        // Exercise the real live-mouth branch for every geometry and amplitude,
        // including invalid/stale observations represented by level zero.
        for(uint8_t level:{uint8_t(0),uint8_t(1),uint8_t(64),uint8_t(128),uint8_t(255)}) {
            frame.state=FaceState::Speaking;frame.mouth_level=level;
            FaceRenderer::Render(guarded.data()+1,frame);
            assert(guarded.front()==0xdead && guarded.back()==0xdead);
        }
    }
    FaceRenderer::Render(nullptr,frame);
    frame=FaceFrame{};frame.shown=true;frame.state=FaceState::Idle;
    frame.preset=EmotionPreset(FaceEmotion::Happy);
    FaceRenderer::Render(guarded.data()+1,frame);
    assert(guarded[1+230*FaceRenderer::kWidth+120]==0xfed5); // SVG grin reaches cy+24
    frame.preset=EmotionPreset(FaceEmotion::Delicious);
    FaceRenderer::Render(guarded.data()+1,frame);
    assert(guarded[1+228*FaceRenderer::kWidth+120]==0xfed5); // tongue mouth reaches cy+22
    frame.shown=false; FaceRenderer::Render(guarded.data()+1,frame);
    for(size_t i=2;i<=pixels;++i)assert(guarded[i]==guarded[1]);
    return total;
}
static void WritePpm(const std::string& path,const FaceFrame& frame) {
    std::array<uint16_t,FaceRenderer::kWidth*FaceRenderer::kHeight> pixels{};
    FaceRenderer::Render(pixels.data(),frame); auto* file=std::fopen(path.c_str(),"wb"); assert(file);
    std::fprintf(file,"P6\n%d %d\n255\n",FaceRenderer::kWidth,FaceRenderer::kHeight);
    for(auto c:pixels){unsigned char rgb[]={static_cast<unsigned char>(((c>>11)&31)*255/31),
        static_cast<unsigned char>(((c>>5)&63)*255/63),static_cast<unsigned char>((c&31)*255/31)};
        assert(std::fwrite(rgb,1,3,file)==3);}
    assert(std::fclose(file)==0);
}
static void Gallery(const char* directory) {
    FaceFrame f; f.shown=true; f.breath=1; f.motion_ms=420;
    const char* states[]={"boot","wifi_config","connecting","idle","listening","thinking",
        "speaking","notifying","error","upgrading","low_battery","charging"};
    const char* emotions[]={"neutral","happy","loving","embarrassed","confident","delicious",
        "sad","crying","sleepy","silly","angry","surprised","winking","relaxed","confused","thinking"};
    for(int i=0;i<12;++i){f.preset=StatePreset(static_cast<FaceState>(i));
        WritePpm(std::string(directory)+"/L0_"+states[i]+".ppm",f);}
    for(int i=0;i<16;++i){f.preset=EmotionPreset(ParseEmotion(emotions[i]));
        WritePpm(std::string(directory)+"/L1_"+emotions[i]+".ppm",f);}
    puts("28 production-renderer gallery frames written (27 source presets plus shared thinking)");
}
static void DumpPreset(const FacePreset& p) {
    const char* eyes[]={"open","wide","half","squint","closed","happy","sad","heart","star","spiral"};
    const char* mouths[]={"none","flat","smile","sad","wavy","o","open","grin","tongue","smirk"};
    std::printf("{\"left\":{\"shape\":\"%s\",\"open\":%.5g,\"pupil_x\":%.5g,\"pupil_y\":%.5g},"
        "\"right\":{\"shape\":\"%s\",\"open\":%.5g,\"pupil_x\":%.5g,\"pupil_y\":%.5g},"
        "\"mouth\":{\"shape\":\"%s\",\"width\":%.5g,\"open\":%.5g},"
        "\"brow\":{\"visible\":%s,\"y\":%.5g,\"angle\":%.5g,\"split\":%s},"
        "\"blush\":%.5g,\"background\":%u,\"eye_color\":%u,\"effects\":[",
        eyes[static_cast<int>(p.left.shape)],p.left.open,p.left.pupil_x,p.left.pupil_y,
        eyes[static_cast<int>(p.right.shape)],p.right.open,p.right.pupil_x,p.right.pupil_y,
        mouths[static_cast<int>(p.mouth.shape)],p.mouth.width,p.mouth.open,
        p.brow.visible?"true":"false",p.brow.y,p.brow.angle,p.brow.split?"true":"false",
        p.blush,p.background,p.eye_color);
    const char* effects[]={"zzz","question","exclaim","tears","sweat","sparkle","dots","bars","ring","battery"};
    bool first=true;for(int bit=0;bit<10;++bit)if(p.effects&(1<<bit)) {
        std::printf("%s\"%s\"",first?"":",",effects[bit]);first=false;}
    std::printf("]}");
}
static void DumpPresets() {
    std::printf("[");for(int i=0;i<27;++i){if(i)std::printf(",");
        DumpPreset(i<12?StatePreset(static_cast<FaceState>(i)):EmotionPreset(static_cast<FaceEmotion>(i-12)));}
    std::printf("]\n");
}
int main(int argc,char** argv) {
    if(argc==3 && !std::strcmp(argv[1],"--gallery")){Gallery(argv[2]);return 0;}
    if(argc==2 && !std::strcmp(argv[1],"--presets")){DumpPresets();return 0;}
    Presets(); StateAndAudio(); SystemCaptions(); EmotionsAndMotion();TransitionRegressions();
    std::printf("CHICK-01 model/render checks passed; pixel checksum=%llu\n",
        static_cast<unsigned long long>(RenderChecks()));
}
