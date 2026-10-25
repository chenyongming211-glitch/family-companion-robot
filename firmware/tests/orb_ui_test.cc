#include "orb_activity.h"
#include "orb_model.h"
#include "orb_renderer.h"
#include "orb_refresh_policy.h"
#include "playback_progress.h"
#include <array>
#include <cassert>
#include <cstdio>
#include <cstring>
#include <chrono>

using namespace family_robot;

static void StateTruth() {
    OrbModel model;
    OrbFacts f{}; f.now_us=1000000; f.screen_on=true;
    assert(model.Update(f).phase==OrbPhase::Hidden);
    f.session=true; f.connecting=true;
    assert(model.Update(f).phase==OrbPhase::Connecting);
    f.connecting=false;
    assert(model.Update(f).phase==OrbPhase::Preparing);
    f.audio.gate_open=true; f.audio.capture=4; f.audio.epoch=9;
    f.audio.input={4,9,220,f.now_us};
    assert(model.Update(f).phase==OrbPhase::Listening);
    assert(model.Update(f).level==220);
    f.audio.input.capture=3;
    assert(model.Update(f).level==0);
    f.audio.input.capture=4; f.audio.input.stamp_us=f.now_us-400000;
    assert(model.Update(f).level==0);
    // A response start or pending DMA data is insufficient: completion required.
    f.audio.output.connection=2; f.audio.output.identity={2,1};
    f.audio.output.active=true; f.audio.output.valid=true; f.audio.output.level=180;
    assert(model.Update(f).phase==OrbPhase::Listening);
    f.audio.output.stamp_us=f.now_us;
    assert(model.Update(f).phase==OrbPhase::Speaking);
    f.audio.output.identity.connection=1;
    assert(model.Update(f).phase==OrbPhase::Listening);
    f.audio.output.identity.connection=2; f.audio.output.valid=false;
    assert(model.Update(f).phase==OrbPhase::Listening);
    f.audio.output.valid=true; f.audio.output.stamp_us=f.now_us-400000;
    assert(model.Update(f).phase==OrbPhase::Listening);
    f.audio.output.stamp_us=f.now_us; f.audio.output.draining=true;
    assert(model.Update(f).phase==OrbPhase::Listening && model.Update(f).draining);
    f.audio.gate_open=false;
    assert(model.Update(f).phase==OrbPhase::Preparing && model.Update(f).draining);
    f.system_page=true;
    assert(model.Update(f).phase==OrbPhase::Hidden);
    f.system_page=false; f.screen_on=false;
    assert(model.Update(f).phase==OrbPhase::Hidden);
    f.screen_on=true; f.session=false;
    assert(model.Update(f).phase==OrbPhase::Hidden);
    // Re-entry cannot reuse the previous session's completed output.
    f.now_us+=10000; f.session=true; f.audio.gate_open=true;
    f.audio.output.draining=false;
    assert(model.Update(f).phase==OrbPhase::Listening);
    f.audio.output.stamp_us=f.now_us;
    assert(model.Update(f).phase==OrbPhase::Speaking);
    f.error=true;
    assert(model.Update(f).phase==OrbPhase::Error);
    assert(model.Update(f).level==0);
    // Missing server end-of-turn evidence never invents a waiting phase.
    f.error=false; f.audio.output.active=false; f.now_us+=5000000;
    assert(model.Update(f).phase==OrbPhase::Listening);
}

static void RefreshPolicyTruth() {
    OrbRefreshPolicy policy;
    assert(policy.requested()==2 && policy.effective()==2);
    // Reproduce the observed cold/preemption spike without changing user intent.
    policy.ObserveFrame(138822);
    assert(policy.requested()==2 && policy.effective()==1);
    for(int i=0;i<9;++i) {policy.ObserveFrame(25000);assert(policy.effective()==1);}
    policy.ObserveFrame(25000);assert(policy.effective()==2);
    // Recovery needs ten consecutive fast frames, not an accumulating total.
    policy.ObserveFrame(30000);assert(policy.effective()==1);
    for(int i=0;i<6;++i) policy.ObserveFrame(10000);
    policy.ObserveFrame(25001);
    for(int i=0;i<9;++i) {policy.ObserveFrame(10000);assert(policy.effective()==1);}
    policy.ObserveFrame(10000);assert(policy.effective()==2);
    policy.Request(1);
    for(int i=0;i<30;++i) policy.ObserveFrame(5000);
    assert(policy.requested()==1 && policy.effective()==1);
    policy.Request(0);
    policy.ObserveFrame(200000);
    for(int i=0;i<30;++i) policy.ObserveFrame(5000);
    assert(policy.requested()==0 && policy.effective()==0);
    policy.Request(2);
    policy.ObserveFrame(90000);assert(policy.effective()==1);
    policy.ObserveFrame(90000);assert(policy.effective()==0 && policy.requested()==2);
    // A critical static fallback does not generate frames to probe recovery.
    for(int i=0;i<30;++i) policy.ObserveFrame(5000);
    assert(policy.effective()==0);
    policy.Request(2); // same requested value is an explicit retry
    assert(policy.requested()==2 && policy.effective()==2);
    policy.ObserveFrame(90000);
    policy.ObserveFrame(30000); // isolated critical frames cannot latch static
    policy.ObserveFrame(90000);assert(policy.effective()==1);
    assert(sizeof(OrbRefreshPolicy)<=16);
}

