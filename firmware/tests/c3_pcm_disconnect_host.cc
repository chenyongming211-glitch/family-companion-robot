#include "button_uplink.h"
#include "playback_progress.h"
#include <cassert>
#include <atomic>
#include <future>
#include <thread>
#include <chrono>
#include <cstring>
#include <functional>
#include <map>
#include <memory>
#include <mutex>
#include <string>
#include <vector>
#include <cstdlib>
#include <arpa/inet.h>
struct cJSON {std::map<std::string,std::unique_ptr<cJSON>> items;std::string text;const char* valuestring=nullptr;double valuedouble=0;int kind=0;};
static cJSON* cJSON_CreateObject(){return new cJSON;}
static void cJSON_AddStringToObject(cJSON* root,const char* key,const char* text){auto value=std::make_unique<cJSON>();value->text=text;value->valuestring=value->text.c_str();value->kind=1;root->items[key]=std::move(value);}
static void cJSON_AddNumberToObject(cJSON* root,const char* key,double n){auto value=std::make_unique<cJSON>();value->valuedouble=n;value->kind=2;root->items[key]=std::move(value);}
static void cJSON_AddBoolToObject(cJSON* root,const char* key,bool b){cJSON_AddNumberToObject(root,key,b?1:0);}
static void cJSON_AddItemToObject(cJSON* root,const char* key,cJSON* item){root->items[key].reset(item);}
static cJSON* cJSON_GetObjectItem(cJSON* root,const char* key){if(!root)return nullptr;auto found=root->items.find(key);return found==root->items.end()?nullptr:found->second.get();}
static bool cJSON_IsString(const cJSON* value){return value&&value->kind==1;}
[[maybe_unused]] static cJSON* cJSON_ParseWithLength(const char*,size_t){return nullptr;}
static void cJSON_Delete(cJSON* root){delete root;}static void cJSON_free(void* p){std::free(p);}
static int hello_checks=0;
static char* cJSON_PrintUnformatted(cJSON* root){
 auto* params=cJSON_GetObjectItem(root,"audio_params");auto* features=cJSON_GetObjectItem(root,"features");
 assert(cJSON_GetObjectItem(root,"version")->valuedouble==1);assert(cJSON_GetObjectItem(root,"transport")->text=="websocket");
 assert(cJSON_GetObjectItem(params,"format")->text=="pcm_s16le"&&cJSON_GetObjectItem(params,"sample_rate")->valuedouble==16000);
 assert(cJSON_GetObjectItem(params,"channels")->valuedouble==1&&cJSON_GetObjectItem(params,"frame_duration")->valuedouble==60);
 for(auto* name:{"pcm_s16le_v1","playback_progress_v1","playback_drain_v1"})assert(cJSON_GetObjectItem(features,name)->valuedouble==1);
 ++hello_checks;return ::strdup("{}");}
struct AudioStreamPacket{std::vector<uint8_t> payload;uint32_t uplink_epoch=0,timestamp=0;};
struct AudioService {family_robot::ButtonUplink gate;unsigned faults=0,flushes=0,resets=0,wakes=0;bool receiving=false,preserve=false;
 void SetExperimentUplink(bool enabled){if(!enabled)gate.Close([&]{++flushes;});}
 void ResetDecoder(){++resets;}void PreserveServerPlayback(bool b){preserve=b;}void SetC3ServerReceiving(bool b){receiving=b;}
 struct Callbacks{std::function<void()> on_send_queue_available;}callbacks_;
 AudioService(){callbacks_.on_send_queue_available=[this]{++wakes;};}
 void WakeC3PcmOwner();
void RequestC3PcmFault(uint32_t epoch){gate.Revoke(epoch);++faults;}auto& C3Uplink(){return gate;}};
struct Application {static Application& GetInstance(){static Application app;return app;}AudioService audio;auto& GetAudioService(){return audio;}};
struct Result {bool ok=true;explicit operator bool()const{return ok;}Result error()const{return *this;}std::string ToString()const{return "fake";}};
static std::map<int,bool> event_alive;static int last_event=0;
static int xEventGroupCreate(){event_alive[++last_event]=true;return last_event;}
static void vEventGroupDelete(int event){assert(event_alive[event]);event_alive[event]=false;}
static std::function<void()> send_hook;
static int creates=0,sends=0,connects=0;static size_t sent_bytes=0;
struct WebSocket {int owner_event=last_event;
 ~WebSocket(){assert(event_alive[owner_event]&&"protocol event must outlive joined socket callbacks");if(disconnected)disconnected();}
 std::function<void(const char*,size_t,bool)> data;std::function<void()> disconnected;
 bool IsConnected()const{return true;}void SetHeader(const char*,const char*){}
 void OnData(std::function<void(const char*,size_t,bool)> fn){data=std::move(fn);}void OnDisconnected(std::function<void()> fn){disconnected=std::move(fn);}
 Result Connect(const char*){++connects;return {};}
 bool Send(const void*,size_t n,bool){if(send_hook)send_hook();++sends;sent_bytes=n;return true;}};
