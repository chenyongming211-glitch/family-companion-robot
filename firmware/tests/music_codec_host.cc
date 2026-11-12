#define main voice_regression_main
#include "waveshare_codec_host.cc"
#undef main
int main() {
    assert(voice_regression_main()==0);
    auto c=make_codec();c.Start();
    assert(!c.BeginMusic(44100)&&!c.MusicActive());
    c.SetCaptureActive(true);c.NotifyAecReady(true,c.CaptureGeneration());
    const auto old=c.PlaybackGeneration();
    assert(c.BeginMusic(48000)&&c.MusicActive()&&!c.ReferenceValid()&&!c.RealtimeEligible());
    assert(c.output_sample_rate_==24000&&c.standard.clk_cfg.sample_rate_hz==48000&&!c.muted);
    assert(!c.BeginMusic(24000));
    assert(!c.PlaybackControl(AudioCodec::PlaybackOp::Start,{1,1}));
    std::vector<int16_t> input(240),pcm(240,42);
    assert(!c.InputData(input)&&!c.ReferenceFaulted());
    c.EnableOutput(false);c.EnableInput(false);
    assert(c.output_enabled_&&c.input_enabled_);
    c.OutputTagged(pcm,{},0,old);assert(!c.PlaybackBusy());
    assert(c.MusicWrite(pcm));
    int16_t dma[6][240]{};
    for(int i=0;i<7;++i)tick(c,dma[i%6],480,5000);
    assert(c.MusicCompleted()==240&&!c.PlaybackBusy()&&!c.ReferenceFaulted());
    const auto capture=c.CaptureGeneration();
    assert(c.EndMusic()&&!c.MusicActive()&&c.output_sample_rate_==24000&&!c.muted);
    assert(c.CaptureGeneration()!=capture&&!c.RealtimeEligible()&&c.ReferenceValid());
    assert(!c.MusicWrite(pcm));
    c.SetCaptureActive(true);c.NotifyAecReady(true,c.CaptureGeneration());assert(c.RealtimeEligible());
    // Restore cannot clear an existing fault or claim a failed clock change.
    auto failed=make_codec();failed.Start();failed.reopen_fails=true;
    assert(!failed.BeginMusic(48000)&&failed.ReferenceFaulted());
    assert(!failed.EndMusic()&&failed.MusicActive());
    auto bad=make_codec();bad.Start();bad.mute_fails=true;
    assert(!bad.BeginMusic(48000)&&bad.ReferenceFaulted());
    auto stalled=make_codec();stalled.Start();assert(stalled.BeginMusic(48000));
    std::vector<int16_t> block(1440,7);
    assert(stalled.MusicWrite(block)&&stalled.MusicWrite(block));
    assert(!stalled.MusicWrite(block)); // full queue/no DMA must never be ACKed
    puts("Exclusive music profiles, completion, cancellation boundary and restoration passed");
}
