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
    void Feed(std::vector<int16_t>&& data) { OutputRawAudio(data); }
    void FeedCaptured(std::vector<int16_t>&& data, uint32_t generation);
    uint32_t OutputEpoch() const { return capture_epoch_.load(); }
    void OutputRawAudio(const std::vector<int16_t>& data);
};
#include "c3_lite_uplink.inc"
int main() {
    AudioCodec codec;
    LiteAudioEngine engine;
    engine.codec_ = &codec;
#if CONFIG_XMINI_C3_VOICE_BARGE
    codec.channels=2;
    int count=0;
    engine.output_callback_=[&](std::vector<int16_t>&& pcm){assert(pcm.size()==60);for(auto value:pcm)assert(value==10);++count;};
    auto feed=[&](size_t frames,uint32_t epoch){std::vector<int16_t> mr(frames*2,3);for(size_t i=0;i<frames;++i)mr[2*i]=13;engine.FeedCaptured(std::move(mr),epoch);};
    engine.OutputRawAudio(std::vector<int16_t>(1000,99));assert(count==0);
    feed(320,3);assert(count==0&&codec.ready_generation==0);
    feed(320,3);assert(count==8&&codec.ready_generation==7);
    const auto created=aec_created;
    feed(384,3);assert(count==17&&aec_created==created); // continuous playback-time samples
    feed(320,5);assert(count==17&&codec.ready_generation==0&&aec_created==created+1);
    feed(192,5);assert(count==25);
    ++codec.generation;feed(320,5);assert(count==25&&codec.ready_generation==0&&aec_created==created+2);
    feed(192,5);assert(count==33&&codec.ready_generation==8);
    codec.valid=false;feed(1024,5);assert(count==33&&!codec.fault);
    codec.valid=true;feed(1025,5);assert(codec.fault&&count==33&&codec.ready_generation==0);
#else
    int count = 0;
    uint32_t expected = 3;
    engine.output_callback_ = [&](std::vector<int16_t>&& pcm) {
        assert(engine.OutputEpoch() == expected && pcm.size() == 60);
        for (auto sample : pcm) assert(sample == static_cast<int16_t>(expected));
        ++count;
    };
    engine.FeedCaptured(std::vector<int16_t>(30, 1), 1);
    engine.FeedCaptured(std::vector<int16_t>(30, 3), 3);
    assert(count == 0);
    engine.FeedCaptured(std::vector<int16_t>(30, 3), 3);
    assert(count == 1);
    engine.voice_processing_enabled_ = false;
    engine.FeedCaptured(std::vector<int16_t>(60, 1), 1);
    assert(count == 1);
    engine.voice_processing_enabled_ = true;
    expected = 5;
    engine.FeedCaptured(std::vector<int16_t>(120, 5), 5);
    assert(count == 3);
    engine.FeedCaptured(std::vector<int16_t>(10, 5), 5);
    expected = 7;
    engine.FeedCaptured(std::vector<int16_t>(30, 7), 7);
    engine.FeedCaptured(std::vector<int16_t>(30, 7), 7);
    assert(count == 4);
    codec.channels = 2;
    expected = 9;
    std::vector<int16_t> stereo(120, 123);
    for (size_t i = 0; i < stereo.size(); i += 2) stereo[i] = 9;
    engine.FeedCaptured(std::move(stereo), 9);
    assert(count == 5);
#endif
}
