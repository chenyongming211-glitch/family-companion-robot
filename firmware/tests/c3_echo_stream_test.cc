#include "c3_echo_stream.h"
#include <cassert>
#include <cstdint>
#include <cstdio>
#include <vector>
using family_robot::C3EchoStream;
int main() {
    C3EchoStream stream;
    static_assert(sizeof(C3EchoStream)<4096);
    unsigned calls=0,emissions=0;std::vector<int16_t> heard;
    auto process=[&](int16_t* mic,int16_t* ref,int16_t* output,size_t n) {
        assert(n==512);
        assert(reinterpret_cast<uintptr_t>(mic)%16==0 && reinterpret_cast<uintptr_t>(ref)%16==0 &&
               reinterpret_cast<uintptr_t>(output)%16==0);
        ++calls;for(size_t i=0;i<n;++i)output[i]=mic[i]-ref[i];return true;
    };
    auto emit=[&](const int16_t* output,size_t n){++emissions;heard.insert(heard.end(),output,output+n);};
    std::vector<int16_t> mr(160*2);
    for(size_t i=0;i<160;++i){mr[2*i]=3000;mr[2*i+1]=2000;}
    stream.Begin(1);
    for(int i=0;i<3;++i)assert(stream.Feed(mr.data(),160,1,process,emit));
    assert(calls==0 && heard.empty()); // retain all 480 initial samples
    assert(stream.Feed(mr.data(),160,1,process,emit));
    assert(calls==1 && heard.size()==512 && stream.pending()==128);
    for(auto sample:heard)assert(sample==1000);
    // 10ms and 32ms frames must keep the first syllable across arbitrary chunks.
    for(int i=0;i<28;++i)assert(stream.Feed(mr.data(),160,1,process,emit));
    assert(heard.size()==5120 && calls==10 && stream.pending()==0);
    assert(stream.Feed(mr.data(),159,1,process,emit));
    stream.Begin(2);assert(stream.pending()==0);
    assert(!stream.Feed(mr.data(),160,1,process,emit));
    assert(stream.Feed(mr.data(),160,2,process,emit));
    assert(stream.pending()==160);
    const auto old_calls=calls,old_emissions=emissions;
    auto fail=[](int16_t*,int16_t*,int16_t*,size_t){return false;};
    std::vector<int16_t> large(1024*2,7);
    assert(!stream.Feed(large.data(),1024,2,fail,emit));
    assert(calls==old_calls && emissions==old_emissions && stream.pending()==0);
    assert(!stream.Feed(mr.data(),160,2,process,emit)); // no raw fallback after DSP failure
    stream.Begin(3);
    assert(!stream.Feed(nullptr,160,3,process,emit));
    stream.Begin(4);
    assert(!stream.Feed(mr.data(),0,4,process,emit));
    stream.Begin(5);
    assert(!stream.Feed(large.data(),1025,5,process,emit));
    stream.Begin(0);
    assert(!stream.Feed(mr.data(),160,0,process,emit));
    std::puts("C3 echo stream: frame continuity, alignment, epochs and fail-closed checks passed");
}
