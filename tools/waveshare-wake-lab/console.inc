#include "wave_wake_runtime.h"
#include <fcntl.h>
#include <unistd.h>
namespace wave_wake_lab {
inline void Start(Application* app) {
    const auto created=xTaskCreate([](void* p) {
        auto* app=static_cast<Application*>(p);
        char line[64]{};size_t used=0;bool overflow=false;
        // Pinned IDF polling USB VFS must keep O_NONBLOCK clear (same as the
        // existing M5 diagnostic console); the polling read itself is bounded.
        fcntl(STDIN_FILENO,F_SETFL,0);
        int64_t last_flush=esp_timer_get_time();puts("WWD_READY");fflush(stdout);
        while(true) {
            const auto now=esp_timer_get_time();if(now-last_flush>=100000){Flush();last_flush=now;}
            char c;const auto n=read(STDIN_FILENO,&c,1);
            if(n!=1){vTaskDelay(pdMS_TO_TICKS(20));continue;}
            if(c=='\r'||c=='\n') {
                if(overflow)puts("WWD_REJECT length");
                else if(used) {
                    line[used]='\0';const auto request=Parse(line);
                    if(request.kind==Kind::Probe)printf("WWD_PROBE accepted=%d seconds=%u\n",Begin(request.seconds),request.seconds);
                    else if(request.kind==Kind::Invalid)puts("WWD_REJECT syntax");
                    else app->Schedule([app,request]() {
                        const auto state=app->GetDeviceState();
                        if(request.kind==Kind::Status) {
                            auto* codec=Board::GetInstance().GetAudioCodec();
                            printf("WWD_STATUS state=%u wake=%d ref=%d fault=%d ready=%d volume=%d\n",unsigned(state),app->GetAudioService().IsWakeWordRunning(),codec->ReferenceValid(),codec->ReferenceFaulted(),codec->RealtimeEligible(),codec->output_volume());
                        } else if((request.kind==Kind::Start&&state==kDeviceStateIdle)||(request.kind==Kind::Stop&&state==kDeviceStateListening)) {
                            puts("WWD_TOGGLE queued");app->ToggleChatState();
                        } else puts("WWD_REJECT state");
                        fflush(stdout);
                    });
                }
                used=0;overflow=false;fflush(stdout);
            } else if(c>=32&&c<=126) {if(used+1<sizeof(line))line[used++]=c;else overflow=true;}
            else overflow=true;
        }
    },"wave_wake_usb",4096,app,1,nullptr);
    if(created!=pdPASS)puts("WWD_ERROR task");
}
}