static void DmaTruth() {
    std::array<int16_t,240> pcm{};
    assert(OrbPcmLevel(pcm.data(),pcm.size())==0);
    pcm.fill(32767); assert(OrbPcmLevel(pcm.data(),pcm.size())==255);
    pcm.fill(-32768); assert(OrbPcmLevel(pcm.data(),pcm.size())==255);
    pcm.fill(2000); const auto level=OrbPcmLevel(pcm.data(),pcm.size());
    assert(level>0 && level<255);
    PlaybackProgress p;
    const PlaybackIdentity id{7,1}; p.Connect(7); assert(p.Start(id));
    assert(p.Admit(id,0,240));
    assert(p.Submit(id,0,pcm.data(),240,level)==PlaybackProgress::SubmitResult::Accepted);
    assert(p.OrbActivity().stamp_us==0); // enqueue is not playback
    std::array<std::array<int16_t,240>,6> dma{};
    int64_t now=1000000;
    for (auto& block:dma) {
        p.Complete(block.data(),240,now); now+=10000;
        assert(p.OrbActivity().stamp_us==0); // discovery/refill isn't completion
    }
    p.Complete(dma[0].data(),240,now);
    auto a=p.OrbActivity();
    assert(a.valid && a.identity==id && a.level==level && a.stamp_us==now);
    // Once sealed and physically drained it must not keep claiming speech.
    p.Release(id); p.Seal(id,false);
    assert(!p.OrbActivity().active);
    p.HardwareFault(); assert(!p.OrbActivity().valid);
    p.Connect(8); assert(p.OrbActivity().stamp_us==0);
    assert(p.Start({8,1}));
    // Old DMA completions cannot animate a new connection.
    now+=10000; p.Complete(dma[1].data(),240,now);
    assert(p.OrbActivity().stamp_us==0);

    PlaybackProgress drain; drain.Connect(9); const PlaybackIdentity old{9,1};
    assert(drain.Start(old)); assert(drain.Admit(old,0,240));
    assert(drain.Submit(old,0,pcm.data(),240,level)==PlaybackProgress::SubmitResult::Accepted);
    drain.Complete(dma[0].data(),240,1000000);
    drain.MarkOrbDraining(old);
    drain.Release(old);
    drain.Seal(old,false); // negotiated drain preserves queued PCM and physical tail
    assert(drain.OrbActivity().draining); // real submitted tail only
    for (size_t i=1;i<6;++i) drain.Complete(dma[i].data(),240,1000000+int64_t(i)*10000);
    drain.Complete(dma[0].data(),240,1060000);
    assert(!drain.OrbActivity().draining);
}

static void RenderBudget(const char* destination) {
    constexpr size_t pixels=OrbRenderer::kSide*OrbRenderer::kSide;
    std::array<uint16_t,pixels+2> image{};
    image.front()=0x1234; image.back()=0x4321;
    const auto start=std::chrono::steady_clock::now();
    for (uint32_t i=0;i<90;++i) OrbRenderer::Render(image.data()+1,i*67,180,false);
    assert(image.front()==0x1234 && image.back()==0x4321);
    const auto corner=image[1];
    assert(corner==OrbRenderer::kBackground);
    size_t colored=0; for (size_t i=1;i<=pixels;++i) colored+=image[i]!=corner;
    assert(colored>6000 && colored<15000);
    assert(image[1+64*128+64]!=corner); // filled orb, not an outline ring
    std::array<uint16_t,pixels> quiet{};
    OrbRenderer::Render(quiet.data(),0,0,true);
    assert(std::memcmp(quiet.data(),image.data()+1,pixels*2)!=0);
    assert(OrbRenderer::kBytes<=96*1024);
    assert(sizeof(OrbModel)<512);
    const auto ms=std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now()-start).count();
    std::printf("orb state/completion/render passed; 90 host frames %lldms; buffer %zu bytes; tracker %zu bytes\n",static_cast<long long>(ms),OrbRenderer::kBytes,sizeof(PlaybackProgress));
    if(destination) {
        FILE* out=std::fopen(destination,"wb"); assert(out);
        std::fprintf(out,"P6\n128 128\n255\n");
        for (size_t i=1;i<=pixels;++i) {
            const uint16_t c=image[i];
            const unsigned char rgb[]{static_cast<unsigned char>(((c>>11)&31)*255/31),static_cast<unsigned char>(((c>>5)&63)*255/63),static_cast<unsigned char>((c&31)*255/31)};
            assert(std::fwrite(rgb,1,3,out)==3);
        }
        std::fclose(out);
    }
}
int main(int argc,char** argv) { StateTruth(); RefreshPolicyTruth(); DmaTruth(); RenderBudget(argc>1?argv[1]:nullptr); }
