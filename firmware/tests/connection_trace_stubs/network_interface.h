#pragma once
#include <expected>
#include <functional>
#include <memory>
#include <string>
struct NetworkError { int code=5, native=-76; };
template<class T=void> using NetworkResult=std::expected<T,NetworkError>;
class Tcp {
protected:
    bool connected_=false;
public:
    virtual ~Tcp()=default;
    virtual NetworkResult<> Connect(const std::string&,int)=0;
    virtual void Disconnect()=0;
    virtual int Send(const std::string&)=0;
    virtual void OnStream(std::function<void(const std::string&)>)=0;
    virtual void OnDisconnected(std::function<void()>)=0;
    bool connected()const{return connected_;}
};
struct Http{}; struct Udp{}; struct Mqtt{};
class NetworkInterface;
struct WebSocket { NetworkInterface* network; int id; WebSocket(NetworkInterface* n,int i):network(n),id(i){} };
class NetworkInterface {
public:
    virtual ~NetworkInterface()=default;
    virtual std::unique_ptr<Http> CreateHttp(int=-1)=0;
    virtual std::unique_ptr<Tcp> CreateTcp(int=-1)=0;
    virtual std::unique_ptr<Tcp> CreateSsl(int=-1)=0;
    virtual std::unique_ptr<Udp> CreateUdp(int=-1)=0;
    virtual std::unique_ptr<Mqtt> CreateMqtt(int=-1)=0;
    virtual std::unique_ptr<WebSocket> CreateWebSocket(int=-1)=0;
};
