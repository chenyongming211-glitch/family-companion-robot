#include "playback_progress.h"
#include <algorithm>
#include <cassert>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <mutex>
#include <new>
#include <vector>

// Only platform/Box plumbing is substituted. The production header and all
// lifecycle, readback, submission and ISR methods compile unchanged below.
using gpio_num_t = int;
using portMUX_TYPE = std::mutex;
#define portMUX_INITIALIZER_UNLOCKED {}
#define portENTER_CRITICAL(m) (m)->lock()
#define portEXIT_CRITICAL(m) (m)->unlock()
#define portENTER_CRITICAL_ISR(m) (m)->lock()
#define portEXIT_CRITICAL_ISR(m) (m)->unlock()
constexpr int ESP_OK=0, I2S_DIR_TX=1, I2S_DIR_RX=2, I2S_COMM_MODE_STD=1, I2S_COMM_MODE_TDM=2;
constexpr int I2S_DATA_BIT_WIDTH_16BIT=16,I2S_SLOT_BIT_WIDTH_16BIT=16,I2S_SLOT_BIT_WIDTH_32BIT=32;
constexpr int I2S_SLOT_MODE_MONO=1,I2S_SLOT_MODE_STEREO=2,I2S_STD_SLOT_LEFT=1;
constexpr int I2S_TDM_SLOT0=1,I2S_TDM_SLOT1=2,MALLOC_CAP_INTERNAL=1,MALLOC_CAP_8BIT=2;
struct i2s_event_data_t { void* dma_buf; size_t size; };
struct Channel;
using i2s_chan_handle_t=Channel*;
struct i2s_event_callbacks_t { bool (*on_sent)(Channel*,i2s_event_data_t*,void*)=nullptr; };
struct Clock { unsigned sample_rate_hz=24000; };
struct StdSlot { int data_bit_width=16,slot_bit_width=32,slot_mode=1,slot_mask=1; };
struct TdmSlot { int data_bit_width=16,slot_bit_width=16,slot_mode=2,slot_mask=3,total_slot=4; };
struct i2s_std_config_t { Clock clk_cfg; StdSlot slot_cfg; };
struct i2s_tdm_config_t { Clock clk_cfg; TdmSlot slot_cfg; };
struct i2s_chan_info_t {
    int dir=0,mode=0; bool is_enabled=true; Channel* pair_chan=nullptr;
    unsigned total_dma_buf_size=0,bclk_hz=1536000,mclk_hz=6144000;
    const void* mode_cfg=nullptr;
};
struct Channel {
    bool enabled=true;
    i2s_event_callbacks_t callbacks{};
    void* argument=nullptr;
    i2s_chan_info_t info{};
};
static int64_t now_us=1000000;
static int64_t esp_timer_get_time() { return now_us; }
static void vTaskDelay(int) { now_us+=10000; }
static bool allocation_fails=false,registration_fails=false,read_fails=false;
static void* heap_caps_malloc(size_t size,int) { return allocation_fails?nullptr:std::malloc(size); }
static void heap_caps_free(void* p) { std::free(p); }
static int i2s_channel_disable(Channel* c) { c->enabled=false; return ESP_OK; }
static int i2s_channel_enable(Channel* c) { c->enabled=true; return ESP_OK; }
static int i2s_channel_get_info(Channel* c,i2s_chan_info_t* i) { *i=c->info;i->is_enabled=c->enabled;return ESP_OK; }
static int i2s_channel_register_event_callback(Channel* c,i2s_event_callbacks_t* cb,void* arg) {
    assert(!c->enabled);if(registration_fails)return -1;c->callbacks=*cb;c->argument=arg;return ESP_OK;
}
class AudioCodec {
public:
    enum class PlaybackOp { Connect,Start,Admit,Release,Seal,Interrupt,Invalidate,Reset };
    virtual ~AudioCodec()=default;
    virtual void Start() { volume_loaded=true; }
    virtual bool InputData(std::vector<int16_t>&) { return true; }
    virtual void OutputData(std::vector<int16_t>&) {}
    virtual bool PlaybackSupported() const {return false;}
    virtual bool PlaybackControl(PlaybackOp,family_robot::PlaybackIdentity={},uint64_t=0,size_t=0){return false;}
    virtual uint32_t PlaybackGeneration()const{return 0;}
    virtual bool PlaybackPoll(family_robot::PlaybackSnapshot&){return false;}
    virtual bool PlaybackBusy()const{return false;}
    virtual family_robot::OrbOutputActivity GetOrbOutputActivity()const{return {};}
    virtual void MarkOrbDraining(family_robot::PlaybackIdentity){}
    virtual void OutputTagged(std::vector<int16_t>&,family_robot::PlaybackIdentity,uint64_t,uint32_t){}
    virtual void EnableInput(bool){}
    virtual void EnableOutput(bool){}
    virtual bool ManagedCapture()const{return false;}
    virtual bool SoftwareReference()const{return false;}
    virtual bool ReferenceValid()const{return true;}
    virtual bool ReferenceFaulted()const{return false;}
    virtual bool RealtimeEligible()const{return true;}
    virtual void NotifyAecReady(bool,uint32_t=0){}
    virtual void ReportReferenceFailure(){}
    virtual uint32_t CaptureGeneration()const{return 0;}
    virtual uint32_t WakeAudioGeneration()const{return 0;}
    virtual uint32_t ReferenceIntegrity()const{return 0;}
    virtual void SetCaptureActive(bool){}
    virtual void CaptureBoundary(){}
    bool volume_loaded=false;
};
class BoxAudioCodec : public AudioCodec {
public:
    BoxAudioCodec(void*,int,int,gpio_num_t,gpio_num_t,gpio_num_t,gpio_num_t,gpio_num_t,gpio_num_t,
                  uint8_t,uint8_t,bool reference,float,int,float,bool callback) {
        assert(reference&&callback);
        tx.info={I2S_DIR_TX,I2S_COMM_MODE_STD,true,&rx,2880,1536000,6144000,&standard};
        rx.info={I2S_DIR_RX,I2S_COMM_MODE_TDM,true,&tx,5760,1536000,6144000,&tdm};
    }
    // Official esp_codec_dev1.6.2 with the pinned S3 HW2 caps: IN close
    // disables RX immediately. OUT close defers TX while IN is active; a
    // pending OUT close is completed by IN close. Input-only open/close by
    // itself leaves TX enabled, which is why the production adapter opens the
    // complete pair before arming rather than inventing an RX-enabled waiver.
    void EnableInput(bool enabled)override {
        assert(volume_loaded);input_enabled_=enabled;rx.enabled=enabled;
        if(enabled)tx.enabled=true;
        else if(output_disable_pending_) {tx.enabled=false;output_disable_pending_=false;}
        ++input_changes;
    }
    void EnableOutput(bool enabled)override {
        assert(volume_loaded&&(!enabled||input_enabled_));output_enabled_=enabled;
        output_disable_pending_=!enabled&&input_enabled_;
        tx.enabled=enabled||output_disable_pending_;++output_changes;
    }
    bool InputData(std::vector<int16_t>& data)override {if(read_fails)return false;std::fill(data.begin(),data.end(),17);return true;}
    bool input_enabled_=false,output_enabled_=false;
    bool output_disable_pending_=false;
    Channel tx,rx; Channel *tx_handle_=&tx,*rx_handle_=&rx;
    i2s_std_config_t standard; i2s_tdm_config_t tdm;
    int input_changes=0,output_changes=0;
};
#include "xiaoman_box_codec.h"
#include "xiaoman_box_lifecycle.inc"
#include "xiaoman_box_playback.inc"
#include "xiaoman_box_dma.inc"