struct Network {std::unique_ptr<WebSocket>CreateWebSocket(int){++creates;return std::make_unique<WebSocket>();}};
struct AudioCodec {enum class PlaybackOp{Connect};bool PlaybackControl(PlaybackOp,family_robot::PlaybackIdentity){return true;}};
struct Codec:AudioCodec {bool PlaybackSupported(){return true;}};
struct Board {static Board& GetInstance(){static Board board;return board;}Network network;Codec codec;Network* GetNetwork(){return &network;}Codec* GetAudioCodec(){return &codec;}std::string GetUuid(){return "uuid";}};
struct SystemInfo {static std::string GetMacAddress(){return "mac";}};
struct Settings {static std::string url;static int version;Settings(const char*,bool){}std::string GetString(const char* key){return std::string(key)=="url"?url:"";}int GetInt(const char*){return version;}};
std::string Settings::url;int Settings::version=1;
namespace Lang {namespace Strings {constexpr auto SERVER_TIMEOUT="timeout",SERVER_NOT_CONNECTED="not connected";}}
constexpr int WEBSOCKET_PROTOCOL_SERVER_HELLO_EVENT=1,OPUS_FRAME_DURATION_MS=60;
constexpr bool pdTRUE=true,pdFALSE=false;
#define pdMS_TO_TICKS(x) (x)
#define ESP_LOGE(...) (void)0
#define ESP_LOGI(...) (void)0
#define TAG "WS"
using EventBits_t=int;static void xEventGroupClearBits(int,int){}static int xEventGroupWaitBits(int,int,bool,bool,int){return 1;}
struct WebsocketProtocol {
 std::unique_ptr<WebSocket> websocket_;int version_=1,event_group_handle_=0;
 bool error_occurred_=false,negotiated=false;uint32_t playback_connection_=0,socket_connection_=0;
 std::atomic<uint32_t> c3_current_connection_{0},c3_pending_connection_{0};
 family_robot::PlaybackIdentity playback_id_{};uint64_t playback_first_=0;
 bool playback_negotiated_=false,playback_receiving_=false,playback_tracking_=false,playback_drain_negotiated_=false;
 std::string session_id_;
 std::mutex playback_mutex_;std::chrono::steady_clock::time_point last_incoming_time_;
 std::function<void()> on_audio_channel_opened_;std::function<void(const cJSON*)> on_incoming_json_;
 WebsocketProtocol();~WebsocketProtocol();
 bool ResetPlaybackConnection(uint32_t expected=0,uint32_t* closed_epoch=nullptr);bool PlaybackNegotiated()const{return negotiated;}
 void RequestC3PcmDisconnect(uint32_t);void PollC3PcmDisconnect();
 std::function<void(uint32_t)> on_audio_channel_closed_;
 void CloseAudioChannel(bool send_goodbye=true);
 bool IsTimeout()const{return false;}void SetError(const char*,const std::string& ={}){error_occurred_=true;}
 void NotifyPlaybackClosed(uint32_t);void DeliverC3PcmAudio(const uint8_t*,size_t,uint32_t){}
 void ParseServerHello(const cJSON*,uint32_t){}bool HandlePlaybackTts(cJSON*,uint32_t){return true;}
 bool SendText(const std::string&){return true;}void AddTextFontCapabilities(cJSON*){}
 bool OpenAudioChannel();bool IsAudioChannelOpened()const;bool SendAudio(std::unique_ptr<AudioStreamPacket>);std::string GetHelloMessage();
};
// PRODUCTION_DISCONNECT_METHODS

