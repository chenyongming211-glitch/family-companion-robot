#include "playback_progress.h"
#include <cassert>
#include <cstdio>
using namespace family_robot;
struct Rig {
    PlaybackProgress core;
    int16_t dma[6][240]{};
    int16_t pcm[1440]{};
    int64_t now = 0;
    size_t cursor = 0;
    Rig() { core.Connect(1); core.HardwareReset(); }
    void tick() { now += 10000; core.Complete(dma[cursor], 240, now); cursor = (cursor + 1) % 6; }
    void ticks(size_t n) { while(n--) tick(); }
    PlaybackIdentity start(uint32_t id) { PlaybackIdentity k{1,id}; assert(core.Start(k)); return k; }
    void submit(PlaybackIdentity k, uint64_t offset, size_t n=240) {
        assert(core.Submit(k, offset, pcm, n)==PlaybackProgress::SubmitResult::Accepted);
    }
};
int main() {
    assert(PlaybackSessionSupported("opaque-session",14));
    char session[65]{}; for(auto& c:session)c='a';
    assert(PlaybackSessionSupported(session,64)); assert(!PlaybackSessionSupported(session,65));
    assert(!PlaybackSessionSupported("\n",1));

    {
        Rig r; auto k=r.start(1); assert(r.core.Admit(k,0,1440));
        for (size_t i=0;i<6;++i) r.submit(k,i*240);
        r.core.Release(k); r.core.Seal(k,false);
        assert(r.core.Snapshot(k).played_samples==0);
        r.ticks(6); assert(r.core.Snapshot(k).played_samples==0);
        r.ticks(6); auto s=r.core.Snapshot(k); assert(s.valid && s.finished && s.played_samples==1440);
    }
    {
        Rig r; auto k=r.start(1); assert(r.core.Admit(k,0,1440));
        for (size_t i=0;i<6;++i) { r.pcm[0]=i%2?123:0; r.submit(k,i*240); }
        r.ticks(3); r.core.Seal(k,true); r.core.Release(k);
        assert(!r.core.Snapshot(k).finished);
        auto next=r.start(2); assert(r.core.Admit(next,0,240)); r.submit(next,0); r.core.Release(next);
        assert(r.core.Submit(k,1440,r.pcm,240)==PlaybackProgress::SubmitResult::Rejected);
        r.ticks(9); auto s=r.core.Snapshot(k); assert(s.valid && s.finished && s.interrupted && s.played_samples==720);
        assert(r.core.Snapshot(next).played_samples==240);
        assert(!r.core.Snapshot(next).finished); // temporary underflow is not EOF
        r.core.Seal(next,false); assert(r.core.Snapshot(next).finished);
    }
    {
        Rig r; auto k=r.start(1); assert(r.core.Admit(k,0,480));
        r.submit(k,240); r.core.Release(k); r.core.Seal(k,false); r.ticks(7);
        assert(!r.core.Snapshot(k).valid);
    }
    {
        Rig r; auto k=r.start(1); assert(r.core.Admit(k,0,240)); r.submit(k,0);
        r.core.Release(k); r.ticks(1); r.core.HardwareReset();
        assert(!r.core.Snapshot(k).valid);
        r.core.Connect(2); assert(!r.core.Admit(k,240,240));
        assert(!r.core.Start(k)); assert(r.core.Start({2,1}));
    }
    {
        Rig r; auto k=r.start(1); assert(r.core.Admit(k,0,240)); r.submit(k,0); r.core.Release(k);
        r.ticks(6); r.now+=20000; r.core.Complete(r.dma[1],240,r.now);
        assert(!r.core.Snapshot(k).valid); // missing and out of order callback
    }
    {
        Rig r; auto k=r.start(1); assert(!r.core.Admit(k,1440,1440)); assert(!r.core.Snapshot(k).valid);
        r.core.Invalidate(k); r.core.Seal(k,false); assert(r.core.Snapshot(k).finished);
        auto n=r.start(2); assert(!r.core.Admit(n,0,UINT64_MAX)); assert(!r.core.Snapshot(n).valid);
    }
    {
        Rig r; auto k=r.start(1); assert(r.core.Admit(k,0,240)); r.submit(k,0); r.core.Release(k);
        r.ticks(7); assert(r.core.Snapshot(k).played_samples==240);
        r.submit({},0); r.ticks(14); assert(r.core.Snapshot(k).played_samples==240);
        PlaybackSnapshot s; assert(r.core.NextReport(r.now,s)); assert(!s.finished);
        assert(!r.core.NextReport(r.now+1000,s));
        r.core.Seal(k,false); assert(r.core.NextReport(r.now+1001,s) && s.finished);
        assert(!r.core.NextReport(r.now+1002,s));
    }
    {
        Rig r; auto k=r.start(1); assert(r.core.Admit(k,0,480));
        r.submit(k,0); r.core.Release(k); r.core.Seal(k,false); r.ticks(7);
        assert(r.core.Snapshot(k).finished && !r.core.Snapshot(k).valid);
    }
    {
        Rig r; auto k=r.start(1); assert(r.core.Admit(k,0,1)); r.submit(k,0,1);
        r.core.Release(k); r.core.Seal(k,false); r.ticks(7);
        assert(r.core.Snapshot(k).played_samples==1); assert(r.dma[0][1]==0);
    }
    {
        Rig r; auto k=r.start(1); assert(r.core.Admit(k,0,1440));
        for(size_t i=0;i<6;++i) r.submit(k,i*240);
        assert(r.core.Admit(k,1440,1440));
        for(size_t i=6;i<12;++i) r.submit(k,i*240);
        assert(r.core.Admit(k,2880,240));
        assert(r.core.Submit(k,2880,r.pcm,240)==PlaybackProgress::SubmitResult::Full);
        r.core.Invalidate(k); r.core.Seal(k,true); r.ticks(12);
        assert(r.core.Snapshot(k).finished && !r.core.Snapshot(k).valid);
    }
    {
        Rig r; auto k=r.start(1); assert(r.core.Admit(k,0,240)); r.submit(k,0); r.core.Release(k);
        r.core.Seal(k,false); auto next=r.start(2); assert(r.core.Admit(next,0,240));
        r.submit(next,0); r.core.Release(next); r.core.Seal(next,false); r.ticks(8);
        assert(r.core.Snapshot(k).valid && r.core.Snapshot(k).played_samples==240);
        assert(r.core.Snapshot(next).valid && r.core.Snapshot(next).played_samples==240);
    }
    {
        Rig r; for(uint32_t i=1;i<=16;++i) assert(r.core.Start({1,i}));
        assert(!r.core.Start({1,17})); r.submit({},0); r.ticks(7); // untracked still plays
        r.core.Seal({1,1},true); PlaybackSnapshot s; assert(r.core.NextReport(200000,s));
        assert(r.core.Start({1,18})); assert(!r.core.Start({1,1}));
    }
    std::printf("playback core cases passed; bytes=%zu\n",sizeof(PlaybackProgress));
}
