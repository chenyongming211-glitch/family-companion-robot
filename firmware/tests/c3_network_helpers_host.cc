#include "c3_tcp_deadline.h"
#include "c3_websocket_bounds.h"
#include <chrono>
#include <fcntl.h>
#include <iostream>
#include <random>
#include <stdexcept>
#include <sys/select.h>
#include <sys/socket.h>
#include <unistd.h>
using namespace family::c3::network;
static void Check(bool b, const char* text) { if (!b) throw std::runtime_error(text); }
static int64_t Monotonic() {
    return std::chrono::duration_cast<std::chrono::microseconds>(
        std::chrono::steady_clock::now().time_since_epoch()).count();
}
int main() {
    int passed = 0, failed = 0;
    auto test = [&](const char* name, auto body) {
        try { body(); ++passed; }
        catch (const std::exception& e) { ++failed; std::cerr << "FAIL " << name << ": " << e.what() << '\n'; }
    };
    auto no_wait = [](int64_t) { return WaitResult{-1, EIO}; };
    test("empty write", [&] {
        int error; bool wrote = false;
        Check(SendAll(nullptr, 0, 1, [] { return 0; }, [&](const char*, size_t) {
            wrote = true; return WriteResult{0, 0}; }, no_wait, error) && !wrote, "empty write");
    });
    test("invalid budget and clock", [&] {
        int error; auto write = [](const char*, size_t n) { return WriteResult{static_cast<std::ptrdiff_t>(n), 0}; };
        Check(!SendAll("x", 1, 0, [] { return 0; }, write, no_wait, error) && error == EINVAL, "budget");
        Check(!SendAll("x", 1, 1, [] { return -1; }, write, no_wait, error), "clock failure");
        Check(!SendAll("x", 1, 2, [] { return INT64_MAX; }, write, no_wait, error), "deadline overflow");
    });
    test("clock failure during send", [&] {
        int error, calls=0;
        Check(!SendAll("x", 1, 10, [&] { return calls++ == 0 ? 0 : -1; },
            [](const char*,size_t) { return WriteResult{1,0}; }, no_wait, error) && error == EIO, "clock fails closed");
    });
    test("partial pointer and total", [&] {
        int error, writes = 0; int64_t clock = 0; const char* text = "abc";
        Check(SendAll(text, 3, 10, [&] { return clock; }, [&](const char* p, size_t n) {
            Check(p == text + writes && n == size_t(3-writes), "partial cursor");
            ++writes; ++clock; return WriteResult{1, 0}; }, no_wait, error) && writes == 3, "partial completion");
    });
    test("impossible write count", [&] {
        int error;
        Check(!SendAll("x", 1, 10, [] { return 0; }, [](const char*, size_t) {
            return WriteResult{2, 0}; }, no_wait, error) && error == EIO, "invalid result");
    });
    test("EINTR cannot extend deadline", [&] {
        int error, writes = 0; int64_t clock = 0;
        Check(!SendAll("x", 1, 10, [&] { return clock; }, [&](const char*, size_t) {
            ++writes; clock += 2; return WriteResult{-1, EINTR}; }, no_wait, error) &&
            error == ETIMEDOUT && writes == 5, "bounded EINTR");
    });
    test("wait remaining after partial", [&] {
        int error, writes = 0; int64_t clock = 0;
        Check(SendAll("xy", 2, 10, [&] { return clock; }, [&](const char*, size_t n) {
            ++writes; if (writes == 1) { clock = 3; return WriteResult{1, 0}; }
            if (writes == 2) return WriteResult{-1, EAGAIN};
            return WriteResult{static_cast<std::ptrdiff_t>(n), 0};
        }, [&](int64_t remaining) { Check(remaining == 7, "remaining budget"); ++clock; return WaitResult{1, 0}; }, error), "wait completion");
    });
    test("wait hard error", [&] {
        int error;
        Check(!SendAll("x", 1, 10, [] { return 0; }, [](const char*, size_t) {
            return WriteResult{-1, EAGAIN}; }, [](int64_t) { return WaitResult{-1, EBADF}; }, error) && error == EBADF, "wait error");
    });
    test("boundary complete is timeout", [&] {
        int error; int64_t clock = 0;
        Check(!SendAll("x", 1, 10, [&] { return clock; }, [&](const char*, size_t) {
            clock = 10; return WriteResult{1, 0}; }, no_wait, error) && error == ETIMEDOUT, "deadline inclusive");
    });
    test("receive raw cap before append", [] {
        std::string raw(kReceiveBytes, 'x'); bool h=true,f=false,b=false; std::vector<char> fragments; int errors=0;
        const size_t before = raw.size();
        Check(!Receive(raw,h,f,b,fragments,std::string(1,'x'),[]{},[](const char*,size_t,bool){},
            [](uint8_t,const char*,size_t){return true;},[&]{++errors;}) && raw.size()==before && errors==1, "raw bounded");
    });
    test("TCP chunk cap before append", [] {
        std::string raw; bool h=true,f=false,b=false; std::vector<char> fragments;
        Check(!Receive(raw,h,f,b,fragments,std::string(kTcpChunkBytes+1,'x'),[]{},[](const char*,size_t,bool){},
            [](uint8_t,const char*,size_t){return true;},[]{}) && raw.empty(), "chunk bounded");
    });
    test("direct borrowed bytes and lazy fragments", [] {
        std::string raw; raw.reserve(kReceiveBytes); bool h=true,f=false,b=false; std::vector<char> fragments;
        const size_t capacity=raw.capacity(); bool direct=false;
        Check(Receive(raw,h,f,b,fragments,std::string("\x81\x03" "abc",5),[]{},[&](const char* p,size_t n,bool){
            direct=p==raw.data()+2&&n==3;
        },[](uint8_t,const char*,size_t){return true;},[]{}), "direct frame");
        Check(direct&&fragments.capacity()==0&&raw.capacity()==capacity,"direct/no fragment allocation");
        Check(Receive(raw,h,f,b,fragments,std::string("\x01\x01" "a",3),[]{},[](const char*,size_t,bool){},
            [](uint8_t,const char*,size_t){return true;},[]{}), "fragment begins");
        Check(f&&fragments.size()==1&&fragments.capacity()==kTextBytes,"bounded lazy reserve");
        std::cout << "Storage: raw capacity=" << raw.capacity() << " (+terminator), fragment capacity=" << fragments.capacity()
                  << ", string metadata=" << sizeof(raw) << ", vector metadata=" << sizeof(fragments) << '\n';
    });
    test("random malformed chunks", [] {
        std::mt19937 random(42);
        for (int scenario=0;scenario<2000;++scenario) {
            std::string raw; raw.reserve(kReceiveBytes); bool h=(scenario%2)==0,f=false,b=false; std::vector<char> fragments; bool invalid=false;
            for(int chunk=0;chunk<8&&!invalid;++chunk) {
                std::string bytes(1+random()%1500,'\0'); for(char& c:bytes)c=static_cast<char>(random());
                Receive(raw,h,f,b,fragments,bytes,[]{},[](const char*,size_t,bool){},
                    [](uint8_t,const char*,size_t){return true;},[&]{invalid=true;});
                Check(raw.size()<=kReceiveBytes&&fragments.size()<=kTextBytes,"random bounds");
            }
        }
    });
    test("POSIX blocked peer total budget", [] {
        int fd[2]; Check(socketpair(AF_UNIX,SOCK_STREAM,0,fd)==0,"socketpair");
        struct Cleanup { int* fd; ~Cleanup(){close(fd[0]);close(fd[1]);} } cleanup{fd};
        int size=4096; setsockopt(fd[0],SOL_SOCKET,SO_SNDBUF,&size,sizeof(size));
        std::string block(4096,'x');
        while(send(fd[0],block.data(),block.size(),MSG_DONTWAIT)>0) {}
        const auto start=Monotonic(); int error;
        const bool sent=SendAll("x",1,kSendBudgetUs,Monotonic,[&](const char* p,size_t n){
            const auto r=send(fd[0],p,n,MSG_DONTWAIT);return WriteResult{r,r<0?errno:0};
        },[&](int64_t left){fd_set writes;FD_ZERO(&writes);FD_SET(fd[0],&writes);timeval t{left/1000000,left%1000000};
            const auto r=select(fd[0]+1,nullptr,&writes,nullptr,&t);return WaitResult{r,r<0?errno:0};},error);
        const auto elapsed=Monotonic()-start;
        Check(!sent&&error==ETIMEDOUT&&elapsed>=200000&&elapsed<2000000,"real blocked peer deadline");
        std::cout<<"POSIX blocked send elapsed_us="<<elapsed<<'\n';
    });
    std::cout << "Helpers: " << passed << " passed, " << failed << " failed\n";
    return failed?1:0;
}
