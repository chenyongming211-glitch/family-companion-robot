#pragma once
#include "connection_trace.h"
#include "network_interface.h"
#include <utility>

namespace ConnectionDiagnostics {
// A socket keeps a value copy of its attempt. Late receive/close callbacks must
// not consult the recorder after a newer button has started another attempt.
class Transport final:public Tcp {
    Attempt attempt_;
    bool tls_,request_seen_=false,response_seen_=false;
    std::string response_prefix_;
    std::unique_ptr<Tcp> inner_; // Destroy first: callbacks can still observe fields above.
public:
    Transport(std::unique_ptr<Tcp> inner,Attempt attempt,bool tls)
        :attempt_(attempt),tls_(tls),inner_(std::move(inner)){}
    NetworkResult<> Connect(const std::string& host,int port)override {
        Mark(attempt_,tls_?Stage::TlsBegin:Stage::TcpBegin);
        auto result=inner_->Connect(host,port);connected_=static_cast<bool>(result);
        if(result)Mark(attempt_,tls_?Stage::TlsReady:Stage::TcpReady);
        else Mark(attempt_,tls_?Stage::TlsFailed:Stage::TcpFailed,static_cast<int>(result.error().code),result.error().native);
        return result;
    }
    void Disconnect()override{connected_=false;inner_->Disconnect();}
    int Send(const std::string& data)override {
        const bool upgrade=!request_seen_&&data.compare(0,4,"GET ")==0;
        if(upgrade){request_seen_=true;Mark(attempt_,Stage::WsSend);}
        const auto result=inner_->Send(data);
        if(upgrade&&result<0)Mark(attempt_,Stage::WsSendFailed,result);
        return result;
    }
    void OnStream(std::function<void(const std::string&)> callback)override {
        inner_->OnStream([this,callback=std::move(callback)](const std::string& data){
            if(!response_seen_){
                const size_t count=data.size()<64-response_prefix_.size()?data.size():64-response_prefix_.size();
                response_prefix_.append(data.data(),count);
                if(response_prefix_.size()>=12){
                    response_seen_=true;int status=0;
                    if((response_prefix_.compare(0,9,"HTTP/1.1 ")==0||response_prefix_.compare(0,9,"HTTP/1.0 ")==0)&&
                       response_prefix_[9]>='1'&&response_prefix_[9]<='5'&&
                       response_prefix_[10]>='0'&&response_prefix_[10]<='9'&&response_prefix_[11]>='0'&&response_prefix_[11]<='9')
                        status=(response_prefix_[9]-'0')*100+(response_prefix_[10]-'0')*10+response_prefix_[11]-'0';
                    Mark(attempt_,Stage::WsHttp,status);response_prefix_.clear();
                }
            }
            if(callback)callback(data);
        });
    }
    void OnDisconnected(std::function<void()> callback)override {
        inner_->OnDisconnected([this,callback=std::move(callback)]{
            connected_=false;Mark(attempt_,Stage::SocketClosed);if(callback)callback();
        });
    }
};
// Preserve the existing network implementation and all non-voice factories.
// Only the voice WebSocket's TCP/TLS seam is decorated, with no vendor fork.
class Network final:public NetworkInterface {
    NetworkInterface* inner_;
    Attempt attempt_;
public:
    explicit Network(NetworkInterface* inner):inner_(inner){}
    void SetAttempt(Attempt attempt){attempt_=attempt;}
    std::unique_ptr<Http> CreateHttp(int id=-1)override{return inner_->CreateHttp(id);}
    std::unique_ptr<Udp> CreateUdp(int id=-1)override{return inner_->CreateUdp(id);}
    std::unique_ptr<Mqtt> CreateMqtt(int id=-1)override{return inner_->CreateMqtt(id);}
    std::unique_ptr<Tcp> CreateTcp(int id=-1)override{
        auto tcp=inner_->CreateTcp(id);if(!tcp)return nullptr;
        return std::make_unique<Transport>(std::move(tcp),attempt_,false);
    }
    std::unique_ptr<Tcp> CreateSsl(int id=-1)override{
        auto tcp=inner_->CreateSsl(id);if(!tcp)return nullptr;
        return std::make_unique<Transport>(std::move(tcp),attempt_,true);
    }
    std::unique_ptr<WebSocket> CreateWebSocket(int id=-1)override{return std::make_unique<WebSocket>(this,id);}
};
}
