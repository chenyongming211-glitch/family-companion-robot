#pragma once
#include "c3_echo_stream.h"
#include <esp_aec.h>
#include <esp_heap_caps.h>
#include <new>
#if CONFIG_XMINI_C3_PCM
#include "c3_echo_stats.h"
#include <chrono>
#endif

namespace family_robot {
// One input-task owner. Allocate the object explicitly aligned in internal RAM:
// the SDK consumes the stream's aligned arrays directly, without another copy.
class C3LiteAec {
public:
    enum class Result { Ok, Stale, Fault };
    static C3LiteAec* Create() {
        void* memory=heap_caps_aligned_alloc(16,sizeof(C3LiteAec),MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT);
        if(!memory)return nullptr;
        auto* owner=new(memory) C3LiteAec;
        if(!owner->CreateDsp()) { Destroy(owner);return nullptr; }
        return owner;
    }
    static void Destroy(C3LiteAec* owner) {
        if(!owner)return;
        owner->~C3LiteAec();heap_caps_free(owner);
    }
    bool Begin(uint32_t epoch,uint32_t generation) {
        if(!healthy_ || !epoch || !generation)return false;
        if(epoch==epoch_ && generation==generation_)return true;
        // Startup already allocated and validated the first DSP state. Every
        // subsequent actual capture boundary gets fresh SDK history.
        if(epoch_ || generation_) {
            aec_destroy(aec_);aec_=nullptr;
            if(!CreateDsp())return false;
        }
        epoch_=epoch;generation_=generation;stream_.Begin(epoch);return true;
    }
    template<class Current,class Emit>
    Result Feed(const int16_t* mr,size_t frames,uint32_t epoch,uint32_t generation,
                Current&& current,Emit&& emit) {
        if(!current() || epoch!=epoch_ || generation!=generation_)return Result::Stale;
        if(!healthy_)return Result::Fault;
        bool stale=false;
        const bool ok=stream_.Feed(mr,frames,epoch,[&](int16_t* mic,int16_t* ref,int16_t* out,size_t) {
            if(!current()) {stale=true;return false;}
#if CONFIG_XMINI_C3_PCM
            const auto started=std::chrono::steady_clock::now();
#endif
            aec_process(aec_,mic,ref,out);
#if CONFIG_XMINI_C3_PCM
            const auto us=std::chrono::duration_cast<std::chrono::microseconds>(std::chrono::steady_clock::now()-started).count();
            statistics_.Record(mic,ref,out,C3EchoStream::kFrame,static_cast<uint64_t>(us));
#endif
            if(!current()) {stale=true;return false;}
            return true;
        },[&](const int16_t* out,size_t count) { emit(out,count); });
        if(stale)return Result::Stale;
        if(!ok) {healthy_=false;return Result::Fault;}
        return Result::Ok;
    }
#if CONFIG_XMINI_C3_PCM
    C3EchoStatistics Statistics() const {return statistics_;}
#endif
private:
    C3LiteAec()=default;
    ~C3LiteAec() {if(aec_)aec_destroy(aec_);}
    bool CreateDsp() {
        #if CONFIG_XMINI_C3_PCM
        aec_=aec_create(16000,2,1,AEC_MODE_FD_LOW_COST);
#else
        aec_=aec_create(16000,2,1,AEC_MODE_SR_LOW_COST);
#endif
        healthy_=aec_ && aec_get_chunksize(aec_)==static_cast<int>(C3EchoStream::kFrame);
        return healthy_;
    }
    C3EchoStream stream_;
#if CONFIG_XMINI_C3_PCM
    C3EchoStatistics statistics_;
#endif
    aec_handle_t* aec_=nullptr;
    uint32_t epoch_=0,generation_=0;
    bool healthy_=true;
};
static_assert(sizeof(C3LiteAec)<=3456,"C3 AEC frame storage must remain bounded");
} // namespace family_robot
