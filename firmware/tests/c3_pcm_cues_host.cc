#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <iterator>
#include <new>
#include <string>
#include <vector>
#include <openssl/sha.h>
#include "c3_pcm_cues.h"
#ifdef FAMILY_C3_PCM_GENERATED_HEADER
#include FAMILY_C3_PCM_GENERATED_HEADER
#endif
static bool forbid_alloc=false;
void* operator new(std::size_t n) { if(forbid_alloc) std::abort(); if(void* p=std::malloc(n)) return p; throw std::bad_alloc(); }
void* operator new[](std::size_t n) { return ::operator new(n); }
void operator delete(void* p) noexcept { std::free(p); }
void operator delete[](void* p) noexcept { std::free(p); }
void operator delete(void* p,std::size_t) noexcept { std::free(p); }
void operator delete[](void* p,std::size_t) noexcept { std::free(p); }
#define CHECK(x) do { if(!(x)) { std::fprintf(stderr,"line %d: %s\n",__LINE__,#x); return 1; } } while(0)
using namespace family_robot;
static bool Verify(const uint8_t* bytes,size_t size,const uint8_t* expected,void*) {
    uint8_t actual[32]; SHA256(bytes,size,actual); return std::memcmp(expected,actual,32)==0;
}
static uint32_t Read(const uint8_t* p) { return uint32_t(p[0])|(uint32_t(p[1])<<8)|(uint32_t(p[2])<<16)|(uint32_t(p[3])<<24); }
static void Write(uint8_t* p,uint32_t n) { for(unsigned i=0;i<4;++i) p[i]=static_cast<uint8_t>(n>>(i*8)); }
static uint32_t Crc(const uint8_t* p,size_t n) {
    uint32_t crc=UINT32_MAX;
    for(size_t i=0;i<n;++i) { crc^=p[i]; for(unsigned b=0;b<8;++b) crc=(crc>>1)^((crc&1u) ? 0xedb88320u : 0u); }
    return ~crc;
}
int main(int argc,char** argv) {
    CHECK(argc==3); std::string mode=argv[2]; std::ifstream file(argv[1],std::ios::binary);
    std::vector<uint8_t> storage(1,0xff); storage.insert(storage.end(),std::istreambuf_iterator<char>(file),{});
    CHECK(storage.size()>160); uint8_t* pack=storage.data()+1; const size_t size=storage.size()-1;
    bool invalid=true, refresh=true;
    if(mode=="valid" || mode=="real" || mode=="digest" || mode=="null-hash" || mode=="body") { invalid=mode!="valid" && mode!="real"; refresh=false; }
    else if(mode=="magic") pack[0]^=1;
    else if(mode=="total") Write(pack+8,static_cast<uint32_t>(size-1));
    else if(mode=="count") Write(pack+12,UINT32_MAX);
    else if(mode=="table") Write(pack+16,65);
    else if(mode=="rate") Write(pack+20,16000);
    else if(mode=="channels") Write(pack+24,2);
    else if(mode=="bits") Write(pack+28,8);
    else if(mode=="duplicate") std::memcpy(pack+112,pack+64,32);
    else if(mode=="alignment") Write(pack+96,Read(pack+96)+1);
    else if(mode=="table-overlap") Write(pack+96,64);
    else if(mode=="data-overlap") { Write(pack+144,Read(pack+96)); Write(pack+156,Crc(pack+Read(pack+144),Read(pack+152))); }
    else if(mode=="range") Write(pack+96,0xfffffff0u);
    else if(mode=="samples") Write(pack+100,UINT32_MAX);
    else if(mode=="clock") Write(pack+100,1439);
    else if(mode=="bytes") Write(pack+104,2879);
    else if(mode=="crc") pack[Read(pack+96)]^=1;
    else CHECK(false);
    if(refresh) SHA256(pack+64,size-64,pack+32);
    if(mode=="body") pack[32]^=1;
    uint8_t expected[32]; SHA256(pack,size,expected); if(mode=="digest") expected[0]^=1;
#ifdef FAMILY_C3_PCM_GENERATED_HEADER
    if(mode=="real") { CHECK(size==kFamilyC3PcmPackBytes); std::memcpy(expected,kFamilyC3PcmPackSha256,32); }
#endif
    C3PcmCuePack reader; C3PcmCuePack::Descriptor d; int16_t frame[1440];
    CHECK(!reader.Find(pack+64,d)); CHECK(reader.CopyFrame(d,0,frame,1440)==C3PcmCuePack::CopyResult::Invalid);
    forbid_alloc=true;
    const bool opened=reader.Open(pack,size,expected,mode=="null-hash" ? nullptr : Verify,nullptr);
    if(invalid) { CHECK(!opened); CHECK(reader.count()==0); return 0; }
    if(mode=="real") {
        CHECK(opened && reader.count()==21);
        uint32_t total=0;
        for(uint32_t i=0;i<reader.count();++i) {
            CHECK(reader.Find(pack+64+i*48,d)); CHECK(d.index==i); total+=d.samples;
            for(uint32_t n=0;n<d.samples/1440;++n) CHECK(reader.CopyFrame(d,n,frame,1440)==C3PcmCuePack::CopyResult::Ok);
            CHECK(reader.CopyFrame(d,d.samples/1440,frame,1440)==C3PcmCuePack::CopyResult::End);
        }
        CHECK(total==744480); return 0;
    }
    CHECK(opened); CHECK(reader.count()==2); CHECK(reader.Find(pack+64,d)); CHECK(d.index==0 && d.samples==1440);
    CHECK(reader.CopyFrame(d,0,frame,1440)==C3PcmCuePack::CopyResult::Ok);
    for(size_t i=0;i<1440;++i) CHECK(frame[i]==static_cast<int16_t>(static_cast<int>(i)-720));
    CHECK(reader.CopyFrame(d,1,frame,1440)==C3PcmCuePack::CopyResult::End);
    CHECK(reader.CopyFrame(d,2,frame,1440)==C3PcmCuePack::CopyResult::Invalid);
    CHECK(reader.CopyFrame(d,0,frame,1439)==C3PcmCuePack::CopyResult::Invalid);
    CHECK(reader.CopyFrame(d,0,nullptr,1440)==C3PcmCuePack::CopyResult::Invalid);
    d.samples=2880; CHECK(reader.CopyFrame(d,0,frame,1440)==C3PcmCuePack::CopyResult::Invalid);
    CHECK(reader.Find(pack+112,d)); CHECK(d.samples==2880);
    CHECK(reader.CopyFrame(d,1,frame,1440)==C3PcmCuePack::CopyResult::Ok);
    for(size_t i=0;i<1440;++i) CHECK(frame[i]==-1234);
    uint8_t absent[32]{}; CHECK(!reader.Find(absent,d));
    CHECK(!reader.Open(pack,63,expected,Verify,nullptr)); CHECK(reader.count()==0);
    CHECK(!reader.Open(pack,C3PcmCuePack::kMaxBytes+1,expected,Verify,nullptr));
    CHECK(!reader.Open(nullptr,size,expected,Verify,nullptr));
    CHECK(!reader.Open(pack,size,nullptr,Verify,nullptr));
    return 0;
}
