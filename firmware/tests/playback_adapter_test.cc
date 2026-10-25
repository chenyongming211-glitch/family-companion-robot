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
#include <cstdlib>
#include <new>
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
    static constexpr int kRate=24000;
    enum class Fault {Format,Setup};
    int16_t observed=0;int64_t tx=0,rx=0;
    void Fail(Fault) {}
    int64_t tx_samples(){return tx;}int64_t rx_samples(){return rx;}
    void Tx(int64_t,int64_t,const int16_t* pcm,size_t n){observed=pcm[0];tx+=n;}
    void Rx(int64_t,int64_t,const int16_t*,size_t n){rx+=n;}
};
namespace family_robot { using DuplexReference=::D; }
struct Channel { bool enabled=true, callback=false, deleted=false, auto_clear=false; };
using i2s_chan_handle_t=Channel*;
struct i2s_chan_info_t { bool is_enabled; };
struct i2s_event_callbacks_t { bool (*on_sent)(i2s_chan_handle_t,i2s_event_data_t*,void*)=nullptr; bool (*on_recv)(i2s_chan_handle_t,i2s_event_data_t*,void*)=nullptr; };
struct i2s_chan_config_t {int id,role,dma_desc_num,dma_frame_num;bool auto_clear_after_cb,auto_clear_before_cb;int intr_priority;};
struct audio_codec_i2s_cfg_t {int port;Channel *rx_handle,*tx_handle;};
constexpr int ESP_OK=0,I2S_NUM_0=0,I2S_ROLE_MASTER=0,AUDIO_CODEC_DMA_DESC_NUM=6,AUDIO_CODEC_DMA_FRAME_NUM=240;
constexpr int MALLOC_CAP_INTERNAL=1,MALLOC_CAP_8BIT=2;
#define ESP_ERROR_CHECK(expr) assert((expr)==ESP_OK)
static int alloc_index=0,fail_alloc=0,register_index=0,fail_register=0;
static bool diagnostics_start=true;
static std::vector<void*> allocations;
static void* heap_caps_malloc(size_t n,int) {
    if (++alloc_index == fail_alloc) {
        return nullptr;
    }
    void* p = std::malloc(n);
    allocations.push_back(p);
    return p;
}
static int i2s_channel_get_info(Channel* h,i2s_chan_info_t* i){assert(!h->deleted);i->is_enabled=h->enabled;return ESP_OK;}
static int i2s_channel_disable(Channel* h){assert(!h->deleted);h->enabled=false;return ESP_OK;}
static int i2s_channel_enable(Channel* h){assert(!h->deleted);h->enabled=true;return ESP_OK;}
static int i2s_channel_register_event_callback(Channel* h,i2s_event_callbacks_t*,void*) {
    assert(!h->enabled);
    if (++register_index == fail_register) {
        return -1;
    }
    h->callback = true;
    return ESP_OK;
}
static int i2s_del_channel(Channel* h){assert(!h->enabled);h->deleted=true;h->callback=false;return ESP_OK;}
static Channel rebuilt[2];
static int i2s_new_channel(i2s_chan_config_t* cfg,Channel** tx,Channel** rx){assert(cfg->dma_desc_num==6&&cfg->dma_frame_num==240&&!cfg->auto_clear_before_cb);rebuilt[0]={true,false,false,cfg->auto_clear_after_cb};rebuilt[1]=rebuilt[0];*tx=&rebuilt[0];*rx=&rebuilt[1];return ESP_OK;}
static void audio_codec_delete_data_if(void*){}
static void* audio_codec_new_i2s_data(audio_codec_i2s_cfg_t* c){assert(!c->tx_handle->callback&&!c->rx_handle->callback);return c->tx_handle;}
class SoftwareAecCodec;
class AecDiagnostics { public:explicit AecDiagnostics(SoftwareAecCodec&){} bool Start(){return diagnostics_start;} };
// SDK/class plumbing is stubbed; production allocation, callback registration,
// fault decisions, channel deletion/recreation and writer dispatch execute below.
// Only platform/class plumbing is stubbed. All state transitions, submissions,
// cancellation checks and lock acquisition below compile the production include.
class Es8311AudioCodec {
public:
    size_t written=0;
    Channel initial_[2];Channel *tx_handle_=&initial_[0],*rx_handle_=&initial_[1];
    void *dev_=nullptr,*data_if_=initial_;
    bool callback_refill_=true;int duplex_pins_[5]{};
    void CreateI2sChannels();
    void RecreateDuplexForOrdinaryWrite();
    void CreateDuplexChannels(int,int,int,int,int){CreateI2sChannels();}

