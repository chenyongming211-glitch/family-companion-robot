#include <cassert>
#include <cstdint>
#include <cstdio>
#include <mutex>
#include <functional>
#include <future>
constexpr int ESP_ERR_INVALID_ARG=-1;
constexpr int ESP_OK=0,ESP_CODEC_DEV_OK=0,ESP_CODEC_DEV_INVALID_ARG=-1,ESP_CODEC_DEV_WRONG_STATE=-2,
 ESP_CODEC_DEV_NOT_SUPPORT=-3,ESP_CODEC_DEV_DRV_ERR=-4,ESP_CODEC_DEV_WRITE_FAIL=-5;
constexpr int ESP_CODEC_DEV_TYPE_IN=1,ESP_CODEC_DEV_TYPE_OUT=2,ESP_CODEC_DEV_WORK_MODE_DAC=2,
 ESP_CODEC_DEV_WORK_MODE_BOTH=3,CODEC_REF_STAGE_ENABLE=1,ES_PA_ENABLE=1,ES_PA_DISABLE=2,ES_PA_SETUP=4,
 AUDIO_GPIO_DIR_OUT=1,AUDIO_GPIO_MODE_FLOAT=0,ES8311_DAC_REG31=0x31,VOL_TRANSITION_TIME=0;
using gpio_num_t=int;using es_pa_setting_t=int;
constexpr int GPIO_NUM_NC=-1,GPIO_MODE_OUTPUT=1;
#define ESP_CODEC_DEV_MAKE_CHANNEL_MASK(n) (1u<<(n))
#define ESP_LOGI(...) ((void)0)
#define ESP_LOGD(...) ((void)0)
#define ESP_LOGE(...) ((void)0)
static bool held=false,held_level=false,pending_level=false,write_error=false,read_error=false,exposed=false;
static int gpio_error=0;static uint8_t dac_register=0;
static bool pad(){return held?held_level:pending_level;}
static void observe(){if(pad() && !(dac_register&0x60))exposed=true;}
static int gpio_set_level(int,uint32_t v){if(gpio_error==1)return -31;pending_level=v;observe();return 0;}
static int gpio_set_direction(int,int){return gpio_error==2?-32:0;}
static int gpio_hold_en(int){if(gpio_error==3)return -33;if(!held)held_level=pad();held=true;return 0;}
static int gpio_hold_dis(int){if(gpio_error==4)return -34;held=false;observe();return 0;}
struct audio_codec_ctrl_info_t{int unused;};
struct audio_codec_ctrl_if_t {
 int(*read_reg)(const audio_codec_ctrl_if_t*,int,int,void*,int);
 int(*get_info)(const audio_codec_ctrl_if_t*,audio_codec_ctrl_info_t*);
};
static int read_register(const audio_codec_ctrl_if_t*,int reg,int,void* out,int){assert(reg==0x31);if(read_error)return -41;*static_cast<uint8_t*>(out)=dac_register;return 0;}
static int get_info(const audio_codec_ctrl_if_t*,audio_codec_ctrl_info_t*){return 0;}
struct audio_codec_gpio_if_t{int(*setup)(int,int,int);int(*set)(int,bool);};
static int setup_gpio(int,int,int){return 0;}
static int set_gpio(int pin,bool high){return gpio_set_level(pin,high);}
static audio_codec_ctrl_if_t ctrl{read_register,get_info};
static audio_codec_gpio_if_t gpio{setup_gpio,set_gpio};
struct esp_codec_dev_sample_info_t{int bits_per_sample=0,channel=0;unsigned channel_mask=0,sample_rate=0,mclk_multiple=0;};
struct audio_codec_if_t{
 int(*mute)(const audio_codec_if_t*,bool)=nullptr;
 int(*enable)(const audio_codec_if_t*,bool)=nullptr;
 int(*set_fs)(const audio_codec_if_t*,esp_codec_dev_sample_info_t*)=nullptr;
 int(*set_vol)(const audio_codec_if_t*,float)=nullptr;
};
struct audio_codec_es8311_t {
 audio_codec_if_t base;bool is_open=true,enabled=true;
 struct {int16_t pa_pin=46;audio_codec_gpio_if_t* gpio_if=&gpio;int codec_mode=2;bool pa_reverted=false;audio_codec_ctrl_if_t* ctrl_if=&ctrl;}cfg;
};
static int es8311_read_reg(audio_codec_es8311_t*,int,int* out){*out=dac_register;return read_error?-41:0;}
static int es8311_write_reg(audio_codec_es8311_t*,int,int val){if(write_error)return -42;dac_register=val;observe();return 0;}
static int es8311_start(audio_codec_es8311_t*){return 0;}
static int es8311_suspend(audio_codec_es8311_t*){return 0;}
static int codec_ref_acquire(audio_codec_ctrl_info_t*,int){return 1;}
static int codec_ref_release(audio_codec_ctrl_info_t*,int){return 0;}
// ACTUAL_ES8311_METHODS
struct audio_codec_data_if_t{
 int(*set_fmt)(const audio_codec_data_if_t*,int,esp_codec_dev_sample_info_t*)=nullptr;
 int(*enable)(const audio_codec_data_if_t*,int,bool)=nullptr;
};
struct SoftwareVolume{
 void(*set_vol)(SoftwareVolume*,float)=nullptr;
 void(*open)(SoftwareVolume*,esp_codec_dev_sample_info_t*,int)=nullptr;
 void(*close)(SoftwareVolume*)=nullptr;
};
struct codec_dev_t{
 bool input_opened=false,output_opened=false,disable_when_closed=true;void* mirror=nullptr;int dev_caps=0;audio_codec_if_t* codec_if=nullptr;
 audio_codec_data_if_t* data_if=nullptr;int volume=70;bool muted=false;int mic_gain=30;bool mic_muted=false;
 SoftwareVolume* sw_vol=nullptr;bool sw_vol_alloced=false;int vol_curve=0;
};
using esp_codec_dev_handle_t=codec_dev_t*;
static int _verify_codec_setting(codec_dev_t*,bool){return 0;}
static bool _verify_drv_ready(codec_dev_t*,bool){return true;}
static float _get_vol_db(int*,int){return 0;}
static SoftwareVolume* audio_codec_new_sw_vol(){assert(false);return nullptr;}
static int esp_codec_dev_set_out_vol(codec_dev_t*,int){return 0;}
static int esp_codec_dev_set_in_gain(codec_dev_t*,int){return 0;}
static int esp_codec_dev_set_in_mute(codec_dev_t*,bool){return 0;}
static void codec_dev_mirror_deinit(void*){}
// ACTUAL_DEVICE_METHODS
static int esp_codec_dev_set_in_channel_gain(codec_dev_t*,unsigned,float){return 0;}
struct BoxAudioCodec {
 std::mutex data_if_mutex_;codec_dev_t* output_dev_;codec_dev_t* input_dev_;
 audio_codec_ctrl_if_t* out_ctrl_if_=&ctrl;
 bool input_enabled_=true,output_enabled_=true,input_reference_=true;
 int reference_gain_channel_=-1,output_volume_=70;float input_gain_=30,reference_gain_=0;
 gpio_num_t pa_pin_=46;int output_inhibit_error_=0;
 bool LockDuplex(std::unique_lock<std::mutex>&,bool);bool CloseDuplexForFirmware();bool ReopenDuplexForMusic(int,bool=false);bool MuteMusicOutput(bool,bool=false);
 // Optional additions are defined by actual production methods after RED.
 bool InitializeOutputInhibit(gpio_num_t);bool SetOutputInhibit(bool);
};
static int64_t clock_us=1000;
static int64_t esp_timer_get_time(){return clock_us;}
static void vTaskDelay(int){clock_us+=1000;}
// PRODUCTION_BOX_METHODS
int main(){
 audio_codec_es8311_t es;es.base.mute=es8311_set_mute;es.base.enable=es8311_enable;
 es.base.set_vol=[](const audio_codec_if_t*,float){return 0;};
 audio_codec_data_if_t data;
 codec_dev_t out;out.output_opened=true;out.dev_caps=2;out.codec_if=&es.base;out.data_if=&data;
 codec_dev_t in;in.input_opened=true;in.dev_caps=1;in.data_if=&data;
 BoxAudioCodec box;box.output_dev_=&out;box.input_dev_=&in;
 pending_level=true;dac_register=0;
 assert(box.MuteMusicOutput(true));exposed=false;
 assert(box.ReopenDuplexForMusic(24000));
 assert(!exposed&&"actual SDK open temporarily unmutes and enables PA during recovery");
 assert(held&&!pad()&&"PA inhibition must survive actual SDK close/open");
 assert(box.CloseDuplexForFirmware()&&held&&!pad()&&!box.input_enabled_&&!box.output_enabled_);
 assert(box.ReopenDuplexForMusic(24000,true));
 assert(box.MuteMusicOutput(false,true)&&!held&&pad());
 // The real two-layer mute API hides register writes that fail. Guard readback must not.
 write_error=true;exposed=false;
 assert(!box.MuteMusicOutput(true)&&held&&!pad());write_error=false;
 assert(box.MuteMusicOutput(true));write_error=true;
 assert(!box.MuteMusicOutput(false)&&held&&!pad());write_error=false;
 read_error=true;assert(!box.MuteMusicOutput(false)&&held&&!pad());read_error=false;
 for(int fail=1;fail<=4;++fail){
  gpio_error=0;assert(box.MuteMusicOutput(true));gpio_error=fail;
  if(fail==2)continue; // direction failure belongs to startup normalization
  assert(!box.MuteMusicOutput(fail!=4));
 }
 gpio_error=0;
 held=true;held_level=false;pending_level=true;exposed=false;
 assert(box.InitializeOutputInhibit(46)&&!held&&!pad()&&!exposed);
 held=true;held_level=false;pending_level=true;gpio_error=2;
 assert(!box.InitializeOutputInhibit(46)&&held&&!pad()&&box.output_inhibit_error_==-32);
 gpio_error=0;assert(box.InitializeOutputInhibit(46)&&!held&&!pad());
 std::promise<void> entered,release;auto wait=release.get_future();
 auto held_lock=std::async(std::launch::async,[&]{std::lock_guard<std::mutex> lock(box.data_if_mutex_);entered.set_value();wait.wait();});
 entered.get_future().wait();const auto began=clock_us;
 assert(!box.MuteMusicOutput(true,true)&&clock_us-began==200000);
 assert(!box.CloseDuplexForFirmware());assert(!box.ReopenDuplexForMusic(24000,true));
 release.set_value();held_lock.get();
 puts("Actual pinned SDK PA/unmute sequence, checked close and bounded ownership remain inhibited");
}
