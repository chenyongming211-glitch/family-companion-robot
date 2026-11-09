#include <assert.h>
#include <stdio.h>
#include <stdlib.h>
#include <stdint.h>
#include <stdbool.h>
#include <string.h>
#include <sys/types.h>
#include <stdarg.h>
typedef int esp_gatt_if_t;
typedef int esp_err_t;
enum { ESP_OK=0,ESP_GATT_OK=0,ESP_GATT_ERROR=1 };
typedef struct {struct {bool is_prep,need_rsp;uint16_t conn_id,handle;unsigned trans_id;uint8_t* value;size_t len;} write;} esp_ble_gatts_cb_param_t;
static char logs[4096];static const char* endpoint="prov-session";static int handler_status;static unsigned now;
static void info(const char* tag,const char* fmt,...){(void)tag;va_list a;va_start(a,fmt);vsnprintf(logs+strlen(logs),sizeof(logs)-strlen(logs),fmt,a);va_end(a);strcat(logs,"\n");}
#define ESP_LOGI(...) info(__VA_ARGS__)
#define TAG "test"
static unsigned esp_log_timestamp(void){return now++;}
static const char* handle_to_handler(uint16_t h){(void)h;return endpoint;}
static struct {void* pc_ble;} internal;
#define protoble_internal (&internal)
static int protocomm_req_handle(void* p,const char* ep,unsigned c,const uint8_t* in,size_t len,uint8_t** out,ssize_t* n){(void)p;(void)ep;(void)c;assert(len==10);assert(memcmp(in,"TOP_SECRET",10)==0);*n=4;*out=malloc(4);memcpy(*out,"KEY!",4);now+=7;return handler_status;}
static int esp_ble_gatts_set_attr_value(unsigned h,size_t n,const uint8_t* b){(void)h;(void)n;(void)b;return ESP_OK;}
static int esp_ble_gatts_send_response(int g,unsigned c,unsigned t,int s,void* b){(void)g;(void)c;(void)t;(void)s;(void)b;return ESP_OK;}
static void simple_ble_gatts_clear_char_values(void){}
static void esp_ble_gatts_close(int g,unsigned c){(void)g;(void)c;}
#include "protocomm_ble_framed_transport.inc"
int main(void){
 (void)info;
 uint8_t packet[]={0x46,0x52,1,1,1,0,0,0,10,0,'T','O','P','_','S','E','C','R','E','T'};
 esp_ble_gatts_cb_param_t param={.write={.conn_id=1,.handle=2,.need_rsp=true,.value=packet,.len=sizeof(packet)}};
 assert(transport_family_framed_write(0,&param));
 assert(strstr(logs,"request endpoint=prov-session len=10"));assert(strstr(logs,"complete endpoint=prov-session status=0 outlen=4 elapsed_ms=8"));
 assert(!strstr(logs,"TOP_SECRET")&&!strstr(logs,"KEY!"));
 family_ble_frame_reset(&family_frame);logs[0]=0;endpoint="PRIVATE_ENDPOINT";handler_status=9;
 assert(transport_family_framed_write(0,&param));assert(strstr(logs,"endpoint=other"));assert(strstr(logs,"status=9"));assert(!strstr(logs,"PRIVATE_ENDPOINT"));
 puts("ok");
}
