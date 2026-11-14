#include <cassert>
#include <string>
#include <vector>
#include <atomic>
namespace FamilyOta{bool reserved=false;bool Reserved(){return reserved;}}
namespace FamilyControl{bool busy=false;bool FirmwareBusy(){return busy;}}
enum DeviceState{kDeviceStateIdle,kDeviceStateUpgrading,kDeviceStateListening,kDeviceStateSpeaking,kDeviceStateConnecting};
enum class PowerSaveLevel{PERFORMANCE,LOW_POWER};
namespace ConnectionDiagnostics{enum class Stage{State};void Observe(Stage,int){}}
struct Codec{bool busy=false,available=true;bool PlaybackBusy(){return busy;}bool FirmwareAvailable(){return available;}bool ManagedCapture(){return true;}}codec;
struct Display{void SetChatMessage(const char*,const char*){}};
struct Board{static Board& GetInstance(){static Board b;return b;}Codec* GetAudioCodec(){return &codec;}Display* GetDisplay(){static Display d;return &d;}void SetPowerSaveLevel(PowerSaveLevel){}};
struct Audio{bool processor=false,wake=true,idle=true,playback=true;int starts=0,stops=0,resets=0;bool IsAudioTestingRunning(){return false;}bool IsAudioProcessorRunning(){return processor;}bool IsPlaybackIdle(){return playback;}bool IsIdle(){return idle;}void EnableVoiceProcessing(bool value){processor=value;}void EnableWakeWordDetection(bool value){wake=value;}void SetExperimentUplink(bool){}void ResetDecoder(){++resets;}void* PopPacketFromSendQueue(){return nullptr;}void Start(){++starts;}void Stop(){++stops;}};
struct Notify{bool busy=false;bool IsBusy(){return busy;}};
struct Protocol{bool open=false;bool IsAudioChannelOpened(){return open;}};
struct Machine{DeviceState state=kDeviceStateIdle;bool TransitionTo(DeviceState s){state=s;return true;}DeviceState GetState(){return state;}};
constexpr int kListeningModeRealtime=1;
struct Assets{static Assets& GetInstance(){static Assets a;return a;}bool good=true;int applies=0;bool partition_valid(){return good;}bool Apply(bool refresh){assert(!refresh);++applies;return good;}};
class Application{public:bool assets_version_checked_=false;bool PrepareFamilyFirmwareBootAudio();Machine state_machine_;Audio audio_service_;Notify notify_player_;Protocol* protocol_=nullptr;int listening_mode_=0;bool SetDeviceState(DeviceState);DeviceState GetDeviceState(){return state_machine_.GetState();}bool FamilyFirmwareEligible();bool BeginFamilyFirmware();bool FamilyFirmwareQuiescent();void PauseFamilyFirmwareInput();void ResumeFamilyFirmwareInput();void FinishFamilyFirmware();};
// PRODUCTION_SET_STATE
#include "family_ota_application.inc"
int main(){
 Application app;
 Assets::GetInstance().good=false;assert(!app.PrepareFamilyFirmwareBootAudio()&&!app.assets_version_checked_);
 Assets::GetInstance().good=true;assert(app.PrepareFamilyFirmwareBootAudio()&&app.assets_version_checked_);
 for(int reject=0;reject<7;++reject){app.state_machine_.state=reject==0?kDeviceStateListening:kDeviceStateIdle;app.notify_player_.busy=reject==1;app.audio_service_.processor=reject==2;app.audio_service_.playback=reject!=3;codec.busy=reject==4;codec.available=reject!=5;FamilyControl::busy=reject==6;assert(!app.FamilyFirmwareEligible());}
 app.state_machine_.state=kDeviceStateIdle;app.notify_player_.busy=false;app.audio_service_.processor=false;app.audio_service_.playback=true;codec.busy=false;codec.available=true;FamilyControl::busy=false;
 assert(app.FamilyFirmwareEligible()&&app.audio_service_.wake);
 FamilyOta::reserved=true;assert(app.BeginFamilyFirmware());assert(app.GetDeviceState()==kDeviceStateUpgrading&&!app.audio_service_.wake&&!app.audio_service_.processor);
 assert(!app.SetDeviceState(kDeviceStateIdle)&&!app.SetDeviceState(kDeviceStateListening));
 assert(app.FamilyFirmwareQuiescent());app.audio_service_.idle=false;assert(!app.FamilyFirmwareQuiescent());
 app.ResumeFamilyFirmwareInput();assert(app.audio_service_.wake);FamilyOta::reserved=false;app.FinishFamilyFirmware();assert(app.GetDeviceState()==kDeviceStateIdle);
 assert(!app.audio_service_.starts&&!app.audio_service_.stops&&app.audio_service_.resets==1);
}
