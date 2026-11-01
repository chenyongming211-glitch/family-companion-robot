#include "c3_lite_aec.h"
#include "c3_startup_diagnostics.h"
#include <cassert>
#include <mutex>
#include <vector>
using srmodel_list_t=int;
struct AudioCodec {bool fault=false;void ReportReferenceFailure(){fault=true;}};
template<class... Args>static void Log(Args&&... args){((void)args,...);}
#define ESP_LOGE(...) Log(__VA_ARGS__)
#define ESP_LOGI(...) Log(__VA_ARGS__)
#define TAG "LiteAudioEngine"
class LiteAudioEngine {public:
 AudioCodec* codec_=nullptr;srmodel_list_t* models_list_=nullptr;int frame_samples_=0;
 std::vector<int16_t> output_buffer_;family_robot::C3LiteAec* c3_aec_=nullptr;
 family_robot::C3StartupStep c3_aec_startup_=family_robot::C3StartupStep::NotEntered;
 bool should_have_wake_word_=true;void* wake_word_=nullptr;std::mutex wake_word_mutex_;
 bool CreateWakeWordLocked(){return true;}
 bool Initialize(AudioCodec*,int,srmodel_list_t*);
 ~LiteAudioEngine(){family_robot::C3LiteAec::Destroy(c3_aec_);}
};
// PRODUCTION_LITE_INITIALIZE_METHOD
int main(){
 using Step=family_robot::C3StartupStep;AudioCodec codec;
 {LiteAudioEngine engine;assert(engine.Initialize(&codec,60,nullptr));assert(engine.c3_aec_startup_==Step::Succeeded&&engine.c3_aec_);
  auto created=aec_created;assert(engine.Initialize(&codec,60,nullptr));assert(aec_created==created&&engine.c3_aec_startup_==Step::Succeeded&&engine.output_buffer_.capacity()==0);}
 {LiteAudioEngine engine;heap_fail=true;assert(!engine.Initialize(&codec,60,nullptr));heap_fail=false;
  assert(engine.c3_aec_startup_==Step::Failed&&!engine.c3_aec_&&codec.fault);
  assert(engine.Initialize(&codec,60,nullptr));assert(engine.c3_aec_startup_==Step::Failed&&engine.c3_aec_);}
 {LiteAudioEngine engine;aec_chunks=256;assert(!engine.Initialize(&codec,60,nullptr));aec_chunks=512;assert(engine.c3_aec_startup_==Step::Failed);}
}
