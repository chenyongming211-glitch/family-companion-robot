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
#include <algorithm>
#include <vector>

using namespace family_robot;

template<class T> static void SnapshotAvailable(T& audio,bool available) {
    if constexpr (requires { audio.snapshot_available; }) audio.snapshot_available=available;
}
static void UnavailableSnapshotTruth() {
    OrbModel model;
    OrbFacts f{};f.session=true;f.now_us=1000000;
    f.audio.capture=4;f.audio.epoch=9;f.audio.gate_open=true;
    f.audio.output.connection=2;
    f.audio.input={4,9,200,f.now_us};
    assert(model.Update(f).phase==OrbPhase::Listening);
    // Queue contention is missing evidence, not proof that the mic closed.
    // Only the caption is held briefly; unavailable amplitude is always zero.
    f.now_us+=67000;SnapshotAvailable(f.audio,false);f.audio.gate_open=false;
    assert(model.Update(f).phase==OrbPhase::Listening);
    assert(model.Update(f).level==0);
    f.now_us+=200000;
    assert(model.Update(f).phase==OrbPhase::Preparing);
    SnapshotAvailable(f.audio,true);f.audio.gate_open=true;
    assert(model.Update(f).phase==OrbPhase::Listening);
    // Authoritative closure and lifecycle revisions invalidate immediately.
    f.audio.gate_open=false;
    assert(model.Update(f).phase==OrbPhase::Preparing);
    f.audio.gate_open=true;assert(model.Update(f).phase==OrbPhase::Listening);
    SnapshotAvailable(f.audio,false);f.audio.gate_open=false;++f.audio.epoch;
    assert(model.Update(f).phase==OrbPhase::Preparing);
    SnapshotAvailable(f.audio,true);f.audio.gate_open=true;
    assert(model.Update(f).phase==OrbPhase::Listening);
    SnapshotAvailable(f.audio,false);f.audio.gate_open=false;++f.audio.capture;
    assert(model.Update(f).phase==OrbPhase::Preparing);
    SnapshotAvailable(f.audio,true);f.audio.gate_open=true;
    assert(model.Update(f).phase==OrbPhase::Listening);
    SnapshotAvailable(f.audio,false);f.audio.gate_open=false;++f.audio.output.connection;
    assert(model.Update(f).phase==OrbPhase::Preparing);
    // Speaking is never cached: current completion evidence is still required.
    SnapshotAvailable(f.audio,true);f.audio.gate_open=true;
    f.audio.output.identity={3,1};f.audio.output.valid=true;f.audio.output.active=true;
    f.audio.output.stamp_us=f.now_us;f.audio.output.level=180;
    assert(model.Update(f).phase==OrbPhase::Speaking);
    SnapshotAvailable(f.audio,false);f.audio.output={};
    assert(model.Update(f).phase!=OrbPhase::Speaking);
    f.error=true;assert(model.Update(f).phase==OrbPhase::Error);
    f.error=false;f.screen_on=false;assert(model.Update(f).phase==OrbPhase::Hidden);
    f.screen_on=true;assert(model.Update(f).phase==OrbPhase::Preparing);
}

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
    assert(OrbRenderer::kBytes==32768 && OrbRenderer::kTableBytes==2564);
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
static int Channel(uint16_t pixel,int channel) {
    if(channel==0)return ((pixel>>11)&31)*255/31;
    if(channel==1)return ((pixel>>5)&63)*255/63;
    return (pixel&31)*255/31;
}
static void RenderContinuityTruth() {
    constexpr size_t count=OrbRenderer::kSide*OrbRenderer::kSide;
    std::array<uint16_t,count> before{},after{};
    OrbRenderer::Render(before.data(),12287,0,false);
    OrbRenderer::Render(after.data(),12288,0,false);
    uint64_t boundary=0;
    for(size_t i=0;i<count;++i)for(int c=0;c<3;++c)
        boundary+=static_cast<unsigned>(std::abs(Channel(before[i],c)-Channel(after[i],c)));
    assert(boundary<count*2); // one millisecond cannot jump the entire field
    for(int threshold:{100,200}) {
        OrbRenderer::Render(before.data(),3000,static_cast<uint8_t>(threshold-1),false);
        OrbRenderer::Render(after.data(),3000,static_cast<uint8_t>(threshold),false);
        uint64_t envelope_delta=0;
        for(size_t i=0;i<count;++i)for(int c=0;c<3;++c)
            envelope_delta+=static_cast<unsigned>(std::abs(Channel(before[i],c)-Channel(after[i],c)));
        assert(envelope_delta<count/2); // adjacent sound levels cannot jump one source pixel
        std::printf("renderer adjacent envelope %d->%d delta=%llu\n",threshold-1,threshold,static_cast<unsigned long long>(envelope_delta));
    }
    size_t pale=0,body=0;uint64_t saturation=0;std::vector<int> light;
    OrbRenderer::Render(before.data(),3000,80,false);
    for(int y=-38;y<=38;++y)for(int x=-38;x<=38;++x)if(x*x+y*y<38*38) {
        const auto pixel=before[(y+64)*128+x+64];
        const int r=Channel(pixel,0),g=Channel(pixel,1),b=Channel(pixel,2);
        const int high=std::max(r,std::max(g,b)),low=std::min(r,std::min(g,b));
        saturation+=high-low;++body;pale+=low>128 && high-low<55;
        light.push_back((r+g+b)/3);
    }
    assert(pale<body/20 && saturation>body*75); // retain four clear hues, no white fog
    std::sort(light.begin(),light.end());
    std::fprintf(stderr,"renderer clear color pale=%zu/%zu mean_saturation=%llu light_spread=%d\n",
        pale,body,static_cast<unsigned long long>(saturation/body),light[light.size()*9/10]-light[light.size()/10]);
    // Luminance spread is diagnostic: approved A has a soft filled color field
    // (spread17 here). Its clear hues are guarded by saturation/no-fog above;
    // the old style's spread25 proxy must not darken the approved artwork.
    // Contour evolves even without a change in sound level.
    size_t contour_changes=0;
    OrbRenderer::Render(after.data(),6300,80,false);
    for(int y=-63;y<64;++y)for(int x=-63;x<64;++x) {
        const size_t i=(y+64)*128+x+64;
        const bool a=Channel(before[i],2)>60,b=Channel(after[i],2)>60;
        contour_changes+=a!=b;
    }
    assert(contour_changes>60);
    std::printf("renderer continuity boundary_delta=%llu pale=%zu/%zu mean_saturation=%llu contour_changes=%zu\n",
        static_cast<unsigned long long>(boundary),pale,body,static_cast<unsigned long long>(saturation/body),contour_changes);
}
static void RenderSilhouetteTruth() {
    std::array<uint16_t,128*128> image{};
    int min_width=128,max_width=0,min_height=128,max_height=0;
    for(uint64_t frame=0;frame<=240;++frame) {
        OrbRenderer::Render(image.data(),frame*1000/15,0,false);
        int x0=128,x1=-1,y0=128,y1=-1;
        for(int y=0;y<128;++y)for(int x=0;x<128;++x) {
            const auto p=image[y*128+x];
            // Exclude the faint glow: measure the filled, visible silhouette.
            if(Channel(p,0)+Channel(p,1)+Channel(p,2)<=90)continue;
            x0=std::min(x0,x);x1=std::max(x1,x);
            y0=std::min(y0,y);y1=std::max(y1,y);
        }
        assert(x1>=x0 && y1>=y0);
        min_width=std::min(min_width,x1-x0+1);max_width=std::max(max_width,x1-x0+1);
        min_height=std::min(min_height,y1-y0+1);max_height=std::max(max_height,y1-y0+1);
    }
    std::fprintf(stderr,"renderer silent 16s silhouette widths=%d..%d heights=%d..%d\n",
        min_width,max_width,min_height,max_height);
    assert(max_width-min_width>=16 && max_height-min_height>=16);
    int extent=0;
    // Independent periods must stay safe after long uptime as well as at boot.
    for(uint64_t base:{uint64_t{0},uint64_t{1}<<32,(uint64_t{1}<<48)+12345,UINT64_MAX-16000})
        for(uint64_t ms=0;ms<=16000;ms+=100)for(uint8_t level:{uint8_t{0},uint8_t{255}}) {
            OrbRenderer::Render(image.data(),base+ms,level,false);
            for(int y=-64;y<64;++y)for(int x=-64;x<64;++x) {
                if(image[(y+64)*128+x+64]!=OrbRenderer::kBackground)
                    extent=std::max(extent,std::max(std::abs(x),std::abs(y)));
            }
        }
    // These independent-phase collisions came from a dispersed long-uptime
    // scan: adding two pixels to A's peak radius reached the source edge.
    for(uint64_t ms:{uint64_t{10141147297338840946ULL},uint64_t{684944971140478936ULL},
                    uint64_t{5686623564773156651ULL},uint64_t{11672169487981444559ULL}}) {
        OrbRenderer::Render(image.data(),ms,255,false);
        for(int y=-64;y<64;++y)for(int x=-64;x<64;++x)
            if(image[(y+64)*128+x+64]!=OrbRenderer::kBackground)
                extent=std::max(extent,std::max(std::abs(x),std::abs(y)));
    }
    std::fprintf(stderr,"renderer dispersed-phase maximum painted extent=%d\n",extent);
    assert(extent<=OrbRenderer::kArtworkRadius && OrbRenderer::kArtworkRadius<=63);
    assert(extent<63); // painted pixels must not touch either edge of the 128px source
    std::printf("renderer maximum painted extent=%d source pixels\n",extent);
}
int main(int argc,char** argv) { RenderSilhouetteTruth(); RenderContinuityTruth(); UnavailableSnapshotTruth(); StateTruth(); RefreshPolicyTruth(); DmaTruth(); RenderBudget(argc>1?argv[1]:nullptr); }
