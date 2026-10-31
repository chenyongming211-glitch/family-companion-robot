#include "playback_progress.h"
#include <algorithm>
#include <cassert>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <functional>
#include <mutex>
#include <new>
#include <thread>
#include <vector>

// SDK and base-codec plumbing only. Every FamilyC3Codec method below is the
// unmodified production implementation, also compiled as a standalone TU.
using gpio_num_t=int;
using i2c_port_t=int;
using portMUX_TYPE=std::mutex;
#define portMUX_INITIALIZER_UNLOCKED {}
#define portENTER_CRITICAL(m) (m)->lock()
#define portEXIT_CRITICAL(m) (m)->unlock()
#define portENTER_CRITICAL_ISR(m) (m)->lock()
#define portEXIT_CRITICAL_ISR(m) (m)->unlock()
#define SOC_I2S_HW_VERSION_2 1
constexpr int ESP_OK=0,ESP_ERR_TIMEOUT=1;
constexpr int I2S_DIR_TX=1,I2S_DIR_RX=2,I2S_COMM_MODE_STD=1;
constexpr int I2S_DATA_BIT_WIDTH_16BIT=16,I2S_SLOT_BIT_WIDTH_16BIT=16;
constexpr int I2S_SLOT_MODE_MONO=1,I2S_STD_SLOT_LEFT=1;
constexpr int MALLOC_CAP_INTERNAL=1,MALLOC_CAP_8BIT=2;
constexpr int GPIO_NUM_NC=-1;
struct i2s_event_data_t {void* dma_buf;size_t size;};
struct Channel;
using i2s_chan_handle_t=Channel*;
struct i2s_event_callbacks_t {bool(*on_sent)(Channel*,i2s_event_data_t*,void*)=nullptr;};
struct Clock {unsigned sample_rate_hz=24000;};
struct StdSlot {
    int data_bit_width=16,slot_bit_width=16,slot_mode=1,slot_mask=1,ws_width=16;
    bool ws_pol=false,bit_shift=true,left_align=true,big_endian=false,bit_order_lsb=false;
};
struct i2s_std_config_t {Clock clk_cfg;StdSlot slot_cfg;};
struct i2s_chan_info_t {
    int dir=0,mode=0;bool is_enabled=true;Channel* pair_chan=nullptr;
    unsigned total_dma_buf_size=0,bclk_hz=768000,mclk_hz=6144000;
    const void* mode_cfg=nullptr;
};
struct Channel {
    bool enabled=true,disable_fails=false,enable_fails=false,info_fails=false;
    i2s_event_callbacks_t callbacks{};void* argument=nullptr;i2s_chan_info_t info{};
};
static int64_t now_us=1000000;
static bool allocation_fails=false,registration_fails=false,read_fails=false;
static bool initial_disable_fails=false,initial_enable_fails=false;
static int drain_chunks=0,drain_error=0,drain_behavior=0,raw_writes=0,reads=0,drain_reads=0;
static bool boundary_inflight=false;
static int boundary_reads=0,boundary_behavior=0;
static int pa_level=0;
static int gpio_set_level(gpio_num_t pin,int level){assert(pin!=GPIO_NUM_NC);pa_level=level;return ESP_OK;}
static std::function<void()> delay_hook;
static int64_t esp_timer_get_time(){return now_us;}
static void vTaskDelay(int){now_us+=10000;if(delay_hook)delay_hook();}
static void* heap_caps_malloc(size_t size,int caps){
    assert(caps==(MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT));
    return allocation_fails?nullptr:std::malloc(size);
}
static void heap_caps_free(void* p){std::free(p);}
static int i2s_channel_disable(Channel* c){if(c->disable_fails)return -1;c->enabled=false;return ESP_OK;}
static int i2s_channel_enable(Channel* c){if(c->enable_fails)return -1;c->enabled=true;return ESP_OK;}
static int i2s_channel_get_info(Channel* c,i2s_chan_info_t* i){
    if(c->info_fails)return -1;
    *i=c->info;i->is_enabled=c->enabled;return ESP_OK;
}
static int i2s_channel_register_event_callback(Channel* c,i2s_event_callbacks_t* cb,void* arg){
    assert(!c->enabled);if(registration_fails)return -1;c->callbacks=*cb;c->argument=arg;return ESP_OK;
}
static int i2s_channel_read(Channel*,void* output,size_t n,size_t* count,int timeout){
    assert((timeout==0||timeout==100)&&n==480);*count=0;
    if(timeout) {
        ++boundary_reads;assert(drain_chunks==0);
        if(boundary_behavior==1)return -1;
        if(boundary_behavior==2)return ESP_ERR_TIMEOUT;
        if(boundary_behavior==3){*count=n-2;return ESP_OK;}
        if(boundary_behavior==4){*count=n;return ESP_ERR_TIMEOUT;}
        std::fill(static_cast<int16_t*>(output),static_cast<int16_t*>(output)+n/2,
                  boundary_inflight?int16_t(23):int16_t(71));
        boundary_inflight=false;*count=n;return ESP_OK;
    }
    ++drain_reads;
    if(drain_error)return drain_error;
    if(drain_behavior==1)return ESP_OK;
    if(drain_behavior==2){*count=1;return ESP_ERR_TIMEOUT;}
    if(drain_behavior==3){*count=2;drain_behavior=0;return ESP_ERR_TIMEOUT;}
    if(drain_chunks){--drain_chunks;std::fill(static_cast<int16_t*>(output),
        static_cast<int16_t*>(output)+n/2,int16_t(17));*count=n;return ESP_OK;}
    return ESP_ERR_TIMEOUT;
}
class AudioCodec {
public:
    enum class PlaybackOp {Connect,Start,Admit,Release,Seal,Interrupt,Invalidate,Reset};
    virtual ~AudioCodec()=default;
    virtual void Start(){volume_loaded=true;}
    virtual bool InputData(std::vector<int16_t>& data){return Read(data.data(),int(data.size()))>0;}
    virtual void OutputData(std::vector<int16_t>& data){Write(data.data(),int(data.size()));}
    virtual void EnableInput(bool enable){input_enabled_=enable;}
    virtual void EnableOutput(bool enable){output_enabled_=enable;}
    virtual bool PlaybackSupported()const{return false;}
    virtual bool PlaybackControl(PlaybackOp,family_robot::PlaybackIdentity={},uint64_t=0,size_t=0){return false;}
    virtual uint32_t PlaybackGeneration()const{return 0;}
    virtual bool PlaybackPoll(family_robot::PlaybackSnapshot&){return false;}
    virtual bool PlaybackBusy()const{return false;}
    virtual void OutputTagged(std::vector<int16_t>&,family_robot::PlaybackIdentity,uint64_t,uint32_t){}
    virtual bool RealtimeEligible()const{return true;}
    virtual bool ReferenceFaulted()const{return false;}
    virtual void CaptureBoundary(){}
    bool volume_loaded=false,input_enabled_=false,output_enabled_=false;
protected:
    virtual int Read(int16_t*,int)=0;
    virtual int Write(const int16_t*,int)=0;
};
class Es8311AudioCodec:public AudioCodec {
public:
    Es8311AudioCodec(void*,i2c_port_t,int,int,gpio_num_t,gpio_num_t,gpio_num_t,gpio_num_t,
                    gpio_num_t,gpio_num_t pa,uint8_t,bool,bool inverted,bool refill):pa_(pa),inverted_(inverted){
        assert(refill);tx.disable_fails=initial_disable_fails;tx.enable_fails=initial_enable_fails;
        tx.info={I2S_DIR_TX,I2S_COMM_MODE_STD,true,&rx,2880,768000,6144000,&tx_config};
        rx.info={I2S_DIR_RX,I2S_COMM_MODE_STD,true,&tx,2880,768000,6144000,&rx_config};
    }
    void EnableInput(bool enable)override {
        assert(volume_loaded);if(enable==input_enabled_)return;
        input_enabled_=enable;++input_changes;Update();
    }
    void EnableOutput(bool enable)override {
        assert(volume_loaded);if(enable==output_enabled_)return;
        output_enabled_=enable;++output_changes;Update();
    }
    Channel tx,rx;Channel *tx_handle_=&tx,*rx_handle_=&rx;
    i2s_std_config_t tx_config{},rx_config{};
    unsigned input_changes=0,output_changes=0,opens=0,closes=0;
protected:
    virtual void OnDeviceEpoch(bool){}
    int Read(int16_t* dest,int n)override {
        ++reads;if(read_fails)return 0;
        std::fill(dest,dest+n,boundary_inflight?int16_t(23):int16_t(71));
        boundary_inflight=false;return n;
    }
    int Write(const int16_t*,int n)override{++raw_writes;return n;}
private:
    void Update(){
        const bool need=input_enabled_||output_enabled_;
        if(need&&!opened_){opened_=true;tx.enabled=rx.enabled=true;++opens;OnDeviceEpoch(true);}
        else if(!need&&opened_){OnDeviceEpoch(false);opened_=false;tx.enabled=rx.enabled=false;++closes;}
        if(pa_!=GPIO_NUM_NC)gpio_set_level(pa_,output_enabled_?!inverted_:inverted_);
    }
    bool opened_=false;
    gpio_num_t pa_;bool inverted_;
};
#include "family_c3_codec.h"
#include "family_c3_lifecycle.inc"
#include "family_c3_playback.inc"
#include "family_c3_dma.inc"

