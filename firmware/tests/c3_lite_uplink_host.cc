#include <atomic>
#include <cassert>
#include <cstdint>
#include <functional>
#include <mutex>
#include <vector>
struct AudioCodec {
    size_t channels = 1;
    size_t input_channels() const { return channels; }
};
class LiteAudioEngine {
public:
    AudioCodec* codec_ = nullptr;
    std::atomic<bool> voice_processing_enabled_{true};
    std::atomic<uint32_t> capture_epoch_{0};
    uint32_t buffer_epoch_ = 0;
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
}
