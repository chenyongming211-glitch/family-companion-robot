#pragma once
#include <cstdint>
#include <string>

namespace FamilyControl {
enum class Field { Volume, Brightness };
struct Command { std::string id; Field field=Field::Volume; int value=-1; };
inline bool Valid(const Command& command) {
    if(command.id.size()!=36 || command.id.substr(0,4)!="cmd_" || command.value<0 || command.value>100) return false;
    for(size_t i=4;i<command.id.size();++i) if(!((command.id[i]>='0'&&command.id[i]<='9')||(command.id[i]>='a'&&command.id[i]<='f'))) return false;
    return true;
}
// Accessed only under the worker/main-thread mutex. A command is applied once;
// a driver snapshot is required before acknowledging success, including fades.
class State {
    Command command_;
    uint64_t offered_=0;
    bool started_=false,done_=false,applied_=false;
    int actual_=-1;
public:
    bool Offer(const Command& command,uint64_t now) {
        if(!Valid(command) || command.id==command_.id || (!command_.id.empty()&&!done_)) return false;
        command_=command;offered_=now;started_=done_=applied_=false;actual_=-1;return true;
    }
    const Command* Take(uint64_t now) {
        if(command_.id.empty()||started_||done_)return nullptr;
        if(now-offered_>=15000){done_=true;return nullptr;}
        started_=true;return &command_;
    }
    void Observe(int volume,int brightness,uint64_t now) {
        if(!started_||done_)return;
        actual_=command_.field==Field::Volume?volume:brightness;
        if(now-offered_>=15000){done_=true;return;}
        if(actual_==command_.value){done_=applied_=true;return;}
        if(actual_<0)done_=true;
    }
    bool HasAck()const{return done_;}
    bool Applied()const{return applied_;}
    int Actual()const{return actual_;}
    const Command& Current()const{return command_;}
};
void Connected(bool connected);
bool FirmwareBusy();
bool ReserveFirmware();
void ReleaseFirmware();
}
