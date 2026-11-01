#pragma once
#include <cstddef>
#include <cstdint>
#include <cstring>
namespace family_robot {
// Immutable mapped bytes must outlive the reader and may start at any address.
// Open is required before OTA validation. The platform callback must compute
// SHA256 and compare all 32 bytes; a CRC or a constant-success stub is unsafe.
class C3PcmCuePack {
public:
    static constexpr uint32_t kHeaderBytes=64, kEntryBytes=48, kMaxCues=32;
    static constexpr uint32_t kMaxBytes=0x200000-0x50000, kFrameSamples=1440;
    struct Descriptor { uint32_t index=0, samples=0; };
    enum class CopyResult { Ok, End, Invalid };
    using VerifyHash=bool (*)(const uint8_t*,size_t,const uint8_t*,void*);
    bool Open(const uint8_t* bytes,size_t size,const uint8_t* expected,VerifyHash verify,void* context) {
        bytes_=nullptr; count_=0;
        if(!bytes || !expected || !verify || size<kHeaderBytes || size>kMaxBytes) return false;
        if(std::memcmp(bytes,"FPCM24V1",8) || Read(bytes+8)!=size || Read(bytes+16)!=64 ||
           Read(bytes+20)!=24000 || Read(bytes+24)!=1 || Read(bytes+28)!=16) return false;
        const auto count=Read(bytes+12);
        if(!count || count>kMaxCues || uint64_t(kHeaderBytes)+uint64_t(count)*kEntryBytes>size) return false;
        if(!verify(bytes,size,expected,context) || !verify(bytes+64,size-64,bytes+32,context)) return false;
        const uint32_t data_start=(kHeaderBytes+count*kEntryBytes+15u)&~15u;
        uint64_t last_end=data_start;
        for(uint32_t i=0;i<count;++i) {
            const auto* entry=bytes+kHeaderBytes+i*kEntryBytes;
            const auto offset=Read(entry+32), samples=Read(entry+36), length=Read(entry+40);
            const uint64_t end=uint64_t(offset)+length;
            if(offset<data_start || (offset&15u) || !samples || samples%kFrameSamples ||
               uint64_t(samples)*2!=length || end>size) return false;
            if(Crc32(bytes+offset,length)!=Read(entry+44)) return false;
            if(end>last_end) last_end=end;
            for(uint32_t j=0;j<i;++j) {
                const auto* earlier=bytes+kHeaderBytes+j*kEntryBytes;
                const auto earlier_offset=Read(earlier+32), earlier_length=Read(earlier+40);
                if(!std::memcmp(entry,earlier,32) ||
                   (uint64_t(offset)<uint64_t(earlier_offset)+earlier_length && uint64_t(earlier_offset)<end)) return false;
            }
        }
        if(last_end!=size) return false;
        bytes_=bytes; count_=count; return true;
    }
    bool Find(const uint8_t* source_sha256,Descriptor& descriptor) const {
        if(!source_sha256) return false;
        for(uint32_t i=0;i<count_;++i) {
            const auto* entry=Entry(i);
            if(!std::memcmp(source_sha256,entry,32)) { descriptor={i,Read(entry+36)}; return true; }
        }
        return false;
    }
    CopyResult CopyFrame(Descriptor descriptor,uint32_t frame,int16_t* destination,size_t count) const {
        if(!bytes_ || descriptor.index>=count_ || !destination || count!=kFrameSamples) return CopyResult::Invalid;
        const auto* entry=Entry(descriptor.index); const auto samples=Read(entry+36);
        if(descriptor.samples!=samples) return CopyResult::Invalid;
        if(frame==samples/kFrameSamples) return CopyResult::End;
        if(frame>samples/kFrameSamples) return CopyResult::Invalid;
        const auto* pcm=bytes_+Read(entry+32)+size_t(frame)*kFrameSamples*2;
        for(size_t i=0;i<kFrameSamples;++i) {
            const uint16_t value=uint16_t(pcm[i*2])|(uint16_t(pcm[i*2+1])<<8);
            destination[i]=static_cast<int16_t>(value<0x8000 ? int32_t(value) : int32_t(value)-65536);
        }
        return CopyResult::Ok;
    }
    uint32_t count() const { return count_; }
private:
    static uint32_t Read(const uint8_t* p) {
        return uint32_t(p[0])|(uint32_t(p[1])<<8)|(uint32_t(p[2])<<16)|(uint32_t(p[3])<<24);
    }
    static uint32_t Crc32(const uint8_t* bytes,size_t size) {
        uint32_t crc=UINT32_MAX;
        for(size_t i=0;i<size;++i) {
            crc^=bytes[i];
            for(unsigned bit=0;bit<8;++bit) crc=(crc>>1)^((crc&1u) ? 0xedb88320u : 0u);
        }
        return ~crc;
    }
    const uint8_t* Entry(uint32_t i) const { return bytes_+kHeaderBytes+i*kEntryBytes; }
    const uint8_t* bytes_=nullptr;
    uint32_t count_=0;
};
static_assert(sizeof(C3PcmCuePack)<=16,"bounded cue mapping metadata");
}
