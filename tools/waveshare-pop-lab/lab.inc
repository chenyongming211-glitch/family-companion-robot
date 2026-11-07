#include "wave_pop_runtime.h"
#include "wave_pop_fixture.h"
#include "boards/waveshare/esp32-s3-touch-amoled-2.16/xiaoman_box_codec.h"
#include "backlight.h"
#include <esp_heap_caps.h>
#include <cmath>
#include <cstdio>

namespace wave_pop_lab {
inline void Dump(int phase) {
    capturing.store(false,std::memory_order_release);
    while(input_busy.load(std::memory_order_acquire))vTaskDelay(1);
    const auto count=captured.load(std::memory_order_acquire);
    printf("WPL_CAPTURE %d %u 24000 2\n",phase,unsigned(count));
    const auto* bytes=reinterpret_cast<const unsigned char*>(capture);
    constexpr char hex[]="0123456789abcdef";
    char line[769];
    for(size_t at=0;at<size_t(count)*2;at+=384) {
        const auto n=std::min(size_t(384),size_t(count)*2-at);
        for(size_t i=0;i<n;++i){line[i*2]=hex[bytes[at+i]>>4];line[i*2+1]=hex[bytes[at+i]&15];}
        line[n*2]=0;
        printf("WPL_DATA %d %u %s\n",phase,unsigned(at),line);
        if(at%6144==0)vTaskDelay(1);
    }
    printf("WPL_CAPTURE_END %d %u\n",phase,unsigned(count)*2);
    for(uint32_t i=0;i<time_count.load();++i)printf("WPL_INPUT_TIME %d %u %u\n",phase,unsigned(capture_times[i*2]),unsigned(capture_times[i*2+1]));
    fflush(stdout);
}

inline void Start(Application* app,AudioService* service) {
    struct Context {Application* app;AudioService* service;};
    static Context context{app,service};
    xTaskCreate([](void* ptr) {
        const auto c=*static_cast<Context*>(ptr);
        auto& board=Board::GetInstance();
        auto* codec=static_cast<XiaomanBoxCodec*>(board.GetAudioCodec());
        auto* light=board.GetBacklight();
        const auto began=esp_timer_get_time();
        while(c.app->GetDeviceState()!=kDeviceStateIdle && esp_timer_get_time()-began<90000000)vTaskDelay(pdMS_TO_TICKS(100));
        if(c.app->GetDeviceState()!=kDeviceStateIdle){printf("WPL_ABORT not_idle\n");vTaskDelete(nullptr);return;}
        capture=static_cast<int16_t*>(heap_caps_malloc(kCaptureSamples*2,MALLOC_CAP_SPIRAM|MALLOC_CAP_8BIT));
        capture_times=static_cast<uint32_t*>(heap_caps_malloc(kTimeEntries*2*sizeof(uint32_t),MALLOC_CAP_SPIRAM|MALLOC_CAP_8BIT));
        if(!capture || !capture_times){printf("WPL_ABORT capture_allocation\n");heap_caps_free(capture);heap_caps_free(capture_times);vTaskDelete(nullptr);return;}
        board.SetPowerSaveLevel(PowerSaveLevel::PERFORMANCE);
        const auto original_light=light?light->brightness():0;
        const auto task=c.service->WavePopCodecTask();
        const auto original_priority=uxTaskPriorityGet(task);
        printf("WPL_BEGIN volume=%d brightness=%u codec_priority=%u capture_capacity=%u\n",codec->output_volume(),unsigned(original_light),unsigned(original_priority),unsigned(kCaptureSamples));
        c.app->Schedule([app=c.app](){app->SetDeviceState(kDeviceStateSpeaking);});
        vTaskDelay(pdMS_TO_TICKS(600));
        c.service->EnableWakeWordDetection(false);
        for(int phase=0;phase<8;++phase) {
            const bool direct=phase==0 || phase==7, screen_off=phase==3, high=phase==2;
            const bool paced=phase==4, network=phase==5 || phase==6;
            if(network && !kFixtureUrl[0]){printf("WPL_SKIP %d no_fixture_url\n",phase);continue;}
            if(light)light->SetBrightness(screen_off?0:(original_light?original_light:30),false);
            vTaskPrioritySet(task,high?6:original_priority);
            vTaskDelay(pdMS_TO_TICKS(600));
            c.service->ResetDecoder();
            const family_robot::PlaybackIdentity id{0x504f5001u,uint32_t(phase+1)};
            bool ok=codec->PlaybackControl(AudioCodec::PlaybackOp::Connect,{id.connection,0}) && codec->PlaybackControl(AudioCodec::PlaybackOp::Start,id);
            std::unique_ptr<Http> http;
            if(network) {
                http=board.GetNetwork()->CreateHttp(4);
                const std::string url=std::string(kFixtureUrl)+(phase==5?"/burst":"/paced");
                if(!http->Open("GET",url))ok=false;
                auto status=http->GetStatusCode();if(!status || *status!=200)ok=false;
            }
            active.store(false);capturing.store(false);
            while(input_busy.load())vTaskDelay(1);
            captured.store(0);time_count.store(0);max_decode_us.store(0);max_wait_us.store(0);
            codec->WavePopProbe(true);
            printf("WPL_PHASE %d direct=%d screen_off=%d priority=%u paced=%d network=%d\n",phase,direct,screen_off,unsigned(uxTaskPriorityGet(task)),paced,network);fflush(stdout);
            const auto start=esp_timer_get_time();
            uint32_t max_source_gap=0;int64_t last_source=0;
            for(int f=0;ok && f<kFrames;++f) {
                if(paced)while(esp_timer_get_time()-start<int64_t(f)*60000)vTaskDelay(1);
                if(f==8){active.store(true);capturing.store(true,std::memory_order_release);}
                if(f==kFrames-4){active.store(false);capturing.store(false);}
                std::vector<uint8_t> payload;
                if(network) {
                    auto read_exact=[&](void* dst,size_t size){size_t n=0;while(n<size){auto got=http->Read(static_cast<char*>(dst)+n,size-n);if(!got || *got<=0)return false;n+=*got;}return true;};
                    unsigned char length[2]{};
                    ok=read_exact(length,2);
                    const size_t n=size_t(length[0])|(size_t(length[1])<<8);
                    if(!ok || !n || n>4096){ok=false;break;}
                    payload.resize(n);ok=read_exact(payload.data(),n);
                } else if(!direct)payload.assign(kOpus+kOffsets[f],kOpus+kOffsets[f+1]);
                const auto now=esp_timer_get_time();
                if(last_source) {max_source_gap=std::max(max_source_gap,uint32_t(now-last_source));}
                last_source=now;
                if(!ok || !codec->PlaybackControl(AudioCodec::PlaybackOp::Admit,id,uint64_t(f)*1440,1440)){ok=false;break;}
                if(direct) {
                    std::vector<int16_t> pcm(1440);
                    const double amplitude=phase==7?12000:3000;
                    for(int j=0;j<1440;++j){const int s=f*1440+j;const double fade=std::min(1.0,std::min(s/240.0,(kFrames*1440-1-s)/240.0));pcm[j]=std::lround(amplitude*fade*std::sin(2*3.141592653589793*400*s/24000));}
                    codec->OutputTagged(pcm,id,uint64_t(f)*1440,codec->PlaybackGeneration());
                    codec->PlaybackControl(AudioCodec::PlaybackOp::Release,id);
                } else {
                    auto packet=std::make_unique<AudioStreamPacket>();packet->sample_rate=24000;packet->frame_duration=60;packet->payload=std::move(payload);
                    packet->receipt_id=id;packet->receipt_first=f*1440;packet->output_generation=codec->PlaybackGeneration();
                    ok=c.service->PushPacketToDecodeQueue(std::move(packet),true);
                }
            }
            active.store(false);capturing.store(false);
            codec->PlaybackControl(AudioCodec::PlaybackOp::Seal,id);
            const auto end=esp_timer_get_time();
            while((!c.service->IsPlaybackIdle() || codec->PlaybackBusy()) && esp_timer_get_time()-end<5000000)vTaskDelay(1);
            const auto p=codec->WavePopProbe(false);
            printf("WPL_RESULT %d ok=%d fault=%d completed_samples=%u dma=%u empty=%u decode_waiting=%u pcm_waiting=%u no_backlog=%u max_decode_wall_us=%u max_wait_us=%u max_source_gap_us=%u feed_us=%lld\n",phase,ok,codec->ReferenceFaulted(),unsigned(p.completed_samples),unsigned(p.dma),unsigned(p.empty),unsigned(p.with_decode),unsigned(p.with_pcm),unsigned(p.without_backlog),unsigned(max_decode_us.load()),unsigned(max_wait_us.load()),unsigned(max_source_gap),static_cast<long long>(end-start));
            Dump(phase);
            if(!ok || codec->ReferenceFaulted()){printf("WPL_ABORT phase_failed\n");break;}
        }
        vTaskPrioritySet(task,original_priority);
        if(light)light->SetBrightness(original_light,false);
        heap_caps_free(capture);heap_caps_free(capture_times);capture=nullptr;capture_times=nullptr;
        c.app->Schedule([app=c.app](){app->SetDeviceState(kDeviceStateIdle);});
        board.SetPowerSaveLevel(PowerSaveLevel::LOW_POWER);
        printf("WPL_DONE\n");fflush(stdout);vTaskDelete(nullptr);
    },"wave_pop_lab",8192,&context,5,nullptr);
}
}
