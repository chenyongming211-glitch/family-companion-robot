#pragma once
#include <array>
#include <cmath>
#include <cstdint>
#include <cstring>

namespace wave_wake_lab {
enum class Kind { Invalid,Status,Start,Stop,Probe };
struct Request {Kind kind=Kind::Invalid;unsigned seconds=0;};
inline Request Parse(const char* s) {
    if(!s)return {};
    if(!std::strcmp(s,"wwd status"))return {Kind::Status,0};
    if(!std::strcmp(s,"wwd start"))return {Kind::Start,0};
    if(!std::strcmp(s,"wwd stop"))return {Kind::Stop,0};
    if(!std::strncmp(s,"wwd probe ",10)) {
        const char* n=s+10;
        if(n[0]>='1'&&n[0]<='9'&&!n[1])return {Kind::Probe,unsigned(n[0]-'0')};
        if(!std::strcmp(n,"10"))return {Kind::Probe,10};
    }
    return {};
}
struct Stats {
    uint64_t energy=0;int64_t sum=0;
    uint32_t samples=0,peak=0,clipped=0;
    void Add(const int16_t* x,size_t n,size_t stride) {
        for(size_t i=0;i<n;++i) {
            const int32_t v=x[i*stride];const uint32_t mag=v<0?uint32_t(-v):uint32_t(v);
            energy+=uint64_t(int64_t(v)*v);sum+=v;++samples;
            if(mag>peak)peak=mag;
            if(mag>=32760)++clipped;
        }
    }
    void Merge(const Stats& b) {energy+=b.energy;sum+=b.sum;samples+=b.samples;clipped+=b.clipped;if(b.peak>peak)peak=b.peak;}
    double Rms()const{return samples?std::sqrt(double(energy)/samples):0;}
    double AcRms()const {if(!samples)return 0;const double dc=double(sum)/samples;const double v=double(energy)/samples-dc*dc;return v>0?std::sqrt(v):0;}
};
struct Snapshot {uint32_t id=0;bool done=false;std::array<Stats,3> channels{};};
// Caller serializes window transitions and merges, never the sample loop.
class Window {
    uint32_t id_=0;int64_t until_=0;std::array<Stats,3> channels_{};
public:
    bool Begin(int64_t now,unsigned seconds) {
        if(until_||seconds<1||seconds>10||id_==UINT32_MAX)return false;
        ++id_;until_=now+int64_t(seconds)*1000000;channels_={};return true;
    }
    uint32_t Token(int64_t now)const{return until_&&now<until_?id_:0;}
    void Merge(uint32_t token,unsigned channel,const Stats& s) {if(until_&&token==id_&&channel<channels_.size())channels_[channel].Merge(s);}
    Snapshot Take(int64_t now) {
        if(!until_)return {};
        Snapshot result{id_,now>=until_,channels_};channels_={};
        if(result.done)until_=0;
        return result;
    }
};
}
