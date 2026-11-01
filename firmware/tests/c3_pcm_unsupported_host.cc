#include <cassert>
#include <cstdint>
#include <functional>
#include <memory>
#include <string>
#include <vector>
struct NotifySubtitle{};
using SubtitleCallback=std::function<void()>;using FinishedCallback=std::function<void()>;
#define ESP_LOGW(...) (void)0
#define ESP_LOGE(...) (void)0
#define TAG "Application"
enum {kDeviceStateIdle,kDeviceStateListening,kDeviceStateSpeaking,kDeviceStateNotifying,kDeviceStateActivating,kDeviceStateWifiConfiguring,kDeviceStateAudioTesting,kDeviceStateConnecting};
enum ListeningMode{kListeningModeAutoStop,kListeningModeManualStop};enum AbortReason{kAbortReasonNone};
struct AudioService{unsigned calls=0;void EnableAudioTesting(bool);void SetExperimentUplink(bool){++calls;}};
struct Protocol{unsigned calls=0;bool IsAudioChannelOpened(){++calls;return true;}void CloseAudioChannel(){++calls;}};
struct NotifyPlayer{bool Start(std::string,std::vector<NotifySubtitle>,uint32_t,SubtitleCallback,FinishedCallback);};
struct Application{
 int state=kDeviceStateWifiConfiguring;unsigned state_changes=0,scheduled=0;AudioService audio_service_;Protocol transport,*protocol_=&transport;
 int GetDeviceState(){return state;}bool SetDeviceState(int next){++state_changes;state=next;return true;}
 void StopNotification(){++state_changes;}ListeningMode GetDefaultListeningMode(){return kListeningModeAutoStop;}
 void Schedule(std::function<void()>){++scheduled;}void ContinueOpenAudioChannel(ListeningMode){}void SetListeningMode(ListeningMode){++state_changes;}
 void AbortSpeaking(AbortReason){++state_changes;}
 void HandleToggleChatEvent();void HandleStartListeningEvent();void StartNotification(std::string,std::vector<NotifySubtitle>);
};
// PRODUCTION_UNSUPPORTED_METHODS
int main(){Application app;app.HandleToggleChatEvent();assert(app.state==kDeviceStateWifiConfiguring&&app.state_changes==0&&app.audio_service_.calls==0&&app.transport.calls==0&&app.scheduled==0);
 app.HandleStartListeningEvent();assert(app.state==kDeviceStateWifiConfiguring&&app.state_changes==0&&app.audio_service_.calls==0&&app.transport.calls==0&&app.scheduled==0);
 app.audio_service_.EnableAudioTesting(true);app.audio_service_.EnableAudioTesting(false);assert(app.audio_service_.calls==0);
 app.StartNotification("https://x/ogg",{{}});assert(app.state_changes==0&&app.scheduled==0);
 NotifyPlayer player;assert(!player.Start("https://x/ogg",{{}},1,[]{},[]{}));
 app.state=kDeviceStateIdle;app.HandleToggleChatEvent();assert(app.state_changes==1&&app.transport.calls==1);
}
