#include "family_ota_protocol.h"
#include <cassert>
#include <atomic>
#include <new>
#include <cstdlib>
#include <algorithm>
#include <cstring>
#include <vector>
#include <openssl/evp.h>
using esp_err_t=int;using esp_ota_handle_t=unsigned;
constexpr int ESP_OK=0,ESP_FAIL=-1,HTTP_METHOD_GET=0,ESP_ERR_HTTP_EAGAIN=8,ESP_ERR_HTTP_CONNECTING=9,HTTP_EVENT_ERROR=1;
struct esp_partition_t {unsigned address=0x390000,size=0x370000;};
static esp_partition_t partition;
static uint64_t now=100;
static uint64_t Now(){return now;}
struct Http{int post_len=0;bool is_async=true;void* transport=nullptr;};using esp_http_client_handle_t=Http*;
struct esp_http_client_config_t {const char* url;int(*crt_bundle_attach)(void*);int timeout_ms,buffer_size;bool disable_auto_redirect,is_async;};
static int esp_crt_bundle_attach(void*){return 0;}
static std::vector<uint8_t> image,written;
static size_t cursor,fragment=4096;static int status=200,fail=0,begins=0,ends=0,aborts=0,boots=0,cleans=0,read_delay=0;
static bool chunked=false,complete=true,paused=true;
constexpr int MALLOC_CAP_INTERNAL=1,MALLOC_CAP_8BIT=2;
static unsigned buffer_allocations=0,buffer_frees=0,live_buffers=0;
void* heap_caps_malloc(size_t size,int capabilities){assert(size==4096&&capabilities==(MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT));if(fail==16)return nullptr;++buffer_allocations;++live_buffers;return std::malloc(size);}
void heap_caps_free(void* p){assert(p&&live_buffers);--live_buffers;++buffer_frees;std::free(p);}

static int64_t content_length=1024;
#define pdMS_TO_TICKS(n) (n)
constexpr int SHUT_RDWR=2;
struct Timer{void(*callback)(void*)=nullptr;void* arg=nullptr;uint64_t alarm=0;};
using esp_timer_handle_t=Timer*;
struct esp_timer_create_args_t{void(*callback)(void*)=nullptr;void* arg=nullptr;const char* name=nullptr;};
static void vTaskDelay(int){++now;}
static int open_waits=0,header_waits=0,body_waits=0;
static Timer* active_timer=nullptr;static bool socket_closed=false;
static int shutdown(int fd,int type){assert(fd==17&&type==SHUT_RDWR);socket_closed=true;return 0;}
static int esp_http_client_get_socket(Http*){return 17;}
static int esp_timer_create(const esp_timer_create_args_t* a,Timer** out){if(fail==9)return -1;*out=new Timer{a->callback,a->arg,0};active_timer=*out;return 0;}
static int esp_timer_start_once(Timer* t,uint64_t us){if(fail==10)return -1;t->alarm=now+us/1000;return 0;}
static int esp_timer_stop_blocking(Timer* t,int timeout){assert(timeout==200);if(fail==11)return -1;t->alarm=0;return 0;}
static int esp_timer_delete(Timer* t){delete t;active_timer=nullptr;return 0;}
static void Advance(int delay){now+=delay;if(active_timer&&active_timer->alarm&&now>=active_timer->alarm){active_timer->alarm=0;active_timer->callback(active_timer->arg);}}

static std::string auth,device,url;
namespace FamilyOnboarding{static std::string Key(){return "synthetic-test-only";}}
namespace SystemInfo{static std::string GetMacAddress(){return "02:00:00:00:00:01";}}
static const esp_partition_t* esp_ota_get_next_update_partition(void*){return fail==1?nullptr:&partition;}
static Http* esp_http_client_init(const esp_http_client_config_t* c){assert(c->is_async&&c->disable_auto_redirect&&c->timeout_ms==5000&&c->buffer_size==4096);url=c->url;static Http h;return fail==2?nullptr:&h;}
static int esp_http_client_set_header(Http*,const char* k,const char* v){if(std::string(k)=="Authorization")auth=v;if(std::string(k)=="Device-Id")device=v;return 0;}
static int esp_http_client_set_method(Http*,int method){assert(method==HTTP_METHOD_GET);return 0;}
static int esp_http_client_connect(Http*){++open_waits;if(fail==3)return -1;if(fail==12&&open_waits<=3){Advance(100);return ESP_ERR_HTTP_CONNECTING;}if(fail==13){Advance(1000);return ESP_ERR_HTTP_CONNECTING;}return ESP_OK;}
static int esp_http_client_request_send(Http*,int n){assert(n==0);return ESP_OK;}
static void* esp_transport_get_error_handle(void*){return nullptr;}
static void http_dispatch_event(Http*,int,void*,int){}
static void http_dispatch_event_to_event_loop(int,void*,size_t){}
// PRODUCTION_HTTP_OPEN

