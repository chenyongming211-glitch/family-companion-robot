#include <atomic>
#include <cassert>
#include <cstdint>
#include <condition_variable>
#include <cstdio>
#include <deque>
#include <functional>
#include <memory>
#include <mutex>
#include <string>
#include <vector>
#include "experiment_gate.h"

#define CONFIG_SEND_WAKE_WORD_DATA 1
#define CONFIG_CUSTOM_WAKE_WORD "xiao man"
#define CONFIG_CUSTOM_WAKE_WORD_DISPLAY "小满"
#define CONFIG_CUSTOM_WAKE_WORD_THRESHOLD 20
#define ESP_LOGE(...) ((void)0)
#define ESP_LOGW(...) ((void)0)
#define ESP_LOGI(...) ((void)0)
#define ESP_LOGD(...) ((void)0)
constexpr int ESP_OK = 0;
constexpr int ESP_FAIL = -1;
constexpr int WAKENET_DETECTED = 1;
constexpr int ESP_MN_RESULT_MAX_NUM = 5;
constexpr const char* ESP_MN_PREFIX = "mn";
enum esp_mn_state_t { ESP_MN_STATE_DETECTING, ESP_MN_STATE_DETECTED, ESP_MN_STATE_TIMEOUT };
struct model_iface_data_t {};
struct srmodel_list_t { int num = 1; };
struct esp_mn_error_t { int num = 1; };
struct esp_mn_results_t { int num = 1; int command_id[1] = {1}; const char* string = "xiao man"; float prob[1] = {0.9f}; };
struct AudioCodec {
    bool software = true;
    uint32_t generation = 7;
    uint32_t capture_generation = 7;
    bool SoftwareReference() const { return software; }
    int input_channels() const { return 2; }
    uint32_t WakeAudioGeneration() const { return generation; }
    uint32_t CaptureGeneration() const { return capture_generation; }
};
struct WakeWordAudioCache {
    int initializes = 0, stores = 0, clears = 0;
    size_t samples = 0;
    std::function<void()> on_store;
    bool Initialize(size_t) { ++initializes; return true; }
    void Store(const int16_t*, size_t count) {
        ++stores;
        samples += count;
        if (on_store) on_store();
    }
    void Clear() { ++clears; samples = 0; }
};
static bool create_failed = false, add_failed = false, update_failed = false, empty_commands = false;
static int detects = 0, cleans = 0, registered = 0;
static esp_mn_state_t detection = ESP_MN_STATE_DETECTING;
static std::function<void()> during_detect;
static model_iface_data_t model;
static esp_mn_results_t results;
static model_iface_data_t* create(const char*, int) { return create_failed ? nullptr : &model; }
static int set_threshold(model_iface_data_t*, float threshold) { assert(threshold == 0.2f); return 0; }
static int chunksize(model_iface_data_t*) { return 4; }
static void print_commands(model_iface_data_t*) {}
static esp_mn_state_t detect(model_iface_data_t*, int16_t*) {
    ++detects;
    if (during_detect) during_detect();
    return detection;
}
static esp_mn_results_t* get_results(model_iface_data_t*) { return &results; }
static void clean(model_iface_data_t*) { ++cleans; }
struct esp_mn_iface_t {
    decltype(&::create) create = ::create;
    decltype(&set_threshold) set_det_threshold = set_threshold;
    decltype(&chunksize) get_samp_chunksize = chunksize;
    decltype(&print_commands) print_active_speech_commands = print_commands;
    decltype(&::detect) detect = ::detect;
    decltype(&::get_results) get_results = ::get_results;
    decltype(&::clean) clean = ::clean;
};
static esp_mn_iface_t iface;
static srmodel_list_t models;
static srmodel_list_t* esp_srmodel_init(const char*) { return &models; }
static char* esp_srmodel_filter(srmodel_list_t*, const char*, const char*) { static char name[] = "mn7_cn"; return name; }
static esp_mn_iface_t* esp_mn_handle_from_name(const char*) { return &iface; }
static int esp_mn_commands_clear() { registered = 0; return ESP_OK; }
static int esp_mn_commands_add(int id, const char* command) {
    assert(id == 1 && std::string(command) == "xiao man");
    if (add_failed) return ESP_FAIL;
    ++registered;
    return ESP_OK;
}
static esp_mn_error_t* esp_mn_commands_update() { static esp_mn_error_t error; return update_failed ? &error : nullptr; }

