#include "music_audition_protocol.h"
#include <cassert>
#include <vector>
using namespace music_audition;
std::vector<uint8_t> frame(Op op, uint32_t seq, uint32_t arg, std::vector<uint8_t> payload={}) {
    std::vector<uint8_t> b(24 + payload.size());
    b[0]='F';b[1]='M';b[2]='A';b[3]='1';
    Write32(b.data()+4,static_cast<uint32_t>(op)); Write32(b.data()+8,seq);
    Write32(b.data()+12,arg); Write32(b.data()+16,payload.size());
    for (size_t i=0;i<payload.size();++i) b[24+i]=payload[i];
    Write32(b.data()+20,FrameCrc(b.data(),payload.size())); return b;
}
std::vector<uint8_t> start(uint32_t rate=48000,uint64_t count=1441) {
    std::vector<uint8_t> p(8);for(unsigned i=0;i<8;++i)p[i]=(count>>(i*8))&255;
    return frame(Op::Start,0,rate,p);
}
bool accept(Session& s,const std::vector<uint8_t>& b,uint64_t t=10) {Frame f{};return s.AcceptBytes(b.data(),b.size(),t,&f);}
int main() {
    Session s;auto a=start();assert(a[20]==0x78&&a[21]==0xca&&a[22]==0xf5&&a[23]==0x5a);
    assert(accept(s,a));assert(s.active()&&s.rate()==48000&&s.expected_seq()==1&&s.total_samples()==1441);
    assert(accept(s,frame(Op::Data,1,0,std::vector<uint8_t>(2880))));assert(s.received_samples()==1440);
    assert(accept(s,frame(Op::Data,2,0,{1,2})));assert(accept(s,frame(Op::End,3,0)));assert(!s.active());
    for(auto bad:{start(44100),start(24000,0),start(48000,5760001),frame(Op::Data,0,0,{1}),frame(Op::Data,0,0,std::vector<uint8_t>(2882))}) {
        assert(!accept(s,bad));assert(!s.active());
    }
    for(int mode=0;mode<8;++mode) {
        s.Reset();assert(accept(s,start()));
        auto b=frame(Op::Data,1,0,{1,2});
        if(mode==0)b.pop_back();
        if(mode==1)b.push_back(0);
        if(mode==2)b[20]^=1;
        if(mode==3)b=frame(Op::Data,2,0,{1,2});
        if(mode==4)b=frame(Op::End,1,0);
        if(mode==5)b=frame(Op::Data,1,0,{});
        if(mode==6)b=frame(Op::Data,1,1,{1,2});
        if(mode==7)b=start();
        assert(!accept(s,b));assert(!s.active());assert(!accept(s,frame(Op::Data,1,0,{1,2})));
        assert(accept(s,start()));assert(accept(s,frame(Op::Stop,1,0)));assert(!s.active());
    }
    s.Reset();assert(accept(s,start()));assert(accept(s,frame(Op::Data,1,0,{1,2})));
    assert(!accept(s,frame(Op::Data,1,0,{1,2}))); // replay
    assert(accept(s,start(24000,1)));assert(!accept(s,frame(Op::Data,1,0,{1,2,3,4})));
    assert(accept(s,start()));assert(s.CheckTimeout(5010));assert(!s.active());
    assert(accept(s,start()));assert(!s.CheckTimeout(5009));assert(!accept(s,frame(Op::Data,1,0,{1,2}),5010));
    assert(accept(s,start()));for(uint64_t t=4010;t<120010;t+=4000)assert(accept(s,frame(Op::Data,s.expected_seq(),0,{1,2}),t));
    assert(s.CheckTimeout(120010)); // whole session deadline, even with activity
    assert(accept(s,start()));assert(!accept(s,frame(Op::Data,1,0,{1,2}),9)); // backwards clock
    assert(accept(s,start(24000,2880000)));assert(accept(s,frame(Op::Stop,1,0)));
    for(auto invalid:{frame(static_cast<Op>(99),1,0), frame(Op::Stop,1,1), frame(Op::End,1,0,{1,2})}) {
        assert(accept(s,start()));assert(!accept(s,invalid));assert(!s.active());
    }
    Frame short_frame{};auto full=start();
    for(size_t size=0;size<24;++size)assert(!ParseFrame(full.data(),size,&short_frame));
    for(size_t byte=0;byte<full.size();++byte) {
        auto corrupted=full;corrupted[byte]^=1;
        assert(!ParseFrame(corrupted.data(),corrupted.size(),&short_frame));
    }
    Frame parsed{};auto b=start();Write32(b.data()+16,0xffffffff);assert(!ParseFrame(b.data(),b.size(),&parsed));
    assert(!ParseFrame(nullptr,24,&parsed));assert(!ParseFrame(b.data(),b.size(),nullptr));
}
