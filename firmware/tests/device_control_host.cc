#include "family_control_state.h"
#include "family_control_protocol.h"
#include <cassert>
#include <iostream>
using namespace FamilyControl;
int main() {
    Command command;
    assert(ParseResponse("{\"command\":null}",command) && command.id.empty());
    const std::string id="cmd_"+std::string(32,'a');
    const std::string valid="{\"command\":{\"id\":\""+id+"\",\"field\":\"brightness\",\"value\":40}}";
    assert(ParseResponse(valid,command) && command.field==Field::Brightness && command.value==40);
    for(const auto& bad: {"{}", "{\"Command\":null}", "{\"command\":null,\"extra\":1}", "{\"command\":null,\"command\":null}", "{\"command\":{\"id\":\"bad\",\"field\":\"volume\",\"value\":4}}"}) assert(!ParseResponse(bad,command));
    for(const auto& value: {"-1","101","1.5","null","true","\"20\""}) {
        const auto bad="{\"command\":{\"id\":\""+id+"\",\"field\":\"volume\",\"value\":"+value+"}}";
        assert(!ParseResponse(bad,command));
    }
    assert(!ParseResponse(valid+"{}",command));
    assert(!ParseResponse(valid+std::string("\0tail",5),command));
    for(const auto& bad: std::initializer_list<std::string>{
        "{\"command\\u0000suffix\":null}",
        "{\"command\":{\"id\":\""+id+"\",\"field\":\"volume\\u0000suffix\",\"value\":40}}",
        "{\"command\":{\"id\":\""+id+"\\u0000suffix\",\"field\":\"volume\",\"value\":40}}"}) assert(!ParseResponse(bad,command));
    assert(ParseResponse(valid,command));
    State state;
    assert(state.Offer(command,100));
    assert(!state.Offer(command,101));
    assert(state.Take(102));
    assert(!state.Take(103));
    state.Observe(60,30,104);
    assert(!state.HasAck()); // Brightness fade has not reached its target.
    state.Observe(60,40,200);
    assert(state.HasAck() && state.Applied() && state.Actual()==40);
    assert(!state.Offer(command,201));
    State late;
    assert(late.Offer(command,0));assert(late.Take(1));
    late.Observe(60,30,2);late.Observe(60,40,16000);
    assert(late.HasAck()&&!late.Applied());
    command.id="cmd_"+std::string(32,'b');command.field=Field::Volume;command.value=20;
    assert(state.Offer(command,300));assert(state.Take(301));
    state.Observe(-1,40,302);
    assert(state.HasAck() && !state.Applied() && state.Actual()==-1);
    command.id="cmd_"+std::string(32,'c');
    assert(state.Offer(command,400));assert(state.Take(401));
    state.Observe(50,40,16000);
    assert(state.HasAck() && !state.Applied());
    command.id="cmd_"+std::string(32,'d');
    assert(state.Offer(command,20000));
    assert(!state.Take(36000)); // A delayed main-thread callback cannot apply an expired command.
    assert(state.HasAck() && !state.Applied());
    auto packet=Report(std::string(32,'e'),9,20,-1,state);
    auto json=cJSON_Parse(packet.c_str());assert(json);
    assert(cJSON_GetObjectItemCaseSensitive(json,"seq")->valuedouble==9);
    assert(cJSON_IsNull(cJSON_GetObjectItemCaseSensitive(json,"brightness")));
    auto ack=cJSON_GetObjectItemCaseSensitive(json,"ack");
    assert(std::string(cJSON_GetObjectItemCaseSensitive(ack,"status")->valuestring)=="failed");
    cJSON_Delete(json);
    std::cout<<"ok\n";
}