class CustomWakeWord {
public:
    struct Command { std::string command, text, action; };
    AudioCodec* codec_ = nullptr;
    srmodel_list_t* models_ = nullptr;
    bool owns_models_ = false;
    std::string language_ = "cn", last_detected_wake_word_;
    int duration_ = 3000;
    float threshold_ = 0.2f;
    char* mn_name_ = nullptr;
    esp_mn_iface_t* multinet_ = nullptr;
    model_iface_data_t* multinet_model_data_ = nullptr;
    std::vector<Command> commands_;
    std::vector<int16_t> input_buffer_;
    std::mutex input_buffer_mutex_;
    std::atomic<bool> running_{false};
    WakeWordAudioCache wake_word_audio_cache_;
    std::function<void(const std::string&)> wake_word_detected_callback_;
    void ParseWakenetModelConfig() { if (!empty_commands) commands_.push_back({"xiao man", "小满", "wake"}); }
    bool Initialize(AudioCodec*, srmodel_list_t*);
    void Start();
    void Stop();
    void ResetCapture();
    // FEED_SAMPLES_DECLARATION
    // FEED_MONO_DECLARATION
};
struct afe_fetch_result_t { int16_t* data; int data_size; int wakeup_state = 0; int wakenet_model_index = 1; };
class AfeAudioEngine {
public:
    struct WakePacket { std::vector<uint8_t> opus; uint32_t batch, generation; };
    enum class WakeDetector { kNone, kWakeNet, kMultiNet };
    WakeDetector wake_detector_ = WakeDetector::kMultiNet;
    AudioCodec* codec_ = nullptr;
    std::unique_ptr<CustomWakeWord> custom_wake_word_;
    WakeWordAudioCache wake_word_audio_cache_;
    std::mutex wake_word_mutex_;
    std::condition_variable wake_word_cv_;
    std::atomic<uint32_t> wake_batch_{1};
    std::deque<WakePacket> experiment_wake_word_opus_;
    uint32_t emitted_wake_generation_ = 7, wake_cache_generation_ = 0;
    std::vector<std::string> wake_words_{"你好小智"};
    std::string last_detected_wake_word_;
    std::function<void(const std::string&)> wake_word_detected_callback_;
    int event_group_ = 0;
    static constexpr int kWakeWordEnabled = 1;
    void UpdateActiveState() {}
    void ResetWakeWordCapture();
    // WAKE_RESULT_DECLARATION
    bool GetExperimentWakeWordOpus(std::vector<uint8_t>&);
};
static void xEventGroupClearBits(int, int) {}

// PRODUCTION_METHODS

// Accept the pre-fix signatures too, so the Store-hook race runs against old source.
template <class Detector>
auto FeedMonoFrame(Detector& detector, const int16_t* data, size_t samples, uint32_t capture, int)
    -> decltype(detector.FeedMono(data, samples, capture), void()) {
    detector.FeedMono(data, samples, capture);
}
template <class Detector>
auto FeedMonoFrame(Detector& detector, const int16_t* data, size_t samples, uint32_t, long)
    -> decltype(detector.FeedMono(data, samples), void()) {
    detector.FeedMono(data, samples);
}
template <class Engine>
auto FeedWakeFrame(Engine& engine, const afe_fetch_result_t* frame, uint32_t capture, int)
    -> decltype(engine.HandleWakeWordResult(frame, capture), void()) {
    engine.HandleWakeWordResult(frame, capture);
}
template <class Engine>
auto FeedWakeFrame(Engine& engine, const afe_fetch_result_t* frame, uint32_t, long)
    -> decltype(engine.HandleWakeWordResult(frame), void()) {
    engine.HandleWakeWordResult(frame);
}

