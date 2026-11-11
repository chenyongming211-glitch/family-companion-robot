#include <cassert>
#include <cstdarg>
#include <cstdint>
#include <cstdio>
#include <string>
#include <vector>
int64_t trace_test_us=1000000;
static std::vector<std::string> logs;
void trace_test_log(const char*, const char* format, ...) {
    char buffer[512];va_list args;va_start(args,format);vsnprintf(buffer,sizeof(buffer),format,args);va_end(args);
    logs.emplace_back(buffer);
}
#include "connection_trace_network.h"
struct FakeTcp:Tcp {
    bool fail_connect=false;int send_result=7, disconnects=0;
    std::string host,sent;int port=0;
    std::function<void(const std::string&)> stream;
    std::function<void()> close;
    NetworkResult<> Connect(const std::string& h,int p)override {host=h;port=p;trace_test_us+=25000;if(fail_connect)return std::unexpected(NetworkError{});connected_=true;return {};}
    void Disconnect()override{++disconnects;connected_=false;if(close)close();}
    int Send(const std::string& s)override{sent=s;return send_result;}
    void OnStream(std::function<void(const std::string&)> cb)override{stream=std::move(cb);}
    void OnDisconnected(std::function<void()> cb)override{close=std::move(cb);}
};
struct FakeNetwork:NetworkInterface {
    FakeTcp* last=nullptr;int id=0,http=0,udp=0,mqtt=0;
    std::unique_ptr<Tcp> CreateSsl(int i=-1)override{id=i;auto t=std::make_unique<FakeTcp>();last=t.get();return t;}
    std::unique_ptr<Tcp> CreateTcp(int i=-1)override{return CreateSsl(i);}
    std::unique_ptr<Http> CreateHttp(int i=-1)override{id=i;++http;return std::make_unique<Http>();}
    std::unique_ptr<Udp> CreateUdp(int i=-1)override{id=i;++udp;return std::make_unique<Udp>();}
    std::unique_ptr<Mqtt> CreateMqtt(int i=-1)override{id=i;++mqtt;return std::make_unique<Mqtt>();}
    std::unique_ptr<WebSocket> CreateWebSocket(int i=-1)override{return std::make_unique<WebSocket>(this,i);}
};
static bool has(const std::string& id,const std::string& stage) {
    for(const auto& line:logs)if(line.find("id="+id+" ")!=std::string::npos&&line.find("stage="+stage+" ")!=std::string::npos)return true;
    return false;
}
int main() {
    using namespace ConnectionDiagnostics;
    const auto first=Begin(Stage::Button,2);
    assert(std::string(first.id).size()==28&&std::string(first.id).substr(0,3)=="c1-");
    assert(has(first.id,"button"));
    FakeNetwork raw;Network network(&raw);network.SetAttempt(first);
    auto socket=network.CreateWebSocket(7);assert(socket->network==&network&&socket->id==7);
    auto old=network.CreateSsl(7);auto old_raw=raw.last;assert(raw.id==7);
    std::string received;int closed=0;
    old->OnStream([&](const std::string& s){received+=s;});old->OnDisconnected([&]{++closed;});
    assert(old->Connect("private-host",443));assert(old->connected());
    assert(old_raw->host=="private-host"&&old_raw->port==443&&has(first.id,"tls_ready"));
    const std::string request="GET /voice HTTP/1.1\r\nAuthorization: Bearer SECRET-DO-NOT-LOG\r\n\r\n";
    assert(old->Send(request)==7&&old_raw->sent==request&&has(first.id,"ws_send"));
    old_raw->stream("HTTP/1.");old_raw->stream("1 101 Switching Protocols\r\n\r\nPRIVATE-BODY");
    assert(received=="HTTP/1.1 101 Switching Protocols\r\n\r\nPRIVATE-BODY"&&has(first.id,"ws_http"));
    assert(logs.back().find("code=101 ")!=std::string::npos);
    const auto second=Begin(Stage::Button,2);network.SetAttempt(second);
    assert(std::string(first.id)!=second.id);
    old_raw->close();assert(closed==1&&!old->connected());
    assert(has(first.id,"socket_closed")&&!has(second.id,"socket_closed"));
    for(int code:{401,503}) {
        auto tcp=network.CreateSsl(1);auto native=raw.last;int calls=0;
        tcp->OnStream([&](const std::string&){++calls;});
        native->stream("HTTP/1.1 "+std::to_string(code)+" Denied\r\n\r\nSENSITIVE");
        assert(calls==1&&logs.back().find("code="+std::to_string(code)+" ")!=std::string::npos);
    }
    auto failure=network.CreateSsl(1);raw.last->fail_connect=true;
    auto result=failure->Connect("private-host",443);
    assert(!result&&result.error().native==-76&&has(second.id,"tls_failed"));
    raw.last->send_result=-9;assert(failure->Send(request)==-9&&has(second.id,"ws_send_failed"));
    assert(network.CreateHttp(8)&&raw.http==1&&raw.id==8);
    assert(network.CreateUdp(9)&&raw.udp==1&&raw.id==9);
    assert(network.CreateMqtt(10)&&raw.mqtt==1&&raw.id==10);
    auto plain=network.CreateTcp(3);assert(plain->Connect("private-host",80)&&has(second.id,"tcp_ready"));
    old->Disconnect();assert(closed==2&&old_raw->disconnects==1);
    for(const auto& line:logs)for(const auto& forbidden:{"SECRET","PRIVATE-BODY","SENSITIVE","private-host","Authorization","/voice"})assert(line.find(forbidden)==std::string::npos);
}
