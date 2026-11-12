// Actual worker/application include; platform and physical DMA progress are host
// simulations. These tests cannot establish USB/I2S/device playback evidence.
#include "music_audition_protocol.h"
#include <algorithm>
#include <atomic>
#include <cassert>
#include <cstdarg>
#include <cstdio>
#include <cstdlib>
#include <deque>
#include <functional>
#include <memory>
#include <stdexcept>
#include <string>
#include <vector>

namespace MusicAudition { bool Active();bool Cancelled();void Cancel(); }
namespace host {
struct Done {};
uint64_t now=0,stop_at=15000,cancel_at=0;
uint64_t quiet_at=0;
size_t fragment=4096;
std::deque<uint8_t> input;
std::vector<std::string> events,replies;
bool restore_ok=true,drain=true,begin_ok=true,finish_after_reply=false;
bool hold_main=false,reset_busy=false;
std::vector<std::function<void()>> pending_main;
unsigned empty_reads=0;
void Check(bool value,const char* why){if(!value)throw std::runtime_error(why);}
void Tick(uint64_t delta);
}
enum { kDeviceStateIdle,kDeviceStateNotifying,kDeviceStateListening };
struct AudioCodec {
    bool music=false,busy=false,fault=false,clean=true;
    int rate=24000,begins=0,ends=0,writes=0;
    uint64_t accepted=0,completed=0,drain_at=0;
    bool BeginMusic(int value) {
        ++begins;host::events.push_back("begin-profile");music=true;clean=false;rate=value;
        return host::begin_ok;
    }
    // The real adapter explicitly no-ops EndMusic if it never acquired music.
    bool EndMusic() {
        ++ends;
        if(!music)return rate==24000&&!fault;
        host::events.push_back("restore-profile");
        if(!host::restore_ok)return false;
        rate=24000;busy=false;music=false;clean=true;
        host::events.push_back("cleanup-complete");return true;
    }
    int output_sample_rate()const{return rate;}
    bool ReferenceFaulted()const{return fault;}
    bool PlaybackBusy()const{return busy;}
    uint64_t MusicCompleted()const{return completed;}
    bool MusicWrite(std::vector<int16_t>& pcm) {
        ++writes;accepted+=pcm.size();busy=true;drain_at=host::now+50;
        host::events.push_back("queued");return true;
    }
};
struct Display {
    int failures=0;
    void SetChatMessage(const char*,const char*){}
    void ShowNotification(const char*,int){++failures;}
};
struct Board {
    AudioCodec codec;Display display;
    static Board& GetInstance(){static Board board;return board;}
    AudioCodec* GetAudioCodec(){return &codec;}
    Display* GetDisplay(){return &display;}
};
struct AudioService {
    bool idle=true,voice=true,wake=true;int resets=0,packets=2;
    bool IsIdle()const{return idle;}
    void EnableVoiceProcessing(bool enable){voice=enable;}
    void EnableWakeWordDetection(bool enable){wake=enable;}
    void ResetDecoder(){++resets;if(host::reset_busy)idle=false;}
    bool PopPacketFromSendQueue(){if(!packets)return false;--packets;return true;}
};
struct Notification {bool busy=false;bool IsBusy()const{return busy;}};
struct Application {
    int state=kDeviceStateIdle,normal_events=0;
    AudioService audio_service_;Notification notify_player_;
    static Application& GetInstance(){static Application app;return app;}
    void Schedule(std::function<void()> task){if(host::hold_main)host::pending_main.push_back(std::move(task));else task();}
    AudioService& GetAudioService(){return audio_service_;}
    int GetDeviceState()const{return state;}
    bool SetDeviceState(int next) {
        const int state=next;
        // Exact added guard extracted from scoped production application patch.
#include "actual_state_guard.inc"
        if(next==kDeviceStateIdle) {
            host::Check(Board::GetInstance().codec.clean,"idle before profile/queue cleanup");
            host::events.push_back("idle");
        }
        this->state=next;return true;
    }
    void PhysicalButton() {
        // Exact HandleToggleChatEvent guard; the remaining UI is outside scope.
#include "actual_button_guard.inc"
        ++normal_events;
    }
    bool BeginMusicAudition();
    void FinishMusicAudition(bool restored);
};
namespace host {
void Tick(uint64_t delta) {
    now+=delta;
    if(quiet_at && now>=quiet_at){quiet_at=0;Application::GetInstance().audio_service_.idle=true;}
    if(cancel_at && now>=cancel_at){cancel_at=0;Application::GetInstance().PhysicalButton();}
    auto& codec=Board::GetInstance().codec;
    if(drain && codec.busy && codec.music && now>=codec.drain_at) {
        codec.completed=codec.accepted;codec.busy=false;events.push_back("simulated-dma-completion");
    }
    if(now>=stop_at)throw Done{};
}
int Printf(const char* format,...) {
    char line[256];va_list args;va_start(args,format);
    const int count=vsnprintf(line,sizeof(line),format,args);va_end(args);
    replies.emplace_back(line);events.emplace_back(line);
    if(finish_after_reply && input.empty() && !MusicAudition::Active())throw Done{};
    return count;
}
}
int64_t esp_timer_get_time(){return static_cast<int64_t>(host::now*1000);}
void vTaskDelay(int ticks){host::Tick(ticks);}
int usb_serial_jtag_read_bytes(void* target,size_t size,int ticks) {
    if(host::input.empty()){++host::empty_reads;host::Tick(ticks);return 0;}
    const size_t count=std::min({size,host::fragment,host::input.size()});
    auto* out=static_cast<uint8_t*>(target);
    for(size_t i=0;i<count;++i){out[i]=host::input.front();host::input.pop_front();}
    return static_cast<int>(count);
}
struct usb_serial_jtag_driver_config_t {int rx_buffer_size=0,tx_buffer_size=0;};
int usb_serial_jtag_driver_install(usb_serial_jtag_driver_config_t*){return 0;}
void usb_serial_jtag_vfs_use_driver(){}
int xTaskCreate(void(*)(void*),const char*,int,void*,int,void*){return 1;}
#define pdMS_TO_TICKS(value) (value)
#define pdPASS 1
#define ESP_OK 0
#define ESP_LOGE(...) ((void)0)
#define printf host::Printf
#include "music_audition.inc"
#undef printf

