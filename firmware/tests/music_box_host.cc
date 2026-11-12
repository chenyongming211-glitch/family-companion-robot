#include <cassert>
#include <atomic>
#include <chrono>
#include <condition_variable>
#include <future>
#include <mutex>
#include <stdexcept>
constexpr int ESP_CODEC_DEV_OK=0;
#define ESP_CODEC_DEV_MAKE_CHANNEL_MASK(n) (1u<<(n))
#define ESP_ERROR_CHECK(x) do{if((x)!=0)throw std::runtime_error("codec closed");}while(0)
struct esp_codec_dev_sample_info_t{int bits_per_sample=0,channel=0;unsigned channel_mask=0,sample_rate=0,mclk_multiple=0;};
struct Device{bool open=true;unsigned rate=24000;};
std::mutex barrier;std::condition_variable cv;bool closed=false,proceed=false;
Device output,input;
int esp_codec_dev_close(Device* d){
 d->open=false;
 if(d==&output){std::unique_lock<std::mutex> lock(barrier);closed=true;cv.notify_all();cv.wait(lock,[]{return proceed;});}
 return 0;
}
int esp_codec_dev_open(Device* d,esp_codec_dev_sample_info_t* fs){d->open=true;d->rate=fs->sample_rate;return 0;}
int esp_codec_dev_set_in_channel_gain(Device*,unsigned,float){return 0;}
int esp_codec_dev_set_out_mute(Device* d,bool){return d->open?0:-1;}
int esp_codec_dev_set_out_vol(Device* d,int){return d->open?0:-1;}
struct AudioCodec{int output_volume_=70;void SetOutputVolume(int n){output_volume_=n;}};
struct BoxAudioCodec:AudioCodec{
 std::mutex data_if_mutex_;Device* output_dev_=&output;Device* input_dev_=&input;
 bool input_enabled_=true,output_enabled_=true,input_reference_=true;
 int input_sample_rate_=24000,output_sample_rate_=24000,reference_gain_channel_=-1;
 float input_gain_=30,reference_gain_=0;
 bool ReopenDuplexForMusic(int);void SetOutputVolume(int);
};
// PRODUCTION_BOX_METHODS
int main(){
 using namespace std::chrono_literals;
 BoxAudioCodec codec;
 auto profile=std::async(std::launch::async,[&]{return codec.ReopenDuplexForMusic(48000);});
 {std::unique_lock<std::mutex> lock(barrier);cv.wait(lock,[]{return closed;});}
 auto volume=std::async(std::launch::async,[&]{try{codec.SetOutputVolume(80);return true;}catch(...){return false;}});
 const bool serialized=volume.wait_for(30ms)==std::future_status::timeout;
 {std::lock_guard<std::mutex> lock(barrier);proceed=true;}cv.notify_all();
 assert(profile.get());const bool volume_ok=volume.get();
 assert(serialized&&volume_ok&&codec.output_volume_==80);
 assert(output.rate==48000&&input.rate==48000);
 // Public rates are stable voice configuration; only the exclusive hardware
 // profile changes, avoiding concurrent writes to voice-worker rate getters.
 assert(codec.input_sample_rate_==24000&&codec.output_sample_rate_==24000);
}
