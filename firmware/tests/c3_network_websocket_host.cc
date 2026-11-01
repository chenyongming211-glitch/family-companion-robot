// Compiles the verified SDK implementation (original for RED, generated for GREEN).
#include "web_socket.h"
#include "network_interface.h"
#include <atomic>
#include <chrono>
#include <cstdlib>
#include <iostream>
#include <new>
#include <stdexcept>
#include <string>
#include <thread>
#include <vector>
int test_event_deletes = 0;
static int bad_tcp_cleanup = 0;
static thread_local bool track_allocations = false;
static std::atomic<size_t> allocations{0};
void* operator new(size_t n) { if (track_allocations) ++allocations; if (auto p = std::malloc(n ? n : 1)) return p; throw std::bad_alloc(); }
__attribute__((noinline)) void operator delete(void* p) noexcept { std::free(p); }
__attribute__((noinline)) void operator delete(void* p, size_t) noexcept { std::free(p); }
std::string NetworkError::ToString() const { return "network-error"; }
NetworkError NetworkError::FromErrno(int n) { return {NetworkErrc::TransmitFailed,n}; }
NetworkError NetworkError::FromHErrno(int n) { return {NetworkErrc::DnsFailed,n}; }
static void Check(bool b, const char* text) { if (!b) throw std::runtime_error(text); }
struct FakeTcp : Tcp {
 ~FakeTcp() override { if (disconnects == 0 || events_at_disconnect != test_event_deletes) ++bad_tcp_cleanup; }
 std::string handshake = "HTTP/1.1 101 Switching Protocols\r\n\r\n";
 int send_result = -2;
 int handshake_result = -2;
 int writes = 0;
 int disconnects = 0;
 bool sent_handshake = false;
 int events_at_disconnect = -1;
 std::function<void()> before_send;
 NetworkResult<> Connect(const std::string&, int) override { connected_=true; return {}; }
 void Disconnect() override { ++disconnects; events_at_disconnect=test_event_deletes; connected_=false; if(disconnect_callback_) disconnect_callback_(); }
 int Send(const std::string& s) override {
  if (!sent_handshake) { sent_handshake=true; if(stream_callback_) for(size_t p=0;p<handshake.size();p+=1500) stream_callback_(handshake.substr(p,1500)); return handshake_result == -2 ? static_cast<int>(s.size()) : handshake_result; }
  ++writes;
  if (before_send) before_send();
  return send_result == -2 ? static_cast<int>(s.size()) : send_result;
 }
 void Emit(const std::string& s) { if(stream_callback_) stream_callback_(s); }
};
struct Network : NetworkInterface {
 FakeTcp* tcp=nullptr;
 std::string handshake = "HTTP/1.1 101 Switching Protocols\r\n\r\n";
 int handshake_result = -2;
 std::unique_ptr<Tcp> CreateTcp(int) override { auto p=std::make_unique<FakeTcp>(); tcp=p.get(); p->handshake=handshake; p->handshake_result=handshake_result; return p; }
 std::unique_ptr<Tcp> CreateSsl(int i) override { return CreateTcp(i); }
};
static std::string Frame(int op, size_t n, bool fin=true, bool mask=false, int encoding=0) {
 std::string s; s.push_back(static_cast<char>((fin?128:0)|op));
 if(encoding==127) { s.push_back(static_cast<char>((mask?128:0)|127)); for(int i=7;i>=0;--i) s.push_back(static_cast<char>((uint64_t(n)>>(i*8))&255)); }
 else if(n>=126 || encoding==126) { s.push_back(static_cast<char>((mask?128:0)|126)); s.push_back(static_cast<char>((n>>8)&255)); s.push_back(static_cast<char>(n&255)); }
 else s.push_back(static_cast<char>((mask?128:0)|n));
 if(mask) s.append(4,'\0'); s.append(n,'x'); return s;
}
struct Session {
 Network network; WebSocket ws{&network,0}; int delivered=0,errors=0,disconnected=0; size_t size=0; bool binary=false;
 Session() { ws.OnData([this](const char*,size_t n,bool b){++delivered;size=n;binary=b;}); ws.OnError([this](const NetworkError&){++errors;}); ws.OnDisconnected([this]{++disconnected;}); Check(bool(ws.Connect("ws://test:80/")),"connect"); }
 void Emit(const std::string& s) { for(size_t p=0;p<s.size();p+=1500) network.tcp->Emit(s.substr(p,1500)); }
};
int main() {
 int failed=0,passed=0;
 auto test=[&](const char* name,auto f){try{f();++passed;}catch(const std::exception& e){++failed;std::cerr<<"FAIL "<<name<<": "<<e.what()<<'\n';}};
 test("complete binary",[]{Session s;s.Emit(Frame(2,2880));Check(s.delivered==1&&s.size==2880&&s.binary,"valid PCM");});
 test("short binary",[]{Session s;s.Emit(Frame(2,2879));Check(s.errors==1&&s.delivered==0,"short binary rejected");});
 test("declared oversize rejected before body",[]{Session s;auto f=Frame(2,2881);s.network.tcp->Emit(f.substr(0,4));Check(s.errors==1,"early declared bound");});
 test("oversize text",[]{Session s;s.Emit(Frame(1,4097));Check(s.errors==1&&s.delivered==0,"text bound");});
 test("text at bound",[]{Session s;s.Emit(Frame(1,4096));Check(s.delivered==1&&s.size==4096,"text accepted");});
 test("fragment binary",[]{Session s;s.Emit(Frame(2,1400,false)+Frame(9,0)+Frame(0,1480));Check(s.delivered==1&&s.size==2880,"fragment binary");});
 test("fragment text at bound",[]{Session s;s.Emit(Frame(1,2048,false)+Frame(0,2048));Check(s.delivered==1&&s.size==4096&&!s.binary,"fragment text");});
 test("fragment total bound",[]{Session s;s.Emit(Frame(1,3000,false)+Frame(0,1097));Check(s.errors==1&&s.delivered==0,"cumulative bound");});
 test("short final fragment",[]{Session s;s.Emit(Frame(2,1400,false)+Frame(0,1479));Check(s.errors==1&&s.delivered==0,"exact fragmented PCM");});
 test("orphan continuation",[]{Session s;s.Emit(Frame(0,1));Check(s.errors==1&&s.delivered==0,"orphan");});
 test("nested fragments",[]{Session s;s.Emit(Frame(1,1,false)+Frame(1,1));Check(s.errors==1&&s.delivered==0,"nested");});
 test("masked server",[]{Session s;s.Emit(Frame(1,1,true,true));Check(s.errors==1,"mask rejected");});
 test("RSV",[]{Session s;auto f=Frame(1,1);f[0]|=0x40;s.Emit(f);Check(s.errors==1,"RSV rejected");});
 test("reserved opcode",[]{Session s;s.Emit(Frame(3,1));Check(s.errors==1,"opcode rejected");});
 test("fragmented control",[]{Session s;s.Emit(Frame(9,1,false));Check(s.errors==1,"control FIN");});
 test("oversized control",[]{Session s;s.Emit(Frame(9,126));Check(s.errors==1,"control size");});
 test("close one byte",[]{Session s;s.Emit(Frame(8,1));Check(s.errors==1,"close format");});
 test("nonminimal extended",[]{Session s;s.Emit(Frame(1,1,true,false,126));Check(s.errors==1,"nonminimal length");});
 test("127 length",[]{Session s;auto f=Frame(1,1,true,false,127);s.Emit(f);Check(s.errors==1,"malformed 64 bit length");});
 test("127 oversized declared length",[]{Session s;auto f=Frame(1,65536,true,false,127);s.network.tcp->Emit(f.substr(0,10));Check(s.errors==1,"64 bit bound before body");});
 test("127 high bit",[]{Session s;auto f=Frame(1,1,true,false,127);f[2]=static_cast<char>(128);s.network.tcp->Emit(f.substr(0,10));Check(s.errors==1,"64 bit sign bit");});
 test("partial and coalesced",[]{Session s;auto f=Frame(1,126)+Frame(1,3);for(char c:f)s.network.tcp->Emit(std::string(1,c));Check(s.delivered==2&&s.size==3,"partial parsing");});
 test("failure latch",[]{Session s;s.Emit(Frame(3,0));s.Emit(Frame(1,1));s.Emit(Frame(3,0));Check(s.errors==1&&s.disconnected==1&&s.delivered==0&&!s.ws.IsConnected(),"failure latch");Check(s.network.tcp->disconnects==0,"no receive-side disconnect");Check(!s.ws.Send("x"),"failed send");});
 test("close latch",[]{Session s;s.Emit(Frame(8,0)+Frame(1,1));Check(s.disconnected==1&&s.delivered==0&&!s.ws.IsConnected(),"close latch");});
 test("zero send",[]{Session s;s.network.tcp->send_result=0;Check(!s.ws.Send("x"),"zero rejected");});
 test("short send",[]{Session s;s.network.tcp->send_result=1;Check(!s.ws.Send("x"),"short rejected");});
 test("pong write failure",[]{Session s;s.network.tcp->send_result=0;s.Emit(Frame(9,0));Check(s.errors==1&&s.disconnected==1&&!s.ws.IsConnected(),"failed pong wakes owner");});
 test("pong callback opcode",[]{Session s;int pongs=0;s.ws.OnPong([&](const char*,size_t n){Check(n==3,"pong payload");++pongs;});s.Emit(Frame(9,0)+Frame(10,3));Check(pongs==1,"only actual pong callback");});
 test("ping coalesced with handshake",[]{Network n;n.handshake+=Frame(9,0);WebSocket w(&n,0);Check(bool(w.Connect("ws://test/"))&&n.tcp->writes==1,"coalesced ping answered");});
 test("handshake send failure latch",[]{Network n;n.handshake_result=0;WebSocket w(&n,0);int delivered=0;w.OnData([&](const char*,size_t,bool){++delivered;});Check(!w.Connect("ws://test/"),"short handshake send");n.tcp->Emit(Frame(1,1));Check(delivered==0&&!w.IsConnected(),"late data ignored after failed connect");});
 test("handshake timeout latch",[]{Network n;n.handshake.clear();WebSocket w(&n,0);int delivered=0;w.OnData([&](const char*,size_t,bool){++delivered;});Check(!w.Connect("ws://test/"),"handshake timeout");n.tcp->Emit("HTTP/1.1 101 Switching Protocols\r\n\r\n"+Frame(1,1));Check(delivered==0&&!w.IsConnected(),"late handshake ignored");});
 test("direct complete frame",[]{Session s;auto f=Frame(1,3);allocations=0;track_allocations=true;s.network.tcp->Emit(f);track_allocations=false;Check(s.delivered==1&&allocations==0,"complete receive no payload allocations");});
 test("serialize before frame allocation",[]{
  Session s;std::string payload(1920,'x');std::atomic<bool> first=false,release=false,second=false,done=false;
  s.network.tcp->before_send=[&]{if(!first.exchange(true))while(!release.load())std::this_thread::yield();};
  std::thread one([&]{s.ws.Send(payload.data(),payload.size(),true);});
  while(!first.load())std::this_thread::yield();
  allocations=0;
  std::thread two([&]{track_allocations=true;second=true;s.ws.Send(payload.data(),payload.size(),true);track_allocations=false;done=true;});
  while(!second.load())std::this_thread::yield();
  std::this_thread::sleep_for(std::chrono::milliseconds(20));
  const bool serialized=allocations.load()==0&&!done.load();
  release=true;one.join();two.join();
  Check(serialized&&allocations.load()==1,"second frame allocated only after send lock");
 });
 test("handshake oversize",[]{Network n;n.handshake="HTTP/1.1 101 "+std::string(2033,'x')+"\r\n\r\n";WebSocket w(&n,0);Check(!w.Connect("ws://test/"),"handshake cap");});
 test("handshake exactly at bound",[]{Network n;n.handshake="HTTP/1.1 101 "+std::string(2031,'x')+"\r\n\r\n";WebSocket w(&n,0);Check(bool(w.Connect("ws://test/")),"handshake exact cap");});
 test("unterminated handshake cap",[]{Network n;n.handshake="HTTP/1.1 101 "+std::string(2036,'x');WebSocket w(&n,0);Check(!w.Connect("ws://test/"),"unterminated handshake");});
 test("invalid handshake prefix",[]{Network n;n.handshake="bad HTTP/1.1 101 \r\n\r\n";WebSocket w(&n,0);Check(!w.Connect("ws://test/"),"prefix anchored");});
 test("success then failure in handshake chunk",[]{Network n;n.handshake+=Frame(3,0);WebSocket w(&n,0);Check(!w.Connect("ws://test/"),"failure beats handshake success");});
 test("destructor after failed connection",[]{Network n;int before=test_event_deletes;int bad_before=bad_tcp_cleanup;{WebSocket w(&n,0);Check(bool(w.Connect("ws://test/")),"connect");n.tcp->Emit(Frame(8,0));}Check(test_event_deletes==before+1,"event deleted"); Check(bad_tcp_cleanup==bad_before,"TCP stopped before event group delete");});
 std::cout<<"WebSocket: "<<passed<<" passed, "<<failed<<" failed\n";return failed?1:0;
}
