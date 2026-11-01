#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <new>
#include <string>
#include "c3_pcm_buffer.h"
static bool forbid_alloc = false;
void* operator new(std::size_t n) { if (forbid_alloc) std::abort(); if (void* p=std::malloc(n)) return p; throw std::bad_alloc(); }
void* operator new[](std::size_t n) { return ::operator new(n); }
void operator delete(void* p) noexcept { std::free(p); }
void operator delete[](void* p) noexcept { std::free(p); }
void operator delete(void* p, std::size_t) noexcept { std::free(p); }
void operator delete[](void* p, std::size_t) noexcept { std::free(p); }
#define CHECK(x) do { if (!(x)) { std::fprintf(stderr,"line %d: %s\n",__LINE__,#x); return 1; } } while (0)
using namespace family_robot;
using S=C3PcmResult;
int main(int argc,char** argv) {
    CHECK(argc==2); const std::string name=argv[1]; forbid_alloc=true;
    int16_t source[512]; for (size_t i=0;i<512;++i) source[i]=static_cast<int16_t>(i-256);
    uint8_t bytes[2882]{}; uint32_t epoch=0;
    C3PcmInputPool input; C3PcmOutputPool output;
    if (name=="input") {
        for (size_t n=0;n<15;++n) {
            for (size_t i=0;i<512;++i) source[i]=static_cast<int16_t>(n*512+i);
            CHECK(input.Feed(source,512,3)==S::Ok);
            while (input.queued()) {
                CHECK(input.Pop(bytes+1,1920,&epoch)==S::Ok); CHECK(epoch==3);
                static size_t next=0;
                for(size_t i=0;i<960;++i,++next) CHECK(static_cast<uint16_t>(bytes[1+i*2]|(bytes[2+i*2]<<8))==next);
            }
        }
        CHECK(input.partial_samples()==0); CHECK(input.high_water()==2);
        CHECK(input.Feed(source,512,3)==S::Ok); CHECK(input.Feed(source,512,3)==S::Ok);
        CHECK(input.Feed(source,512,3)==S::Ok); CHECK(input.partial_samples()==576);
        CHECK(input.Feed(source,512,3)==S::Full); CHECK(input.partial_samples()==576); CHECK(input.queued()==1);
        CHECK(input.Pop(bytes,1920,&epoch)==S::Ok); CHECK(input.Feed(source,512,3)==S::Ok);
    } else if(name=="epoch") {
        CHECK(input.Feed(source,512,0)==S::Invalid);
        CHECK(input.Feed(source,512,7)==S::Ok); CHECK(input.Feed(source,512,9)==S::EpochMismatch);
        input.Reset(9); CHECK(input.partial_samples()==0); CHECK(input.Feed(source,512,7)==S::EpochMismatch);
        CHECK(input.Feed(source,512,9)==S::Ok); input.Reset();
        CHECK(input.Feed(source,512,11)==S::Ok); CHECK(input.Feed(source,448,11)==S::Ok);
        CHECK(input.Pop(bytes,1920,&epoch)==S::Ok); CHECK(epoch==11); CHECK(input.occupied()==0);
    } else if(name=="shapes") {
        CHECK(input.Feed(nullptr,512,1)==S::Invalid); CHECK(input.Feed(source,0,1)==S::Invalid);
        CHECK(input.Feed(source,513,1)==S::Invalid); CHECK(input.Feed(source,512,1)==S::Ok);
        CHECK(input.Feed(source,448,1)==S::Ok); CHECK(input.Pop(bytes,1919,&epoch)==S::Invalid);
        CHECK(input.Pop(bytes,1920,nullptr)==S::Invalid); CHECK(input.queued()==1);
        CHECK(input.Pop(bytes,1920,&epoch)==S::Ok); CHECK(input.Pop(bytes,1920,&epoch)==S::Empty);
        CHECK(output.Push(bytes,2879,{})==S::Invalid); CHECK(output.Push(nullptr,2880,{})==S::Invalid);
    } else if(name=="output") {
        for(size_t n=0;n<5;++n) { for(size_t i=0;i<1440;++i){bytes[1+i*2]=static_cast<uint8_t>(i+n);bytes[2+i*2]=0x80;}
            CHECK(output.Push(bytes+1,2880,{4,8,static_cast<uint32_t>(n*1440),12})==S::Ok); }
        CHECK(output.queued()==5); CHECK(output.in_flight()==0); CHECK(output.high_water()==5);
        CHECK(output.Push(bytes,2880,{})==S::Full);
        for(size_t n=0;n<5;++n) { C3PcmOutputPool::Lease l;
            CHECK(output.Acquire(l)==S::Ok); CHECK(output.occupied()==5-n); CHECK(output.in_flight()==1);
            CHECK(l.metadata.connection==4 && l.metadata.playback==8 && l.metadata.first==n*1440 && l.metadata.output_generation==12);
            for(size_t i=0;i<1440;++i) CHECK(static_cast<uint16_t>(l.samples[i])==(0x8000|static_cast<uint8_t>(i+n)));
            CHECK(output.Release(l)==S::Ok); CHECK(output.Release(l)==S::Invalid);
        }
        C3PcmOutputPool::Lease none; CHECK(output.Acquire(none)==S::Empty); CHECK(output.occupied()==0);
    } else if(name=="reset") {
        std::memset(bytes,0x55,2880);
        CHECK(output.Push(bytes,2880,{1,2,0,3})==S::Ok); C3PcmOutputPool::Lease old;
        CHECK(output.Acquire(old)==S::Ok); CHECK(output.Push(bytes,2880,{})==S::Ok); output.Reset();
        CHECK(output.in_flight()==1 && output.queued()==0); CHECK(old.samples[0]==0x5555);
        std::memset(bytes,0xab,2880);
        for(size_t n=0;n<4;++n) CHECK(output.Push(bytes,2880,{})==S::Ok);
        for(size_t i=0;i<1440;++i) CHECK(old.samples[i]==0x5555);
        CHECK(output.Push(bytes,2880,{})==S::Full); CHECK(output.Release(old)==S::Ok);
        output.Reset(); CHECK(output.Push(bytes,2880,{})==S::Ok); C3PcmOutputPool::Lease fresh;
        CHECK(output.Acquire(fresh)==S::Ok); CHECK(output.Release(old)==S::Invalid); CHECK(output.in_flight()==1);
        C3PcmOutputPool other; CHECK(other.Release(fresh)==S::Invalid); CHECK(output.Release(fresh)==S::Ok);
    } else if(name=="exhaustion") {
        C3PcmOutputPool last(UINT64_MAX); CHECK(last.Push(bytes,2880,{})==S::Ok);
        C3PcmOutputPool::Lease lease; CHECK(last.Acquire(lease)==S::Ok); CHECK(last.Release(lease)==S::Ok);
        CHECK(last.Push(bytes,2880,{})==S::Exhausted); last.Reset(); CHECK(last.Push(bytes,2880,{})==S::Exhausted);
        C3PcmOutputPool invalid(0); CHECK(invalid.Push(bytes,2880,{})==S::Exhausted);
    } else CHECK(false);
    return 0;
}
