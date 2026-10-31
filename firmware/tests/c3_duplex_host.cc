// SDK plumbing prepended by test_c3_duplex.py; all codec code is production.
#include "family_c3_codec.h"
#include "family_c3_lifecycle.inc"
#include "family_c3_playback.inc"
#include "family_c3_dma.inc"
#if CONFIG_XMINI_C3_VOICE_BARGE
#include "family_c3_capture.inc"
#endif
using Op=AudioCodec::PlaybackOp;
using Snapshot=family_robot::PlaybackSnapshot;
static FamilyC3Codec make_codec(){return FamilyC3Codec(nullptr,0,24000,24000,0,0,0,0,0,0,1);}
struct Physical {
    int16_t tx[6][240]{},mic[240]{};
    unsigned n=0;
    void step(FamilyC3Codec& c,int16_t value=71,bool rx_first=false,int phase=0){
        now_us+=10000;
        std::fill(mic,mic+240,value);
        i2s_event_data_t out{tx[n%6],480},in{mic,480};
        auto sent=[&]{c.tx.callbacks.on_sent(&c.tx,&out,c.tx.argument);};
        auto recv=[&]{now_us+=phase;c.rx.callbacks.on_recv(&c.rx,&in,c.rx.argument);now_us-=phase;};
        if(rx_first){recv();sent();}else{sent();recv();}
        ++n;
    }
    void open(FamilyC3Codec& c){c.Start();assert(c.PlaybackSupported());step(c);step(c);}
    void capture(FamilyC3Codec& c){c.SetCaptureActive(true);step(c,23);}
};
static void consume(FamilyC3Codec& c,Physical& p,int16_t value=71){
    p.step(c,value);std::vector<int16_t> mr(480);assert(c.InputData(mr));
    for(size_t i=0;i<240;++i)assert(mr[2*i]==value);
}
static void assert_stopped(FamilyC3Codec& c){
    Snapshot out;c.PlaybackPoll(out);
    assert(c.ReferenceFaulted()&&!c.RealtimeEligible()&&!c.ReferenceValid());
    assert(!c.PlaybackSupported()&&pa_level==0&&!c.tx.enabled&&!c.rx.enabled);
}
using SmallPair=family_robot::BoundedDuplexReference<4096,8,75000>;
static_assert(sizeof(SmallPair)<=13000);
static_assert(SmallPair::kRxBlocks==8 && SmallPair::kHoldbackUs==75000);
static_assert(family_robot::DuplexReference::kRxBlocks==16 && family_robot::DuplexReference::kHoldbackUs==150000);
static_assert(family_robot::PlaybackReferenceBuffer::kCapacity==16000);
int main(){
    unsigned scenarios=0;
    {
        auto c=make_codec();assert(c.input_reference_&&c.input_channels_==2);
        assert(c.rx.callbacks.on_recv&&!c.SoftwareReference()&&!c.ManagedCapture());
        Physical p;p.open(c);assert(!c.ReferenceValid()&&!c.RealtimeEligible());
        p.capture(c);consume(c,p);
        assert(c.ReferenceValid()&&!c.RealtimeEligible());
        auto g=c.CaptureGeneration();c.NotifyAecReady(true,g);assert(c.RealtimeEligible());
        c.NotifyAecReady(false,g);assert(!c.RealtimeEligible());++scenarios;
    }
    {
        // Completed old PCM (19) must be copied before Complete refills 99.
        auto c=make_codec();Physical p;p.open(c);p.capture(c);
        assert(c.PlaybackControl(Op::Connect,{1,0})&&c.PlaybackControl(Op::Start,{1,1}));
        assert(c.PlaybackControl(Op::Admit,{1,1},0,240));std::vector<int16_t> next(240,99);
        c.OutputTagged(next,{1,1},0,c.PlaybackGeneration());c.PlaybackControl(Op::Release,{1,1});
        std::fill(p.tx[p.n%6],p.tx[p.n%6]+240,19);p.step(c,83);
        std::vector<int16_t> mr(480);assert(c.InputData(mr));
        for(size_t i=0;i<240;++i)assert(mr[2*i]==83&&mr[2*i+1]==19);
        assert(p.tx[(p.n-1)%6][0]==99);
        c.PlaybackControl(Op::Seal,{1,1});
        for(unsigned i=0;i<6;++i)consume(c,p);
        Snapshot s;bool final=false;
        for(unsigned i=0;i<18;++i)if(c.PlaybackPoll(s)&&s.finished){final=true;break;}
        assert(final&&s.valid&&s.played_samples==240);++scenarios;
    }
    for(bool rx_first:{false,true})for(int phase:{-125,0,125}){
        auto c=make_codec();Physical p;c.Start();
        // Distinct callback phases preserve integer MR mapping and channel order.
        for(unsigned b=0;b<3;++b)p.step(c,71,rx_first,phase);
        c.SetCaptureActive(true);p.step(c,23,rx_first,phase);
        for(unsigned b=0;b<3;++b){std::fill(p.tx[p.n%6],p.tx[p.n%6]+240,int16_t(100+b));p.step(c,int16_t(200+b),rx_first,phase);}
        std::vector<int16_t> mr(480);assert(c.InputData(mr));
        for(size_t i=0;i<240;++i){assert(mr[2*i]==200);assert(mr[2*i+1]==(phase<0&&i<3?0:phase>0&&i>=237?101:100));}
        ++scenarios;
    }
    {
        auto c=make_codec();Physical p;p.open(c);p.capture(c);consume(c,p);
        const auto g=c.CaptureGeneration();c.NotifyAecReady(true,g);assert(c.RealtimeEligible());
        p.step(c,17);c.CaptureBoundary();const auto fresh=c.CaptureGeneration();assert(fresh!=g);
        assert(!c.ReferenceValid());c.NotifyAecReady(true,g);assert(!c.RealtimeEligible());
        p.step(c,23);p.step(c,91);std::vector<int16_t> mr(480);assert(c.InputData(mr)&&mr[0]==91);
        c.NotifyAecReady(true,fresh);assert(c.RealtimeEligible());
        c.SetCaptureActive(false);assert(c.CaptureGeneration()!=fresh&&!c.ReferenceValid());
        const auto before=reads;assert(!c.InputData(mr)&&reads==before);++scenarios;
    }
    {
        // A capture boundary preserves real TX history, including before start.
        auto c=make_codec();Physical p;p.open(c);p.capture(c);
        p.step(c,17);std::vector<int16_t> half(146);assert(c.InputData(half));
        auto old=c.CaptureGeneration();c.CaptureBoundary();assert(c.CaptureGeneration()!=old);
        p.step(c,23);p.step(c,97);std::vector<int16_t> mr(480);assert(c.InputData(mr));
        assert(mr[0]==97&&mr[478]==97&&!c.ReferenceFaulted());++scenarios;
    }
    {
        auto c=make_codec();Physical p;p.open(c);p.capture(c);
        for(int b=0;b<9;++b)p.step(c);
        assert_stopped(c);++scenarios;
    }
    {
        auto c=make_codec();Physical p;p.open(c);p.capture(c);const auto before=now_us;
        std::vector<int16_t> mr(480);assert(!c.InputData(mr));assert(now_us-before<=100000);
        assert_stopped(c);assert(reads==0);++scenarios;
    }
    for(int error=0;error<10;++error){
        auto c=make_codec();Physical p;p.open(c);p.capture(c);
        i2s_event_data_t bad{p.mic,error==0?478u:480u};
        if(error==1)bad.dma_buf=nullptr;
        if(error<3)c.rx.callbacks.on_recv(&c.rx,error==2?nullptr:&bad,c.rx.argument);
        if(error==3){now_us+=20000;c.rx.callbacks.on_recv(&c.rx,&bad,c.rx.argument);}
        if(error==4){std::vector<int16_t> odd(481);assert(!c.InputData(odd));}
        if(error==5)c.ReportReferenceFailure();
        if(error==6){std::vector<int16_t> empty;assert(!c.InputData(empty));}
        if(error==7){std::vector<int16_t> large(2882);assert(!c.InputData(large));}
        if(error==8){bad.size=478;c.tx.callbacks.on_sent(&c.tx,&bad,c.tx.argument);}
        if(error==9){now_us+=20000;p.step(c);}
        assert_stopped(c);++scenarios;
    }
    {
        // Logical cancel while a read waits returns no old/partial frame.
        auto c=make_codec();Physical p;p.open(c);p.capture(c);
        delay_hook=[&]{c.CaptureBoundary();};std::vector<int16_t> mr(480);
        assert(!c.InputData(mr));delay_hook={};assert(!c.ReferenceFaulted());++scenarios;
    }
    {
        auto c=make_codec();Physical p;p.open(c);p.capture(c);consume(c,p);
        auto old=c.CaptureGeneration();c.NotifyAecReady(true,old);
        c.SetCaptureActive(false);c.EnableOutput(false);c.EnableInput(false);
        assert(!c.tx.enabled&&!c.rx.enabled);for(auto& block:p.tx)for(auto sample:block)assert(sample==0);
        c.Start();p.step(c);p.step(c);p.capture(c);consume(c,p);
        c.NotifyAecReady(true,old);assert(!c.RealtimeEligible());assert(c.CaptureGeneration()!=old);++scenarios;
    }
    {
        // Destructor unregisters both live callback contexts after disabling.
        auto* c=new FamilyC3Codec(nullptr,0,24000,24000,0,0,0,0,0,0,1);Physical p;p.open(*c);p.capture(*c);
        unsigned callbacks_during_disable=0;
        disable_hook=[&](Channel* channel){
            i2s_event_data_t e{channel==&c->tx?static_cast<void*>(p.tx[0]):static_cast<void*>(p.mic),480};
            if(channel->callbacks.on_sent){++callbacks_during_disable;channel->callbacks.on_sent(channel,&e,channel->argument);}
            if(channel->callbacks.on_recv){++callbacks_during_disable;channel->callbacks.on_recv(channel,&e,channel->argument);}
        };
        codec_destroy_hook=[&](const Channel& tx,const Channel& rx){
            assert(!tx.enabled&&!rx.enabled&&!tx.callbacks.on_sent&&!rx.callbacks.on_recv);
            assert(tx.argument==nullptr&&rx.argument==nullptr);
        };
        delete c;disable_hook={};codec_destroy_hook={};assert(callbacks_during_disable==2);++scenarios;
    }
    {
        //60ms task requests consume every integer sample across240-frame DMAs.
        auto c=make_codec();Physical p;p.open(c);p.capture(c);
        unsigned supplied=0;
        delay_hook=[&]{now_us-=10000;p.step(c,int16_t(100+supplied++));};
        std::vector<int16_t> mr(2880);assert(c.InputData(mr));delay_hook={};
        assert(supplied==6);
        for(size_t i=0;i<1440;++i)assert(mr[2*i]==int16_t(100+i/240)&&mr[2*i+1]==0);
        p.step(c,201);std::vector<int16_t> prefix(146),suffix(334);
        assert(c.InputData(prefix)&&c.InputData(suffix));
        for(size_t i=0;i<prefix.size()/2;++i)assert(prefix[2*i]==201);
        for(size_t i=0;i<suffix.size()/2;++i)assert(suffix[2*i]==201);
        ++scenarios;
    }
    {
        SmallPair d;d.Begin(1,0);d.Capture(true);
        int16_t tx[240],mic[240]{},mr[480];
        for(int b=0;b<2000;++b){
            std::fill(tx,tx+240,int16_t(b));
            assert(d.Tx(b*240,(b+1)*10000,tx,240));assert(d.Rx(b*240,(b+1)*10000,mic,240));
            assert(d.Take(mr,73,(b+1)*10000)==SmallPair::Result::Ready);
            for(size_t i=0;i<73;++i)assert(mr[2*i+1]==b);
            assert(d.Take(mr,167,(b+1)*10000)==SmallPair::Result::Ready);
            for(size_t i=0;i<167;++i)assert(mr[2*i+1]==b);
        }
        d.Begin(2,0);d.Capture(true);assert(d.Rx(0,10000,mic,240));
        for(int b=0;b<18;++b)assert(d.Tx(b*240,(b+1)*10000,tx,240));
        assert(d.Take(mr,240,180000)==SmallPair::Result::Fault&&d.fault()==SmallPair::Fault::Expired);
        ++scenarios;
    }
    {
        // A negative RX phase needs the prior physical TX block across the
        // capture boundary; that history must survive queue/epoch invalidation.
        auto c=make_codec();Physical p;c.Start();
        p.step(c,71,false,-125);p.step(c,71,false,-125);
        c.SetCaptureActive(true);p.step(c,23,false,-125);
        std::fill(p.tx[p.n%6],p.tx[p.n%6]+240,31);p.step(c,17,false,-125);
        c.CaptureBoundary();
        std::fill(p.tx[p.n%6],p.tx[p.n%6]+240,41);p.step(c,23,false,-125);
        std::fill(p.tx[p.n%6],p.tx[p.n%6]+240,51);p.step(c,91,false,-125);
        std::vector<int16_t> mr(480);assert(c.InputData(mr));
        for(size_t i=0;i<240;++i)assert(mr[2*i]==91&&mr[2*i+1]==(i<3?41:51));
        ++scenarios;
    }
    for(int failure=0;failure<4;++failure) {
        if(failure<2)fail_allocation_call=allocation_calls+1+failure;
        else fail_registration_call=registration_calls+failure-1;
        {
            auto c=make_codec();c.Start();assert(c.ReferenceFaulted()&&!c.PlaybackSupported());
            codec_destroy_hook=[](const Channel& tx,const Channel& rx){
                assert(!tx.enabled&&!rx.enabled&&!tx.callbacks.on_sent&&!rx.callbacks.on_recv);
                assert(tx.argument==nullptr&&rx.argument==nullptr);
            };
        }
        codec_destroy_hook={};fail_allocation_call=fail_registration_call=0;++scenarios;
    }
    for(bool poll:{false,true}) {
        auto c=make_codec();Physical p;p.open(c);p.capture(c);p.step(c,73);
        now_us+=75001;
        if(poll){Snapshot out;c.PlaybackPoll(out);}else{std::vector<int16_t> mr(480);assert(!c.InputData(mr));}
        assert_stopped(c);++scenarios;
    }
    assert(raw_writes==0&&reads==0);
    std::printf("C3 bounded duplex production adapter: %u scenarios passed; raw reads/writes=0; pair=%zu bytes\n",scenarios,sizeof(SmallPair));
}
