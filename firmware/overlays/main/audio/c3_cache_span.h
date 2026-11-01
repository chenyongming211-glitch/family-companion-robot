#pragma once
#include <cstddef>
#include <cstdint>
#include <climits>

namespace family_robot {
// Single-core, bounded metadata only. Every shared storage cell is an aligned
// 32-bit word, including the two halves of a timestamp. No ISR retries a read.
template<unsigned Slots=16,uint32_t CounterMax=UINT32_MAX>
class C3CacheSpanState {
    friend struct C3CacheSpanTestPeer;
public:
    static constexpr unsigned kSlots=Slots;
    struct Token {
        uint32_t slot=UINT32_MAX,generation=0;int64_t entered_us=0;bool staged=false;
        bool valid() const {return staged || (slot<Slots && generation);}
    };
    struct Record {
        int64_t enter_before=0,disable_after=0,enable_before=0,enable_after=0;
        uint32_t generation=0,rx=0,tx=0;bool proven=false;
    };
    bool enabled() const {return Acquire(enabled_)!=0;}
    void Enable() {Publish(enabled_,1);}
    bool overflowed() const {return Load(overflow_)!=0;}
    unsigned used() const {const auto n=Load(used_);return n<Slots?n:Slots;}
    Token EnterBefore(int64_t now) {
        if(!enabled())return {};
        // No shared writer state before real disable: another task may finish
        // a complete Flash operation between this ticket and our guard entry.
        return {UINT32_MAX,0,now,true};
    }
    void DisableAfter(Token& token,int64_t now) {
        if(!token.staged)return;
        // Fixed SDK UNICORE/non-autosuspend guard has now paused scheduling.
        // Only its IRAM IRQs can preempt, and Observe sees inactive until LAST.
        const auto depth=Load(depth_);
        if(depth) {
            Store(depth_,depth==UINT32_MAX?depth:depth+1);
            const auto current=Current();if(current.valid())Store(slots_[current.slot].bad,1);
            if(depth==UINT32_MAX)Store(overflow_,1);
            token={};return;
        }
        Store(depth_,1);
        const auto generation=Load(used_);
        if(generation==UINT32_MAX) {Store(overflow_,1);token={};return;}
        unsigned index=Slots,completed=0;int64_t shortest=INT64_MAX;
        // Bounded scan while the SDK guard has paused scheduling. An unfinished
        // is never reusable. Keep at least one longest proven completed span;
        // later short PHY writes cannot erase the useful calibration interval.
        for(unsigned i=0;i<Slots;++i) {
            if(!Load(slots_[i].generation)) {index=i;break;}
            Record previous;
            if(!Read(i,previous) || Load(slots_[i].seq)>UINT32_MAX-8)continue;
            if(!previous.proven) {index=i;shortest=-1;continue;}
            ++completed;
            const auto duration=previous.enable_before-previous.disable_after;
            if(duration<shortest) {index=i;shortest=duration;}
        }
        if(index==Slots || (Load(slots_[index].generation) && shortest>=0 && completed<2)) {
            Store(overflow_,1);token={};return;
        }
        Store(used_,generation+1);
        auto& s=slots_[index];WriteBegin(s);Store(s.generation,generation+1);
        Store(s.phase,0);Store(s.rx,0);Store(s.tx,0);
        StoreTime(s.disabled,0);StoreTime(s.before_enabled,0);StoreTime(s.after_enabled,0);
        StoreTime(s.enter,token.entered_us);Store(s.bad,token.entered_us<0 || token.entered_us==INT64_MAX);
        Store(current_,index);WriteEnd(s);
        token={index,generation+1};
        WriteBegin(s);StoreTime(s.disabled,now);
        Store(s.phase,Load(s.phase)|4u);
        if(now<0 || now==INT64_MAX || now<ReadTime(s.enter))Store(s.bad,1);
        WriteEnd(s);
        // Final publication: counters cannot claim an interval before all its
        // disable-after bytes are available. Invalid intervals never activate.
        if(!Load(s.bad))Publish(active_,token.slot);
    }
    Token Current() const {
        const auto index=Load(current_);
        return index<Slots?Token{index,Load(slots_[index].generation)}:Token{};
    }
    Token BeginEnable() {
        Publish(active_,UINT32_MAX); // stop counting BEFORE reading enable-before time
        const auto depth=Load(depth_);
        if(!depth)return {};
        Store(depth_,depth-1);
        if(depth!=1)return {};
        const auto token=Current();
        Store(current_,UINT32_MAX);Store(depth_,0);return token;
    }
    void EnableBefore(Token token,int64_t now) {
        if(!Matches(token)) {if(token.staged)Store(overflow_,1);return;}
        Publish(active_,UINT32_MAX); // FIRST: a callback after this point is outside
        Store(current_,UINT32_MAX);Store(depth_,0);
        auto& s=slots_[token.slot];WriteBegin(s);StoreTime(s.before_enabled,now);
        if(!(Load(s.phase)&4u) || now<0 || now==INT64_MAX || now<ReadTime(s.disabled))Store(s.bad,1);
        Store(s.phase,Load(s.phase)|2u);WriteEnd(s);
    }
    void EnableAfter(Token token,int64_t now) {
        if(!Matches(token))return;
        auto& s=slots_[token.slot];WriteBegin(s);StoreTime(s.after_enabled,now);
        if(!(Load(s.phase)&2u) || now<0 || now==INT64_MAX || now<ReadTime(s.before_enabled))Store(s.bad,1);
        Store(s.phase,Load(s.phase)|1u);WriteEnd(s);
    }
    bool Observe(bool rx) {
        const auto index=Acquire(active_);if(index>=Slots)return false;
        auto& s=slots_[index];const auto before=Acquire(s.seq);
        if((before&1) || Load(s.bad))return false;
        // Even a sequence change yields no claim. This never spins on RV32.
        __atomic_thread_fence(__ATOMIC_ACQUIRE);
        if(before!=Load(s.seq) || index!=Acquire(active_))return false;
        auto& count=rx?s.rx:s.tx;const auto value=Load(count);
        if(value>=CounterMax) {Store(s.bad,1);return false;}
        Store(count,value+1);if(value+1==CounterMax)Store(s.bad,1);return true;
    }
    bool Read(unsigned index,Record& out) const {
        if(index>=Slots || index>=Load(used_))return false;
        const auto& s=slots_[index];const auto before=Acquire(s.seq);
        if((before&1) || !(Load(s.phase)&1u))return false;
        Record copy;copy.enter_before=ReadTime(s.enter);copy.disable_after=ReadTime(s.disabled);
        copy.enable_before=ReadTime(s.before_enabled);copy.enable_after=ReadTime(s.after_enabled);
        copy.generation=Load(s.generation);copy.rx=Load(s.rx);copy.tx=Load(s.tx);
        copy.proven=!Load(s.bad);
        __atomic_thread_fence(__ATOMIC_ACQUIRE);
        if(before!=Load(s.seq))return false;
        out=copy;return true;
    }
    bool ReadPeak(Record& out) const {
        bool found=false;int64_t longest=-1;
        for(unsigned i=0;i<Slots;++i) {
            Record candidate;
            if(!Read(i,candidate) || !candidate.proven)continue;
            const auto duration=candidate.enable_before-candidate.disable_after;
            if(duration>longest) {out=candidate;longest=duration;found=true;}
        }
        return found;
    }
private:
    struct Time {uint32_t low=0,high=0;};
    struct Slot {
        uint32_t seq=0,generation=0,bad=0,phase=0,rx=0,tx=0;
        Time enter,disabled,before_enabled,after_enabled;
    };
    // Loads/stores only: RV32IMC has no A extension; never introduce an atomic
    // RMW/CAS helper or infer its lock-freedom from the host architecture.
    static_assert(sizeof(uint32_t)==4,"cache trace requires 32-bit words");
    static uint32_t Load(const uint32_t& value) {return __atomic_load_n(&value,__ATOMIC_RELAXED);}
    static void Store(uint32_t& dest,uint32_t value) {__atomic_store_n(&dest,value,__ATOMIC_RELAXED);}
    static uint32_t Acquire(const uint32_t& value) {return __atomic_load_n(&value,__ATOMIC_ACQUIRE);}
    static void Publish(uint32_t& dest,uint32_t value) {__atomic_store_n(&dest,value,__ATOMIC_RELEASE);}
    static void StoreTime(Time& dest,int64_t value) {
        Store(dest.low,static_cast<uint32_t>(value));Store(dest.high,static_cast<uint32_t>(uint64_t(value)>>32));
    }
    static int64_t ReadTime(const Time& time) {const auto low=Load(time.low);return static_cast<int64_t>((uint64_t(Load(time.high))<<32)|low);}
    static void WriteBegin(Slot& slot) {
        Store(slot.seq,Load(slot.seq)+1);__atomic_thread_fence(__ATOMIC_RELEASE);
    }
    static void WriteEnd(Slot& slot) {Publish(slot.seq,Load(slot.seq)+1);}
    bool Matches(Token token) const {return !token.staged && token.valid() && Load(slots_[token.slot].generation)==token.generation;}
    uint32_t enabled_=0,used_=0,overflow_=0,depth_=0,current_=UINT32_MAX,active_=UINT32_MAX;
    Slot slots_[Slots]{};
};
using C3CacheSpan=C3CacheSpanState<>;
static_assert(sizeof(C3CacheSpan)<=1024,"cache trace fixed internal RAM budget: 1024 bytes");
} // namespace family_robot
