#define main voice_regression_main
#include "waveshare_codec_host.cc"
#undef main
#include <atomic>
using esp_ae_sample_t=int16_t*;
static int esp_ae_rate_cvt_reset(void*){return 0;}
static int esp_ae_rate_cvt_get_max_out_sample_num(void*,uint32_t in,uint32_t* out){*out=in;return 0;}
static int esp_ae_rate_cvt_process(void*,esp_ae_sample_t,uint32_t,esp_ae_sample_t,uint32_t*){return 0;}
static int esp_timer_stop(int){return 0;}
static int esp_timer_start_periodic(int,int){return 0;}
constexpr int AUDIO_POWER_CHECK_INTERVAL_MS=1000;
struct Engine {void ResetCapture(){}};
class AudioService {public:
    XiaomanBoxCodec codec=make_codec();XiaomanBoxCodec* codec_=&codec;
    Engine engine,*audio_engine_=&engine;
    std::atomic<uint32_t> capture_generation_{0};bool capture_faulted_=false;
    std::mutex input_resampler_mutex_;void* input_resampler_=this;
    int audio_power_timer_=0,expected_saves=0;std::chrono::steady_clock::time_point last_input_time_;
    struct {int input_count=0;}debug_statistics_;
    void UpdateExperimentGate(){assert(saves==expected_saves&&"first fault must be persisted before capture gate");}
    bool ReadAudioData(std::vector<int16_t>&,int,int);
};
// PRODUCTION_METHOD
int main() {
    AudioService a;a.codec.Start();a.expected_saves=saves;
    std::vector<int16_t> input;
    assert(a.ReadAudioData(input,24000,240));
    int16_t dma[6][240]{};tick(a.codec,dma[0]);tick(a.codec,dma[0]);
    ++a.expected_saves;
    assert(!a.ReadAudioData(input,24000,240));
    assert(!a.ReadAudioData(input,24000,240));
    assert(saved_fault.source==3&&saved_fault.detail==5&&a.codec.muted);
    puts("Actual ReadAudioData flushes first fault before gate and never repeats NVS save");
}
