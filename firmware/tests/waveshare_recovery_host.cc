#define main voice_regression_main
#include "waveshare_codec_host.cc"
#undef main
int main() {
    using Op=AudioCodec::PlaybackOp;
    {auto c=make_codec();c.Start();const auto before=saves;
     c.ReportReferenceFailure();c.PollAudioFault();assert(saves==before+1);
     c.PollAudioFault();assert(saves==before+1&&c.ReferenceFaulted());}
    for(int phase=1;phase<=8;++phase) {
        pa_initialization_error=phase==8?-38:0;
        allocation_fails=phase==1;tx_disable_error=phase<=2?-12:0; // later SDK failure must not replace allocation cause
        rx_disable_error=phase==3?-13:0;registration_fails=phase==5;
        tx_enable_error=phase==6?-16:0;rx_enable_error=phase==7?-17:0;
        XiaomanBoxCodec c(nullptr,phase==4?16000:24000,24000,0,0,0,0,0,0,1,2,true);
        allocation_fails=registration_fails=false;pa_initialization_error=0;
        tx_disable_error=rx_disable_error=tx_enable_error=rx_enable_error=0;
        c.Start();assert(c.ReferenceFaulted()&&saved_fault.source==4&&saved_fault.detail==unsigned(phase));
        const auto native=phase==8?-38:phase==5?-1:phase==1 || phase==4?0:-10-phase;
        assert(saved_fault.native==native);
    }
    for(int detail=1;detail<=6;++detail) {
        auto c=make_codec();
        if(detail==1)c.tx.get_result=-21;
        if(detail==2)c.rx.get_result=-22;
        if(detail==3)c.tx.info.pair_chan=nullptr;
        if(detail==4)c.standard.slot_cfg.slot_bit_width=16;
        if(detail==5)c.standard.slot_cfg.slot_mask=3;
        if(detail==6)c.rx.info.total_dma_buf_size=100;
        c.Start();assert(c.ReferenceFaulted()&&saved_fault.source==1&&saved_fault.detail==unsigned(detail));
        assert(saved_fault.native==(detail<=2?-20-detail:0));
    }
    auto probe=[](XiaomanBoxCodec& c,int16_t (&dma)[6][240],size_t& n) {
        delay_hook=[&c,&dma,&n] {
            assert(c.ReferenceFaulted()&&!c.ReferenceValid()&&!c.PlaybackSupported()&&c.muted);
            i2s_event_data_t event{dma[n++%6],sizeof(dma[0])};
            c.tx.callbacks.on_sent(&c.tx,&event,c.tx.argument);
            assert(dma[(n-1)%6][0]==0);
        };
    };
    {
        auto c=make_codec();c.Start();
        const auto before=reopens,old_saves=saves;
        assert(c.PrepareAudioConnection()&&reopens==before&&saves==old_saves);
        c.PlaybackControl(Op::Connect,{8,0});assert(c.PlaybackControl(Op::Start,{8,1}));
        c.PlaybackControl(Op::Admit,{8,1},0,240);
        const auto old=c.PlaybackGeneration(),wake=c.WakeAudioGeneration(),capture=c.CaptureGeneration();
        std::vector<int16_t> pcm(240,87);c.OutputTagged(pcm,{8,1},0,old);
        int16_t dma[6][240]{};tick(c,dma[0]);tick(c,dma[0]); // first duplicate, then other faults
        c.ReportReferenceFailure();
        size_t n=0;probe(c,dma,n);
        save_hook=[&]{assert(c.muted&&c.ReferenceFaulted()&&n==0);};
        reopen_hook=[&]{assert(saves==old_saves+1);};
        read_hook=[&]{assert(n>=12&&c.ReferenceFaulted()&&c.muted);};
        assert(c.PrepareAudioConnection());delay_hook={};save_hook={};reopen_hook={};read_hook={};
        assert(!c.ReferenceFaulted()&&"runtime fault must recover after fresh DMA and bounded input");
        assert(c.PlaybackSupported()&&c.ReferenceValid()&&n>=12&&raw_reads==1&&raw_read_timeout==200&&!c.muted);
        assert(saved_fault.source==3&&saved_fault.detail==5&&saved_fault.dma_bytes==480&&saved_fault.dma_gap_us==10000);
        assert(c.PlaybackGeneration()!=old&&c.CaptureGeneration()!=capture&&c.WakeAudioGeneration()!=wake);
        assert(!family_robot::WakeBatchValid(1,1,wake,c.WakeAudioGeneration()));
        c.OutputTagged(pcm,{8,1},0,old);assert(!c.PlaybackBusy());
        family_robot::PlaybackSnapshot result;while(c.PlaybackPoll(result))assert(!result.valid);
        assert(c.PlaybackControl(Op::Start,{8,2})); // current connection survives hardware recovery
        c.ReportReferenceFailure();const auto attempts=reopens;
        assert(!c.PrepareAudioConnection()&&reopens==attempts&&saves==old_saves+1);
        assert(saved_fault.source==3&&saved_fault.detail==5);
    }
    // Stored prior failure is diagnostic only: a fresh healthy boot stays healthy.
    {auto c=make_codec();const auto before=loads;c.Start();assert(loads==before+1&&c.PrepareAudioConnection());}
    for(int failure=0;failure<11;++failure) {
        auto c=make_codec();c.Start();c.ReportReferenceFailure();
        int16_t dma[6][240]{};size_t n=0;
        probe(c,dma,n);
        if(failure==0)delay_hook={};
        if(failure==1)c.reopen_fails=true;
        if(failure==2)c.standard.slot_cfg.slot_mask=3;
        if(failure==3)raw_read_fails=true;
        if(failure==4)raw_short=true;
        if(failure==5)delay_hook=[&]{i2s_event_data_t e{dma[0],480};c.tx.callbacks.on_sent(&c.tx,&e,c.tx.argument);};
        if(failure==6)c.mute_fails=true;
        if(failure==7)c.unmute_fails=true;
        if(failure==8)read_hook=[&]{i2s_event_data_t e{dma[4],480};c.tx.callbacks.on_sent(&c.tx,&e,c.tx.argument);};
        if(failure==9)read_hook=[&]{now_us+=200000;};
        if(failure==10)unmute_hook=[&]{i2s_event_data_t e{dma[4],480};c.tx.callbacks.on_sent(&c.tx,&e,c.tx.argument);};
        const auto began=now_us;
        assert(!c.PrepareAudioConnection()&&c.ReferenceFaulted()&&!c.PlaybackSupported());
        assert(now_us-began<=500000);
        if(failure==8 || failure==9)assert(c.unmute_calls==0&&"failed probe must stay muted without unmute attempt");
        if(failure==10)assert(c.muted);
        delay_hook={};read_hook={};unmute_hook={};raw_read_fails=raw_short=false;
        const auto attempts=reopens;assert(!c.PrepareAudioConnection()&&reopens==attempts);
    }
    {auto c=make_codec();c.Start();c.SetCaptureActive(true);c.ReportReferenceFailure();
     const auto attempts=reopens;assert(!c.PrepareAudioConnection()&&reopens==attempts);
     c.SetCaptureActive(false);int16_t dma[6][240]{};size_t n=0;probe(c,dma,n);
     store_fails=true;assert(c.PrepareAudioConnection());store_fails=false;delay_hook={};}
    {auto c=make_codec();c.Start();assert(c.BeginMusic(48000));c.ReportReferenceFailure();
     const auto attempts=reopens;assert(!c.PrepareAudioConnection()&&reopens==attempts);}
    for(int failure=0;failure<3;++failure) {
        allocation_fails=failure==0;registration_fails=failure==1;
        auto c=make_codec();if(failure==2)c.standard.slot_cfg.slot_mask=3;c.Start();
        allocation_fails=registration_fails=false;
        if(failure==1)assert(saved_fault.source==4&&saved_fault.native==-1);
        const auto attempts=reopens;assert(!c.PrepareAudioConnection()&&reopens==attempts);
    }
    {auto c=make_codec();c.Start();
     std::promise<void> entered,release;auto wait=release.get_future();
     box_read_hook=[&]{entered.set_value();wait.wait();};
     std::vector<int16_t> input(480);
     auto blocked=std::async(std::launch::async,[&]{return c.InputData(input);});
     entered.get_future().wait();c.ReportReferenceFailure();
     const auto attempts=reopens,before_saves=saves;const auto began=now_us;
     assert(!c.PrepareAudioConnection()&&now_us-began==200000&&reopens==attempts&&saves==before_saves);
     c.PollAudioFault();assert(saves==before_saves);
     release.set_value();assert(!blocked.get());box_read_hook={};
     c.PollAudioFault();assert(saves==before_saves+1);
     int16_t dma[6][240]{};size_t n=0;probe(c,dma,n);
     assert(c.PrepareAudioConnection());delay_hook={};}
    {auto c=make_codec();c.Start();c.ReportReferenceFailure();
     std::promise<void> entered,release;auto wait=release.get_future();
     const auto before=saves,attempts=reopens;
     save_hook=[&]{assert(c.muted&&c.ReferenceFaulted());entered.set_value();wait.wait();};
     auto saving=std::async(std::launch::async,[&]{c.PollAudioFault();});
     entered.get_future().wait();c.PollAudioFault();
     assert(!c.PrepareAudioConnection()&&reopens==attempts);
     release.set_value();saving.get();save_hook={};assert(saves==before+1);
     int16_t dma[6][240]{};size_t n=0;probe(c,dma,n);
     assert(c.PrepareAudioConnection()&&saves==before+1);delay_hook={};}
    puts("Waveshare controlled recovery passed: fresh DMA/input, first fault, once/boot, isolation, failures and eligibility");
}
