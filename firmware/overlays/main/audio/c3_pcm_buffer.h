#pragma once
#include <cstddef>
#include <cstdint>
#include <cstring>
namespace family_robot {
enum class C3PcmResult { Ok, Empty, Full, Invalid, EpochMismatch, Exhausted };
struct C3PcmOutputMetadata { uint32_t connection=0, playback=0, first=0, output_generation=0; };
// Pure task-context storage. The adapter must hold its queue mutex for every
// operation, including stats and Reset. Never feed revoked capture tokens.
class C3PcmInputPool {
public:
    static constexpr size_t kSlots=2, kSamples=960, kBytes=1920;
    C3PcmInputPool() = default;
    C3PcmInputPool(const C3PcmInputPool&)=delete;
    C3PcmInputPool& operator=(const C3PcmInputPool&)=delete;
    C3PcmInputPool(C3PcmInputPool&&)=delete;
    C3PcmInputPool& operator=(C3PcmInputPool&&)=delete;
    C3PcmResult Feed(const int16_t* samples,size_t count,uint32_t epoch) {
        if (!samples || !count || count>512 || !epoch) return C3PcmResult::Invalid;
        if (epoch_ && epoch_!=epoch) return C3PcmResult::EpochMismatch;
        if (count>kSlots*kSamples-queued_*kSamples-partial_) return C3PcmResult::Full;
        epoch_=epoch; // Reset(0) permits the first externally validated token.
        while (count) {
            const auto n=count<kSamples-partial_ ? count : kSamples-partial_;
            std::memcpy(pcm_[(head_+queued_)%kSlots]+partial_,samples,n*sizeof(int16_t));
            partial_+=n; samples+=n; count-=n;
            if (partial_==kSamples) { ++queued_; partial_=0; }
        }
        if (occupied()>high_water_) high_water_=occupied();
        return C3PcmResult::Ok;
    }
    C3PcmResult Pop(uint8_t* bytes,size_t count,uint32_t* epoch) {
        if (!bytes || count!=kBytes || !epoch) return C3PcmResult::Invalid;
        if (!queued_) return C3PcmResult::Empty;
        for(size_t i=0;i<kSamples;++i) {
            const auto sample=static_cast<uint16_t>(pcm_[head_][i]);
            bytes[i*2]=static_cast<uint8_t>(sample); bytes[i*2+1]=static_cast<uint8_t>(sample>>8);
        }
        *epoch=epoch_; head_=(head_+1)%kSlots; --queued_;
        return C3PcmResult::Ok;
    }
    void Reset(uint32_t epoch=0) { head_=queued_=partial_=0; epoch_=epoch; }
    size_t queued() const { return queued_; } size_t partial_samples() const { return partial_; }
    size_t occupied() const { return queued_+(partial_ ? 1 : 0); }
    size_t high_water() const { return high_water_; }
private:
    int16_t pcm_[kSlots][kSamples]{}; // exactly 3840 PCM payload bytes
    size_t head_=0, queued_=0, partial_=0, high_water_=0;
    uint32_t epoch_=0;
};
class C3PcmOutputPool {
public:
    static constexpr size_t kSlots=5, kSamples=1440, kBytes=2880;
    struct Lease {
        const int16_t* samples=nullptr;
        C3PcmOutputMetadata metadata{};
    private:
        friend class C3PcmOutputPool;
        const C3PcmOutputPool* owner=nullptr;
        uint64_t serial=0;
        size_t slot=0;
    };
    // A caller may seed a fresh pool's lease domain. Zero is permanently closed;
    // UINT64_MAX can be issued once. Reset never rewinds the domain.
    explicit C3PcmOutputPool(uint64_t initial_serial=1): next_serial_(initial_serial) {}
    C3PcmOutputPool(const C3PcmOutputPool&)=delete;
    C3PcmOutputPool& operator=(const C3PcmOutputPool&)=delete;
    C3PcmOutputPool(C3PcmOutputPool&&)=delete;
    C3PcmOutputPool& operator=(C3PcmOutputPool&&)=delete;
    C3PcmResult Push(const uint8_t* bytes,size_t count,C3PcmOutputMetadata metadata) {
        if (!bytes || count!=kBytes) return C3PcmResult::Invalid;
        if (!next_serial_) return C3PcmResult::Exhausted;
        size_t slot=0; while (slot<kSlots && states_[slot]!=Free) ++slot;
        if(slot==kSlots) return C3PcmResult::Full;
        for(size_t i=0;i<kSamples;++i) {
            const uint16_t value=uint16_t(bytes[i*2])|(uint16_t(bytes[i*2+1])<<8);
            pcm_[slot][i]=static_cast<int16_t>(value<0x8000 ? int32_t(value) : int32_t(value)-65536);
        }
        metadata_[slot]=metadata; serials_[slot]=next_serial_;
        next_serial_=next_serial_==UINT64_MAX ? 0 : next_serial_+1;
        states_[slot]=Queued; queue_[(head_+queued_)%kSlots]=slot; ++queued_;
        if(occupied()>high_water_) high_water_=occupied();
        return C3PcmResult::Ok;
    }
    C3PcmResult Acquire(Lease& lease) {
        if (!queued_) return C3PcmResult::Empty;
        const auto slot=queue_[head_]; head_=(head_+1)%kSlots; --queued_; ++in_flight_;
        states_[slot]=InFlight; lease.samples=pcm_[slot]; lease.metadata=metadata_[slot];
        lease.owner=this; lease.serial=serials_[slot]; lease.slot=slot;
        return C3PcmResult::Ok;
    }
    C3PcmResult Release(const Lease& lease) {
        if(lease.owner!=this || lease.slot>=kSlots || states_[lease.slot]!=InFlight ||
           serials_[lease.slot]!=lease.serial || lease.samples!=pcm_[lease.slot]) return C3PcmResult::Invalid;
        states_[lease.slot]=Free; --in_flight_; return C3PcmResult::Ok;
    }
    void Reset() {
        for(auto& state:states_) if(state==Queued) state=Free;
        head_=queued_=0;
    }
    size_t queued() const { return queued_; } size_t in_flight() const { return in_flight_; }
    size_t occupied() const { return queued_+in_flight_; } size_t high_water() const { return high_water_; }
private:
    enum State : uint8_t { Free, Queued, InFlight };
    int16_t pcm_[kSlots][kSamples]{}; // exactly 14400 PCM payload bytes
    C3PcmOutputMetadata metadata_[kSlots]{};
    uint64_t serials_[kSlots]{};
    State states_[kSlots]{};
    size_t queue_[kSlots]{}, head_=0, queued_=0, in_flight_=0, high_water_=0;
    uint64_t next_serial_;
};
static_assert(sizeof(C3PcmInputPool)<=4096, "bounded input metadata");
static_assert(sizeof(C3PcmOutputPool)<=14848, "bounded output metadata");
}
