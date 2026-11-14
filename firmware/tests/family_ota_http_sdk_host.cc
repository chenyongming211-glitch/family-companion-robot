// Real locked SDK header/body loops, with only transport and parser plumbing
// substituted. Exercise async WANT_READ after partial transport progress.
#include <cassert>
#include <cstdint>
#include <cstring>
#include <cerrno>
#include <string>
#include <vector>
#include <algorithm>
using esp_err_t=int;
constexpr int ESP_FAIL=-1,ESP_ERR_HTTP_EAGAIN=8,ESP_OK=0,HTTP_STATE_REQ_COMPLETE_HEADER=1,HTTP_STATE_REQ_COMPLETE_DATA=2,HTTP_STATE_RES_COMPLETE_HEADER=3,HTTP_STATE_RES_ON_DATA_START=4;
constexpr int ERR_TCP_TRANSPORT_CONNECTION_TIMEOUT=-9,ERR_TCP_TRANSPORT_CONNECTION_CLOSED_BY_FIN=-10,HTTP_EVENT_ERROR=1,ESP_LOG_WARN=1,ESP_LOG_DEBUG=0;
using esp_log_level_t=int;
#define ESP_LOGD(...) ((void)0)
#define ESP_LOGW(...) ((void)0)
#define ESP_LOGE(...) ((void)0)
#define ESP_LOG_LEVEL(sev,...) ((void)(sev))
struct esp_http_buffer_t{char* data=nullptr;int len=0,raw_len=0;char* raw_data=nullptr;char* output_ptr=nullptr;};
struct Response{esp_http_buffer_t* buffer=nullptr;int status_code=-1;int64_t content_length=4,data_process=0;bool is_chunked=false;};
struct Client{Response* response;int state=HTTP_STATE_REQ_COMPLETE_HEADER,buffer_size_rx=4096,timeout_ms=5000;bool is_chunk_complete=false;void* transport=nullptr;void* parser=nullptr;void* parser_settings=nullptr;};
using esp_http_client_handle_t=Client*;
static std::vector<std::string> transport;static size_t step=0;static Client* active=nullptr;static bool headers=true;
static int esp_transport_read(void*,char* out,int capacity,int timeout){assert(timeout==5000&&step<transport.size());auto text=transport[step++];if(text=="EAGAIN")return ERR_TCP_TRANSPORT_CONNECTION_TIMEOUT;assert(text.size()<=size_t(capacity));std::memcpy(out,text.data(),text.size());return text.size();}
static size_t http_parser_execute(void*,void*,const char* data,size_t n){auto& c=*active;auto& b=*c.response->buffer;if(headers){if(std::string(data,n)=="H"){c.state=HTTP_STATE_RES_COMPLETE_HEADER;c.response->status_code=200;}}else{assert(b.output_ptr);std::memcpy(b.output_ptr,data,n);b.raw_len=n;c.response->data_process+=n;}return n;}
static void esp_http_client_cached_buf_cleanup(esp_http_buffer_t*){}
static bool esp_http_client_is_complete_data_received(Client* c){return c->response->data_process==c->response->content_length;}
static int esp_transport_translate_error(int){return -1;}
static const char* esp_err_to_name(int){return "synthetic";}
static void* esp_transport_get_error_handle(void*){return nullptr;}
static void http_dispatch_event(Client*,int,void*,int){}
static void http_dispatch_event_to_event_loop(int,void*,size_t){}
// PRODUCTION_SDK_IO
int main(){
 char buffer[4096]{};esp_http_buffer_t b;b.data=buffer;Response response;response.buffer=&b;Client c;c.response=&response;active=&c;
 transport={"partial","EAGAIN","H"};step=0;
 assert(esp_http_client_fetch_headers(&c)==-ESP_ERR_HTTP_EAGAIN);assert(c.state==HTTP_STATE_REQ_COMPLETE_DATA);
 assert(esp_http_client_fetch_headers(&c)==4&&c.state==HTTP_STATE_RES_ON_DATA_START&&response.status_code==200);
 headers=false;transport={"a","EAGAIN","EAGAIN","bcd"};step=0;char out[4]{};
 assert(esp_http_client_read(&c,out,4)==1&&out[0]=='a'&&response.data_process==1);
 assert(esp_http_client_read(&c,out+1,3)==-ESP_ERR_HTTP_EAGAIN&&response.data_process==1);
 assert(esp_http_client_read(&c,out+1,3)==3&&std::string(out,4)=="abcd"&&esp_http_client_is_complete_data_received(&c));
 assert(esp_http_client_read(&c,out,1)==0);
}
