#if CONFIG_FAMILY_MUSIC_AUDITION
#include "music_audition_protocol.h"
#include <array>
#include <atomic>
#include <cstdio>
#include <memory>
#include <driver/usb_serial_jtag.h>
#include <driver/usb_serial_jtag_vfs.h>

namespace MusicAudition {
namespace {
std::atomic<bool> active{false},cancelled{false},failed{false};
bool main_owned=false; // accessed only by scheduled application callbacks
struct MainJob {std::atomic<bool> abandoned{false};std::atomic<int> result{-1};};
uint64_t Now(){return static_cast<uint64_t>(esp_timer_get_time()/1000);}
bool OnMain(bool beginning,bool restored=true) {
    auto job=std::make_shared<MainJob>();
    Application::GetInstance().Schedule([job,beginning,restored] {
        if(job->abandoned.load())return;
        bool ok=true;
        if(beginning)ok=Application::GetInstance().BeginMusicAudition();
        else Application::GetInstance().FinishMusicAudition(restored);
        job->result.store(ok?1:0);
    });
    const auto end=Now()+5000;
    while(job->result.load()<0 && Now()<end)vTaskDelay(1);
    if(job->result.load()<0){job->abandoned=true;cancelled=true;return false;}
    return job->result.load()==1;
}
void Reply(uint32_t seq,const char* status,int rate,uint64_t played) {
    printf("\nFMA1 %lu %s %d %llu\n",static_cast<unsigned long>(seq),status,rate,
           static_cast<unsigned long long>(played));fflush(stdout);
}
bool Restore(AudioCodec* codec,uint32_t seq,const char* status,uint64_t played) {
    const bool restored=codec->EndMusic() && codec->output_sample_rate()==24000 && !codec->ReferenceFaulted();
    const bool owner=OnMain(false,restored);
    if(!restored || !owner)failed=true; // latched: no repeated hardware recovery
    Reply(seq,restored&&owner?status:"ERROR",restored&&owner?24000:0,played);
    return restored&&owner;
}
// Input is the installed USB driver, with bounded reads and no stdio CR/LF
// conversion. Binary samples never share a parser with application log output.
bool Read(uint8_t* target,size_t size,uint64_t deadline,music_audition::Session& session) {
    size_t used=0;
    while(used<size) {
        if(cancelled.load() || (deadline && Now()>=deadline) || session.CheckTimeout(Now()))return false;
        const int count=usb_serial_jtag_read_bytes(target+used,size-used,pdMS_TO_TICKS(10));
        if(count<0)return false;
        used+=static_cast<size_t>(count);
    }
    return true;
}
void Worker(void*) {
    music_audition::Session session;
    std::array<uint8_t,24+2880> bytes{};
    auto codec=Board::GetInstance().GetAudioCodec();
    while(true) {
        if(failed.load()){vTaskDelay(pdMS_TO_TICKS(1000));continue;}
        if(!active.load())cancelled=false;
        // Scan for the magic only while idle; a damaged active stream fails
        // closed instead of interpreting arbitrary PCM as a new command.
        size_t magic=0;
        while(magic<4) {
            uint8_t byte=0;
            if(!Read(&byte,1,active.load()?Now()+5000:0,session))break;
            if(byte==static_cast<uint8_t>("FMA1"[magic]))bytes[magic++]=byte;
            else if(active.load())break;
            else magic=byte=='F'?1:0;
        }
        const uint32_t expected=session.expected_seq();
        if(magic!=4 || !Read(bytes.data()+4,20,Now()+5000,session)) {
            if(active.load())Restore(codec,expected,cancelled.load()?"STOPPED":"ERROR",codec->MusicCompleted());
            session.Reset();continue;
        }
        const uint32_t seq=music_audition::Read32(bytes.data()+8);
        const uint32_t size=music_audition::Read32(bytes.data()+16);
        if(size>2880 || !Read(bytes.data()+24,size,Now()+5000,session)) {
            if(active.load())Restore(codec,seq,cancelled.load()?"STOPPED":"ERROR",codec->MusicCompleted());
            else Reply(seq,"ERROR",24000,0);
            session.Reset();continue;
        }
        music_audition::Frame frame{};
        if(!session.AcceptBytes(bytes.data(),24+size,Now(),&frame)) {
            if(active.load())Restore(codec,seq,"ERROR",codec->MusicCompleted());
            else Reply(seq,"ERROR",24000,0);
            session.Reset();continue;
        }
        if(frame.op==music_audition::Op::Start) {
            cancelled=false;
            if(!OnMain(true)) {
                // Rejected requests never acquired notification/audio ownership.
                if(active.load())Restore(codec,seq,"ERROR",0);
                else Reply(seq,"ERROR",24000,0);
                session.Reset();continue;
            }
            const auto quiet_until=Now()+500;
            while(!cancelled.load() && !Application::GetInstance().GetAudioService().IsIdle() && Now()<quiet_until)vTaskDelay(1);
            if(cancelled.load() || !Application::GetInstance().GetAudioService().IsIdle() || !codec->BeginMusic(session.rate())) {
                Restore(codec,seq,"ERROR",0);session.Reset();continue;
            }
            Reply(seq,"READY",session.rate(),0);
        } else if(frame.op==music_audition::Op::Data) {
            std::vector<int16_t> pcm(size/2);
            for(size_t i=0;i<pcm.size();++i)pcm[i]=static_cast<int16_t>(uint16_t(frame.payload[i*2])|(uint16_t(frame.payload[i*2+1])<<8));
            if(cancelled.load() || !codec->MusicWrite(pcm)) {
                Restore(codec,seq,cancelled.load()?"STOPPED":"ERROR",codec->MusicCompleted());session.Reset();continue;
            }
            Reply(seq,"ACK",session.rate(),codec->MusicCompleted());
        } else if(frame.op==music_audition::Op::Stop) {
            Restore(codec,seq,"STOPPED",codec->MusicCompleted());session.Reset();
        } else if(frame.op==music_audition::Op::End) {
            const auto end=Now()+2000;
            while(!cancelled.load() && codec->PlaybackBusy() && !codec->ReferenceFaulted() && Now()<end)vTaskDelay(1);
            const auto played=codec->MusicCompleted();
            const bool complete=!codec->PlaybackBusy() && !codec->ReferenceFaulted() && played==session.received_samples();
            Restore(codec,seq,cancelled.load()?"STOPPED":complete?"DONE":"ERROR",played);session.Reset();
        }
    }
}
}
bool Active(){return active.load();}
bool Cancelled(){return cancelled.load();}
void Cancel(){if(active.load())cancelled=true;}
void Start() {
    usb_serial_jtag_driver_config_t config{};config.rx_buffer_size=4096;config.tx_buffer_size=1024;
    if(usb_serial_jtag_driver_install(&config)!=ESP_OK){ESP_LOGE("MusicAudition","USB driver unavailable");return;}
    usb_serial_jtag_vfs_use_driver();
    if(xTaskCreate(Worker,"music_usb",8192,nullptr,2,nullptr)!=pdPASS)ESP_LOGE("MusicAudition","USB task unavailable");
}
}