using Op=AudioCodec::PlaybackOp;
using Id=family_robot::PlaybackIdentity;
using Snapshot=family_robot::PlaybackSnapshot;
static FamilyC3Codec make_codec(int rate=24000){return FamilyC3Codec(nullptr,0,rate,rate,0,0,0,0,0,0,1);}
static void tick(FamilyC3Codec& c,int16_t* dma,size_t bytes=480,int step=10000){
    now_us+=step;i2s_event_data_t e{dma,bytes};assert(c.tx.callbacks.on_sent);
    c.tx.callbacks.on_sent(&c.tx,&e,c.tx.argument);
}
static void begin(FamilyC3Codec& c,Id id={1,1}){
    c.Start();assert(c.PlaybackSupported());assert(!c.RealtimeEligible());
    assert(c.PlaybackControl(Op::Connect,{id.connection,0}));assert(c.PlaybackControl(Op::Start,id));
}
static void queue(FamilyC3Codec& c,Id id,size_t n,uint64_t first=0,int16_t value=99){
    assert(c.PlaybackControl(Op::Admit,id,first,n));std::vector<int16_t> pcm(n,value);
    c.OutputTagged(pcm,id,first,c.PlaybackGeneration());c.PlaybackControl(Op::Release,id);
}
static Snapshot final(FamilyC3Codec& c){Snapshot s;for(int i=0;i<18;++i)if(c.PlaybackPoll(s)&&s.finished)return s;assert(false);return {};}
int main(){
    unsigned scenarios=0;
    {
        // A short sealed reply never reaches queue backpressure. Without a
        // task-polled heartbeat, lost callbacks leave valid evidence and let
        // the same nonzero descriptor repeat through PA indefinitely.
        auto c=make_codec();begin(c);queue(c,{1,1},240);c.PlaybackControl(Op::Seal,{1,1});
        int16_t dma[6][240]{};tick(c,dma[0]);assert(pa_level==1&&dma[0][0]==99);
        now_us+=5000000;Snapshot s;assert(c.PlaybackPoll(s));
        assert(!s.valid&&s.played_samples==0&&c.ReferenceFaulted()&&!c.PlaybackSupported());
        assert(pa_level==0&&!c.input_enabled_&&!c.output_enabled_&&!c.tx.enabled&&!c.rx.enabled);
        assert(dma[0][0]==0);++scenarios;
    }
    {
        auto c=make_codec();begin(c);queue(c,{1,1},240);c.PlaybackControl(Op::Seal,{1,1});
        now_us+=200001;Snapshot s;c.PlaybackPoll(s);
        assert(c.ReferenceFaulted()&&!c.PlaybackSupported()&&pa_level==0&&!c.tx.enabled);++scenarios;
    }
    {
        auto c=make_codec();begin(c);queue(c,{1,1},240);c.PlaybackControl(Op::Seal,{1,1});
        int16_t dma[6][240]{};tick(c,dma[0]);now_us+=199000;Snapshot s;c.PlaybackPoll(s);
        assert(!c.ReferenceFaulted()&&c.PlaybackSupported()&&pa_level==1);
        now_us+=2000;c.PlaybackPoll(s);assert(c.ReferenceFaulted()&&pa_level==0&&!c.tx.enabled);++scenarios;
    }
    {
        // No pending PCM means no playback deadline. Both ordinary silence and
        // a physically closed power idle may last arbitrarily long.
        auto c=make_codec();c.Start();now_us+=300000000;Snapshot s;c.PlaybackPoll(s);
        assert(c.PlaybackSupported()&&!c.ReferenceFaulted());
        c.EnableOutput(false);c.EnableInput(false);now_us+=300000000;c.PlaybackPoll(s);
        assert(c.PlaybackSupported()&&!c.ReferenceFaulted());
        begin(c);queue(c,{1,1},240);c.PlaybackControl(Op::Seal,{1,1});int16_t dma[6][240]{};
        for(int i=0;i<7;++i)tick(c,dma[i%6]);
        s=final(c);assert(s.valid&&s.played_samples==240);++scenarios;
    }
    {
        // Poll may run while the producer owns lifecycle_ and waits for space.
        // PA must mute immediately; a later poll completes the shared close.
        auto c=make_codec();begin(c);queue(c,{1,1},1440);queue(c,{1,1},1440,1440);
        unsigned waits=0;delay_hook=[&]{if(++waits==1){
            now_us+=300000;
            std::thread poller([&]{Snapshot s;c.PlaybackPoll(s);assert(pa_level==0);});
            poller.join();
        }};
        queue(c,{1,1},240,2880);delay_hook={};assert(c.ReferenceFaulted());
        Snapshot s;c.PlaybackPoll(s);assert(!c.tx.enabled&&!c.output_enabled_&&pa_level==0);++scenarios;
    }
    {
        // A stale producer cannot invalidate a newly started reply after Reset
        // changes the output generation while the transport remains connected.
        auto c=make_codec();begin(c);const auto old=c.PlaybackGeneration();
        c.PlaybackControl(Op::Reset);assert(final(c).interrupted);
        assert(c.PlaybackControl(Op::Start,{1,2}));
        std::vector<int16_t> pcm(240,99);c.OutputTagged(pcm,{1,2},0,old);
        queue(c,{1,2},240);c.PlaybackControl(Op::Seal,{1,2});int16_t dma[6][240]{};
        for(int i=0;i<7;++i)tick(c,dma[i%6]);
        const auto s=final(c);assert(s.valid&&s.played_samples==240);++scenarios;
    }
    {
        auto c=make_codec();begin(c);const auto g=c.PlaybackGeneration();
        assert(!c.PlaybackControl(Op::Connect,{1,0}));
        assert(!c.PlaybackControl(Op::Connect,{0,0}));
        assert(c.PlaybackGeneration()==g&&c.PlaybackSupported());
        queue(c,{1,1},240);int16_t dma[6][240]{};tick(c,dma[0]);
        assert(c.PlaybackControl(Op::Connect,{2,0}));
        assert(!c.PlaybackControl(Op::Connect,{1,0}));
        assert(c.PlaybackControl(Op::Start,{2,1}));queue(c,{2,1},240);c.PlaybackControl(Op::Seal,{2,1});
        for(int i=0;i<13;++i)tick(c,dma[(i+1)%6]);
        const auto s=final(c);assert(s.valid&&s.played_samples==240);++scenarios;
    }
    for(int fault=0;fault<2;++fault){
        auto c=make_codec();begin(c);int16_t dma[6][240]{};queue(c,{1,1},240);tick(c,dma[0]);
        c.EnableOutput(false);
        if(fault==1)delay_hook=[&]{tick(c,dma[1]);};
        c.EnableOutput(true);delay_hook={};
        assert(c.ReferenceFaulted()&&!c.PlaybackSupported()&&!c.output_enabled_);++scenarios;
    }
    {
        // PA mute keeps the shared ADC/codec open. Wait for all six callback
        // refills before re-enabling PA, so no previous descriptor replays.
        auto c=make_codec();begin(c);int16_t dma[6][240]{};queue(c,{1,1},240);tick(c,dma[0]);
        c.EnableOutput(false);unsigned step=1;
        delay_hook=[&]{tick(c,dma[step++%6]);};c.EnableOutput(true);delay_hook={};
        assert(!c.ReferenceFaulted()&&c.output_enabled_&&dma[0][0]==0&&step==7);
        c.PlaybackControl(Op::Connect,{2,0});assert(c.PlaybackControl(Op::Start,{2,1}));
        queue(c,{2,1},240);c.PlaybackControl(Op::Seal,{2,1});
        for(int i=0;i<7;++i)tick(c,dma[(1+i)%6]);
        const auto s=final(c);assert(s.valid&&s.played_samples==240);++scenarios;
    }
    {
        auto c=make_codec();begin(c);int16_t dma[6][240]{};queue(c,{1,1},240);tick(c,dma[0]);
        c.EnableOutput(false);c.EnableInput(false);assert(dma[0][0]==0&&c.PlaybackSupported());
        c.PlaybackControl(Op::Connect,{2,0});assert(c.PlaybackControl(Op::Start,{2,1}));
        queue(c,{2,1},240);c.PlaybackControl(Op::Seal,{2,1});
        for(int i=0;i<7;++i)tick(c,dma[i%6]);
        const auto s=final(c);assert(s.valid&&s.played_samples==240);++scenarios;
    }
    {
        auto c=make_codec();begin(c);const auto g=c.PlaybackGeneration();
        c.EnableInput(true);c.EnableOutput(true);assert(c.PlaybackGeneration()==g);
        queue(c,{1,1},257);c.PlaybackControl(Op::Seal,{1,1});assert(c.PlaybackBusy());
        Snapshot s;assert(c.PlaybackPoll(s)&&s.valid&&!s.finished&&s.played_samples==0);
        int16_t dma[6][240]{};
        tick(c,dma[0]);assert(dma[0][0]==99&&dma[0][239]==99);
        tick(c,dma[1]);assert(dma[1][16]==99&&dma[1][17]==0);
        for(int i=2;i<6;++i)tick(c,dma[i]);
        tick(c,dma[0]);assert(c.PlaybackBusy());tick(c,dma[1]);
        s=final(c);assert(s.valid&&s.played_samples==257&&!c.PlaybackBusy());++scenarios;
    }
    {
        auto c=make_codec();begin(c);queue(c,{1,1},240);int16_t dma[6][240]{};
        tick(c,dma[0]);c.PlaybackControl(Op::Interrupt,{1,1});
        Snapshot s;assert(c.PlaybackPoll(s)&&!s.finished&&s.interrupted);
        for(int i=1;i<6;++i)tick(c,dma[i]);
        tick(c,dma[0]);
        s=final(c);assert(s.valid&&s.interrupted&&s.played_samples==240);++scenarios;
    }
    {
        auto c=make_codec();begin(c);queue(c,{1,1},240);int16_t dma[6][240]{};tick(c,dma[0]);
        auto old=c.PlaybackGeneration();c.PlaybackControl(Op::Connect,{2,0});assert(c.PlaybackGeneration()!=old);
        assert(!c.PlaybackControl(Op::Start,{1,2}));assert(c.PlaybackControl(Op::Start,{2,1}));
        assert(c.PlaybackControl(Op::Admit,{2,1},0,240));std::vector<int16_t> pcm(240,2);
        c.OutputTagged(pcm,{2,1},0,old);c.PlaybackControl(Op::Release,{2,1});c.PlaybackControl(Op::Seal,{2,1});
        for(int i=1;i<6;++i)tick(c,dma[i]);
        tick(c,dma[0]);
        auto s=final(c);assert(!s.valid&&s.played_samples==0);++scenarios;
    }
    {
        auto c=make_codec();begin(c);queue(c,{1,1},240);const auto g=c.PlaybackGeneration();
        c.EnableOutput(false);assert(c.PlaybackGeneration()!=g);c.PlaybackControl(Op::Release,{1,1});
        c.PlaybackControl(Op::Seal,{1,1});auto s=final(c);assert(!s.valid&&s.played_samples==0);
        c.EnableInput(false);assert(c.PlaybackSupported()&&c.closes==1);
        c.PlaybackControl(Op::Connect,{2,0});assert(c.PlaybackControl(Op::Start,{2,1}));assert(c.opens==2);
        queue(c,{2,1},240);c.PlaybackControl(Op::Seal,{2,1});int16_t dma[6][240]{};
        for(int i=0;i<7;++i)tick(c,dma[i%6]);
        s=final(c);assert(s.valid&&s.played_samples==240);++scenarios;
    }
    // Queue saturation must wait outside mux_, support completion/cancel from
    // another context, and terminate bounded if DMA stops progressing.
    for(int action=0;action<5;++action){
        auto c=make_codec();begin(c);queue(c,{1,1},1440);queue(c,{1,1},1440,1440);
        const auto old=c.PlaybackGeneration();int16_t dma[6][240]{};unsigned delayed=0;
        delay_hook=[&]{++delayed;if(action==0)tick(c,dma[(delayed-1)%6]);
            if(delayed==1&&action==1)c.PlaybackControl(Op::Reset);
            if(delayed==1&&action==2)c.PlaybackControl(Op::Connect,{2,0});
            if(delayed==1&&action==3)tick(c,dma[0],478);};
        queue(c,{1,1},240,2880);delay_hook={};assert(delayed>0&&delayed<=22);
        if(action==0){c.PlaybackControl(Op::Seal,{1,1});for(unsigned i=0;i<20;++i)tick(c,dma[(delayed+i)%6]);
            auto s=final(c);assert(s.valid&&s.played_samples==3120);}
        else if(action==1)assert(c.PlaybackGeneration()!=old);
        else if(action==2)assert(c.PlaybackGeneration()!=old&&!c.PlaybackBusy());
        else if(action==3)assert(!c.PlaybackSupported());
        else {c.PlaybackControl(Op::Seal,{1,1});assert(!c.PlaybackSupported());}
        ++scenarios;
    }
    for(int fault=0;fault<6;++fault){
        auto c=make_codec();begin(c);queue(c,{1,1},240);c.PlaybackControl(Op::Seal,{1,1});
        int16_t dma[6][240]{};tick(c,dma[0]);
        if(fault==0)tick(c,dma[1],478);
        if(fault==1)tick(c,dma[0]); // duplicate discovery
        if(fault==2)tick(c,dma[1],480,20000);
        if(fault==3){for(int i=1;i<6;++i)tick(c,dma[i]);tick(c,dma[1]);}
        if(fault==4)tick(c,nullptr);
        if(fault==5){i2s_event_data_t* absent=nullptr;c.tx.callbacks.on_sent(&c.tx,absent,c.tx.argument);}
        assert(!c.PlaybackSupported()&&c.ReferenceFaulted());
        c.EnableOutput(false);c.EnableInput(false);c.EnableInput(true);c.EnableOutput(true);
        assert(!c.PlaybackSupported());Snapshot s;assert(c.PlaybackPoll(s)&&!s.valid);++scenarios;
    }
    for(int fault=0;fault<15;++fault){
        auto c=make_codec();
        if(fault==0)c.tx.info.total_dma_buf_size=5760;
        if(fault==1)c.tx_config.slot_cfg.slot_mode=2;
        if(fault==2)c.rx_config.slot_cfg.slot_mask=3;
        if(fault==3)c.tx.info.bclk_hz=1536000;
        if(fault==4)c.rx.info.mclk_hz=1;
        if(fault==5)c.tx_config.clk_cfg.sample_rate_hz=16000;
        if(fault==6)c.rx_config.slot_cfg.slot_bit_width=32;
        if(fault==7)c.tx.info.pair_chan=nullptr;
        if(fault==8)c.rx.info_fails=true;
        if(fault==9)c.tx.info.mode_cfg=nullptr;
        if(fault==10)c.tx_config.slot_cfg.ws_width=32;
        if(fault==11)c.rx_config.slot_cfg.bit_shift=false;
        if(fault==12)c.tx_config.slot_cfg.left_align=false;
        if(fault==13)c.rx_config.slot_cfg.big_endian=true;
        if(fault==14)c.tx_config.slot_cfg.bit_order_lsb=true;
        c.Start();assert(!c.PlaybackSupported()&&c.ReferenceFaulted());++scenarios;
    }
    for(int fault=0;fault<5;++fault){
        allocation_fails=fault==0;registration_fails=fault==1;
        initial_disable_fails=fault==2;initial_enable_fails=fault==3;
        {auto c=make_codec(fault==4?16000:24000);c.Start();assert(!c.PlaybackSupported());
         assert(!c.PlaybackControl(Op::Start,{1,1}));std::vector<int16_t> pcm(1,1);c.OutputData(pcm);}
        allocation_fails=registration_fails=initial_disable_fails=initial_enable_fails=false;++scenarios;
    }
    {
        // Queued old PCM (17), the partially captured boundary descriptor (23),
        // and the first entirely fresh voice frame (71) are distinct. Merely
        // polling the empty completed-descriptor queue must not release 23.
        auto c=make_codec();begin(c);drain_chunks=3;boundary_inflight=true;
        const auto before=boundary_reads;c.CaptureBoundary();
        std::vector<int16_t> pcm(240);assert(c.InputData(pcm));
        assert(pcm[0]==71&&pcm.back()==71&&boundary_reads==before+1&&drain_chunks==0);
        ++scenarios;
    }
    for(int fault=1;fault<=4;++fault){
        auto c=make_codec();begin(c);boundary_behavior=fault;boundary_inflight=true;
        c.CaptureBoundary();assert(c.ReferenceFaulted()&&!c.PlaybackSupported());
        std::vector<int16_t> pcm(240);assert(!c.InputData(pcm));
        boundary_behavior=0;boundary_inflight=false;++scenarios;
    }
    {
        auto c=make_codec();begin(c);std::vector<int16_t> pcm(240);
        c.CaptureBoundary();drain_chunks=6;c.CaptureBoundary();assert(!c.ReferenceFaulted()&&drain_chunks==0);
        drain_behavior=3;c.CaptureBoundary();assert(!c.ReferenceFaulted());
        const auto before=reads;assert(c.InputData(pcm)&&reads==before+1&&pcm[0]==71);
        read_fails=true;assert(!c.InputData(pcm));read_fails=false;
        assert(c.ReferenceFaulted()&&!c.PlaybackSupported());assert(!c.InputData(pcm));++scenarios;
    }
    for(int fault=0;fault<4;++fault){
        auto c=make_codec();begin(c);
        if(fault==0)drain_error=-1;
        if(fault==1)drain_chunks=8;
        if(fault==2)drain_behavior=1;
        if(fault==3)drain_behavior=2;
        c.CaptureBoundary();assert(c.ReferenceFaulted()&&!c.PlaybackSupported());
        drain_error=drain_chunks=drain_behavior=0;++scenarios;
    }
    {
        auto c=make_codec();begin(c);std::vector<int16_t> prompt(120,5);c.OutputData(prompt);
        assert(c.PlaybackBusy());int16_t dma[6][240]{};for(int i=0;i<7;++i)tick(c,dma[i%6]);
        assert(!c.PlaybackBusy());++scenarios;
    }
    assert(raw_writes==0);assert(drain_reads>0);
    std::printf("C3 production adapter: %u scenarios passed; ordinary writes=%d\n",scenarios,raw_writes);
}
