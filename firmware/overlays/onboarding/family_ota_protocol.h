#pragma once
#include "family_control_protocol.h"
#include "family_ota.h"
#include <cstdint>
namespace FamilyOta {
constexpr uint32_t SlotBytes=0x370000,MaxImage=SlotBytes-65536;
struct Command {std::string id,releaseId,version,url,sha256,elfSha256;uint32_t size=0;};
inline std::string Hex(const uint8_t* data,size_t length) {
    const char digits[]="0123456789abcdef";std::string result;result.reserve(length*2);
    for(size_t i=0;i<length;++i){result+=digits[data[i]>>4];result+=digits[data[i]&15];}return result;
}
inline bool HexString(const std::string& value,size_t length) {
    if(value.size()!=length)return false;
    for(auto c:value)if(!((c>='0'&&c<='9')||(c>='a'&&c<='f')))return false;
    return true;
}
inline bool Version(const std::string& value) {
    if(value.empty()||value.size()>31)return false;
    unsigned fields=0,digits=0;
    for(auto c:value){if(c=='.'){if(!digits||++fields>2)return false;digits=0;}else if(c>='0'&&c<='9'){if(++digits>9)return false;}else return false;}
    return fields==2&&digits>0;
}
inline bool ValidUrl(const Command& command) {
    return command.id.size()==36&&command.id.compare(0,4,"ota_")==0&&HexString(command.id.substr(4),32)&&
        command.url==std::string(Endpoint)+"/"+command.id;
}
inline bool ParseResponse(const std::string& body,Command& command) {
    command={};
    if(body.empty()||body.size()>2048||body.find('\0')!=std::string::npos||body.find("\\u0000")!=std::string::npos)return false;
    const char* end=nullptr;auto root=cJSON_ParseWithLengthOpts(body.c_str(),body.size()+1,&end,true);
    bool valid=root&&end==body.data()+body.size()&&FamilyControl::ExactObject(root,{"command"});
    auto item=cJSON_GetObjectItemCaseSensitive(root,"command");
    if(valid&&cJSON_IsNull(item)){cJSON_Delete(root);return true;}
    valid=valid&&FamilyControl::ExactObject(item,{"id","releaseId","version","url","size","sha256","elfSha256","ttlMs"});
    const char* names[]={"id","releaseId","version","url","sha256","elfSha256"};
    std::string* values[]={&command.id,&command.releaseId,&command.version,&command.url,&command.sha256,&command.elfSha256};
    for(unsigned i=0;i<6&&valid;++i){auto value=cJSON_GetObjectItemCaseSensitive(item,names[i]);valid=cJSON_IsString(value)&&value->valuestring;if(valid)*values[i]=value->valuestring;}
    auto size=cJSON_GetObjectItemCaseSensitive(item,"size"),ttl=cJSON_GetObjectItemCaseSensitive(item,"ttlMs");
    valid=valid&&cJSON_IsNumber(size)&&std::isfinite(size->valuedouble)&&size->valuedouble>=288&&size->valuedouble<=MaxImage&&std::floor(size->valuedouble)==size->valuedouble&&
        cJSON_IsNumber(ttl)&&ttl->valuedouble==15000&&Version(command.version)&&ValidUrl(command)&&
        HexString(command.sha256,64)&&HexString(command.elfSha256,64)&&command.releaseId=="fw_"+command.sha256;
    if(valid)command.size=static_cast<uint32_t>(size->valuedouble);
    cJSON_Delete(root);if(!valid)command={};return valid;
}
// ESP-IDF esp_image_header(24), first segment header(8), esp_app_desc(256).
// SDK esp_ota_end performs the complete image/chip/checksum validation as well.
inline bool ImageIdentity(const uint8_t* data,const Command& command) {
    return data[0]==0xe9&&data[12]==9&&data[13]==0&&
        data[32]==0x32&&data[33]==0x54&&data[34]==0xcd&&data[35]==0xab&&
        std::memchr(data+48,0,32)&&std::string(reinterpret_cast<const char*>(data+48))==command.version&&
        std::memchr(data+80,0,32)&&std::strcmp(reinterpret_cast<const char*>(data+80),"xiaozhi")==0&&
        Hex(data+176,32)==command.elfSha256;
}
}