static XiaomanBoxCodec make_codec() {
    return XiaomanBoxCodec(nullptr,24000,24000,0,0,0,0,0,0,1,2,true);
}
static void tick(XiaomanBoxCodec& c,int16_t* dma,size_t bytes=480,int step=10000) {
    now_us+=step;i2s_event_data_t event{dma,bytes};assert(c.tx.callbacks.on_sent);
    c.tx.callbacks.on_sent(&c.tx,&event,c.tx.argument);
}
int main() {
    using Op=AudioCodec::PlaybackOp;
    auto c=make_codec();c.Start();
    assert(c.PlaybackSupported()&&!c.SoftwareReference()&&c.ManagedCapture());
    assert(c.input_changes==1&&c.output_changes==1);
    c.SetCaptureActive(true);const auto capture=c.CaptureGeneration();
    c.NotifyAecReady(true,capture);assert(c.RealtimeEligible());
    c.NotifyAecReady(true,capture-1);assert(!c.RealtimeEligible());
    c.NotifyAecReady(true,capture);
    const auto wake=c.WakeAudioGeneration();
    const uint32_t frozen_batch=1;
    c.PlaybackControl(Op::Connect,{1,0});assert(!c.RealtimeEligible());
    assert(c.CaptureGeneration()!=capture);
    c.NotifyAecReady(true,capture);assert(!c.RealtimeEligible());
    assert(family_robot::WakeBatchValid(frozen_batch,frozen_batch,wake,c.WakeAudioGeneration()));
    assert(!family_robot::WakeBatchValid(frozen_batch,frozen_batch+1,wake,c.WakeAudioGeneration()));
    c.NotifyAecReady(true,c.CaptureGeneration());assert(c.RealtimeEligible());
    assert(c.PlaybackControl(Op::Start,{1,1}));
    const auto generation=c.PlaybackGeneration();
    assert(c.PlaybackControl(Op::Admit,{1,1},0,240));
    c.EnableOutput(true);c.EnableInput(true);
    assert(c.PlaybackGeneration()==generation&&c.input_changes==1&&c.output_changes==1);
    std::vector<int16_t> pcm(240,99);c.OutputTagged(pcm,{1,1},0,generation);
    c.PlaybackControl(Op::Release,{1,1});c.PlaybackControl(Op::Seal,{1,1});
    family_robot::PlaybackSnapshot s;assert(c.PlaybackPoll(s)&&s.played_samples==0&&s.valid&&!s.finished);
    int16_t dma[6][240]{};
    for(int i=0;i<6;++i) {tick(c,dma[i]);assert(c.GetOrbOutputActivity().stamp_us==0);}
    tick(c,dma[0]);assert(c.PlaybackPoll(s)&&s.valid&&s.finished&&s.played_samples==240);
    assert(c.PlaybackControl(Op::Start,{1,2}));c.PlaybackControl(Op::Admit,{1,2},0,240);
    c.OutputTagged(pcm,{1,2},0,c.PlaybackGeneration());c.PlaybackControl(Op::Release,{1,2});
    const auto stale=c.PlaybackGeneration();const auto old_capture=c.CaptureGeneration();
    c.EnableOutput(false);c.EnableOutput(true);
    assert(c.PlaybackGeneration()!=stale&&c.CaptureGeneration()!=old_capture&&!c.RealtimeEligible());
    assert(!family_robot::WakeBatchValid(frozen_batch,frozen_batch,wake,c.WakeAudioGeneration()));
    c.OutputTagged(pcm,{1,2},0,stale);c.PlaybackControl(Op::Seal,{1,2});
    assert(c.PlaybackPoll(s)&&!s.valid&&s.played_samples==0);
    const auto changes=c.output_changes;c.PlaybackControl(Op::Connect,{2,0});
    assert(c.output_changes==changes);assert(!c.PlaybackControl(Op::Start,{1,3}));
    assert(c.PlaybackControl(Op::Start,{2,1}));c.PlaybackControl(Op::Admit,{2,1},0,240);
    c.OutputTagged(pcm,{2,1},0,c.PlaybackGeneration());
    c.PlaybackControl(Op::Interrupt,{2,1});
    for(int i=0;i<12;++i)tick(c,dma[i%6]);
    assert(c.PlaybackPoll(s)&&s.valid&&s.interrupted&&s.finished&&s.played_samples==0);
    // Capability survives an intentional power idle. Hello can negotiate first;
    // Start opens/verifies hardware before the first record and Admit.
    c.EnableOutput(false);c.EnableInput(false);
    const auto opens=c.input_changes+c.output_changes;
    assert(c.PlaybackSupported()&&!c.ReferenceValid()&&!c.RealtimeEligible());
    c.PlaybackControl(Op::Connect,{3,0});
    assert(c.input_changes+c.output_changes==opens);
    assert(c.PlaybackControl(Op::Start,{3,1}));
    assert(c.input_changes+c.output_changes==opens+2);
    assert(c.PlaybackControl(Op::Admit,{3,1},0,240));
    c.OutputTagged(pcm,{3,1},0,c.PlaybackGeneration());
    c.PlaybackControl(Op::Release,{3,1});c.PlaybackControl(Op::Seal,{3,1});
    for(int i=0;i<7;++i)tick(c,dma[i%6]);
    assert(c.PlaybackPoll(s)&&s.valid&&s.finished&&s.played_samples==240);
    for(int fault=0;fault<3;++fault) {
        auto bad=make_codec();bad.Start();bad.PlaybackControl(Op::Connect,{1,0});
        assert(bad.PlaybackControl(Op::Start,{1,1}));bad.PlaybackControl(Op::Admit,{1,1},0,240);
        bad.OutputTagged(pcm,{1,1},0,bad.PlaybackGeneration());
        tick(bad,dma[0]);
        if(fault==0)tick(bad,dma[0]); // duplicate address before six-slot discovery
        if(fault==1)tick(bad,dma[1],478);
        if(fault==2)tick(bad,dma[1],480,20000);
        assert(bad.ReferenceFaulted()&&!bad.RealtimeEligible());
        assert(bad.PlaybackPoll(s)&&!s.valid);
        tick(bad,dma[2]);assert(dma[2][0]==0);
    }
    for(int fault=0;fault<5;++fault) {
        auto bad=make_codec();
        if(fault==0)bad.standard.slot_cfg.slot_mask=3;
        if(fault==1)bad.tx.info.total_dma_buf_size=5760;
        if(fault==2)bad.rx.info.total_dma_buf_size=11520;
        if(fault==3)bad.standard.slot_cfg.data_bit_width=32;
        if(fault==4)bad.rx.info.bclk_hz=768000;
        bad.Start();assert(!bad.PlaybackSupported()&&bad.ReferenceFaulted());
    }
    for(int failure=0;failure<2;++failure) {
        allocation_fails=failure==0;registration_fails=failure==1;
        auto bad=make_codec();bad.Start();
        assert(!bad.PlaybackSupported());bad.OutputData(pcm);assert(!bad.PlaybackBusy());
        allocation_fails=registration_fails=false;
    }
    {auto bad=make_codec();bad.Start();bad.SetCaptureActive(true);
     const auto before=bad.CaptureGeneration();read_fails=true;
     assert(!bad.InputData(pcm)&&bad.ReferenceFaulted()&&bad.CaptureGeneration()!=before);
     read_fails=false;assert(!bad.InputData(pcm));} // no automatic fault recovery
    puts("Waveshare production lifecycle, readback, callback evidence, faults and reconnect passed");
}
