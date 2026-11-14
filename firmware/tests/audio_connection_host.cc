// Only platform facilities are doubled. Open/Send/Hello/Reset/Close methods are
// copied verbatim from the prepared production sources by test_audio_connection.
#include <algorithm>
#include <arpa/inet.h>
#include <cassert>
#include <chrono>
#include <cmath>
#include <cstdarg>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <expected>
#include <functional>
#include <map>
#include <memory>
#include <mutex>
#include <string>
#include <vector>
int64_t trace_test_us=1000000;
static std::vector<std::string> logs;
void trace_test_log(const char* tag,const char* format,...) {
    if(std::strcmp(tag,"ConnectionTrace"))return;
    char line[512];va_list args;va_start(args,format);vsnprintf(line,sizeof(line),format,args);va_end(args);logs.emplace_back(line);
}
#include "connection_trace.h"
#define TAG "Host"
#define CONFIG_STOPWATCH_SOFTWARE_AEC 1
#define OPUS_FRAME_DURATION_MS 60
// EVENT_CONSTANTS
using EventBits_t=uint32_t;
struct EventGroup {EventBits_t bits=0;int elapsed=0;std::function<void()> during_wait,after_wait;};
static EventBits_t xEventGroupSetBits(EventGroup* g,EventBits_t bits){return g->bits|=bits;}
static EventBits_t xEventGroupClearBits(EventGroup* g,EventBits_t bits){auto old=g->bits;g->bits&=~bits;return old;}
static EventBits_t xEventGroupGetBits(EventGroup* g){return g->bits;}
constexpr bool pdTRUE=true,pdFALSE=false;
static int pdMS_TO_TICKS(int n){return n;}
static EventBits_t xEventGroupWaitBits(EventGroup* g,EventBits_t bits,bool clear,bool,int timeout){
    if(g->during_wait){auto callback=std::move(g->during_wait);callback();}
    auto observed=g->bits;
    if(!(observed&bits))g->elapsed+=timeout;
    if(clear)g->bits&=~bits;
    if(g->after_wait){auto callback=std::move(g->after_wait);callback();}
    return observed;
}
#include "cJSON.h"
static std::string hello(const char* variant="valid") {
    std::string scenario=variant;auto* r=cJSON_CreateObject();cJSON_AddStringToObject(r,"type","hello");
    cJSON_AddNumberToObject(r,"version",scenario=="bad-version"?2:1);
    if(scenario!="missing-transport")cJSON_AddStringToObject(r,"transport",scenario=="bad-transport"?"udp":"websocket");
    if(scenario!="missing-session")cJSON_AddStringToObject(r,"session_id","synthetic-session");
    auto* audio=cJSON_CreateObject();
    cJSON_AddStringToObject(audio,"format",scenario=="bad-format"?"pcm_s16le":"opus");
    cJSON_AddNumberToObject(audio,"channels",scenario=="bad-channels"?2:1);
    cJSON_AddNumberToObject(audio,"sample_rate",scenario=="bad-rate"?16000:24000);
    cJSON_AddNumberToObject(audio,"frame_duration",scenario=="bad-duration"?20:60);
    cJSON_AddItemToObject(r,"audio_params",audio);
    auto* caps=cJSON_CreateObject();
    if(scenario!="missing-progress")cJSON_AddBoolToObject(caps,"playback_progress_v1",true);
    if(scenario!="missing-drain")cJSON_AddBoolToObject(caps,"playback_drain_v1",true);
    cJSON_AddItemToObject(r,"features",caps);
    char* encoded=cJSON_PrintUnformatted(r);std::string result=encoded;cJSON_free(encoded);cJSON_Delete(r);return result;
}
struct Identity {uint32_t connection=0,playback=0;};
namespace family_robot {static bool PlaybackSessionSupported(const char*,size_t n){return n>0&&n<=64;}}
class AudioCodec {public:
    // CODEC_API
    enum class PlaybackOp{Connect};
    virtual ~AudioCodec()=default;
    bool ManagedCapture()const{return managed;}
    bool PlaybackSupported()const{return supported;}
    void PlaybackControl(PlaybackOp,Identity){}
    bool managed=true,supported=true;
};
struct TestCodec:AudioCodec {bool allowed=true;int preparations=0;
    // The method deliberately has no override marker so baseline runs RED on
    // the missing invocation rather than a missing declaration compiler error.
    bool PrepareAudioConnection(){++preparations;return allowed;}
};
struct NetworkError {int code=1,native=-7;};
struct WebSocket {
    std::function<void(const char*,size_t,bool)> data;std::function<void()> close;
    std::map<std::string,std::string> headers;bool connected=true;
    static inline std::function<void(WebSocket&)> send_hook;
    static inline bool connect_allowed=true,send_allowed=true;
    static inline int creates=0,connects=0;
    WebSocket(){++creates;}
    void SetHeader(const char* k,const char* v){headers[k]=v;}
    void OnData(std::function<void(const char*,size_t,bool)> cb){data=std::move(cb);}
    void OnDisconnected(std::function<void()> cb){close=std::move(cb);}
    std::expected<void,NetworkError> Connect(const char*){++connects;connected=connect_allowed;if(!connected)return std::unexpected(NetworkError{});return {};}
    bool IsConnected()const{return connected;}
    bool Send(const std::string& message){
        auto* r=cJSON_Parse(message.c_str());assert(r);
        assert(std::string(cJSON_GetObjectItem(r,"type")->valuestring)=="hello");
        assert(cJSON_IsTrue(cJSON_GetObjectItem(cJSON_GetObjectItem(r,"features"),"playback_progress_v1")));
        assert(cJSON_IsTrue(cJSON_GetObjectItem(cJSON_GetObjectItem(r,"features"),"playback_drain_v1")));
        cJSON_Delete(r);if(send_hook)send_hook(*this);return send_allowed;
    }
    void Hello(const char* value="valid"){auto payload=hello(value);data(payload.data(),payload.size(),false);}
    void Drop(){connected=false;close();}
};
struct NativeNetwork {std::unique_ptr<WebSocket> CreateWebSocket(int){return std::make_unique<WebSocket>();}};
namespace ConnectionDiagnostics {
// The TCP/TLS decorator has its own real-SDK tests; this seam is the platform
// socket factory so every application protocol branch remains production code.
struct Network {NativeNetwork* native;explicit Network(NativeNetwork* n):native(n){}void SetAttempt(Attempt){}std::unique_ptr<WebSocket> CreateWebSocket(int id){return native->CreateWebSocket(id);}};
}
class Board {public:TestCodec codec;NativeNetwork network;static Board& GetInstance(){static Board b;return b;}
    AudioCodec* GetAudioCodec(){return &codec;}NativeNetwork* GetNetwork(){return &network;}std::string GetUuid(){return "synthetic-client";}};
