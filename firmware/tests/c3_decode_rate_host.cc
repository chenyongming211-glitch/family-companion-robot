// SDK plumbing only. The tested method is extracted from the prepared product.
#include <cassert>
#include <cstddef>
#include <cstdint>
#include <cstring>
#include <mutex>

struct esp_opus_dec_cfg_t {int rate;int duration;};
struct esp_ae_rate_cvt_cfg_t {int input;int output;int channels;};
#define OPUS_DEC_CFG(rate,duration) esp_opus_dec_cfg_t{rate,duration}
#define RATE_CVT_CFG(input,output,channels) esp_ae_rate_cvt_cfg_t{input,output,channels}
constexpr int ESP_AUDIO_MONO=1;
template<class... Args>static void TestLog(const Args&...) {}
#define TAG "C3DecodeRateTest"
#define ESP_LOGE(...) TestLog(__VA_ARGS__)
#define ESP_LOGI(...) TestLog(__VA_ARGS__)

static int decoder_opens=0,decoder_closes=0,resampler_opens=0,resampler_closes=0;
static int opened_rate=0,opened_duration=0,src_input=0,src_output=0;
static bool forbid_replacement=false;
static int decoder_tag=0,resampler_tag=0;
static int esp_opus_dec_close(void*) {
    assert(!forbid_replacement && "C3 local cue must reuse its physical-rate decoder");
    ++decoder_closes;return 0;
}
static int esp_opus_dec_open(const esp_opus_dec_cfg_t* cfg,size_t,void** decoder) {
    assert(!forbid_replacement && "C3 local cue must not allocate another decoder");
    ++decoder_opens;opened_rate=cfg->rate;opened_duration=cfg->duration;
    *decoder=&decoder_tag;return 0;
}
static int esp_ae_rate_cvt_close(void*) {++resampler_closes;return 0;}
static int esp_ae_rate_cvt_open(const esp_ae_rate_cvt_cfg_t* cfg,void** converter) {
    assert(!forbid_replacement && "C3 native Opus output must not allocate an output SRC");
    ++resampler_opens;src_input=cfg->input;src_output=cfg->output;
    *converter=&resampler_tag;return 0;
}
struct Codec {int output_sample_rate()const{return 24000;}};
class Board {
public:
    static Board& GetInstance(){static Board board;return board;}
    Codec* GetAudioCodec(){return &codec_;}
private:Codec codec_;
};
class AudioService {
public:
    Codec *codec_=Board::GetInstance().GetAudioCodec();
    int decoder_sample_rate_=24000,decoder_duration_ms_=60,decoder_frame_size_=1440;
    void* opus_decoder_=&decoder_tag;
    void* output_resampler_=nullptr;
    std::mutex decoder_mutex_;
    void SetDecodeSampleRate(int,int);
};

// PRODUCTION_METHOD

int main(int argc,char** argv) {
    assert(argc==2);AudioService audio;
    if(std::strcmp(argv[1],"local-cue")==0) {
        forbid_replacement=CONFIG_XMINI_C3_VOICE_BARGE;
        audio.SetDecodeSampleRate(16000,60);
#if CONFIG_XMINI_C3_VOICE_BARGE
        assert(audio.decoder_sample_rate_==24000 && audio.decoder_frame_size_==1440);
        assert(decoder_opens==0 && decoder_closes==0 && resampler_opens==0);
#else
        assert(audio.decoder_sample_rate_==16000 && audio.decoder_frame_size_==960);
        assert(decoder_opens==1 && decoder_closes==1 && resampler_opens==1);
        assert(src_input==16000 && src_output==24000);
#endif
    } else if(std::strcmp(argv[1],"frame-duration")==0) {
        audio.SetDecodeSampleRate(16000,20);
        assert(decoder_opens==1 && decoder_closes==1 && opened_duration==20);
#if CONFIG_XMINI_C3_VOICE_BARGE
        assert(opened_rate==24000 && audio.decoder_frame_size_==480);
        assert(resampler_opens==0);
        audio.SetDecodeSampleRate(48000,20);
        assert(decoder_opens==1 && decoder_closes==1);
        audio.SetDecodeSampleRate(8000,20);
        assert(decoder_opens==1 && decoder_closes==1);
#else
        assert(opened_rate==16000 && audio.decoder_frame_size_==320);
        assert(resampler_opens==1 && src_input==16000 && src_output==24000);
        audio.SetDecodeSampleRate(48000,20);
        assert(decoder_opens==2 && decoder_closes==2 && opened_rate==48000);
        assert(audio.decoder_frame_size_==960 && resampler_opens==2 && resampler_closes==1);
#endif
        audio.SetDecodeSampleRate(24000,60);
        assert(opened_rate==24000 && opened_duration==60);
        assert(audio.decoder_frame_size_==1440);
    } else if(std::strcmp(argv[1],"server-downlink")==0) {
        forbid_replacement=true;
        audio.SetDecodeSampleRate(24000,60);
        assert(audio.decoder_sample_rate_==24000 && audio.decoder_frame_size_==1440);
        assert(decoder_opens==0 && decoder_closes==0 && resampler_opens==0);
    } else assert(false && "unknown case");
}
