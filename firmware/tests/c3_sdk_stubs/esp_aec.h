#pragma once
#include <cassert>
#include <cstdint>
struct aec_handle_t { int calls=0; };
enum { AEC_MODE_SR_LOW_COST=7 };
inline int aec_created=0,aec_destroyed=0,aec_chunks=512;
inline bool aec_fail=false;
inline void (*aec_during_process)()=nullptr;
inline aec_handle_t* aec_create(int rate,int channels,int ref,int mode) {
    assert(rate==16000&&channels==2&&ref==1&&mode==AEC_MODE_SR_LOW_COST);
    if(aec_fail)return nullptr;
    ++aec_created;return new aec_handle_t;
}
inline int aec_get_chunksize(aec_handle_t*) { return aec_chunks; }
inline void aec_destroy(aec_handle_t* handle) { ++aec_destroyed;delete handle; }
inline void aec_process(aec_handle_t* handle,int16_t* mic,int16_t* ref,int16_t* out) {
    assert(reinterpret_cast<uintptr_t>(mic)%16==0&&reinterpret_cast<uintptr_t>(ref)%16==0&&reinterpret_cast<uintptr_t>(out)%16==0);
    ++handle->calls;
    for(int i=0;i<512;++i)out[i]=mic[i]-ref[i];
    if(aec_during_process)aec_during_process();
}
