#pragma once
#include "family_control_state.h"
#include "cJSON.h"
#include <cmath>
#include <cstring>
#include <initializer_list>

namespace FamilyControl {
inline bool ExactObject(const cJSON* object,std::initializer_list<const char*> keys) {
    if(!cJSON_IsObject(object))return false;
    size_t count=0;
    for(auto entry=object->child;entry;entry=entry->next) {
        if(!entry->string)return false;
        size_t matches=0;
        for(auto key:keys)if(std::strcmp(key,entry->string)==0)++matches;
        if(matches!=1)return false;
        for(auto prior=object->child;prior!=entry;prior=prior->next)if(std::strcmp(prior->string,entry->string)==0)return false;
        ++count;
    }
    return count==keys.size();
}
inline bool ParseResponse(const std::string& body,Command& command) {
    command={};
    if(body.empty()||body.size()>2048||body.find("\\u0000")!=std::string::npos)return false;
    const char* end=nullptr;
    auto root=cJSON_ParseWithLengthOpts(body.c_str(),body.size()+1,&end,true);
    bool valid=root&&end==body.data()+body.size()&&ExactObject(root,{"command"});
    auto item=cJSON_GetObjectItemCaseSensitive(root,"command");
    if(valid&&cJSON_IsNull(item)){cJSON_Delete(root);return true;}
    valid=valid&&ExactObject(item,{"id","field","value"});
    auto id=cJSON_GetObjectItemCaseSensitive(item,"id"),field=cJSON_GetObjectItemCaseSensitive(item,"field"),value=cJSON_GetObjectItemCaseSensitive(item,"value");
    valid=valid&&cJSON_IsString(id)&&cJSON_IsString(field)&&cJSON_IsNumber(value)&&std::isfinite(value->valuedouble)&&value->valuedouble>=0&&value->valuedouble<=100&&std::floor(value->valuedouble)==value->valuedouble;
    if(valid) {
        command.id=id->valuestring;command.value=static_cast<int>(value->valuedouble);
        if(std::strcmp(field->valuestring,"volume")==0)command.field=Field::Volume;
        else if(std::strcmp(field->valuestring,"brightness")==0)command.field=Field::Brightness;
        else valid=false;
        valid=valid&&Valid(command);
    }
    cJSON_Delete(root);if(!valid)command={};return valid;
}
inline void Reading(cJSON* object,const char* key,int value) {
    if(value<0)cJSON_AddNullToObject(object,key);else cJSON_AddNumberToObject(object,key,value);
}
inline std::string Report(const std::string& boot,uint64_t seq,int volume,int brightness,const State& state) {
    auto root=cJSON_CreateObject();if(!root)return {};
    cJSON_AddStringToObject(root,"boot",boot.c_str());cJSON_AddNumberToObject(root,"seq",static_cast<double>(seq));
    Reading(root,"volume",volume);Reading(root,"brightness",brightness);
    if(state.HasAck()) {
        auto ack=cJSON_AddObjectToObject(root,"ack");
        cJSON_AddStringToObject(ack,"id",state.Current().id.c_str());
        cJSON_AddStringToObject(ack,"status",state.Applied()?"applied":"failed");Reading(ack,"actual",state.Actual());
    }
    char* encoded=cJSON_PrintUnformatted(root);cJSON_Delete(root);
    if(!encoded)return {};
    std::string result(encoded);cJSON_free(encoded);return result;
}
}
