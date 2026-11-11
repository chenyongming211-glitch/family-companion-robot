#pragma once
#include <memory>
#include "tcp.h"
#include "web_socket.h"
struct Http{};struct Udp{};struct Mqtt{};
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
