#include <algorithm>
#include <cassert>
#include <cstdint>
#include <cstring>
#include <functional>
#include <map>
#include <memory>
#include <string>
#include <vector>
#include <cstdlib>
#include <cstdio>
struct Error {std::string ToString()const{return "fake";}};
struct Result {Error error()const{return {};}int value=0;bool ok=true;explicit operator bool()const{return ok;}int operator*()const{return value;}};
struct Script {size_t announced=0;std::vector<uint8_t> body;std::vector<int> reads;size_t index=0,offset=0;}script;
struct Http {Result Open(const char*,const std::string&){return {};}Result GetStatusCode(){return {200,true};}
 size_t GetBodyLength(){return script.announced;}
 Result Read(char* buffer,size_t capacity){int n=script.index<script.reads.size()?script.reads[script.index++]:0;
  if(n>0){const auto count=std::min({size_t(n),capacity,script.body.size()-std::min(script.offset,script.body.size())});std::memcpy(buffer,script.body.data()+script.offset,count);script.offset+=count;}return {n,true};}
};
struct Network {std::unique_ptr<Http>CreateHttp(int){return std::make_unique<Http>();}};
struct Display {bool MountAssets(const char*){return true;}};
struct Board {Display display;Display* GetDisplay(){return &display;}static Board& GetInstance(){static Board board;return board;}Network network;Network* GetNetwork(){return &network;}};
using esp_err_t=int;
constexpr int ESP_OK=0,MALLOC_CAP_INTERNAL=1,ESP_PARTITION_MMAP_DATA=0,SPI_FLASH_MMAP_DATA=0;
struct esp_partition_t {size_t size=0x200000;};static esp_partition_t partition;
static std::vector<uint8_t> flash(0x200000,0xa5);static unsigned erased=0,writes=0,unmaps=0,maps=0,unapplies=0;
static size_t esp_partition_get_main_flash_sector_size(){return 4096;}
static void* heap_caps_malloc(size_t n,int){return std::malloc(n);}static void heap_caps_free(void* p){std::free(p);}
static int esp_partition_erase_range(const esp_partition_t*,size_t offset,size_t size){assert(offset<=0x50000&&size<=0x50000-offset);std::fill(flash.begin()+offset,flash.begin()+offset+size,0xff);erased+=size;return 0;}
static int esp_partition_write(const esp_partition_t*,size_t offset,const void* bytes,size_t size){assert(offset<=0x50000&&size<=0x50000-offset);std::memcpy(flash.data()+offset,bytes,size);++writes;return 0;}
static int esp_partition_read(const esp_partition_t*,size_t offset,void* bytes,size_t size){assert(offset+size<=flash.size());std::memcpy(bytes,flash.data()+offset,size);return 0;}
static int spi_flash_mmap_get_free_pages(int){return 100;}
static int esp_partition_mmap(const esp_partition_t*,size_t,size_t size,int,const void** bytes,unsigned* handle){assert(size<=0x50000);*bytes=flash.data();*handle=1;++maps;return 0;}
static void esp_partition_munmap(unsigned){++unmaps;}
static int64_t esp_timer_get_time(){return 1;}
[[maybe_unused]] static const char* esp_err_to_name(int){return "fake";}
template<class... A> static void Log(A&&...){}
#define ESP_LOGI(...) Log(__VA_ARGS__)
#define ESP_LOGD(...) Log(__VA_ARGS__)
#define ESP_LOGE(...) Log(__VA_ARGS__)
#define TAG "Assets"
#define PARTITION_LABEL "assets"
struct mmap_assets_table {char asset_name[32];uint32_t asset_size,asset_offset;uint16_t width,height;};
struct Asset {size_t size,offset;};
struct Assets {
 const esp_partition_t* partition_=&partition;bool partition_valid_=true;
 void DisableTextFontGlyphPush(){}
 static bool FindPartition(Assets*){return true;}bool InitializePartition(){return true;}
 void UnApplyPartition(){++unapplies;}
 bool DownloadC3Pcm(std::string,std::function<void(int,size_t)>);
 struct EmoteStrategy {bool InitializePartition(Assets*);};
 struct LvglStrategy {const char* mmap_root_=nullptr;unsigned mmap_handle_=0;bool checksum_valid_=false;std::map<std::string,Asset> assets_;
  static uint32_t CalculateChecksum(const char*,uint32_t);bool InitializePartition(Assets*);
  void UnApplyPartition(Assets*){if(mmap_handle_)esp_partition_munmap(mmap_handle_);mmap_handle_=0;mmap_root_=nullptr;assets_.clear();checksum_valid_=false;}};
};
#include "c3_pcm_assets.inc"
// PRODUCTION_TABLE_METHODS
static void Reset(size_t size=64){script={};script.announced=size;script.body.assign(size,1);uint32_t stored=size-12;std::memcpy(script.body.data()+8,&stored,4);erased=writes=unapplies=maps=unmaps=0;std::fill(flash.begin(),flash.end(),0xa5);}
static void AssertTrailer(){assert(std::all_of(flash.begin()+0x50000,flash.end(),[](uint8_t x){return x==0xa5;}));}
static void Download(){Assets assets;
 Reset();script.reads={5,7,52,0};assert(assets.DownloadC3Pcm("x",{}));assert(erased==4096&&unapplies==1&&writes==2);AssertTrailer();
 Reset();script.body[8]^=1;script.reads={64};assert(!assets.DownloadC3Pcm("x",{}));assert(!erased&&!unapplies&&!writes);AssertTrailer();
 Reset(0x50001);script.reads={4096};assert(!assets.DownloadC3Pcm("x",{}));assert(!erased&&!unapplies);AssertTrailer();
 Reset();script.reads={11,4097};assert(!assets.DownloadC3Pcm("x",{}));assert(!erased&&!unapplies);AssertTrailer();
 Reset();script.reads={64,1};assert(!assets.DownloadC3Pcm("x",{}));assert(writes==1);AssertTrailer();
 Reset();script.reads={13,-1};assert(!assets.DownloadC3Pcm("x",{}));AssertTrailer();
 Reset(0x50000);script.reads.assign(80,4096);script.reads.push_back(0);assert(assets.DownloadC3Pcm("x",{}));assert(erased==0x50000);AssertTrailer();
 Reset();script.reads={10,0};assert(!assets.DownloadC3Pcm("x",{}));assert(!erased&&!unapplies);AssertTrailer();
}
static void Header(uint32_t files,uint32_t len){std::memcpy(flash.data(),&files,4);std::memcpy(flash.data()+8,&len,4);const auto sum=Assets::LvglStrategy::CalculateChecksum(reinterpret_cast<const char*>(flash.data()+12),len);std::memcpy(flash.data()+4,&sum,4);}
static void Table(){Assets assets;Assets::EmoteStrategy emote;assert(emote.InitializePartition(&assets));Assets::LvglStrategy strategy;
 Reset();Header(1,sizeof(mmap_assets_table)+6);mmap_assets_table entry{};std::strcpy(entry.asset_name,"test");entry.asset_size=4;entry.asset_offset=0;
 std::memcpy(flash.data()+12,&entry,sizeof(entry));Header(1,sizeof(entry)+6);assert(strategy.InitializePartition(&assets));assert(strategy.assets_.size()==1);
 strategy.UnApplyPartition(&assets);Reset();Header(0,0x50000);assert(!strategy.InitializePartition(&assets)&&maps==0);
 Reset();Header(100,8);assert(!strategy.InitializePartition(&assets)&&unmaps==1);
 Reset();std::memset(entry.asset_name,'x',32);std::memcpy(flash.data()+12,&entry,sizeof(entry));Header(1,sizeof(entry)+6);assert(!strategy.InitializePartition(&assets)&&unmaps==1);
 Reset();std::strcpy(entry.asset_name,"test");entry.asset_offset=UINT32_MAX;std::memcpy(flash.data()+12,&entry,sizeof(entry));Header(1,sizeof(entry)+6);assert(!strategy.InitializePartition(&assets)&&unmaps==1);
 Reset();entry.asset_offset=0;entry.asset_size=5;std::memcpy(flash.data()+12,&entry,sizeof(entry));Header(1,sizeof(entry)+6);assert(!strategy.InitializePartition(&assets)&&unmaps==1);
}
int main(int argc,char** argv){assert(argc==2);if(std::string(argv[1])=="download")Download();else Table();}
