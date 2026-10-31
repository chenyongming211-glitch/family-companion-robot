#pragma once
#include <algorithm>
#include <array>
#include <cstddef>
#include <cstdint>

namespace family_robot {
// Single capture-task owner. Input is already jointly resampled 16k MR.
// No allocation, clock, platform call or VAD policy belongs in frame assembly.
class C3EchoStream {
public:
    static constexpr size_t kFrame=512,kMaxInput=1024;
    void Begin(uint32_t generation) { generation_=generation;used_=0;healthy_=generation!=0; }
    size_t pending() const { return used_; }
    template<class Process,class Emit>
    bool Feed(const int16_t* mr,size_t frames,uint32_t generation,Process&& process,Emit&& emit) {
        if(!healthy_ || !generation || generation!=generation_)return false;
        if(!mr || !frames || frames>kMaxInput) { healthy_=false;used_=0;return false; }
        size_t offset=0;
        while(offset<frames) {
            const auto count=std::min(kFrame-used_,frames-offset);
            for(size_t i=0;i<count;++i) {
                mic_[used_+i]=mr[2*(offset+i)];
                ref_[used_+i]=mr[2*(offset+i)+1];
            }
            offset+=count;used_+=count;
            if(used_==kFrame) {
                if(!process(mic_.data(),ref_.data(),out_.data(),kFrame)) {
                    healthy_=false;used_=0;return false;
                }
                emit(out_.data(),kFrame);used_=0;
            }
        }
        return true;
    }
private:
    alignas(16) std::array<int16_t,kFrame> mic_{},ref_{},out_{};
    size_t used_=0;
    uint32_t generation_=0;
    bool healthy_=false;
};
} // namespace family_robot
