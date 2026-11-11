#pragma once
// Connection observations only: authorization, timeouts and recovery stay with
// the application/transport. Never pass arbitrary strings or payloads here.
#include <cstdint>
#include <cstdio>
#include <mutex>
#include <esp_heap_caps.h>
#include <esp_log.h>
#include <esp_random.h>
#include <esp_timer.h>

namespace ConnectionDiagnostics {
enum class Stage {
    Button, StartButton, Wake, State, Continue, Open, ResetReady, ResetFailed,
    SocketCreate, SocketFailed, TlsBegin, TlsReady, TlsFailed, TcpBegin, TcpReady,
    TcpFailed, WsSend, WsSendFailed, WsHttp, Connected, ConnectFailed,
    HelloSent, HelloFailed, HelloReceived, HelloReady, HelloTimeout,
    Listening, SocketClosed, Recovery, RecoveryIdle
};
inline const char* Name(Stage stage) {
    switch(stage) {
#define CONNECTION_STAGE(value, name) case Stage::value:return name;
    CONNECTION_STAGE(Button,"button") CONNECTION_STAGE(StartButton,"start_button")
    CONNECTION_STAGE(Wake,"wake") CONNECTION_STAGE(State,"state")
    CONNECTION_STAGE(Continue,"continue") CONNECTION_STAGE(Open,"open")
    CONNECTION_STAGE(ResetReady,"reset_ready") CONNECTION_STAGE(ResetFailed,"reset_failed")
    CONNECTION_STAGE(SocketCreate,"socket_create") CONNECTION_STAGE(SocketFailed,"socket_failed")
    CONNECTION_STAGE(TlsBegin,"tls_begin") CONNECTION_STAGE(TlsReady,"tls_ready") CONNECTION_STAGE(TlsFailed,"tls_failed")
    CONNECTION_STAGE(TcpBegin,"tcp_begin") CONNECTION_STAGE(TcpReady,"tcp_ready") CONNECTION_STAGE(TcpFailed,"tcp_failed")
    CONNECTION_STAGE(WsSend,"ws_send") CONNECTION_STAGE(WsSendFailed,"ws_send_failed") CONNECTION_STAGE(WsHttp,"ws_http")
    CONNECTION_STAGE(Connected,"connected") CONNECTION_STAGE(ConnectFailed,"connect_failed")
    CONNECTION_STAGE(HelloSent,"hello_sent") CONNECTION_STAGE(HelloFailed,"hello_failed")
    CONNECTION_STAGE(HelloReceived,"hello_received") CONNECTION_STAGE(HelloReady,"hello_ready") CONNECTION_STAGE(HelloTimeout,"hello_timeout")
    CONNECTION_STAGE(Listening,"listening") CONNECTION_STAGE(SocketClosed,"socket_closed")
    CONNECTION_STAGE(Recovery,"recovery") CONNECTION_STAGE(RecoveryIdle,"recovery_idle")
#undef CONNECTION_STAGE
    }
    return "unknown";
}
struct Attempt { char id[29]{}; uint64_t started_ms=0; explicit operator bool()const{return id[0]!=0;} };
inline uint64_t Now() { return static_cast<uint64_t>(esp_timer_get_time()/1000); }
inline void Mark(const Attempt& attempt,Stage stage,int code=0,int native=0,int state=-1) {
    if(!attempt)return;
    const auto now=Now();
    ESP_LOGI("ConnectionTrace","id=%s stage=%s code=%d native=%d state=%d elapsed_ms=%llu internal=%u largest=%u",
        attempt.id,Name(stage),code,native,state,
        static_cast<unsigned long long>(now>=attempt.started_ms?now-attempt.started_ms:0),
        static_cast<unsigned>(heap_caps_get_free_size(MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT)),
        static_cast<unsigned>(heap_caps_get_largest_free_block(MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT)));
}
class Recorder {
    std::mutex mutex_;
    uint64_t boot_=(static_cast<uint64_t>(esp_random())<<32)|esp_random();
    uint32_t counter_=0;
    Attempt current_;
    bool current_used_by_socket_=false;
    Attempt BeginLocked() {
        current_used_by_socket_=false;
        if(counter_==UINT32_MAX){current_={};return {};} // Never reuse a saturated identifier.
        ++counter_;current_.started_ms=Now();
        snprintf(current_.id,sizeof(current_.id),"c1-%016llx-%08lx",
            static_cast<unsigned long long>(boot_),static_cast<unsigned long>(counter_));
        return current_;
    }
public:
    Attempt Begin() {
        std::lock_guard<std::mutex> lock(mutex_);
        return BeginLocked();
    }
    Attempt ForSocket() {
        std::lock_guard<std::mutex> lock(mutex_);
        // Consume an application-prepared ID once. A reopen after a remote
        // close must get a fresh ID even when no entry branch called Begin.
        if(!current_||current_used_by_socket_)BeginLocked();
        current_used_by_socket_=true;
        return current_;
    }
    Attempt Current(){std::lock_guard<std::mutex> lock(mutex_);return current_;}
};
inline Recorder& Records(){static Recorder recorder;return recorder;}
inline Attempt Current(){return Records().Current();}
inline Attempt Begin(Stage stage,int state=-1){auto attempt=Records().Begin();Mark(attempt,stage,0,0,state);return attempt;}
inline Attempt ForSocket(){return Records().ForSocket();}
inline Attempt Ensure(){auto attempt=Current();return attempt?attempt:Begin(Stage::Open);}
inline void Observe(Stage stage,int state=-1){Mark(Current(),stage,0,0,state);}
inline void User(Stage stage,int state,bool new_attempt){
    if(new_attempt||!Current())Begin(stage,state);else Observe(stage,state);
}
}
