#include "playback_progress.h"
#include <cassert>
#include <algorithm>
#include <chrono>
#include <cstdio>
#include <cstring>
#include <functional>
#include <mutex>
#include <thread>
#include <vector>
using namespace family_robot;
static int64_t now_us=1000000;
static std::function<void()> on_delay;
static int64_t esp_timer_get_time() { return now_us; }
static void vTaskDelay(int) { now_us+=10000; if(on_delay) on_delay(); }
#define portENTER_CRITICAL(m) (m)->lock()
#define portEXIT_CRITICAL(m) (m)->unlock()
#define portENTER_CRITICAL_ISR(m) (m)->lock()
#define portEXIT_CRITICAL_ISR(m) (m)->unlock()
struct i2s_event_data_t {void* dma_buf;size_t size;};
struct D {
    static constexpr size_t kBlock=240;
    enum class Fault {Format};
    int16_t observed=0;int64_t tx=0,rx=0;
    void Fail(Fault) {}
    int64_t tx_samples(){return tx;}int64_t rx_samples(){return rx;}
    void Tx(int64_t,int64_t,const int16_t* pcm,size_t n){observed=pcm[0];tx+=n;}
    void Rx(int64_t,int64_t,const int16_t*,size_t n){rx+=n;}
};
// Only platform/class plumbing is stubbed. All state transitions, submissions,
// cancellation checks and lock acquisition below compile the production include.
class SoftwareAecCodec {
public:
    enum class PlaybackOp { Connect, Start, Admit, Release, Seal, Interrupt, Invalidate, Reset };
    bool PlaybackControl(PlaybackOp, PlaybackIdentity={}, uint64_t=0, size_t=0);
    uint32_t PlaybackGeneration() const;
    bool PlaybackPoll(PlaybackSnapshot&);
    bool PlaybackBusy() const;
    void OutputData(std::vector<int16_t>&);
    void OutputTagged(std::vector<int16_t>&, PlaybackIdentity, uint64_t, uint32_t);
    mutable std::mutex mux_;
    std::mutex lifecycle_;
    PlaybackProgress owned_;
    PlaybackProgress* playback_=&owned_;
    uint32_t output_generation_=0;
    bool accepting_=true,output_enabled_=true;
    D reference_;D* pair_=&reference_;
    uint8_t discard_tx_=0,discard_rx_=0;
    int64_t last_tx_us_=0,last_rx_us_=0;
    void PublishLocked() {} // AEC status publication is independently tested
    bool Complete(i2s_event_data_t*,bool);
};
#include "software_aec_playback.inc"
#include "software_aec_dma.inc"
int main() {
    SoftwareAecCodec c;
    using Op=SoftwareAecCodec::PlaybackOp;
    c.PlaybackControl(Op::Connect,{1,0});
    c.PlaybackControl(Op::Start,{1,1});
    c.PlaybackControl(Op::Admit,{1,1},0,1440);
    const auto old=c.PlaybackGeneration();
    std::vector<int16_t> pcm(1440,99);
    c.PlaybackControl(Op::Reset);
    c.PlaybackControl(Op::Start,{1,2});
    c.PlaybackControl(Op::Admit,{1,2},0,1440);
    c.OutputTagged(pcm,{1,1},0,old); // dequeued output arriving after reset
    assert(!c.PlaybackBusy());
    assert(c.owned_.Snapshot({1,1}).valid); // intentionally cancelled work is not a lost prefix
    c.OutputTagged(pcm,{1,2},0,c.PlaybackGeneration());
    assert(c.PlaybackBusy());
    c.PlaybackControl(Op::Admit,{1,2},1440,1440);
    c.OutputTagged(pcm,{1,2},1440,c.PlaybackGeneration());
    c.PlaybackControl(Op::Admit,{1,2},2880,1440);
    bool cancelled=false;
    on_delay=[&] {
        assert(!cancelled); cancelled=true;
        c.PlaybackControl(Op::Reset); // would deadlock if mux held while waiting
        c.PlaybackControl(Op::Start,{1,3});
        c.PlaybackControl(Op::Admit,{1,3},0,1440);
    };
    c.OutputTagged(pcm,{1,2},2880,c.PlaybackGeneration());
    assert(cancelled && !c.PlaybackBusy());
    on_delay={};
    c.OutputTagged(pcm,{1,3},0,c.PlaybackGeneration());
    assert(c.PlaybackBusy());
    // Prompt output carries the same cancellation token and is never retagged.
    const auto prompt_token=c.PlaybackGeneration();
    c.PlaybackControl(Op::Reset);
    c.OutputTagged(pcm,{},0,prompt_token);
    assert(!c.PlaybackBusy());
    c.PlaybackControl(Op::Start,{1,4});
    c.PlaybackControl(Op::Admit,{1,4},0,1440);
    c.PlaybackControl(Op::Seal,{1,4});
    c.OutputTagged(pcm,{1,4},0,c.PlaybackGeneration());
    c.PlaybackControl(Op::Release,{1,4});
    int16_t dma[6][240]{};
    std::thread isr([&] {
        for(int i=0;i<12;++i) {
            std::lock_guard<std::mutex> lock(c.mux_);
            c.owned_.Complete(dma[i%6],240,2000000+i*10000);
        }
    });
    for(int i=0;i<100;++i) { c.PlaybackBusy(); c.PlaybackGeneration(); }
    isr.join();
    assert(!c.PlaybackBusy());
    const auto s=c.owned_.Snapshot({1,4});
    assert(s.finished && s.valid && s.played_samples==1440);
    c.output_generation_=UINT32_MAX;
    c.PlaybackControl(Op::Reset);
    assert(c.PlaybackGeneration()==UINT32_MAX);
    c.OutputTagged(pcm,{},0,0); assert(!c.PlaybackBusy());
    SoftwareAecCodec callback;
    callback.PlaybackControl(Op::Connect,{1,0});callback.PlaybackControl(Op::Start,{1,1});
    callback.PlaybackControl(Op::Admit,{1,1},0,1440);
    callback.OutputTagged(pcm,{1,1},0,callback.PlaybackGeneration());
    callback.PlaybackControl(Op::Release,{1,1});callback.PlaybackControl(Op::Seal,{1,1});
    int16_t physical[6][240]{};
    for(int i=0;i<12;++i) {
        i2s_event_data_t event{physical[i%6],480};now_us=3000000+i*10000;
        callback.Complete(&event,true);
        assert(callback.reference_.observed==(i<6?0:99)); // reference BEFORE refill
        assert(physical[i%6][0]==(i<6?99:0));
    }
    auto final=callback.owned_.Snapshot({1,1});assert(final.valid && final.finished && final.played_samples==1440);
    physical[0][0]=123;callback.accepting_=false;i2s_event_data_t event{physical[0],480};
    callback.Complete(&event,true);assert(physical[0][0]==0); // no stale loop while disarmed
    puts("production adapter cancellation/ISR serialization passed");
}