    int Write(const int16_t*,size_t n){written+=n;return static_cast<int>(n);}
};
class SoftwareAecCodec : public Es8311AudioCodec {
public:
    enum class PlaybackOp { Connect, Start, Admit, Release, Seal, Interrupt, Invalidate, Reset };
    bool PlaybackControl(PlaybackOp, PlaybackIdentity={}, uint64_t=0, size_t=0);
    uint32_t PlaybackGeneration() const;
    bool PlaybackPoll(PlaybackSnapshot&);
    bool PlaybackBusy() const;
#if CONFIG_XIAOMAN_ORB_UI
    OrbOutputActivity GetOrbOutputActivity() const;
    void MarkOrbDraining(PlaybackIdentity);
#endif
    void OutputData(std::vector<int16_t>&);
    void OutputTagged(std::vector<int16_t>&, PlaybackIdentity, uint64_t, uint32_t);
    mutable std::mutex mux_;
    std::mutex lifecycle_;
    PlaybackProgress owned_;
    PlaybackProgress* playback_=&owned_;
    uint32_t output_generation_=0;
    bool accepting_=true,output_enabled_=true,refill_mode_=true;
    bool callbacks_ok_=false,infrastructure_ok_=false,input_reference_=false;
    int input_channels_=1;
    AecDiagnostics* diagnostics_=nullptr;
    void InitializePlayback(int,int);
    void StartPlaybackInfrastructure();
    void UseOrdinaryOutput();
    void Fail(D::Fault){if(playback_)playback_->HardwareFault();}
    bool PlaybackSupported()const{return refill_mode_&&playback_&&callbacks_ok_&&infrastructure_ok_;}
    static bool Sent(Channel*,i2s_event_data_t* e,void* p){return static_cast<SoftwareAecCodec*>(p)->Complete(e,true);}
    static bool Received(Channel*,i2s_event_data_t* e,void* p){return static_cast<SoftwareAecCodec*>(p)->Complete(e,false);}

    D reference_;D* pair_=&reference_;
    uint8_t discard_tx_=0,discard_rx_=0;
    int64_t last_tx_us_=0,last_rx_us_=0;
    void PublishLocked() {} // AEC status publication is independently tested
    bool Complete(i2s_event_data_t*,bool);
};
#include "es8311_output_ownership.inc"
#include "software_aec_initialization.inc"
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
#if CONFIG_XIAOMAN_ORB_UI
    assert(callback.GetOrbOutputActivity().stamp_us==0);
#endif
    for(int i=0;i<12;++i) {
        i2s_event_data_t event{physical[i%6],480};now_us=3000000+i*10000;
        callback.Complete(&event,true);
#if CONFIG_XIAOMAN_ORB_UI
        const auto activity=callback.GetOrbOutputActivity();
        if(i<6) assert(activity.stamp_us==0);
        else assert(activity.stamp_us==now_us && activity.level==OrbPcmLevel(pcm.data(),240));
#endif
        assert(callback.reference_.observed==(i<6?0:99)); // reference BEFORE refill
        assert(physical[i%6][0]==(i<6?99:0));
    }
    auto final=callback.owned_.Snapshot({1,1});assert(final.valid && final.finished && final.played_samples==1440);
    physical[0][0]=123;callback.accepting_=false;i2s_event_data_t event{physical[0],480};
    callback.Complete(&event,true);assert(physical[0][0]==0); // no stale loop while disarmed
    // Inject each optional initialization failure through the actual initializer.
    // Allocation order: reference, playback tracker, diagnostics. Registration:
    // TX first, RX second (partial registration must also be removed).
    for(int fault=0;fault<7;++fault) {
        alloc_index=register_index=0;fail_alloc=fault>=1&&fault<=3?fault:0;
        fail_register=fault==4?1:(fault==5?2:0);diagnostics_start=fault!=6;
        SoftwareAecCodec initialized;initialized.playback_=nullptr;initialized.pair_=nullptr;
        initialized.accepting_=false;
        initialized.InitializePlayback(24000,24000);
        initialized.StartPlaybackInfrastructure();
        if(!fault) {
            assert(initialized.PlaybackSupported()&&initialized.refill_mode_);
            assert(initialized.tx_handle_->callback&&initialized.rx_handle_->callback);
            initialized.OutputData(pcm);assert(initialized.written==0);
        } else {
            assert(!initialized.PlaybackSupported()&&!initialized.refill_mode_);
            assert(initialized.input_channels_==1&&!initialized.input_reference_);
            assert(initialized.initial_[0].deleted&&initialized.initial_[1].deleted);
            assert(initialized.tx_handle_->auto_clear&&!initialized.tx_handle_->callback);
            assert(initialized.rx_handle_->auto_clear&&!initialized.rx_handle_->callback);
            assert(!initialized.PlaybackBusy());PlaybackSnapshot report;
            assert(!initialized.PlaybackPoll(report));
            initialized.OutputData(pcm);assert(initialized.written==pcm.size());
            auto token=initialized.PlaybackGeneration();initialized.PlaybackControl(Op::Reset);
            initialized.OutputTagged(pcm,{},0,token);assert(initialized.written==pcm.size());
            initialized.OutputData(pcm);assert(initialized.written==pcm.size()*2);
            // Even an erroneously delayed invocation cannot refill ordinary DMA.
            int16_t normal[240];std::fill_n(normal,240,17);i2s_event_data_t ev{normal,sizeof(normal)};
            initialized.Complete(&ev,true);assert(normal[0]==17);
        }
    }
    for(auto* allocation:allocations)std::free(allocation);
    puts("production adapter cancellation/ISR serialization and initialization fallback passed");
}
