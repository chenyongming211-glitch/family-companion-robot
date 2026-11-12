#include "playback_progress.h"
#include <cassert>
#include <cstdio>

int main() {
    using family_robot::PlaybackProgress;
    using Result=PlaybackProgress::SubmitResult;
    PlaybackProgress p;
    p.HardwareReset(48000);
    p.Connect(1);
    assert(!p.Start({1,1})); // music cannot issue trusted 24kHz voice receipts
    int16_t pcm[240]{},dma[6][240]{};
    assert(p.Submit({},0,pcm,240)==Result::Accepted);
    for(int i=0;i<7;++i)p.Complete(dma[i%6],240,1000000+i*5000);
    assert(p.HardwareHealthy()&&p.UntaggedCompleted()==240&&!p.Busy());
    p.Complete(dma[1],240,1040000); // 10ms is wrong for48kHz
    assert(!p.HardwareHealthy());
    p.HardwareReset(); // voice restores strict10ms, not a widened timing gate
    p.Connect(2);assert(p.Start({2,1}));
    p.Complete(dma[0],240,2000000);p.Complete(dma[1],240,2005000);
    assert(!p.HardwareHealthy());
    p.HardwareReset();assert(p.UntaggedCompleted()==0);
    for(int i=0;i<7;++i)p.Complete(dma[i%6],240,3000000+i*10000);
    assert(p.HardwareHealthy());
    puts("Music48kHz completion and strict24kHz voice restore passed");
}
