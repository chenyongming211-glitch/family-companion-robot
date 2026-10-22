#pragma once
#include <array>
#include <cstddef>
#include <cstdint>
#include <cstring>

namespace family_robot {
// Bound transient report serialization too. Other opaque session formats safely
// keep ordinary audio with no negotiated receipts on this constrained device.
inline bool PlaybackSessionSupported(const char* session, size_t size) {
    if (!session || size == 0 || size > 64) return false;
    for (size_t i = 0; i < size; ++i)
        if (static_cast<unsigned char>(session[i]) < 0x20 || static_cast<unsigned char>(session[i]) > 0x7e) return false;
    return true;
}
struct PlaybackIdentity {
    uint32_t connection = 0, playback = 0;
    bool operator==(PlaybackIdentity b) const { return connection == b.connection && playback == b.playback; }
    bool operator!=(PlaybackIdentity b) const { return !(*this == b); }
    explicit operator bool() const { return connection != 0 && playback != 0; }
};
struct PlaybackSnapshot {
    PlaybackIdentity identity{};
    uint64_t played_samples = 0;
    bool valid = false, finished = false, interrupted = false;
};
// Platform-free production state machine. Every call is serialized by one adapter
// portMUX. Complete runs in the I2S ISR, with no allocation, mutex or atomic.
class PlaybackProgress {
public:
    static constexpr size_t kBlock = 240, kSlots = 6, kQueue = 12, kRecords = 16;
    static constexpr uint64_t kMaxSamples = 9007199254740991ULL; // exact JSON integer
    enum class SubmitResult { Accepted, Full, Rejected };
    void Connect(uint32_t connection) {
        records_ = {}; connection_ = connection; last_id_ = 0; head_ = used_ = 0;
        // DMA tags remain: those physical samples may still be sent. Old identity
        // is ignored on completion and can never become the new connection's PCM.
    }
    bool Start(PlaybackIdentity id) {
        if (!id || id.connection != connection_ || id.playback <= last_id_) return false;
        last_id_ = id.playback;
        for (auto& r : records_) if (!r.id) {
            r = {}; r.id = id; r.valid = hardware_ok_; return true;
        }
        return false; // bounded degradation: caller must not advertise confirmation
    }
    bool Admit(PlaybackIdentity id, uint64_t first, size_t n) {
        auto* r = Find(id);
        if (!r || r->sealed) return false;
        if (n == 0 || n > 1440 || first != r->received || first > kMaxSamples - n || r->work == UINT32_MAX) {
            r->valid = false; return false;
        }
        r->received += n; ++r->work; return true;
    }
    void Release(PlaybackIdentity id) { auto* r=Find(id); if (r && r->work) --r->work; }
    void Invalidate(PlaybackIdentity id) { auto* r=Find(id); if(r) r->valid=false; }
    bool Allows(PlaybackIdentity id) const {
        const auto* r=Find(id); return r && !r->interrupted;
    }
    SubmitResult Submit(PlaybackIdentity id, uint64_t first, const int16_t* pcm, size_t n) {
        auto* r = Find(id);
        if (id && (!r || r->interrupted)) return SubmitResult::Rejected;
        if (!pcm || n == 0 || n > kBlock || (r && (first > kMaxSamples - n || first+n > r->received))) {
            if (r) r->valid=false;
            return SubmitResult::Rejected;
        }
        if (used_ == kQueue) return SubmitResult::Full;
        if (r && first != r->submitted) r->valid = false;
        if (r) r->submitted = first+n;
        auto& b = queue_[(head_+used_)%kQueue];
        b.tag = {id, first, static_cast<uint16_t>(n)};
        std::memcpy(b.pcm.data(), pcm, n*sizeof(int16_t));
        ++used_; return SubmitResult::Accepted;
    }
    void Seal(PlaybackIdentity id, bool interrupted) {
        auto* r=Find(id); if(!r) return;
        r->sealed=true;
        if (!interrupted) return;
        r->interrupted=true; r->work=0;
        // Stable compaction preserves ordering of every other identity.
        size_t keep=0;
        for(size_t i=0;i<used_;++i) {
            const size_t at=(head_+i)%kQueue;
            if(queue_[at].tag.id != id) {
                if(i!=keep) queue_[(head_+keep)%kQueue]=queue_[at];
                ++keep;
            }
        }
        used_=keep;
    }
    void InterruptAll() {
        for(auto& r:records_) if(r.id && !Finished(r)) {
            r.sealed=true; r.interrupted=true; r.work=0;
        }
        // One clear, no repeated PCM compaction while task holds the IRQ mux.
        head_=used_=0; // also discard untagged prompts
    }
    void HardwareReset() {
        for(auto& r:records_) if(r.id && !Finished(r)) r.valid=false;
        head_=used_=0; slots_={}; discovered_=next_=0; last_stamp_=0;
        hardware_ok_=true;
    }
    void HardwareFault() {
        hardware_ok_=false;
        for(auto& r:records_) if(r.id) r.valid=false;
    }
    // Read just-sent PCM for AEC BEFORE this call. Address is the descriptor
    // returned by the pinned SDK. The tag describes its *previous* refill.
    void Complete(int16_t* address, size_t n, int64_t stamp) {
        if(!address || n!=kBlock || stamp<=0) { HardwareFault(); return; }
        if(last_stamp_ && (stamp-last_stamp_<8500 || stamp-last_stamp_>11500)) HardwareFault();
        last_stamp_=stamp;
        Slot* s=nullptr;
        if(discovered_<kSlots) {
            for(size_t i=0;i<discovered_;++i) if(slots_[i].address==address) {
                HardwareFault(); s=&slots_[i]; break;
            }
            if(!s) { s=&slots_[discovered_++]; s->address=address; }
        } else {
            if(slots_[next_].address!=address) HardwareFault();
            for(auto& slot:slots_) if(slot.address==address) { s=&slot; break; }
            next_=(next_+1)%kSlots;
        }
        if(!s) { HardwareFault(); std::memset(address,0,n*sizeof(int16_t)); return; }
        if(auto* r=Find(s->tag.id)) {
            if(r->valid) {
                if(s->tag.first!=r->played || r->played>kMaxSamples-s->tag.count) r->valid=false;
                else r->played+=s->tag.count;
            }
        }
        s->tag={};
        if(used_) {
            auto& b=queue_[head_];
            std::memcpy(address,b.pcm.data(),b.tag.count*sizeof(int16_t));
            std::memset(address+b.tag.count,0,(kBlock-b.tag.count)*sizeof(int16_t));
            s->tag=b.tag; head_=(head_+1)%kQueue; --used_;
        } else std::memset(address,0,n*sizeof(int16_t));
    }
    PlaybackSnapshot Snapshot(PlaybackIdentity id) const {
        const auto* r=Find(id); if(!r) return {};
        const bool final=Finished(*r);
        const bool valid=r->valid && (!final || r->interrupted || r->played==r->received);
        return {r->id,r->played,valid,final,r->interrupted};
    }
    bool NextReport(int64_t now, PlaybackSnapshot& out) {
        for(auto& r:records_) {
            if(!r.id) continue;
            const bool final=Finished(r);
            if(!final && (now-r.report_time<200000 ||
                (r.reported && r.reported_played==r.played && r.reported_valid==r.valid))) continue;
            out=Snapshot(r.id);
            if(final) r={};
            else { r.report_time=now; r.reported_played=r.played; r.reported_valid=r.valid; r.reported=true; }
            return true;
        }
        return false;
    }
    bool Busy() const {
        if(used_) return true;
        for(const auto& s:slots_) if(s.tag.count) return true;
        return false;
    }
private:
    struct Tag { PlaybackIdentity id{}; uint64_t first=0; uint16_t count=0; };
    struct Block { Tag tag{}; std::array<int16_t,kBlock> pcm{}; };
    struct Slot { int16_t* address=nullptr; Tag tag{}; };
    struct Record {
        PlaybackIdentity id{};
        uint64_t received=0,submitted=0,played=0,reported_played=0;
        int64_t report_time=0;
        uint32_t work=0;
        bool valid=false,sealed=false,interrupted=false,reported=false,reported_valid=false;
    };
    Record* Find(PlaybackIdentity id) {
        if(!id) return nullptr;
        for(auto& r:records_) if(r.id==id) return &r;
        return nullptr;
    }
    const Record* Find(PlaybackIdentity id) const {
        if(!id) return nullptr;
        for(const auto& r:records_) if(r.id==id) return &r;
        return nullptr;
    }
    bool Finished(const Record& r) const {
        if(!r.sealed || r.work) return false;
        for(size_t i=0;i<used_;++i) if(queue_[(head_+i)%kQueue].tag.id==r.id) return false;
        for(const auto& s:slots_) if(s.tag.id==r.id) return false;
        return true;
    }
    std::array<Block,kQueue> queue_{};
    std::array<Slot,kSlots> slots_{};
    std::array<Record,kRecords> records_{};
    size_t head_=0,used_=0,discovered_=0,next_=0;
    int64_t last_stamp_=0;
    uint32_t connection_=0,last_id_=0;
    bool hardware_ok_=true;
};
static_assert(sizeof(PlaybackProgress)<=7360,"playback tracker internal SRAM bound");
} // namespace family_robot
