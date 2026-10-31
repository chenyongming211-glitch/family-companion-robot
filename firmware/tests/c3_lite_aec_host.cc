#include "c3_lite_aec.h"
#include <cassert>
#include <vector>
using family_robot::C3LiteAec;
static bool current=true;
int main() {
    heap_fail=true;assert(!C3LiteAec::Create());heap_fail=false;
    aec_fail=true;assert(!C3LiteAec::Create());aec_fail=false;
    aec_chunks=256;assert(!C3LiteAec::Create());aec_chunks=512;
    auto* owner=C3LiteAec::Create();assert(owner&&reinterpret_cast<uintptr_t>(owner)%16==0);
    assert(owner->Begin(3,7));
    const auto created=aec_created;
    std::vector<int16_t> mr(320,1);for(size_t i=0;i<mr.size();i+=2)mr[i]=11;
    int emitted=0;
    auto emit=[&](const int16_t* data,size_t n) {assert(n==512);for(size_t i=0;i<n;++i)assert(data[i]==10);++emitted;};
    auto valid=[] {return current;};
    for(int i=0;i<4;++i)assert(owner->Feed(mr.data(),160,3,7,valid,emit)==C3LiteAec::Result::Ok);
    assert(emitted==1&&aec_created==created);
    // TTS has no lifecycle hook; the same epoch keeps DSP and the 128-sample tail.
    assert(owner->Begin(3,7));assert(aec_created==created);
    assert(owner->Feed(mr.data(),160,3,7,valid,emit)==C3LiteAec::Result::Ok);
    assert(owner->Begin(5,9)&&aec_created==created+1);
    for(int i=0;i<3;++i)owner->Feed(mr.data(),160,5,9,valid,emit);
    aec_during_process=[] {current=false;};
    assert(owner->Feed(mr.data(),160,5,9,valid,emit)==C3LiteAec::Result::Stale);
    assert(emitted==1);
    aec_during_process=nullptr;current=true;
    assert(owner->Begin(7,11));
    assert(owner->Feed(mr.data(),1025,7,11,valid,emit)==C3LiteAec::Result::Fault);
    assert(owner->Feed(mr.data(),160,7,11,valid,emit)==C3LiteAec::Result::Fault);
    C3LiteAec::Destroy(owner);
    auto* failed=C3LiteAec::Create();assert(failed&&failed->Begin(1,1));
    aec_fail=true;assert(!failed->Begin(3,2));aec_fail=false;
    C3LiteAec::Destroy(failed);
    assert(aec_created==aec_destroyed);
}