struct Service {int resets=0;bool preserve=false;void ResetDecoder(){++resets;}void PreserveServerPlayback(bool b){preserve=b;}};
struct Application {Service audio;static Application& GetInstance(){static Application a;return a;}Service& GetAudioService(){return audio;}};
struct Settings {Settings(const char*,bool){}std::string GetString(const char* k){return std::string(k)=="token"?"synthetic-token":"wss://synthetic.invalid/voice";}int GetInt(const char*){return 1;}};
struct SystemInfo {static std::string GetMacAddress(){return "synthetic-device";}};
namespace Lang::Strings {const char* SERVER_ERROR="server-error";const char* SERVER_NOT_CONNECTED="server-not-connected";const char* SERVER_TIMEOUT="server-timeout";}
struct AudioStreamPacket {int sample_rate=0,frame_duration=0;uint32_t timestamp=0;std::vector<uint8_t> payload;};
struct BinaryProtocol2 {uint16_t version,type,reserved;uint32_t timestamp,payload_size;uint8_t payload[1];};
struct BinaryProtocol3 {uint8_t type,reserved;uint16_t payload_size;uint8_t payload[1];};
class WebsocketProtocol {public:
    EventGroup events;EventGroup* event_group_handle_=&events;
    std::unique_ptr<ConnectionDiagnostics::Network> connection_trace_network_;std::unique_ptr<WebSocket> websocket_;
    mutable std::mutex playback_mutex_;uint32_t playback_connection_=0,socket_connection_=0,hello_waiting_connection_=0;Identity playback_id_{};uint64_t playback_first_=0;
    bool playback_negotiated_=false,playback_drain_negotiated_=false,playback_receiving_=false,playback_tracking_=false,error_occurred_=false;
    int version_=1,server_sample_rate_=24000,server_frame_duration_=60,opened=0;std::string session_id_,error,detail;
    std::chrono::steady_clock::time_point last_incoming_time_;
    std::function<void(std::unique_ptr<AudioStreamPacket>)> on_incoming_audio_;std::function<void(const cJSON*)> on_incoming_json_;
    std::function<void()> on_audio_channel_opened_=[this]{++opened;};std::function<void(uint32_t)> on_audio_channel_closed_;
    void SetError(const std::string& s,const std::string& d=""){error_occurred_=true;error=s;detail=d;}
    bool OpenAudioChannel();bool SendText(const std::string&);std::string GetHelloMessage();
    bool ResetPlaybackConnection(uint32_t expected=0,uint32_t* closed_epoch=nullptr);uint32_t AudioChannelEpoch()const;
    void ParseServerHello(const cJSON*,uint32_t);void RejectServerHelloLocked();void NotifyPlaybackClosed(uint32_t);
    bool PlaybackNegotiated(){std::lock_guard<std::mutex> lock(playback_mutex_);return playback_negotiated_;}
    void DeliverAudio(std::unique_ptr<AudioStreamPacket>,uint32_t){}bool HandlePlaybackTts(cJSON*,uint32_t){return true;}
    static void AddTextFontCapabilities(cJSON*){}
};
// PRODUCTION_PROTOCOL
static bool stage(const char* name){for(auto& s:logs)if(s.find(std::string("stage=")+name+" ")!=std::string::npos)return true;return false;}
static void reset(){Board::GetInstance().codec=TestCodec{};WebSocket::send_hook={};WebSocket::creates=WebSocket::connects=0;WebSocket::connect_allowed=WebSocket::send_allowed=true;logs.clear();}
static int failures=0;
static void check(bool value,const char* message){if(!value){fprintf(stderr,"%s\n",message);++failures;}}
int main(){
    reset();{
        WebsocketProtocol ws;Board::GetInstance().codec.allowed=false;
        check(!ws.OpenAudioChannel(),"audio rejection must refuse opening");
        check(WebSocket::creates==0&&WebSocket::connects==0,"audio rejection must precede socket creation");
        check(ws.error=="音频异常"&&ws.detail=="请稍后重试，持续异常时重启"&&ws.events.elapsed==0,"audio rejection must report audio failure immediately");
    }
    reset();{
        WebsocketProtocol ws;WebSocket::send_hook=[](auto& s){s.Hello();};
        check(ws.OpenAudioChannel(),"valid hello opens channel");
        check(Board::GetInstance().codec.preparations==1,"audio preparation must precede normal connection");
        check(ws.opened==1&&ws.events.elapsed==0&&ws.PlaybackNegotiated(),"valid hello negotiated once");
        check(ws.websocket_->headers["Authorization"]=="Bearer synthetic-token","authorization preserved");
        check(stage("hello_ready")&&!stage("hello_failed"),"valid hello trace");
        ws.websocket_->data("{",1,false);
        check(!(ws.events.bits&2),"post-handshake malformed message does not change accepted handshake");
    }
    for(const char* invalid:{"bad-transport","missing-transport","missing-progress","missing-drain","missing-session","bad-version","bad-format","bad-rate","bad-channels","bad-duration"}){
        reset();WebsocketProtocol ws;WebSocket::send_hook=[invalid](auto& s){s.Hello(invalid);s.Hello();};
        check(!ws.OpenAudioChannel(),"invalid hello must remain failed even after late valid hello");
        check(ws.events.elapsed==0&&ws.opened==0&&!stage("hello_ready"),"invalid hello must fail immediately without ready");
        check(stage("hello_failed")&&!stage("hello_timeout"),"known failure cannot be diagnosed as timeout");
    }
    for(auto payload:{std::string("{\"type\":\"hello\","),std::string("{}"),std::string("{\"type\":2}"),std::string("{\"type\":\"error\"}"),hello()+"trailing"}){
        reset();WebsocketProtocol ws;WebSocket::send_hook=[&](auto& socket){socket.data(payload.data(),payload.size(),false);};
        check(!ws.OpenAudioChannel()&&ws.opened==0&&ws.events.elapsed==0,"invalid raw handshake frame must fail immediately");
        check(stage("hello_failed")&&!stage("hello_timeout")&&!stage("hello_ready"),"raw frame rejection cannot become timeout or ready");
    }
    reset();{
        WebsocketProtocol ws;WebSocket::send_hook=[](auto& socket){auto payload=hello()+" \r\n\t";socket.data(payload.data(),payload.size(),false);};
        check(ws.OpenAudioChannel()&&ws.events.elapsed==0,"valid hello permits trailing JSON whitespace");
    }
    for(const char* following:{"{\"type\":\"mcp\"}","{\"type\":\"llm\"}","{\"type\":\"tts\"}","{"}){
        reset();WebsocketProtocol ws;WebSocket::send_hook=[following](auto& socket){socket.Hello();socket.data(following,strlen(following),false);};
        check(ws.OpenAudioChannel()&&ws.opened==1&&ws.events.elapsed==0,"valid hello followed immediately by session frame must open");
        check(stage("hello_ready")&&!stage("hello_failed"),"session frame cannot reject already validated hello");
    }
    reset();{
        WebsocketProtocol ws;WebSocket::send_hook=[](auto& socket){socket.Hello();socket.data("{",1,false);socket.Drop();};
        check(!ws.OpenAudioChannel()&&ws.opened==0&&ws.events.elapsed==0,"close after valid hello and malformed session frame still fails opening");
        check(stage("hello_failed")&&!stage("hello_ready"),"close remains terminal after leaving strict handshake parsing");
    }
    reset();{
        WebsocketProtocol ws;ws.events.during_wait=[&]{ws.websocket_->Drop();};
        check(!ws.OpenAudioChannel()&&ws.events.elapsed==0&&ws.opened==0,"server close must wake hello wait immediately");
        check(stage("hello_failed")&&!stage("hello_timeout"),"close cannot become timeout");
    }
    reset();{
        WebsocketProtocol ws;WebSocket::send_hook=[](auto& s){s.Hello();s.Drop();};
        check(!ws.OpenAudioChannel()&&ws.opened==0&&!stage("hello_ready"),"close after hello must defeat ready");
    }
    reset();{
        WebsocketProtocol ws;WebSocket::send_hook=[](auto& s){s.Hello();};ws.events.after_wait=[&]{ws.websocket_->Drop();};
        check(!ws.OpenAudioChannel()&&ws.opened==0&&!stage("hello_ready"),"post-wait close must defeat stale success bits");
    }
    reset();{
        WebsocketProtocol ws;
        check(!ws.OpenAudioChannel()&&ws.events.elapsed==10000,"no response retains 10 second limit");
        check(ws.error==Lang::Strings::SERVER_TIMEOUT&&stage("hello_timeout")&&!stage("hello_ready"),"silence reports timeout");
        // Save callbacks as values. Replaced socket object can now be destroyed.
        auto old_data=ws.websocket_->data;auto old_close=ws.websocket_->close;
        ws.events.elapsed=0;logs.clear();
        ws.events.during_wait=[&]{auto epoch=ws.AudioChannelEpoch();old_close();old_data("{",1,false);
            for(auto* variant:{"bad-transport","valid"}){auto payload=hello(variant);old_data(payload.data(),payload.size(),false);}
            check(ws.events.bits==0&&ws.AudioChannelEpoch()==epoch&&ws.session_id_.empty(),"old callbacks cannot mutate current wait or session");
            ws.websocket_->Hello();};
        check(ws.OpenAudioChannel()&&ws.opened==1&&ws.events.elapsed==0,"old callbacks must not fail or open new wait");
        check(stage("hello_ready")&&!stage("hello_failed"),"old failure trace must not claim current failure");
    }
    reset();{
        WebsocketProtocol ws;Board::GetInstance().codec.managed=false;
        WebSocket::send_hook=[](auto& s){s.Hello("missing-drain");};
        check(ws.OpenAudioChannel(),"unmanaged codec retains legacy hello policy");
    }
    reset();{
        WebsocketProtocol ws;WebSocket::connect_allowed=false;
        check(!ws.OpenAudioChannel()&&ws.events.elapsed==0&&ws.error==Lang::Strings::SERVER_NOT_CONNECTED,"network rejection keeps existing error and no extra wait");
    }
    reset();{
        WebsocketProtocol ws;WebSocket::send_allowed=false;
        check(!ws.OpenAudioChannel()&&ws.events.elapsed==0&&ws.error==Lang::Strings::SERVER_ERROR,"hello send failure keeps existing error");
    }
    if(failures)return 1;
    puts("actual prepared audio connection/hello failures passed");
}
