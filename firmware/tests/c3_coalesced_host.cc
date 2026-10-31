
#include "button_uplink.h"
#include <cassert>
#include <cstdio>
#include <functional>
#include <string>
#include <vector>
enum DeviceState {kDeviceStateUnknown,kDeviceStateIdle,kDeviceStateConnecting,kDeviceStateListening,kDeviceStateSpeaking,kDeviceStateNotifying,kDeviceStateWifiConfiguring};
enum {kListeningModeAutoStop,kListeningModeManualStop,kListeningModeRealtime};
namespace Lang {namespace Strings {constexpr const char *STANDBY="idle",*CONNECTING="connect",*LISTENING="listen",*SPEAKING="speak";} namespace Sounds{constexpr int OGG_POPUP=1;}}
namespace family_robot {struct PlaybackEvent {explicit operator bool() const{return true;}};}
struct Codec {bool ManagedCapture()const{return false;}};
struct Display {void SetStatus(const char*){} void ClearChatMessages(){} void SetEmotion(const char*){} void SetChatMessage(const char*,const char*){}};
struct Led {void OnStateChanged(){}};
struct Board {Codec codec; Display display; Led led; static Board& GetInstance(){static Board b;return b;} Codec*GetAudioCodec(){return &codec;} Display*GetDisplay(){return &display;} Led*GetLed(){return &led;}};
struct AudioService {
 family_robot::ButtonUplink gate; bool processing=true; int enables=0;
 void SetExperimentUplink(bool enabled,bool=false){if(!enabled)gate.Close([]{});}
 family_robot::ButtonUplink&C3Uplink(){return gate;}
 bool OpenC3Uplink(uint32_t revision){return gate.Open(revision,[]{});}
 bool IsPlaybackIdle()const{return true;} bool IsAudioProcessorRunning()const{return processing;}
 void EnableVoiceProcessing(bool value){processing=value;if(value)++enables;}
 void EnableWakeWordDetection(bool){} bool IsAfeWakeWord()const{return false;}
 bool PreservingServerPlayback()const{return true;} void ResetDecoder(){} void PlaySound(int){}
};
struct Protocol {int starts=0;bool PlaybackEventCurrent(family_robot::PlaybackEvent)const{return true;} bool IsAudioChannelOpened()const{return true;} bool PlaybackAllowsListening()const{return true;} bool SendStartListening(int){++starts;return true;}};
struct StateMachine {DeviceState state=kDeviceStateListening; bool changed=false; bool TransitionTo(DeviceState next){changed|=(state!=next);state=next;return true;} DeviceState GetState()const{return state;}};
class Application {public:
 StateMachine state_machine_;AudioService audio_service_;Protocol transport,*protocol_=&transport;
 int clock_ticks_=0,listening_mode_=kListeningModeAutoStop;bool pending_listening_start_=false,play_popup_on_listening_=false,aborted_=false;
 std::string last_error_message_;std::vector<std::function<void()>>tasks;
 DeviceState GetDeviceState()const{return state_machine_.GetState();}
 void Schedule(std::function<void()>f){tasks.push_back(f);} void RecoverListeningFailure(){assert(false);}
 bool SetDeviceState(DeviceState); void HandleStateChangedEvent();void StartListeningAudio();void ConfigureWakeWordForListening();
 void SchedulePlaybackStart(family_robot::PlaybackEvent);void SchedulePlaybackStop(family_robot::PlaybackEvent,bool);
};

// PRODUCTION_APPLICATION_METHODS
int main(){
 Application a; assert(a.audio_service_.gate.Open(a.audio_service_.gate.Revision(),[]{}));
 // Ordered receive closes immediately. The main task is busy sending PCM.
 a.audio_service_.SetExperimentUplink(false);
 a.SchedulePlaybackStart({}); a.SchedulePlaybackStop({},false);
 auto tasks=std::move(a.tasks);for(auto &task:tasks)task();
 assert(a.GetDeviceState()==kDeviceStateListening && a.state_machine_.changed);
 // Run() coalesces MAIN_EVENT_STATE_CHANGED and reads only the final state.
 a.HandleStateChangedEvent();
 std::printf("state=listening processor=%s gate=%s listen_starts=%d pending=%s\n",a.audio_service_.processing?"running":"stopped",a.audio_service_.gate.Capture()?"open":"closed",a.transport.starts,a.pending_listening_start_?"true":"false");
 return a.audio_service_.gate.Capture()?0:17;
}
