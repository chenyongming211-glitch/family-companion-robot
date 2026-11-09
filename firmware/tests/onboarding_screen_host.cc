#include "family_setup_state.h"
#include <array>
#include <cassert>
#include <cstring>

int main() {
    using namespace FamilyOnboarding;
    const char* id="dev_0123456789abcdef0123456789abcdef";
    std::array<uint8_t,CodeSize> bytes{};
    std::memcpy(bytes.data(),"FRC1",4); bytes[4]=24; bytes[5]=1; bytes[6]=24; bytes[7]=1;
    std::memcpy(bytes.data()+8,id,36); bytes[48]=0x81; bytes.back()=0x40;
    auto crc=CodeCrc(bytes.data()+48,CodePixels);
    for(int i=0;i<4;++i) bytes[44+i]=static_cast<uint8_t>(crc>>(8*i));
    assert(ValidCode(bytes.data(),bytes.size(),id));
    assert(!ValidCode(nullptr,bytes.size(),id));
    assert(!ValidCode(bytes.data(),bytes.size()-1,id));
    assert(!ValidCode(bytes.data(),bytes.size(),"dev_1123456789abcdef0123456789abcdef"));
    bytes[4]=25; assert(!ValidCode(bytes.data(),bytes.size(),id)); bytes[4]=24;
    bytes[100]^=1; assert(!ValidCode(bytes.data(),bytes.size(),id)); bytes[100]^=1;
    bytes[0]='x'; assert(!ValidCode(bytes.data(),bytes.size(),id)); bytes[0]='F';
    assert(ValidCode(bytes.data(),bytes.size(),id));
    SetupScreenState state;
    auto first=state.Begin(); assert(state.phase==SetupPhase::Waiting);
    assert(state.Apply(first,SetupPhase::Connecting));
    assert(state.Apply(first,SetupPhase::Clock));
    assert(state.Apply(first,SetupPhase::Confirming));
    assert(state.Apply(first,SetupPhase::Success));
    assert(!state.Apply(first,SetupPhase::Expired));
    assert(!state.Apply(first,SetupPhase::WifiError));
    assert(state.Apply(first,SetupPhase::Hidden));
    auto second=state.Begin(); assert(second!=first);
    assert(!state.Apply(first,SetupPhase::Success));
    assert(state.Apply(second,SetupPhase::WifiError));
    assert(state.Apply(second,SetupPhase::Connecting));
    assert(state.Apply(second,SetupPhase::Expired));
    assert(!state.Apply(second,SetupPhase::Connecting));
    // A valid cloud receipt arriving late is authoritative even after local expiry.
    assert(state.Apply(second,SetupPhase::Success));
    assert(!state.Apply(second,SetupPhase::ClockError));
    auto third=state.Begin(); assert(state.Apply(third,SetupPhase::ClockError));
    assert(!state.Apply(third,SetupPhase::Confirming));
    assert(!state.Apply(third,SetupPhase::Hidden));
}