int main(int argc, char** argv) {
    assert(argc == 2);
    const std::string scenario = argv[1];
    AudioCodec codec;
    CustomWakeWord custom;
    create_failed = scenario == "create-failure";
    add_failed = scenario == "add-failure";
    update_failed = scenario == "update-failure";
    empty_commands = scenario == "empty-commands";
    if (scenario == "ordinary-cache" || scenario == "nonsoftware-stale-source") codec.software = false;
    const bool initialized = custom.Initialize(&codec, &models);
    if (create_failed || add_failed || update_failed || empty_commands) {
        assert(!initialized);
        return 0;
    }
    assert(initialized && registered == 1);
    custom.Start();
    int16_t samples[4] = {1, 2, 3, 4};
    if (scenario.rfind("tagged-", 0) == 0) {
        AfeAudioEngine engine;
        engine.codec_ = &codec;
        engine.experiment_wake_word_opus_.push_back({{42},
            scenario == "tagged-old-batch" ? 0u : 1u,
            scenario == "tagged-old-generation" ? 6u : codec.generation});
        std::vector<uint8_t> opus{99};
        const bool valid = engine.GetExperimentWakeWordOpus(opus);
        assert(valid == (scenario == "tagged-valid"));
        assert(valid ? opus == std::vector<uint8_t>{42} : opus.empty());
    } else if (scenario == "partial-reset") {
        FeedMonoFrame(custom, samples, 3, codec.CaptureGeneration(), 0);
        assert(detects == 0);
        custom.ResetCapture();
        FeedMonoFrame(custom, samples, 1, codec.CaptureGeneration(), 0);
        assert(detects == 0 && cleans == 1);
    } else if (scenario == "software-cache" || scenario == "ordinary-cache") {
        FeedMonoFrame(custom, samples, 4, codec.CaptureGeneration(), 0);
        assert(detects == 1);
        assert(custom.wake_word_audio_cache_.initializes == (codec.software ? 0 : 1));
        assert(custom.wake_word_audio_cache_.stores == (codec.software ? 0 : 1));
    } else if (scenario == "invalid-command-id") {
        detection = ESP_MN_STATE_DETECTED;
        results.command_id[0] = 0;
        FeedMonoFrame(custom, samples, 4, codec.CaptureGeneration(), 0);
        assert(custom.running_ && custom.last_detected_wake_word_.empty());
    } else if (scenario == "epoch-during-detect") {
        detection = ESP_MN_STATE_DETECTED;
        during_detect = [&] { ++codec.capture_generation; };
        int callbacks = 0;
        custom.wake_word_detected_callback_ = [&](const std::string&) { ++callbacks; };
        FeedMonoFrame(custom, samples, 4, codec.CaptureGeneration(), 0);
        assert(callbacks == 0 && custom.running_ && custom.input_buffer_.empty() && cleans == 1);
    } else if (scenario == "nonsoftware-stale-source") {
        detection = ESP_MN_STATE_DETECTED;
        const auto source_capture = codec.CaptureGeneration();
        ++codec.capture_generation;
        FeedMonoFrame(custom, samples, 4, source_capture, 0);
        assert(detects == 1 && custom.last_detected_wake_word_ == "小满");
    } else if (scenario == "engine-cache-before-detect" || scenario == "engine-epoch-reset" ||
               scenario == "engine-store-capture-only" || scenario == "engine-store-capture-and-wake") {
        AfeAudioEngine engine;
        engine.codec_ = &codec;
        engine.custom_wake_word_ = std::make_unique<CustomWakeWord>();
        assert(engine.custom_wake_word_->Initialize(&codec, &models));
        engine.custom_wake_word_->Start();
        if (scenario == "engine-store-capture-only" || scenario == "engine-store-capture-and-wake") {
            const auto source_capture = codec.CaptureGeneration();
            engine.wake_word_audio_cache_.on_store = [&] {
                ++codec.capture_generation;
                if (scenario == "engine-store-capture-and-wake") ++codec.generation;
            };
            detection = ESP_MN_STATE_DETECTED;
            int callbacks = 0;
            engine.custom_wake_word_->wake_word_detected_callback_ = [&](const std::string&) { ++callbacks; };
            afe_fetch_result_t frame{samples, sizeof(samples)};
            FeedWakeFrame(engine, &frame, source_capture, 0);
            assert(engine.wake_word_audio_cache_.stores == 1 && codec.capture_generation == source_capture + 1);
            assert(detects == 0 && callbacks == 0 && engine.custom_wake_word_->running_);
            assert(engine.custom_wake_word_->input_buffer_.empty());
        } else if (scenario == "engine-cache-before-detect") {
            detection = ESP_MN_STATE_DETECTED;
            engine.custom_wake_word_->wake_word_detected_callback_ = [&](const std::string& word) {
                assert(word == "小满" && engine.wake_word_audio_cache_.samples == 4);
            };
            afe_fetch_result_t frame{samples, sizeof(samples)};
            FeedWakeFrame(engine, &frame, codec.CaptureGeneration(), 0);
            assert(detects == 1 && engine.wake_word_audio_cache_.stores == 1);
            ++codec.generation;
            FeedWakeFrame(engine, &frame, codec.CaptureGeneration(), 0);
            assert(engine.wake_word_audio_cache_.stores == 1);
        } else {
            FeedMonoFrame(*engine.custom_wake_word_, samples, 3, codec.CaptureGeneration(), 0);
            engine.wake_word_audio_cache_.Store(samples, 4);
            engine.ResetWakeWordCapture();
            FeedMonoFrame(*engine.custom_wake_word_, samples, 1, codec.CaptureGeneration(), 0);
            assert(detects == 0 && cleans == 1);
            assert(engine.wake_word_audio_cache_.samples == 0 && engine.wake_cache_generation_ == codec.generation);
        }
    }
    return 0;
}
