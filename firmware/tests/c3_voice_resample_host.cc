#include "c3_echo_stream.h"
#include <atomic>
#include <cassert>
#include <chrono>
#include <cstdint>
#include <mutex>
#include <vector>
using esp_ae_sample_t=int16_t*;
static int capacity_result=0,conversion_result=0,resets=0;
static uint32_t capacity=160,actual=160;
static bool change_generation=false;
static int esp_ae_rate_cvt_reset(void*){++resets;return 0;}
static int esp_ae_rate_cvt_get_max_out_sample_num(void*,uint32_t in,uint32_t* out){assert(in==240);*out=capacity;return capacity_result;}
static int esp_ae_rate_cvt_process(void*,esp_ae_sample_t input,uint32_t in,esp_ae_sample_t out,uint32_t* count){
    assert(in==240&&input[0]==100&&input[1]==10);
    for(uint32_t i=0;i<capacity;++i){out[i*2]=100;out[i*2+1]=10;}
    *count=actual;return conversion_result;
}
static int esp_timer_stop(int){return 0;}
static int esp_timer_start_periodic(int,int){return 0;}
constexpr int AUDIO_POWER_CHECK_INTERVAL_MS=1000;
struct Codec {
    bool fault=false,valid=true;uint32_t generation=7;
    int input_sample_rate()const{return 24000;}int input_channels()const{return 2;}
    bool input_enabled()const{return true;}void EnableInput(bool){}
    bool InputData(std::vector<int16_t>& data){for(size_t i=0;i<data.size();i+=2){data[i]=100;data[i+1]=10;}if(change_generation)++generation;return true;}
    bool ManagedCapture()const{return false;}bool SoftwareReference()const{return false;}
    uint32_t CaptureGeneration()const{return generation;}bool ReferenceFaulted()const{return fault;}bool ReferenceValid()const{return valid;}
    void ReportReferenceFailure(){fault=true;}void ResetDiagnosticStream(int){}void RecordDiagnostic(int,const int16_t*,size_t,int,uint32_t){}
};
struct Engine {void ResetCapture(){}};
class AudioService {public:
    Codec codec,*codec_=&codec;Engine engine,*audio_engine_=&engine;
    std::atomic<uint32_t> capture_generation_{0};bool capture_faulted_=false;
    std::mutex input_resampler_mutex_;void* input_resampler_=this;
    int audio_power_timer_=0;std::chrono::steady_clock::time_point last_input_time_;
    struct {int input_count=0;}debug_statistics_;
    void UpdateExperimentGate(){}bool ReadAudioData(std::vector<int16_t>&,int,int);
};
// PRODUCTION_METHOD
int main(){
    AudioService a;std::vector<int16_t> data;
    assert(a.ReadAudioData(data,16000,160)&&data.size()==320&&resets==1);
    assert(data[0]==100&&data[1]==10);
    assert(a.ReadAudioData(data,16000,160)&&resets==1);
    ++a.codec.generation;assert(a.ReadAudioData(data,16000,160)&&resets==2);
    change_generation=true;assert(!a.ReadAudioData(data,16000,160)&&!a.codec.fault);change_generation=false;
    conversion_result=1;assert(!a.ReadAudioData(data,16000,160)&&a.codec.fault);conversion_result=0;a.codec.fault=false;
    actual=161;assert(!a.ReadAudioData(data,16000,160)&&a.codec.fault);actual=160;a.codec.fault=false;
    capacity=1025;assert(!a.ReadAudioData(data,16000,160)&&a.codec.fault);capacity=160;a.codec.fault=false;
    capacity_result=1;assert(!a.ReadAudioData(data,16000,160)&&a.codec.fault);capacity_result=0;a.codec.fault=false;
    a.input_resampler_=nullptr;assert(!a.ReadAudioData(data,16000,160)&&a.codec.fault);
}
