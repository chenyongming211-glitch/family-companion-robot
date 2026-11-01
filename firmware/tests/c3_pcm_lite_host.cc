#include <atomic>
#include <cassert>
#include <cstdint>
#include <functional>
#include <mutex>
#include <vector>
#if CONFIG_XMINI_C3_VOICE_BARGE
#include "c3_lite_aec.h"
#endif
struct AudioCodec {
    size_t channels = 1;
    size_t input_channels() const { return channels; }
#if CONFIG_XMINI_C3_VOICE_BARGE
    uint32_t generation=7,ready_generation=0;bool fault=false,valid=true;
    uint32_t CaptureGeneration()const{return generation;}
    bool ReferenceValid()const{return valid;}bool ReferenceFaulted()const{return fault;}
    void NotifyAecReady(bool ready,uint32_t gen){ready_generation=ready?gen:0;}
    void ReportReferenceFailure(){fault=true;}
#endif
};
class LiteAudioEngine {
public:
    AudioCodec* codec_ = nullptr;
    std::atomic<bool> voice_processing_enabled_{true};
    std::atomic<uint32_t> capture_epoch_{0};
    uint32_t buffer_epoch_ = 0;
#if CONFIG_XMINI_C3_VOICE_BARGE
    family_robot::C3LiteAec* c3_aec_=family_robot::C3LiteAec::Create();
    uint32_t c3_capture_generation_=0;
    ~LiteAudioEngine(){family_robot::C3LiteAec::Destroy(c3_aec_);}
#endif
    int frame_samples_ = 60;
    std::vector<int16_t> output_buffer_;
    std::mutex output_mutex_;
    std::function<void(std::vector<int16_t>&&)> output_callback_;
    std::function<bool(const int16_t*,size_t,uint32_t)> c3_output_span_;
    void Feed(std::vector<int16_t>&& data) { OutputRawAudio(data); }
    void FeedCaptured(std::vector<int16_t>&& data, uint32_t generation);
    uint32_t OutputEpoch() const { return capture_epoch_.load(); }
    void OutputRawAudio(const std::vector<int16_t>& data);
};
#include "c3_lite_uplink.inc"
int main() {
    AudioCodec codec;codec.channels=2;LiteAudioEngine engine;engine.codec_=&codec;engine.frame_samples_=960;
    int spans=0;engine.output_callback_=[](std::vector<int16_t>&&){assert(false&&"PCM must bypass vector/Opus framing");};
    engine.c3_output_span_=[&](const int16_t* pcm,size_t n,uint32_t epoch) {
        assert(n==512&&epoch==3);for(size_t i=0;i<n;++i)assert(pcm[i]==10);++spans;return true;};
    std::vector<int16_t> mr(1024,3);for(size_t i=0;i<512;++i)mr[i*2]=13;
    engine.FeedCaptured(std::move(mr),3);assert(spans==1);assert(engine.output_buffer_.empty()&&engine.output_buffer_.capacity()==0);
    std::vector<int16_t> more(2048,3);for(size_t i=0;i<1024;++i)more[i*2]=13;
    engine.c3_output_span_=[&](const int16_t*,size_t,uint32_t){++spans;engine.voice_processing_enabled_=false;return false;};
    engine.FeedCaptured(std::move(more),3);assert(spans==2);
    const auto stats=engine.c3_aec_->Statistics();assert(stats.blocks==2&&stats.mic.samples==1024&&stats.reference.samples==1024&&stats.output.samples==1024);
    assert(stats.mic.energy==173056&&stats.reference.energy==9216&&stats.output.energy==102400);
    family_robot::C3EchoStatistics clipping;const int16_t extremes[4]={INT16_MIN,INT16_MAX,0,1};const int16_t zeros[4]{};
    clipping.Record(extremes,zeros,zeros,4,10);clipping.Record(extremes,zeros,zeros,4,15);
    assert(clipping.mic.energy==4294836228ull&&clipping.mic.samples==8&&clipping.mic.clipped==4&&clipping.mic.peak==32768);
    assert(clipping.total_us==25&&clipping.max_us==15&&clipping.blocks==2);
    clipping.mic.energy=UINT64_MAX-1;clipping.Record(extremes,zeros,zeros,4,0);assert(clipping.mic.energy==UINT64_MAX);
}