static int64_t esp_http_client_fetch_headers(Http*){if(fail==15&&header_waits++<2){Advance(100);return -ESP_ERR_HTTP_EAGAIN;}return content_length;}
static int esp_http_client_get_status_code(Http*){return status;}
static bool esp_http_client_is_chunked_response(Http*){return chunked;}
static int esp_http_client_set_timeout_ms(Http*,int n){assert(n>0&&n<=5000);return 0;}
static int esp_http_client_read(Http*,char* data,int length){Advance(read_delay);if(fail==14&&body_waits++<2){Advance(100);return -ESP_ERR_HTTP_EAGAIN;}if(socket_closed)return -1;if(fail==4)return -1;size_t n=std::min({size_t(length),fragment,image.size()-cursor});std::memcpy(data,image.data()+cursor,n);cursor+=n;return int(n);}
static bool esp_http_client_is_complete_data_received(Http*){return complete;}
static int esp_http_client_close(Http*){return 0;}
static int esp_http_client_cleanup(Http*){++cleans;return 0;}
static int esp_ota_begin(const esp_partition_t*,size_t n,unsigned* out){assert(paused&&n==size_t(content_length));++begins;if(fail==5)return -1;*out=7;return 0;}
static int esp_ota_write(unsigned h,const void* data,size_t length){assert(paused&&h==7&&length<=4096);if(fail==6)return -1;auto p=static_cast<const uint8_t*>(data);written.insert(written.end(),p,p+length);return 0;}
static int esp_ota_abort(unsigned h){assert(h==7);++aborts;return 0;}
static int esp_ota_end(unsigned h){assert(paused&&h==7);++ends;return fail==7?-1:0;}
static int esp_ota_set_boot_partition(const esp_partition_t*){assert(paused);++boots;return fail==8?-1:0;}
using psa_hash_operation_t=EVP_MD_CTX*;
#define PSA_HASH_OPERATION_INIT nullptr
constexpr int PSA_SUCCESS=0,PSA_ALG_SHA_256=1;
static int psa_crypto_init(){return 0;}
static int psa_hash_setup(EVP_MD_CTX** p,int){*p=EVP_MD_CTX_new();return EVP_DigestInit_ex(*p,EVP_sha256(),nullptr)==1?0:-1;}
static int psa_hash_update(EVP_MD_CTX** p,const unsigned char* data,size_t n){return EVP_DigestUpdate(*p,data,n)==1?0:-1;}
static int psa_hash_finish(EVP_MD_CTX** p,unsigned char* out,size_t size,size_t* used){assert(size==32);unsigned n=0;int ok=EVP_DigestFinal_ex(*p,out,&n);*used=n;return ok==1?0:-1;}
static int psa_hash_abort(EVP_MD_CTX** p){EVP_MD_CTX_free(*p);return 0;}
static unsigned progress=0;static void Progress(int value){progress=value;}
#include "family_ota_download.inc"
static std::string Digest(const std::vector<uint8_t>& data){uint8_t hash[32];unsigned n;EVP_Digest(data.data(),data.size(),hash,&n,EVP_sha256(),nullptr);return FamilyOta::Hex(hash,32);}
static FamilyOta::Command Fixture(){
 image.assign(9000,0);image[0]=0xe9;image[12]=9;image[32]=0x32;image[33]=0x54;image[34]=0xcd;image[35]=0xab;
 std::memcpy(image.data()+48,"2.5.3",6);std::memcpy(image.data()+80,"xiaozhi",8);std::fill(image.begin()+176,image.begin()+208,0xab);
 FamilyOta::Command c;c.id="ota_"+std::string(32,'a');c.sha256=Digest(image);c.releaseId="fw_"+c.sha256;c.version="2.5.3";c.url=std::string(FamilyOta::Endpoint)+"/"+c.id;c.size=image.size();c.elfSha256=std::string(64,'a');for(size_t i=1;i<64;i+=2)c.elfSha256[i]='b';return c;
}
static void Reset(){assert(live_buffers==0);buffer_allocations=buffer_frees=0;cursor=0;fragment=4096;status=200;fail=0;begins=ends=aborts=boots=cleans=0;now=100;read_delay=0;chunked=false;complete=true;content_length=image.size();partition.size=0x370000;written.clear();progress=0;open_waits=header_waits=body_waits=0;socket_closed=false;FamilyOta::download_disabled=false;}
static std::string Json(const FamilyOta::Command& c){return "{\"command\":{\"id\":\""+c.id+"\",\"releaseId\":\""+c.releaseId+"\",\"version\":\""+c.version+"\",\"url\":\""+c.url+"\",\"size\":"+std::to_string(c.size)+",\"sha256\":\""+c.sha256+"\",\"elfSha256\":\""+c.elfSha256+"\",\"ttlMs\":15000}}";}
int main(){
 auto c=Fixture();FamilyOta::Command parsed;
 assert(FamilyOta::ParseResponse("{\"command\":null}",parsed)&&parsed.id.empty());
 assert(FamilyOta::ParseResponse(Json(c),parsed)&&parsed.sha256==c.sha256);
 for(auto bad:{"{\"command\":null,\"command\":null}","{\"command\":0}","{\"command\":null}x","{\"command\":null,\"extra\":1}"})assert(!FamilyOta::ParseResponse(bad,parsed));
 for(int bad=0;bad<9;++bad){auto b=c;auto text=Json(b);if(bad==0)b.url+="?ticket=x";if(bad==1)b.url="https://evil.invalid/"+c.id;if(bad==2)b.releaseId[3]=b.releaseId[3]=='0'?'1':'0';if(bad==3)b.version="2.5";if(bad==4)b.id[4]='A';if(bad==5)b.size=3538945;if(bad==6)b.elfSha256="ab";text=Json(b);if(bad==7)text.replace(text.find("15000"),5,"15001");if(bad==8)text.replace(text.find("2.5.3"),5,"2.5.3\\u0000evil");assert(!FamilyOta::ParseResponse(text,parsed));}
 Reset();fail=16;auto allocation_error=FamilyOta::Download(c);assert(allocation_error&&std::string(allocation_error)=="download_failed"&&begins==0&&boots==0&&cleans==1&&live_buffers==0);
 for(size_t chunk:{size_t(1),size_t(17),size_t(287),size_t(4096)}){Reset();fragment=chunk;assert(!FamilyOta::Download(c));assert(begins==1&&ends==1&&boots==1&&aborts==0&&cleans==1&&written==image&&progress==100&&buffer_allocations==1&&buffer_frees==1&&live_buffers==0);assert(auth=="Bearer synthetic-test-only"&&device=="02:00:00:00:00:01");}
 for(int f=1;f<=8;++f){Reset();fail=f;assert(FamilyOta::Download(c));assert(boots==(f==8?1:0));assert(aborts==((f==6)?1:0));assert(ends==(f>=7?1:0));assert(live_buffers==0&&buffer_allocations==buffer_frees);}
 Reset();status=302;assert(std::string(FamilyOta::Download(c))=="http_error"&&begins==0);
 Reset();status=429;assert(std::string(FamilyOta::Download(c))=="http_error"&&begins==0);
 Reset();chunked=true;assert(FamilyOta::Download(c)&&begins==0);
 Reset();content_length--;assert(std::string(FamilyOta::Download(c))=="length_mismatch"&&begins==0);
 Reset();partition.size=8000;assert(std::string(FamilyOta::Download(c))=="slot_too_small"&&begins==0);
 Reset();auto bad=c;bad.url+="/extra";assert(std::string(FamilyOta::Download(bad))=="invalid_url"&&begins==0);
 for(size_t offset:{size_t(0),size_t(12),size_t(32),size_t(48),size_t(80),size_t(176)}){auto saved=image[offset];image[offset]^=1;Reset();assert(std::string(FamilyOta::Download(c))=="image_mismatch"&&begins==0);image[offset]=saved;}
 Reset();bad=c;bad.sha256[0]=bad.sha256[0]=='a'?'b':'a';bad.releaseId="fw_"+bad.sha256;assert(std::string(FamilyOta::Download(bad))=="hash_mismatch"&&aborts==1&&ends==0&&boots==0);
 Reset();image.pop_back();assert(std::string(FamilyOta::Download(c))=="length_mismatch"&&aborts==1);image.push_back(0);
 Reset();image.push_back(0);assert(std::string(FamilyOta::Download(c))=="length_mismatch"&&aborts==1);image.pop_back();
 Reset();complete=false;assert(std::string(FamilyOta::Download(c))=="length_mismatch"&&aborts==1);
 Reset();read_delay=5001;assert(std::string(FamilyOta::Download(c))=="timeout"&&boots==0);
 Reset();fragment=1;read_delay=100;assert(std::string(FamilyOta::Download(c))=="timeout"&&boots==0);
 Reset();fail=12;assert(!FamilyOta::Download(c)&&open_waits==4);
 Reset();fail=13;assert(std::string(FamilyOta::Download(c))=="timeout"&&now<=5200&&cleans==1);
 for(int transient:{14,15}){Reset();fail=transient;assert(!FamilyOta::Download(c)&&written==image);}
 for(int failure:{9,10}){Reset();fail=failure;assert(FamilyOta::Download(c)&&cleans==1);}
 Reset();fail=11;assert(std::string(FamilyOta::Download(c))=="timeout"&&cleans==0&&!FamilyOta::DownloadAvailable());
 // Quarantined client/FD/context must outlive a late timer callback. Release
 // synthetic resources only after proving the callback has stopped in this test.
 assert(active_timer);auto context=active_timer->arg;active_timer->callback(context);assert(socket_closed);delete active_timer;delete static_cast<FamilyOta::IoDeadline*>(context);active_timer=nullptr;

}
