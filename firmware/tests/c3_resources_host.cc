// SDK fakes only; the resource method below comes from the prepared product.
#include <cassert>
#include <atomic>
#include <cstddef>
#include <cstdint>
#include <cstdio>
#include <cstring>

constexpr unsigned MALLOC_CAP_INTERNAL=1, MALLOC_CAP_8BIT=2;
static unsigned heap_calls=0, task_calls=0, log_calls=0;
static unsigned scheduler_depth=0,suspends=0,resumes=0;
static char message[256]{};
static int input_tag=0, opus_tag=0, output_tag=0;
using TaskHandle_t=void*;
class AudioService;
static AudioService* restarting_audio=nullptr;
static unsigned creation_gap_checks=0,created_tasks=0;
static void ObserveCreationGap();
[[maybe_unused]] static size_t heap_caps_get_free_size(unsigned caps) {
    assert(scheduler_depth==0 && "heap reads must happen after resuming scheduling");
    assert(caps==(MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT));++heap_calls;return 42000;
}
[[maybe_unused]] static size_t heap_caps_get_largest_free_block(unsigned caps) {
    assert(scheduler_depth==0);
    assert(caps==(MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT));++heap_calls;return 17000;
}
[[maybe_unused]] static size_t heap_caps_get_minimum_free_size(unsigned caps) {
    assert(scheduler_depth==0);
    assert(caps==(MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT));++heap_calls;return 13000;
}
[[maybe_unused]] static unsigned uxTaskGetStackHighWaterMark(TaskHandle_t task) {
    assert(scheduler_depth==1 && "worker lifetime check and query must be serialized");
    assert(task!=nullptr && "NULL would measure the calling main task");++task_calls;
    if(task==&input_tag)return 2100;
    assert(task==&opus_tag);return 3400;
}
template<class... Args> void log_resources(const char* tag,const char* format,Args... args) {
    assert(scheduler_depth==0 && "logging must happen after resuming scheduling");
    assert(std::strcmp(tag,"AudioService")==0);++log_calls;
    assert(std::snprintf(message,sizeof(message),format,args...)>0);
}
#define TAG "AudioService"
#define ESP_LOGI(...) log_resources(__VA_ARGS__)
[[maybe_unused]] static void vTaskSuspendAll() {
    assert(scheduler_depth==0);++scheduler_depth;++suspends;
}
[[maybe_unused]] static int xTaskResumeAll() {
    assert(scheduler_depth==1);--scheduler_depth;++resumes;return 0;
}
constexpr unsigned AS_EVENT_AUDIO_TESTING_RUNNING=1,AS_EVENT_WAKE_WORD_RUNNING=2,
    AS_EVENT_AUDIO_PROCESSOR_RUNNING=4,AS_EVENT_AUDIO_INPUT_STOP_REQUEST=8;
static unsigned xEventGroupClearBits(void*,unsigned) {
    if(restarting_audio)ObserveCreationGap();
    return 0;
}
static void esp_timer_start_periodic(int,int) {}
static void vTaskDelete(void*) {}
static int xTaskCreate(void(*)(void*),const char* name,unsigned,void*,unsigned,TaskHandle_t* result) {
    if(restarting_audio)ObserveCreationGap();
    ++created_tasks;
    if(std::strcmp(name,"audio_input")==0)*result=&input_tag;
    else if(std::strcmp(name,"opus_codec")==0)*result=&opus_tag;
    else {assert(std::strcmp(name,"audio_output")==0);*result=&output_tag;}
    return 1;
}
class AudioService {
public:
    std::atomic<bool> service_stopped_{false};
    TaskHandle_t audio_input_task_handle_=&input_tag;
    TaskHandle_t opus_codec_task_handle_=&opus_tag;
    TaskHandle_t audio_output_task_handle_=nullptr;
    void* event_group_=nullptr;
    int audio_power_timer_=0;
    void Start();
    void AudioInputTask() {}
    void AudioOutputTask() {}
    void OpusCodecTask() {}
    void PrintC3Resources() const;
};

// PRODUCTION_METHOD

static void ObserveCreationGap() {
    assert(restarting_audio && !restarting_audio->service_stopped_.load());
    assert(restarting_audio->audio_input_task_handle_==nullptr ||
           restarting_audio->audio_input_task_handle_==&input_tag);
    assert(restarting_audio->opus_codec_task_handle_==nullptr);
    ++creation_gap_checks;
    restarting_audio->PrintC3Resources();
    if(creation_gap_checks<=2)
        assert(std::strcmp(message,"C3 resources free=42000 largest=17000 min=13000 input_stack_min=0 opus_stack_min=0")==0);
    else
        assert(std::strcmp(message,"C3 resources free=42000 largest=17000 min=13000 input_stack_min=2100 opus_stack_min=0")==0);
}

int main(int argc,char** argv) {
    assert(argc==2);AudioService audio;
    if(std::strcmp(argv[1],"restart")==0) {
        audio.service_stopped_.store(true);
        audio.audio_input_task_handle_=reinterpret_cast<void*>(uintptr_t{0xdeadbeef});
        audio.opus_codec_task_handle_=reinterpret_cast<void*>(uintptr_t{0xfeedbeef});
        restarting_audio=&audio;
        audio.Start();
        restarting_audio=nullptr;
        assert(creation_gap_checks==4 && created_tasks==3);
        assert(audio.audio_input_task_handle_==&input_tag && audio.opus_codec_task_handle_==&opus_tag);
        assert(!audio.service_stopped_.load());
        audio.PrintC3Resources();
        assert(std::strcmp(message,"C3 resources free=42000 largest=17000 min=13000 input_stack_min=2100 opus_stack_min=3400")==0);
        assert(task_calls==4 && suspends==5 && resumes==5 && scheduler_depth==0);
        assert(heap_calls==15 && log_calls==5);
        return 0;
    }
    if(std::strcmp(argv[1],"not-started")==0) {
        audio.audio_input_task_handle_=nullptr;audio.opus_codec_task_handle_=nullptr;
    } else if(std::strcmp(argv[1],"stopped")==0) {
        audio.service_stopped_.store(true);
        audio.audio_input_task_handle_=reinterpret_cast<void*>(uintptr_t{0xdeadbeef});
        audio.opus_codec_task_handle_=reinterpret_cast<void*>(uintptr_t{0xfeedbeef});
    } else assert(std::strcmp(argv[1],"started")==0);
    audio.PrintC3Resources();
#if CONFIG_XMINI_C3_VOICE_BARGE
    assert(heap_calls==3 && log_calls==1);
    assert(suspends==1 && resumes==1 && scheduler_depth==0);
    if(audio.audio_input_task_handle_ && !audio.service_stopped_.load()) {
        assert(task_calls==2);
        assert(std::strcmp(message,"C3 resources free=42000 largest=17000 min=13000 input_stack_min=2100 opus_stack_min=3400")==0);
    } else {
        assert(task_calls==0);
        assert(std::strcmp(message,"C3 resources free=42000 largest=17000 min=13000 input_stack_min=0 opus_stack_min=0")==0);
    }
#else
    assert(heap_calls==0 && task_calls==0 && log_calls==0);
    assert(suspends==0 && resumes==0 && scheduler_depth==0);
#endif
}