static void Open(WebsocketProtocol& ws){Settings::url="ws://example/x";Settings::version=1;ws.negotiated=true;assert(ws.OpenAudioChannel());
 auto& gate=Application::GetInstance().audio.gate;assert(gate.Open(gate.Revision(),[]{}));assert(gate.Capture());}
static void DisconnectWhileSending(){
 auto& audio=Application::GetInstance().audio;WebsocketProtocol ws;Open(ws);const auto first_connection=ws.playback_connection_;const auto first_token=audio.gate.Capture();
 unsigned closed=0;ws.on_audio_channel_closed_=[&](uint32_t){++closed;};
 auto old_callback=ws.websocket_->disconnected;
 std::promise<void> entered,release;auto wait=release.get_future().share();send_hook=[&]{entered.set_value();wait.wait();};
 auto packet=std::make_unique<AudioStreamPacket>();packet->payload.resize(1920);packet->uplink_epoch=first_token;
 auto sending=std::async(std::launch::async,[&]{return ws.SendAudio(std::move(packet));});entered.get_future().wait();
 const auto resets=audio.resets,flushes=audio.flushes;
 auto callback=std::async(std::launch::async,[&]{old_callback();});
 const bool nonblocking=callback.wait_for(std::chrono::milliseconds(100))==std::future_status::ready;
 if(!nonblocking){release.set_value();sending.get();callback.get();assert(nonblocking&&"registered disconnect callback waited for dispatch-owned Send");}
 callback.get();assert(!audio.gate.Allows(first_token)&&audio.resets==resets&&audio.flushes==flushes&&closed==0&&ws.playback_connection_==first_connection);
 assert(audio.wakes);auto poll=std::async(std::launch::async,[&]{ws.PollC3PcmDisconnect();});
 assert(poll.wait_for(std::chrono::milliseconds(30))==std::future_status::timeout);release.set_value();assert(sending.get());poll.get();send_hook={};
 assert(!ws.websocket_&&closed==1&&audio.resets==resets+1&&audio.flushes==flushes+1&&!audio.gate.Capture());
 Open(ws);const auto newer=audio.gate.Capture();const auto wakes=audio.wakes;old_callback();ws.PollC3PcmDisconnect();
 assert(audio.gate.Allows(newer)&&ws.websocket_&&closed==1&&audio.wakes==wakes);
 auto pending=ws.websocket_->disconnected;pending();assert(!audio.gate.Capture());
 ws.CloseAudioChannel();Open(ws);const auto last=audio.gate.Capture();const auto closed_after_owner=closed;
 ws.PollC3PcmDisconnect();assert(audio.gate.Allows(last)&&ws.websocket_&&closed==closed_after_owner);
 ws.CloseAudioChannel();
}
static void ReplacementAndDestruction(){
 auto& audio=Application::GetInstance().audio;
 {auto ws=std::make_unique<WebsocketProtocol>();Open(*ws);ws->websocket_->disconnected();assert(!audio.gate.Capture());const auto wake=audio.wakes;
  ws.reset();assert(audio.wakes==wake);}
 {WebsocketProtocol replacement;Open(replacement);const auto token=audio.gate.Capture();const auto wakes=audio.wakes;
  replacement.PollC3PcmDisconnect();assert(audio.gate.Allows(token)&&replacement.websocket_&&audio.wakes==wakes);replacement.CloseAudioChannel();}
}
int main(){DisconnectWhileSending();ReplacementAndDestruction();}