namespace host {
std::vector<uint8_t> Frame(music_audition::Op op,uint32_t seq,uint32_t arg,std::vector<uint8_t> payload={}) {
    std::vector<uint8_t> frame(24+payload.size());
    std::copy_n("FMA1",4,frame.begin());
    music_audition::Write32(frame.data()+4,static_cast<uint32_t>(op));
    music_audition::Write32(frame.data()+8,seq);music_audition::Write32(frame.data()+12,arg);
    music_audition::Write32(frame.data()+16,payload.size());std::copy(payload.begin(),payload.end(),frame.begin()+24);
    music_audition::Write32(frame.data()+20,music_audition::FrameCrc(frame.data(),payload.size()));return frame;
}
void Push(const std::vector<uint8_t>& frame){input.insert(input.end(),frame.begin(),frame.end());}
void Start(int rate=48000,uint32_t count=4) {
    std::vector<uint8_t> payload(8);music_audition::Write32(payload.data(),count);
    Push(Frame(music_audition::Op::Start,0,rate,payload));
}
void Data(uint32_t seq=1,size_t count=4){Push(Frame(music_audition::Op::Data,seq,0,std::vector<uint8_t>(count*2,42)));}
bool ReplyContains(const std::string& value) {
    return std::any_of(replies.begin(),replies.end(),[&](const auto& line){return line.find(value)!=std::string::npos;});
}
size_t Event(const std::string& value) {
    const auto it=std::find(events.begin(),events.end(),value);
    Check(it!=events.end(),("missing event: "+value).c_str());return it-events.begin();
}
void Run(){try{MusicAudition::Worker(nullptr);}catch(const Done&) {}}
}
int main(int argc,char** argv) {
    using namespace host;using music_audition::Op;
    try {
        Check(argc==2,"one scenario required");const std::string test=argv[1];
        auto& app=Application::GetInstance();auto& codec=Board::GetInstance().codec;
        finish_after_reply=true;
        if(test=="app-admission") {
            app.state=kDeviceStateListening;Check(!app.BeginMusicAudition(),"nonidle admitted");app.state=kDeviceStateIdle;
            app.notify_player_.busy=true;Check(!app.BeginMusicAudition(),"notification admitted");app.notify_player_.busy=false;
            app.audio_service_.idle=false;Check(!app.BeginMusicAudition(),"audio busy admitted");app.audio_service_.idle=true;
            codec.busy=true;Check(!app.BeginMusicAudition(),"DMA busy admitted");codec.busy=false;
            MusicAudition::cancelled=true;Check(!app.BeginMusicAudition(),"cancel admitted");MusicAudition::cancelled=false;
            Check(app.audio_service_.resets==0,"rejection mutated audio");
            Check(app.BeginMusicAudition(),"idle rejected");
            Check(app.state==kDeviceStateNotifying&&!app.audio_service_.voice&&!app.audio_service_.wake,"producer suspension missing");
            Check(app.audio_service_.resets==1&&app.audio_service_.packets==0,"decoder/send queue not cleared");
            MusicAudition::active=true;app.FinishMusicAudition(false);
            Check(MusicAudition::Active()&&MusicAudition::Cancelled()&&app.state==kDeviceStateNotifying,"failed restore released owner");
            Check(Board::GetInstance().display.failures==1,"missing restore warning");
            app.FinishMusicAudition(true);Check(!MusicAudition::Active()&&app.state==kDeviceStateIdle,"restored owner stuck");
            Check(!MusicAudition::main_owned,"restored application owner flag stuck");
            app.state=kDeviceStateNotifying;app.FinishMusicAudition(true);
            Check(app.state==kDeviceStateNotifying,"unowned Finish changed another notification");
        } else if(test=="guards") {
            MusicAudition::active=true;app.state=kDeviceStateNotifying;
            Check(!app.SetDeviceState(kDeviceStateListening),"state guard permitted voice");
            Check(MusicAudition::Cancelled()&&app.state==kDeviceStateNotifying,"state guard did not cancel");
            MusicAudition::cancelled=false;app.PhysicalButton();
            Check(MusicAudition::Cancelled()&&app.normal_events==0,"physical toggle escaped cancel guard");
        } else {
            const bool success=test=="valid24"||test=="valid48"||test=="fragmented";
            if(success) {
                if(test=="fragmented")fragment=1;
                Start(test=="valid24"?24000:48000);Data();Push(Frame(Op::End,2,0));Run();
                Check(ReplyContains("0 READY")&&ReplyContains("1 ACK")&&ReplyContains("2 DONE 24000 4"),"valid session did not complete");
                Check(Event("queued")<Event("simulated-dma-completion"),"queue mistaken for physical completion");
                Check(Event("simulated-dma-completion")<Event("restore-profile"),"End restored before drain");
            } else if(test=="main-timeout") {
                hold_main=true;Start();Run();
                Check(ReplyContains("ERROR")&&!MusicAudition::Active()&&codec.begins==0&&codec.ends==0,"timed out main job acquired hardware");
                for(auto& task:pending_main)task();
                Check(app.state==kDeviceStateIdle&&app.audio_service_.resets==0&&!MusicAudition::main_owned,"abandoned main callback acquired owner");
            } else if(test=="quiet-drain"||test=="quiet-timeout"||test=="quiet-cancel") {
                reset_busy=true;
                if(test=="quiet-drain")quiet_at=50;
                if(test=="quiet-cancel")cancel_at=30;
                Start();Push(Frame(Op::Stop,1,0));Run();
                Check(codec.ends==1&&!MusicAudition::Active()&&!MusicAudition::main_owned,"quiet wait failed to release owner");
                if(test=="quiet-drain")Check(codec.begins==1&&now>=50&&ReplyContains("READY"),"profile changed before producers drained");
                else Check(codec.begins==0&&!ReplyContains("READY")&&ReplyContains("ERROR"),"busy/cancelled producers changed profile");
                if(test=="quiet-timeout")Check(now>=500,"producer timeout not bounded");
            } else if(test=="busy-start"||test=="busy-notifying") {
                const int initial=test=="busy-start"?kDeviceStateListening:kDeviceStateNotifying;
                app.state=initial;app.notify_player_.busy=test=="busy-notifying";Start();Run();
                Check(ReplyContains("ERROR")&&codec.begins==0&&app.state==initial,"busy Start changed owner");
                Check(app.audio_service_.voice&&app.audio_service_.wake,"busy Start disabled producers");
                Check(codec.ends==0&&app.audio_service_.resets==0,"rejected Start entered unrelated hardware recovery");
            } else if(test=="invalid-start") {
                Start(44100);Run();Check(ReplyContains("ERROR")&&codec.begins==0&&codec.ends==0,"invalid format touched codec");
            } else if(test=="cancel"||test=="read-timeout") {
                finish_after_reply=false;Start();if(test=="cancel")cancel_at=30;Run();
                Check(ReplyContains(test=="cancel"?"STOPPED":"ERROR"),"read cancel/timeout missing response");
                Check(codec.ends==1&&!MusicAudition::Active(),"read cancel/timeout did not restore exactly once");
                Check(test!="cancel"||now>=30,"cancel did not run");
            } else if(test=="restore-failure") {
                restore_ok=false;Start();Push(Frame(Op::Stop,1,0));Start();Run();
                Check(MusicAudition::failed&&MusicAudition::Active()&&MusicAudition::Cancelled(),"restore failure not latched");
                Check(codec.ends==1&&codec.begins==1&&codec.rate==48000,"repeated hardware recovery after fault");
                Check(app.state==kDeviceStateNotifying&&!input.empty(),"fault allowed new session/idle");
                Check(!ReplyContains("STOPPED")&&ReplyContains("ERROR 0"),"fault advertised restored state");
            } else {
                Start();
                if(test=="crc") {auto frame=Frame(Op::Data,1,0,std::vector<uint8_t>(8));frame.back()^=1;Push(frame);}
                else if(test=="replay"){Data();Data();}
                else if(test=="bad-count")Data(1,5);
                else if(test=="bad-size") {auto frame=Frame(Op::Data,1,0);music_audition::Write32(frame.data()+16,2881);Push(frame);}
                else if(test=="start-active")Start();
                else if(test=="short-end"){Data(1,2);Push(Frame(Op::End,2,0));}
                else if(test=="drain-timeout"){drain=false;Data();Push(Frame(Op::End,2,0));}
                else if(test=="cancel-drain"){cancel_at=30;drain=false;Data();Push(Frame(Op::End,2,0));}
                else if(test=="partial-header") {auto frame=Frame(Op::Data,1,0,std::vector<uint8_t>(8));frame.resize(10);Push(frame);}
                else if(test=="begin-failure"){begin_ok=false;}
                else Check(false,"unknown scenario");
                Run();
                Check(ReplyContains(test=="cancel-drain"?"STOPPED":"ERROR"),"invalid stream did not fail closed");
                Check(!ReplyContains("DONE"),"invalid/incomplete stream advertised DONE");
                Check(codec.ends==1&&!MusicAudition::Active(),"failure did not restore once");
                if(test=="crc"||test=="bad-size"||test=="bad-count")Check(codec.writes==0,"invalid payload reached codec");
                if(test=="drain-timeout")Check(now>=2000&&codec.completed==0,"drain timeout credited playback");
            }
            if(codec.begins&&restore_ok)Check(Event("cleanup-complete")<Event("idle"),"owner released before cleanup");
            if(restore_ok)Check(!MusicAudition::main_owned,"restored session left main owner flag set");
        }
        std::puts(("PASS "+test+" (host simulation)").c_str());return 0;
    } catch(const std::exception& error){std::fprintf(stderr,"FAIL: %s\n",error.what());return 1;}
}
