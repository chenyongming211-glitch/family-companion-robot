#include "chick_face_model.h"
#include "playback_progress.h"
#include <array>
#include <cassert>
#include <cstdio>
#include <cstring>
using namespace family_robot;
using Identity=OrbOutputActivity::Identity;
// Detection keeps the RED executable against the previous production API.
template<class M> static bool Label(M& m,Identity id,const char* text,int64_t now) {
    if constexpr(requires {m.SetPlaybackEmotion(id,text,now);}) return m.SetPlaybackEmotion(id,text,now);
    return false;
}
template<class M> static void Clear(M& m) {
    if constexpr(requires {m.ClearPlaybackEmotions();}) m.ClearPlaybackEmotions();
}
struct Times {int64_t first=0,last=0;bool terminal=false,valid=false;};
template<class O> static Times Timing(const O& o,Identity id) {
    if constexpr(requires {o.playbacks;}) for(const auto& fact:o.playbacks)
        if(fact.identity==id)return {fact.first_us,fact.last_us,fact.terminal,fact.valid};
    return {};
}
template<class O> static void Fact(O& o,Identity id,int64_t first,int64_t last,bool terminal=false,
                                   bool canceled=false,bool draining=false,bool valid=true,size_t index=0) {
    if constexpr(requires {o.playbacks;}) o.playbacks[index]={id,first,last,valid,terminal,canceled,draining};
}
static FaceFacts F(int64_t now=1000000) {
    FaceFacts f;f.now_us=now;f.audio.output.connection=9;return f;
}
static void Playing(FaceFacts& f,Identity id,int64_t first,bool draining=false) {
    auto& o=f.audio.output;o={};o.connection=id.connection;o.identity=id;o.stamp_us=f.now_us;
    o.level=180;o.active=true;o.valid=true;o.draining=draining;
    Fact(o,id,first,f.now_us,false,draining,draining);
}
static void Ended(FaceFacts& f,Identity id,int64_t first,int64_t last,bool canceled=false) {
    auto& o=f.audio.output;o={};o.connection=id.connection;o.identity=id;o.stamp_us=last;o.valid=true;
    Fact(o,id,first,last,true,canceled);
}
static void ActualDmaTimes() {
    PlaybackProgress p;p.Connect(9);PlaybackIdentity id{9,4};assert(p.Start(id));
    std::array<int16_t,240> pcm{};pcm.fill(2000);
    assert(p.Admit(id,0,240));assert(p.Submit(id,0,pcm.data(),240,160)==PlaybackProgress::SubmitResult::Accepted);
    assert(Timing(p.OrbActivity(),{9,4}).first==0);
    std::array<std::array<int16_t,240>,6> dma{};int64_t now=1000000;
    for(auto& block:dma){p.Complete(block.data(),240,now);now+=10000;}
    assert(Timing(p.OrbActivity(),{9,4}).first==0);
    p.Complete(dma[0].data(),240,now);
    const int64_t actual_end=now;auto t=Timing(p.OrbActivity(),{9,4});
    assert(t.first==actual_end && t.last==actual_end && !t.terminal && t.valid);
    p.Release(id);
    // Seal arrives much later than the actual last DMA; it must not move time.
    for(int i=1;i<=60;++i)p.Complete(dma[i%6].data(),240,now+int64_t(i)*10000);
    p.Seal(id,false);t=Timing(p.OrbActivity(),{9,4});
    assert(t.first==actual_end && t.last==actual_end && t.terminal && t.valid);
    PlaybackSnapshot report;assert(p.NextReport(now+1000000,report) && report.finished);
    assert(!p.Snapshot(id).valid); // receipt API still removes the record
    t=Timing(p.OrbActivity(),{9,4});
    assert(t.first==actual_end && t.last==actual_end && t.terminal && t.valid);
    assert(p.Start({9,5}));assert(!p.Start(id));
}
static void ActualDmaExpression() {
    PlaybackProgress p;p.Connect(9);FaceModel model;auto f=F(990000);
    f.audio.output=p.OrbActivity();model.Update(f);
    std::array<int16_t,240> pcm{};pcm.fill(2000);
    std::array<std::array<int16_t,240>,6> dma{};
    assert(p.Start({9,3}));assert(p.Admit({9,3},0,720));
    for(size_t i=0;i<3;++i)assert(p.Submit({9,3},i*240,pcm.data(),240,160)==PlaybackProgress::SubmitResult::Accepted);
    assert(Label(model,{9,3},"loving",f.now_us));
    auto tick=[&] {
        f.now_us+=10000;const size_t slot=static_cast<size_t>((f.now_us-1000000)/10000)%6;
        p.Complete(dma[slot].data(),240,f.now_us);f.audio.output=p.OrbActivity();return model.Update(f);
    };
    for(int i=0;i<6;++i)assert(tick().emotion==FaceEmotion::Neutral);
    assert(tick().emotion==FaceEmotion::Loving);
    p.MarkOrbDraining({9,3});p.Release({9,3});p.Seal({9,3},false);
    assert(p.Start({9,4}));assert(p.Admit({9,4},0,240));
    assert(p.Submit({9,4},0,pcm.data(),240,160)==PlaybackProgress::SubmitResult::Accepted);p.Release({9,4});
    assert(Label(model,{9,4},"happy",f.now_us));
    for(int i=0;i<7;++i) {
        const auto frame=tick();
        assert(frame.emotion==(i==0?FaceEmotion::Loving:(i==6?FaceEmotion::Happy:FaceEmotion::Neutral)));
    }
    const int64_t first=Timing(p.OrbActivity(),{9,4}).first;
    uint64_t received=240;
    for(int i=0;i<600;++i) {
        assert(p.Admit({9,4},received,240));
        assert(p.Submit({9,4},received,pcm.data(),240,160)==PlaybackProgress::SubmitResult::Accepted);
        received+=240;p.Release({9,4});assert(tick().emotion==FaceEmotion::Happy);
    }
    assert(f.now_us-first==6000000);p.Seal({9,4},false);
    for(int i=0;i<6;++i)assert(tick().emotion==FaceEmotion::Happy);
    const auto end=Timing(p.OrbActivity(),{9,4});assert(end.terminal && end.last==f.now_us);
    PlaybackSnapshot report;while(p.NextReport(f.now_us,report)){}
    f.now_us=end.last+2000001;f.audio.output=p.OrbActivity();
    assert(model.Update(f).emotion==FaceEmotion::Neutral);
}
static void BoundTimeline() {
    FaceModel model;auto f=F();model.Update(f);
    assert(Label(model,{9,3},"loving",f.now_us));
    Playing(f,{9,3},f.now_us);assert(model.Update(f).emotion==FaceEmotion::Loving);
    f.now_us+=10000;Playing(f,{9,3},1000000,true);
    assert(Label(model,{9,4},"happy",f.now_us));
    assert(model.Update(f).emotion==FaceEmotion::Loving);
    assert(!Label(model,{9,3},"angry",f.now_us));
    f.now_us+=10000;Playing(f,{9,4},f.now_us);
    assert(model.Update(f).emotion==FaceEmotion::Happy);
    const int64_t first=f.now_us;
    f.now_us+=6000000;Playing(f,{9,4},first);
    assert(model.Update(f).emotion==FaceEmotion::Happy);
    const int64_t last=f.now_us;Ended(f,{9,4},first,last);
    assert(model.Update(f).emotion==FaceEmotion::Happy);
    f.now_us=last+1999999;assert(model.Update(f).emotion==FaceEmotion::Happy);
    f.now_us=last+2000001;assert(model.Update(f).emotion==FaceEmotion::Neutral);
    assert(!Label(model,{9,4},"sad",f.now_us));
    f.now_us+=1;Playing(f,{9,5},f.now_us);
    assert(model.Update(f).emotion==FaceEmotion::Neutral);
    assert(!Label(model,{8,5},"sad",f.now_us));
}
static void ArrivalWindows() {
    for(int64_t age:{int64_t(2000000),int64_t(2000001)}) {
        FaceModel model;auto f=F();Playing(f,{9,4},f.now_us);model.Update(f);
        f.now_us+=age;Playing(f,{9,4},1000000);model.Update(f);
        assert(Label(model,{9,4},"happy",f.now_us)==(age==2000000));
        assert(model.Update(f).emotion==(age==2000000?FaceEmotion::Happy:FaceEmotion::Neutral));
        assert(!Label(model,{9,4},"sad",f.now_us));
    }
    FaceModel neutral;auto f=F();neutral.Update(f);
    assert(Label(neutral,{9,4},"neutral",f.now_us));
    assert(!Label(neutral,{9,4},"happy",f.now_us));
    Playing(f,{9,4},f.now_us);assert(neutral.Update(f).emotion==FaceEmotion::Neutral);
    for(const char* legacy:{"thinking","laughing","funny","shocked","unknown","Happy",""})
        assert(!Label(neutral,{9,5},legacy,f.now_us));
    assert(!Label(neutral,{9,5},nullptr,f.now_us));
    // Canceled before the first actual completion can never resurrect.
    FaceModel canceled;f=F();canceled.Update(f);assert(Label(canceled,{9,4},"happy",f.now_us));
    Ended(f,{9,4},0,0,true);canceled.Update(f);
    f.now_us+=10000;Playing(f,{9,5},f.now_us);
    assert(canceled.Update(f).emotion==FaceEmotion::Neutral);
    assert(!Label(canceled,{9,4},"happy",f.now_us));
    FaceModel draining;f=F();Playing(f,{9,3},f.now_us,true);draining.Update(f);
    assert(!Label(draining,{9,3},"happy",f.now_us));
}
static void HiddenAndInvalidation() {
    for(bool screen_off:{false,true}) {
        FaceModel model;auto f=F();model.Update(f);assert(Label(model,{9,4},"happy",f.now_us));
        Playing(f,{9,4},f.now_us);model.Update(f);
        f.now_us+=100000;f.screen_on=!screen_off;f.state=screen_off?FaceState::Idle:FaceState::Hidden;
        Playing(f,{9,4},1000000);assert(!model.Update(f).shown);
        f.now_us+=100000;Ended(f,{9,4},1000000,f.now_us);assert(!model.Update(f).shown);
        f.now_us+=2500000;f.screen_on=true;f.state=FaceState::Idle;
        assert(model.Update(f).emotion==FaceEmotion::Neutral);
    }
    for(auto state:{FaceState::Error,FaceState::Upgrading,FaceState::Listening}) {
        FaceModel model;auto f=F();model.Update(f);assert(Label(model,{9,4},"happy",f.now_us));
        Playing(f,{9,4},f.now_us);model.Update(f);
        f.now_us+=10000;f.state=state;f.audio.output.active=false;f.audio.gate_open=true;model.Update(f);
        f.now_us+=10000;f.state=FaceState::Idle;Playing(f,{9,4},1000000);
        assert(model.Update(f).emotion==FaceEmotion::Neutral);
    }
    for(bool disconnect:{false,true}) {
        FaceModel model;auto f=F();model.Update(f);assert(Label(model,{9,4},"happy",f.now_us));
        Playing(f,{9,4},f.now_us);model.Update(f);
        f.now_us+=10000;
        if(disconnect){f.audio.output={};f.audio.output.connection=10;}
        else f.system_page=true;
        model.Update(f);f.system_page=false;f.state=FaceState::Idle;
        if(disconnect){Playing(f,{10,1},f.now_us);assert(!Label(model,{9,4},"happy",f.now_us));}
        else Playing(f,{9,4},1000000);
        assert(model.Update(f).emotion==FaceEmotion::Neutral);
    }
    FaceModel explicit_clear;auto f=F();explicit_clear.Update(f);
    assert(Label(explicit_clear,{9,4},"happy",f.now_us));Clear(explicit_clear);
    Playing(f,{9,4},f.now_us);assert(explicit_clear.Update(f).emotion==FaceEmotion::Neutral);
}
static void UnsafeFacts() {
    for(int kind=0;kind<8;++kind) {
        FaceModel model;auto f=F();model.Update(f);assert(Label(model,{9,4},"happy",f.now_us));
        Playing(f,{9,4},f.now_us);model.Update(f);f.now_us+=10000;Playing(f,{9,4},1000000);
        switch(kind) {
        case 0:f.audio.output.valid=false;Fact(f.audio.output,{9,4},1000000,f.now_us,false,false,false,false);break;
        case 1:f.audio.output.identity.connection=8;break;
        case 2:f.audio.output.stamp_us=f.now_us+1;break;
        case 3:f.now_us+=350001;break;
        case 4:f.now_us=999999;break;
        case 5:Fact(f.audio.output,{9,4},f.now_us+1,f.now_us);break;
        case 6:Fact(f.audio.output,{9,4},1000000,999999);break;
        case 7:f.audio.output.active=false;f.audio.output.valid=false;break;
        }
        auto frame=model.Update(f);assert(frame.mouth_level==0 && frame.emotion==FaceEmotion::Neutral);
        f.now_us=2000000;Playing(f,{9,4},1000000);
        assert(model.Update(f).emotion==FaceEmotion::Neutral);
    }
    FaceModel legacy;auto f=F();Playing(f,{9,4},f.now_us);legacy.SetEmotion("loving",f.now_us);
    assert(legacy.Update(f).emotion==FaceEmotion::Loving);
    f.now_us+=4999999;Playing(f,{9,4},1000000);assert(legacy.Update(f).emotion==FaceEmotion::Loving);
    ++f.now_us;Playing(f,{9,4},1000000);assert(legacy.Update(f).emotion==FaceEmotion::Neutral);
    // Legacy messages cannot overwrite or carry into a bound turn.
    FaceModel mixed;f=F();mixed.Update(f);assert(Label(mixed,{9,4},"happy",f.now_us));
    Playing(f,{9,4},f.now_us);assert(mixed.Update(f).emotion==FaceEmotion::Happy);
    mixed.SetEmotion("angry",f.now_us);assert(mixed.Update(f).emotion==FaceEmotion::Happy);
    ++f.now_us;Playing(f,{9,5},f.now_us);assert(mixed.Update(f).emotion==FaceEmotion::Neutral);
}
static void DelayedObservationAndLegacyReuse(int part) {
    // The actual start can precede the first UI observation (e.g. display off).
    if(part==0)for(int64_t arrival:{int64_t(900000),int64_t(3500000)}) {
        FaceModel model;auto f=F(500000);model.Update(f);
        assert(Label(model,{9,4},"happy",arrival));
        f.now_us=4000000;Playing(f,{9,4},1000000);
        assert(model.Update(f).emotion==(arrival<1000000?FaceEmotion::Happy:FaceEmotion::Neutral));
    }
    if(part==1)for(bool disconnect:{false,true}) {
        FaceModel model;auto f=F();model.Update(f);assert(Label(model,{9,4},"happy",f.now_us));
        Playing(f,{9,4},f.now_us);model.Update(f);
        f.now_us+=100000;Ended(f,{9,4},1000000,f.now_us);model.Update(f);
        f.now_us+=3000000;
        if(disconnect){f.audio.output={};f.audio.output.connection=10;}
        model.Update(f);model.SetEmotion("loving",f.now_us);
        assert(model.Update(f).emotion==FaceEmotion::Loving);
        f.now_us+=2000001;assert(model.Update(f).emotion==FaceEmotion::Neutral);
    }
    if(part==2) {FaceModel backward;auto f=F(900000);backward.Update(f);f.now_us=2000000;
    assert(Label(backward,{9,4},"happy",f.now_us));Playing(f,{9,4},1000000);backward.Update(f);
    f.now_us+=10000;Playing(f,{9,4},1000000);f.audio.output.stamp_us=1999999;
    Fact(f.audio.output,{9,4},1000000,1999999);
    assert(backward.Update(f).mouth_level==0);
    }
    if(part==3) {FaceModel stale;auto f=F();stale.Update(f);assert(Label(stale,{9,4},"happy",f.now_us));
    Playing(f,{9,4},f.now_us);stale.Update(f);f.now_us+=10000;Playing(f,{8,4},f.now_us);
    auto frame=stale.Update(f);assert(frame.mouth_level==0 && frame.emotion==FaceEmotion::Neutral);
    assert(!Label(stale,{8,4},"happy",f.now_us));}
}
static void OnlyShownMoodSurvivesDrain(int part) {
    if(part==0) {
        FaceModel model;auto f=F();model.Update(f);
        Playing(f,{9,4},f.now_us);assert(model.Update(f).emotion==FaceEmotion::Neutral);
        f.now_us+=10000;assert(Label(model,{9,4},"happy",f.now_us));
        // Cancellation races the next frame. Happy has never been shown.
        f.now_us+=10000;Playing(f,{9,4},1000000,true);
        auto frame=model.Update(f);assert(frame.mouth_level>0 && frame.emotion==FaceEmotion::Neutral);
        assert(!Label(model,{9,4},"happy",f.now_us));
        f.now_us+=10000;Playing(f,{9,5},f.now_us);
        assert(model.Update(f).emotion==FaceEmotion::Neutral);
    }
    if(part==1)for(bool off:{false,true}) {
        FaceModel model;auto f=F();model.Update(f);
        assert(Label(model,{9,4},"happy",f.now_us));
        f.now_us+=10000;f.screen_on=!off;f.state=off?FaceState::Idle:FaceState::Hidden;
        Playing(f,{9,4},1010000);assert(!model.Update(f).shown);
        f.now_us+=10000;Playing(f,{9,4},1010000,true);assert(!model.Update(f).shown);
        f.now_us+=10000;f.screen_on=true;f.state=FaceState::Idle;
        Playing(f,{9,4},1010000,true);
        auto frame=model.Update(f);assert(frame.mouth_level>0 && frame.emotion==FaceEmotion::Neutral);
        assert(!Label(model,{9,4},"happy",f.now_us));
    }
    if(part==2)for(bool off:{false,true}) {
        FaceModel model;auto f=F();model.Update(f);
        assert(Label(model,{9,4},"happy",f.now_us));
        Playing(f,{9,4},1000000);assert(model.Update(f).emotion==FaceEmotion::Happy);
        f.now_us+=10000;f.screen_on=!off;f.state=off?FaceState::Idle:FaceState::Hidden;
        Playing(f,{9,4},1000000,true);assert(!model.Update(f).shown);
        f.now_us+=10000;f.screen_on=true;f.state=FaceState::Idle;
        Playing(f,{9,4},1000000,true);
        auto frame=model.Update(f);assert(frame.mouth_level>0 && frame.emotion==FaceEmotion::Happy);
    }
}
static void TerminalCannotIntroduceMood(int part) {
    if(part<2) {
        FaceModel model;auto f=F();model.Update(f);
        Playing(f,{9,4},1000000);assert(model.Update(f).emotion==FaceEmotion::Neutral);
        // Both choices arrive before the next UI frame. One arrives after the
        // actual final DMA; the other arrives before end but was never shown.
        f.now_us=part==0?1020000:1005000;
        assert(Label(model,{9,4},"happy",f.now_us));
        f.now_us=1020000;Ended(f,{9,4},1000000,1010000);
        auto frame=model.Update(f);assert(frame.emotion==FaceEmotion::Neutral && frame.mouth_level==0);
        assert(!Label(model,{9,4},"happy",f.now_us));
    }
    if(part==2)for(bool off:{false,true}) {
        FaceModel model;auto f=F();model.Update(f);assert(Label(model,{9,4},"happy",f.now_us));
        f.now_us=1010000;f.screen_on=!off;f.state=off?FaceState::Idle:FaceState::Hidden;
        Playing(f,{9,4},1010000);assert(!model.Update(f).shown);
        f.now_us=1030000;Ended(f,{9,4},1010000,1020000);assert(!model.Update(f).shown);
        f.now_us=1040000;f.screen_on=true;f.state=FaceState::Idle;
        auto frame=model.Update(f);assert(frame.emotion==FaceEmotion::Neutral && frame.mouth_level==0);
        assert(!Label(model,{9,4},"happy",f.now_us));
    }
}
int main(int argc,char** argv){
    const char* test=argc>1?argv[1]:"all";
    if(!std::strcmp(test,"all") || !std::strcmp(test,"dma")){ActualDmaTimes();ActualDmaExpression();}
    if(!std::strcmp(test,"all") || !std::strcmp(test,"timeline"))BoundTimeline();
    if(!std::strcmp(test,"all") || !std::strcmp(test,"windows"))ArrivalWindows();
    if(!std::strcmp(test,"all") || !std::strcmp(test,"hidden"))HiddenAndInvalidation();
    if(!std::strcmp(test,"all") || !std::strcmp(test,"invalid"))UnsafeFacts();
    const char* gaps[]={"delayed","legacy","backward","stale"};
    for(int part=0;part<4;++part)if(!std::strcmp(test,"all") || !std::strcmp(test,gaps[part]))
        DelayedObservationAndLegacyReuse(part);
    const char* shown[]={"late_drain","hidden_drain","shown_drain"};
    for(int part=0;part<3;++part)if(!std::strcmp(test,"all") || !std::strcmp(test,shown[part]))
        OnlyShownMoodSurvivesDrain(part);
    const char* terminal[]={"terminal_late","terminal_unseen","hidden_terminal"};
    for(int part=0;part<3;++part)if(!std::strcmp(test,"all") || !std::strcmp(test,terminal[part]))
        TerminalCannotIntroduceMood(part);
    std::printf("bound expression DMA timeline checks passed; ledger=%zu output=%zu audio=%zu facts=%zu controller=%zu model=%zu\n",
        sizeof(PlaybackProgress),sizeof(OrbOutputActivity),sizeof(OrbAudioActivity),sizeof(FaceFacts),
        sizeof(FaceExpressionController),sizeof(FaceModel));}
