#pragma once
#include <algorithm>
#include <cstddef>
#include <cstdint>
#include <limits>
namespace family_robot {
// Input owner accumulates numbers only; its engine mutex protects main snapshots.
struct C3EchoStatistics {
    struct Signal { uint64_t energy=0,samples=0,clipped=0;uint32_t peak=0; } mic,reference,output;
    uint64_t blocks=0,total_us=0,max_us=0;
    static void Add(uint64_t& dst,uint64_t value) {dst=value>UINT64_MAX-dst?UINT64_MAX:dst+value;}
    static void Accumulate(Signal& signal,const int16_t* pcm,size_t count) {
        uint64_t energy=0,clipped=0;uint32_t peak=0;
        for(size_t i=0;i<count;++i) {
            const int32_t value=pcm[i];const auto magnitude=static_cast<uint32_t>(value<0?-value:value);
            energy+=uint64_t(int64_t(value)*value);peak=std::max(peak,magnitude);
            clipped+=(value==INT16_MIN || value==INT16_MAX);
        }
        Add(signal.energy,energy);Add(signal.samples,count);Add(signal.clipped,clipped);signal.peak=std::max(signal.peak,peak);
    }
    void Record(const int16_t* m,const int16_t* r,const int16_t* o,size_t n,uint64_t us) {
        Accumulate(mic,m,n);Accumulate(reference,r,n);Accumulate(output,o,n);Add(blocks,1);Add(total_us,us);max_us=std::max(max_us,us);
    }
};
}
