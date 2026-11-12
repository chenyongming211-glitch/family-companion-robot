#ifndef MUSIC_AUDITION_PROTOCOL_H_
#define MUSIC_AUDITION_PROTOCOL_H_
#include <cstddef>
#include <cstdint>

// Wire/session validation only. The application owns exclusion, bounded PCM
// queues, hardware profile changes, physical completion and response emission.
namespace music_audition {
constexpr size_t kHeaderBytes = 24;
constexpr size_t kMaxPayloadBytes = 2880;
constexpr uint64_t kIdleTimeoutMs = 5000;
constexpr uint64_t kSessionTimeoutMs = 120000;
enum class Op : uint32_t { Start=1, Data=2, End=3, Stop=4 };
struct Frame {
    Op op{};
    uint32_t seq=0, arg=0, size=0;
    // Borrowed from input bytes: valid only while the caller retains that buffer.
    const uint8_t* payload=nullptr;
};
inline uint32_t Read32(const uint8_t* p) {
    return uint32_t(p[0]) | uint32_t(p[1])<<8 | uint32_t(p[2])<<16 | uint32_t(p[3])<<24;
}
inline void Write32(uint8_t* p, uint32_t value) {
    for (unsigned i=0;i<4;++i) p[i]=uint8_t(value>>(8*i));
}
inline uint64_t Read64(const uint8_t* p) {
    return uint64_t(Read32(p)) | uint64_t(Read32(p+4))<<32;
}
inline uint32_t CrcUpdate(uint32_t crc, const uint8_t* p, size_t size) {
    for(size_t i=0;i<size;++i) {
        crc ^= p[i];
        for(unsigned j=0;j<8;++j) crc=(crc>>1) ^ (0xedb88320u & (0u-(crc&1u)));
    }
    return crc;
}
// IEEE CRC32: first 20 header bytes, then payload; checksum field is excluded.
// Caller guarantees a complete header and size payload bytes for this helper.
inline uint32_t FrameCrc(const uint8_t* bytes, size_t size) {
    return CrcUpdate(CrcUpdate(0xffffffffu,bytes,20),bytes+24,size)^0xffffffffu;
}
inline bool ValidFrame(const Frame& f) {
    if(f.size>kMaxPayloadBytes || (f.size && !f.payload)) return false;
    switch(f.op) {
        case Op::Start:
            return f.seq==0 && (f.arg==24000 || f.arg==48000) && f.size==8 &&
                Read64(f.payload)>0 && Read64(f.payload)<=uint64_t(f.arg)*120;
        case Op::Data: return f.arg==0 && f.size>0 && (f.size%2)==0;
        case Op::End: case Op::Stop: return f.arg==0 && f.size==0;
        default: return false;
    }
}
inline bool ParseFrame(const uint8_t* bytes, size_t size, Frame* frame) {
    if(!bytes || !frame || size<kHeaderBytes || size>kHeaderBytes+kMaxPayloadBytes) return false;
    if(bytes[0]!='F'||bytes[1]!='M'||bytes[2]!='A'||bytes[3]!='1') return false;
    const uint32_t payload_size=Read32(bytes+16);
    if(payload_size>kMaxPayloadBytes || size!=kHeaderBytes+payload_size) return false;
    if(Read32(bytes+20)!=FrameCrc(bytes,payload_size)) return false;
    Frame candidate{static_cast<Op>(Read32(bytes+4)),Read32(bytes+8),Read32(bytes+12),payload_size,bytes+24};
    if(!ValidFrame(candidate)) return false;
    *frame=candidate;
    return true;
}
class Session {
public:
    void Reset() { *this=Session{}; }
    // Returns true only when an active session expired (or clock moved back).
    // Invalid/expired sessions are inactive; only a fresh valid Start can resume.
    bool CheckTimeout(uint64_t now_ms) {
        if(active_ && (now_ms<last_ms_ || now_ms-last_ms_>=kIdleTimeoutMs ||
                       now_ms-start_ms_>=kSessionTimeoutMs)) { active_=false; return true; }
        return false;
    }
    // Preferred entry point: parse errors invalidate the session, too. Frame
    // payload remains borrowed from bytes, and no hardware side effect occurs.
    bool AcceptBytes(const uint8_t* bytes,size_t size,uint64_t now_ms,Frame* output) {
        Frame f{};
        if(!ParseFrame(bytes,size,&f) || !output) return Fail();
        if(!Accept(f,now_ms)) return false;
        *output=f;
        return true;
    }
    bool Accept(const Frame& f,uint64_t now_ms) {
        if(!ValidFrame(f)) return Fail();
        if(CheckTimeout(now_ms)) return false;
        if(f.op==Op::Start) {
            if(active_) return Fail();
            rate_=f.arg; total_=Read64(f.payload); received_=0; next_=1;
            active_=true; start_ms_=last_ms_=now_ms; return true;
        }
        if(!active_ || f.seq!=next_) return Fail();
        if(f.op==Op::Data) {
            const uint64_t count=f.size/2;
            if(count>total_-received_) return Fail();
            received_+=count;
        } else {
            if(f.op==Op::End && received_!=total_) return Fail();
            // End and Stop mark the *protocol* session inactive immediately.
            // Caller must still drain/cancel, restore+verify 24kHz, then emit
            // DONE/STOPPED. Inactive does not prove hardware restored or idle.
            active_=false;
        }
        ++next_;last_ms_=now_ms;return true;
    }
    bool active() const {return active_;}
    uint32_t rate() const {return rate_;}
    uint32_t expected_seq() const {return next_;}
    uint64_t total_samples() const {return total_;}
    uint64_t received_samples() const {return received_;}
private:
    bool Fail() {active_=false;return false;}
    bool active_=false;
    uint32_t rate_=0,next_=0;
    uint64_t total_=0,received_=0,start_ms_=0,last_ms_=0;
};
} // namespace music_audition
#endif
