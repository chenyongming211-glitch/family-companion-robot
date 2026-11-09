#pragma once
#include <cstddef>
#include <cstdint>
#include <cstring>
#include <string_view>

namespace FamilyOnboarding {
constexpr std::size_t CodeWidth=280, CodePixels=9800, CodeSize=9848;
inline uint32_t CodeCrc(const uint8_t* bytes,std::size_t size) {
    uint32_t crc=0xffffffffu;
    for(std::size_t i=0;i<size;++i) {
        crc^=bytes[i];
        for(int bit=0;bit<8;++bit) crc=(crc>>1)^((crc&1)?0xedb88320u:0u);
    }
    return ~crc;
}
inline bool ValidCode(const uint8_t* bytes,std::size_t size,std::string_view id) {
    if(!bytes || size!=CodeSize || id.size()!=36 || id.substr(0,4)!="dev_")return false;
    for(char c:id.substr(4))if(!((c>='0'&&c<='9')||(c>='a'&&c<='f')))return false;
    if(std::memcmp(bytes,"FRC1",4) || bytes[4]!=24 || bytes[5]!=1 ||
       bytes[6]!=24 || bytes[7]!=1 || std::memcmp(bytes+8,id.data(),36))return false;
    const auto crc=uint32_t(bytes[44])|(uint32_t(bytes[45])<<8)|
                   (uint32_t(bytes[46])<<16)|(uint32_t(bytes[47])<<24);
    return crc==CodeCrc(bytes+48,CodePixels);
}
enum class SetupPhase { Hidden,Waiting,Connecting,WifiError,Clock,ClockError,
                        Confirming,Success,Expired,CodeError,StartError };
// Mutated only under the existing onboarding mutex. An epoch binds worker UI
// results to their own provisioning attempt; cloud receipts remain authoritative.
struct SetupScreenState {
    uint32_t epoch=0;
    SetupPhase phase=SetupPhase::Hidden;
    uint32_t Begin() {
        if(++epoch==0)++epoch;
        phase=SetupPhase::Waiting; return epoch;
    }
    bool Apply(uint32_t source,SetupPhase next) {
        if(source!=epoch || next==SetupPhase::Waiting)return false;
        if(phase==SetupPhase::Success)return next==SetupPhase::Hidden?(phase=next,true):false;
        if(phase==SetupPhase::Hidden)return false;
        if(next==SetupPhase::Hidden)return false;
        const bool terminal=phase==SetupPhase::Expired || phase==SetupPhase::ClockError ||
                            phase==SetupPhase::CodeError || phase==SetupPhase::StartError;
        if(terminal && next!=SetupPhase::Success)return false;
        phase=next; return true;
    }
};
}