bool Application::BeginMusicAudition() {
    if(GetDeviceState()!=kDeviceStateIdle || notify_player_.IsBusy() || !audio_service_.IsIdle() ||
       Board::GetInstance().GetAudioCodec()->PlaybackBusy() || MusicAudition::Cancelled())return false;
    MusicAudition::active=true;
    if(!SetDeviceState(kDeviceStateNotifying)){MusicAudition::active=false;return false;}
    MusicAudition::main_owned=true;
    audio_service_.EnableVoiceProcessing(false);
    audio_service_.EnableWakeWordDetection(false);
    audio_service_.ResetDecoder();
    while(audio_service_.PopPacketFromSendQueue()){}
    Board::GetInstance().GetDisplay()->SetChatMessage("assistant","音乐试听");
    return !MusicAudition::Cancelled();
}
void Application::FinishMusicAudition(bool restored) {
    // Owner state is released only after the worker has restored/verified24kHz.
    if(!restored) {
        MusicAudition::cancelled=true;
        Board::GetInstance().GetDisplay()->ShowNotification("音乐恢复失败，请重启",10000);
        return;
    }
    MusicAudition::active=false;
    if(MusicAudition::main_owned && GetDeviceState()==kDeviceStateNotifying) {
        Board::GetInstance().GetDisplay()->SetChatMessage("assistant","");
        SetDeviceState(kDeviceStateIdle);
    }
    MusicAudition::main_owned=false;
}
#endif
