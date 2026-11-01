#include "button_uplink.h"
#include "playback_progress.h"
#include <cassert>
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
struct GateOwner {void WakeC3PcmOwner(){}family_robot::ButtonUplink gate;unsigned faults=0;void RequestC3PcmFault(uint32_t epoch){gate.Revoke(epoch);++faults;}auto& C3Uplink(){return gate;}};
struct Application {static Application& GetInstance(){static Application app;return app;}GateOwner audio;auto& GetAudioService(){return audio;}};
struct Result {bool ok=true;explicit operator bool()const{return ok;}Result error()const{return *this;}std::string ToString()const{return "fake";}};
static int creates=0,sends=0,connects=0;static size_t sent_bytes=0;
struct WebSocket {std::function<void(const char*,size_t,bool)> data;std::function<void()> disconnected;
 bool IsConnected()const{return true;}void SetHeader(const char*,const char*){}
 void OnData(std::function<void(const char*,size_t,bool)> fn){data=std::move(fn);}void OnDisconnected(std::function<void()> fn){disconnected=std::move(fn);}
 Result Connect(const char*){++connects;return {};}
 bool Send(const void*,size_t n,bool){++sends;sent_bytes=n;return true;}};
struct Network {std::unique_ptr<WebSocket>CreateWebSocket(int){++creates;return std::make_unique<WebSocket>();}};
struct Codec {bool PlaybackSupported(){return true;}};
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
 void RequestC3PcmDisconnect(uint32_t);void PollC3PcmDisconnect();
 std::mutex playback_mutex_;std::chrono::steady_clock::time_point last_incoming_time_;
 std::function<void()> on_audio_channel_opened_;std::function<void(const cJSON*)> on_incoming_json_;
 bool ResetPlaybackConnection(){++playback_connection_;return true;}bool PlaybackNegotiated()const{return negotiated;}
 bool IsTimeout()const{return false;}void SetError(const char*,const std::string& ={}){error_occurred_=true;}
 void NotifyPlaybackClosed(uint32_t){}void DeliverC3PcmAudio(const uint8_t*,size_t,uint32_t){}
 void ParseServerHello(const cJSON*,uint32_t){}bool HandlePlaybackTts(cJSON*,uint32_t){return true;}
 bool SendText(const std::string&){return true;}void AddTextFontCapabilities(cJSON*){}
 bool OpenAudioChannel();bool IsAudioChannelOpened()const;bool SendAudio(std::unique_ptr<AudioStreamPacket>);std::string GetHelloMessage();
};
// PRODUCTION_WS_METHODS
int main(){
 for(auto url:{"wss://example/x","https://example/x","WS://example/x","file://x"}){Settings::url=url;WebsocketProtocol ws;assert(!ws.OpenAudioChannel());assert(creates==0&&connects==0);}
 Settings::url="ws://example/x";Settings::version=2;{WebsocketProtocol ws;assert(!ws.OpenAudioChannel()&&creates==0);}
 Settings::version=1;{WebsocketProtocol ws;assert(!ws.OpenAudioChannel()&&creates==1&&connects==1);assert(!ws.IsAudioChannelOpened());}
 {WebsocketProtocol ws;ws.negotiated=true;assert(ws.OpenAudioChannel()&&ws.IsAudioChannelOpened());auto& gate=Application::GetInstance().audio.gate;assert(gate.Open(0,[]{}));
  for(size_t bytes:{1919u,1921u,1920u}){if(!gate.Capture()){gate.Close([]{});assert(gate.Open(gate.Revision(),[]{}));}auto packet=std::make_unique<AudioStreamPacket>();packet->payload.resize(bytes);packet->uplink_epoch=gate.Capture();const bool ok=ws.SendAudio(std::move(packet));assert(ok==(bytes==1920));}
  assert(sends==1&&sent_bytes==1920&&hello_checks==2&&Application::GetInstance().audio.faults==2);}
}
