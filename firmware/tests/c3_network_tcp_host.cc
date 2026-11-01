#include "esp_tcp.h"
#include <cerrno>
#include <chrono>
#include <cstring>
#include <fcntl.h>
#include <iostream>
#include <stdexcept>
#include <sys/select.h>
#include <sys/socket.h>
#include <time.h>
#include <unistd.h>
int test_event_deletes=0;
std::string NetworkError::ToString() const { return "network-error"; }
NetworkError NetworkError::FromErrno(int n) { return {NetworkErrc::TransmitFailed,n}; }
NetworkError NetworkError::FromHErrno(int n) { return {NetworkErrc::DnsFailed,n}; }
static int mode=0, writes=0, waits=0, clock_calls=0, last_fd=-1;
static int64_t clock_us=0, last_wait=0;
static bool nonblock_write=true, nonblock_connect=true;
extern "C" int __wrap_clock_gettime(clockid_t, timespec* t) { if(mode==9&&clock_calls++>0){errno=EIO;return -1;}t->tv_sec=clock_us/1000000;t->tv_nsec=(clock_us%1000000)*1000;return 0; }
extern "C" int __wrap_connect(int fd,const sockaddr*,socklen_t) {
 last_fd=fd;
 nonblock_connect=(fcntl(fd,F_GETFL)&O_NONBLOCK)!=0;
 if(mode==7 || mode==8 || mode==9) {errno=EINPROGRESS;return -1;}
 if(mode==10)clock_us=5000000;
 return 0;
}
extern "C" int __wrap_select(int,fd_set*,fd_set*,fd_set*,timeval* t) {
 ++waits;last_wait=int64_t(t->tv_sec)*1000000+t->tv_usec;
 if(mode==4 && waits==1){clock_us+=10000;errno=EINTR;return -1;}
 if(mode==6 || mode==8){clock_us+=last_wait;return 0;}
 clock_us+=1000;return 1;
}
extern "C" ssize_t __wrap_send(int,const void*,size_t n,int flags) {
 nonblock_write=nonblock_write&&((flags&MSG_DONTWAIT)!=0);++writes;
 if(mode==1)return 0;
 if(mode==2){clock_us+=150000;return n>1?1:n;}
 if(mode==3 && writes==1){errno=EAGAIN;return -1;}
 if(mode==4 && writes<=2){errno=writes==1?EINTR:EAGAIN;return -1;}
 if(mode==5){errno=ECONNRESET;return -1;}
 if(mode==6){errno=EWOULDBLOCK;return -1;}
 return n;
}
static void Check(bool b,const char* msg){if(!b)throw std::runtime_error(msg);}
static void Reset(int m){mode=m;writes=waits=clock_calls=0;clock_us=last_wait=0;nonblock_write=nonblock_connect=true;}
int main(){int passed=0,failed=0;auto test=[&](const char* n,auto f){try{f();++passed;}catch(const std::exception& e){++failed;std::cerr<<"FAIL "<<n<<": "<<e.what()<<'\n';}};
 auto connected=[](int m,auto f){Reset(m);EspTcp tcp;Check(bool(tcp.Connect("127.0.0.1",80)),"connect");f(tcp);};
 test("zero write",[&]{connected(1,[](EspTcp& t){Check(t.Send("abc")==-1,"zero must fail");});});
 test("partial total deadline",[&]{connected(2,[](EspTcp& t){Check(t.Send("abc")==-1&&clock_us==300000,"partial does not reset 250ms budget");});});
 test("wouldblock waits",[&]{connected(3,[](EspTcp& t){Check(t.Send("abc")==3&&waits==1,"wait then complete");Check(nonblock_write&&last_wait<=250000,"nonblocking remaining budget");});});
 test("EINTR send and wait",[&]{connected(4,[](EspTcp& t){Check(t.Send("abc")==3&&waits==1,"retry EINTR");});});
 test("hard error",[&]{connected(5,[](EspTcp& t){Check(t.Send("abc")==-1&&errno==ECONNRESET,"hard send error");});});
 test("wait timeout",[&]{connected(6,[](EspTcp& t){Check(t.Send("abc")==-1&&clock_us==250000&&errno==ETIMEDOUT,"250ms total timeout");});});
 test("nonblocking connect and restored recv",[&]{connected(0,[](EspTcp&){Check(nonblock_connect,"post-DNS connect nonblocking");Check((fcntl(last_fd,F_GETFL)&O_NONBLOCK)==0,"restore blocking receive");});});
 test("connect readiness",[&]{connected(7,[](EspTcp&){Check(waits==1&&last_wait<=5000000,"bounded connect readiness");});});
 test("connect timeout",[]{Reset(8);EspTcp t;Check(!t.Connect("127.0.0.1",80)&&clock_us==5000000&&errno==ETIMEDOUT,"post-DNS five second timeout");});
 test("connect clock failure",[]{Reset(9);EspTcp t;Check(!t.Connect("127.0.0.1",80)&&errno==EIO,"connect clock fails closed");});
 test("late connect completion",[]{Reset(10);EspTcp t;Check(!t.Connect("127.0.0.1",80)&&errno==ETIMEDOUT,"late connect cannot claim success");});
 test("disconnected empty send",[]{Reset(0);EspTcp t;Check(t.Send("")==-1,"disconnected send");});
 std::cout<<"EspTcp: "<<passed<<" passed, "<<failed<<" failed\n";return failed?1:0;}
