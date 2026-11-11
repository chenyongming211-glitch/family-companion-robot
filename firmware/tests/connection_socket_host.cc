// Compile the unchanged, pinned SDK WebSocket implementation. Only the TLS
// driver and RTOS waits are fixtures; handshake/parser/reconnect code is real.
#include <cassert>
#include <cstdarg>
#include <cstdio>
#include <cstring>
#include <string>
#include <vector>
#include "connection_trace_network.h"
int64_t trace_test_us=1000000;
static std::vector<std::string> logs;
void trace_test_log(const char* tag,const char* format,...) {
    if(std::strcmp(tag,"ConnectionTrace"))return;
    char text[512];va_list args;va_start(args,format);vsnprintf(text,sizeof(text),format,args);va_end(args);logs.emplace_back(text);
}
std::string NetworkError::ToString()const{return "fixture-driver-error";}
enum class Failure{None,Tls,Unauthorized,Unavailable,Silent,Transmit};
struct Driver:Tcp {
    Failure failure;bool sent=false;
    explicit Driver(Failure f):failure(f){}
    NetworkResult<> Connect(const std::string&,int)override{
        trace_test_us+=25000;
        if(failure==Failure::Tls)return Fail(NetworkError::TlsFailed(-76));
        connected_=true;return {};
    }
    void Disconnect()override{connected_=false;if(disconnect_callback_)disconnect_callback_();}
    int Send(const std::string& data)override{
        if(!sent){
            sent=true;
            if(failure==Failure::Transmit)return -9;
            const char* response=failure==Failure::Unauthorized?"HTTP/1.1 401 Denied\r\n\r\n":
                failure==Failure::Unavailable?"HTTP/1.1 503 Unavailable\r\n\r\n":
                failure==Failure::Silent?"":"HTTP/1.1 101 Switching Protocols\r\n\r\n";
            if(stream_callback_&&response[0]){
                stream_callback_(std::string(response,7));stream_callback_(std::string(response+7));
            }
        }
        return static_cast<int>(data.size());
    }
    void Drop(){connected_=false;if(disconnect_callback_)disconnect_callback_();}
    void ServerClose(){if(stream_callback_)stream_callback_(std::string("\x88\x00",2));}
};
struct NativeNetwork:NetworkInterface {
    Failure failure=Failure::None;Driver* driver=nullptr;
    std::unique_ptr<Tcp> CreateSsl(int=-1)override{auto result=std::make_unique<Driver>(failure);driver=result.get();return result;}
    std::unique_ptr<Tcp> CreateTcp(int id=-1)override{return CreateSsl(id);}
    std::unique_ptr<Http> CreateHttp(int=-1)override{return std::make_unique<Http>();}
    std::unique_ptr<Udp> CreateUdp(int=-1)override{return std::make_unique<Udp>();}
    std::unique_ptr<Mqtt> CreateMqtt(int=-1)override{return std::make_unique<Mqtt>();}
    std::unique_ptr<WebSocket> CreateWebSocket(int id=-1)override{return std::make_unique<WebSocket>(this,id);}
};
static bool stage(const char* id,const char* name,int code=-999) {
    for(const auto& line:logs)
        if(line.find(std::string("id=")+id+" ")!=std::string::npos&&line.find(std::string("stage=")+name+" ")!=std::string::npos&&
           (code==-999||line.find("code="+std::to_string(code)+" ")!=std::string::npos))return true;
    return false;
}
int main(){
    using namespace ConnectionDiagnostics;
    NativeNetwork native;Network observed(&native);
    for(auto failure:{Failure::Tls,Failure::Unauthorized,Failure::Unavailable,Failure::Silent,Failure::Transmit}){
        const auto attempt=Begin(Stage::Button,1);observed.SetAttempt(attempt);native.failure=failure;
        auto socket=observed.CreateWebSocket(1);socket->SetHeader("Authorization","Bearer DO-NOT-LOG");
        const auto result=socket->Connect("wss://private-fixture/voice");assert(!result);
        if(failure==Failure::Tls){assert(result.error().native==-76&&stage(attempt.id,"tls_failed"));}
        if(failure==Failure::Unauthorized)assert(stage(attempt.id,"ws_http",401));
        if(failure==Failure::Unavailable)assert(stage(attempt.id,"ws_http",503));
        if(failure==Failure::Silent)assert(result.error().code==NetworkErrc::Timeout&&stage(attempt.id,"ws_send"));
        if(failure==Failure::Transmit)assert(stage(attempt.id,"ws_send_failed",-9));
        socket.reset();
        // Each failure must permit the next newly-created production socket.
        const auto retry=Begin(Stage::Button,1);observed.SetAttempt(retry);native.failure=Failure::None;
        auto recovered=observed.CreateWebSocket(1);assert(recovered->Connect("wss://private-fixture/voice"));
        assert(recovered->IsConnected()&&stage(retry.id,"ws_http",101));
        native.driver->Drop();assert(!recovered->IsConnected()&&stage(retry.id,"socket_closed"));
    }
    for(bool remote_close:{false,true}){
        const auto first=Begin(Stage::Button,1);observed.SetAttempt(first);native.failure=Failure::None;
        auto old=observed.CreateWebSocket(1);int closes=0;old->OnDisconnected([&]{++closes;});assert(old->Connect("wss://private-fixture/voice"));
        if(remote_close)native.driver->ServerClose();else native.driver->Drop();
        assert(closes==1&&!old->IsConnected());
        trace_test_us+=24LL*60*60*1000000; // Clock arithmetic only, not 24h field acceptance.
        const auto next=Begin(Stage::Button,1);observed.SetAttempt(next);
        auto fresh=observed.CreateWebSocket(1);assert(fresh->Connect("wss://private-fixture/voice"));
        assert(stage(next.id,"tls_ready")&&stage(next.id,"ws_http",101));
    }
    for(const auto& line:logs)for(const auto& secret:{"DO-NOT-LOG","Authorization","private-fixture","/voice"})assert(line.find(secret)==std::string::npos);
    puts("actual SDK handshake faults and reconnect passed");
}
