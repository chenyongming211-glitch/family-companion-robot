// Cloud actual-LVGL render probe. Uses only a synthetic circular bitmap.
#include <lvgl.h>
#include <vector>
#include <cassert>
#include <cstdio>
#include <fstream>
#include <string>
#define private public
#include "family_setup_view.h"
#undef private
#include "family_setup_state.h"
#include "family_setup_font.h"
static uint16_t frame[240*240];
static unsigned tick=0;
static uint32_t ticks(){return tick;}
static void flush(lv_display_t* d,const lv_area_t* a,uint8_t* data){
 auto* pix=(uint16_t*)data;
 for(int y=a->y1;y<=a->y2;y++)for(int x=a->x1;x<=a->x2;x++)frame[y*240+x]=*pix++;
 lv_display_flush_ready(d);
}
static void refresh(lv_display_t* d){tick+=50;lv_timer_handler();lv_refr_now(d);}
static void save(int phase){
 std::ofstream f("round-phase-"+std::to_string(phase)+".ppm",std::ios::binary);f<<"P6\n240 240\n255\n";
 for(int y=0;y<240;y++)for(int x=0;x<240;x++){
  auto v=frame[y*240+x];if((2*x-239)*(2*x-239)+(2*y-239)*(2*y-239)>240*240)v=0;
  unsigned char b[3]={(unsigned char)(((v>>11)&31)*255/31),(unsigned char)(((v>>5)&63)*255/63),(unsigned char)((v&31)*255/31)};f.write((char*)b,3);
 }
}
int main(){
 lv_init();lv_tick_set_cb(ticks);auto* disp=lv_display_create(240,240);lv_display_set_color_format(disp,LV_COLOR_FORMAT_RGB565);
 static uint8_t buf[240*240*2];lv_display_set_buffers(disp,buf,nullptr,sizeof(buf),LV_DISPLAY_RENDER_MODE_FULL);lv_display_set_flush_cb(disp,flush);
 using namespace FamilyOnboarding;
 {
 SetupView view;view.Initialize(&family_setup_font,240,240);
 std::vector<uint8_t> code(CodePixels,0);
 for(int y=0;y<280;y++)for(int x=0;x<280;x++)if((x-140)*(x-140)+(y-140)*(y-140)<130*130&&(x*3+y*5)%17<7)code[y*35+x/8]|=0x80>>(x%8);
 assert(view.Update("微信扫码添加设备","扫码后按提示配网",code.data(),code.size()));refresh(disp);
 lv_area_t img;lv_obj_get_coords(view.image_,&img);assert(img.x1==8&&img.y1==8&&img.x2==231&&img.y2==231);
 assert(lv_obj_has_flag(view.title_,LV_OBJ_FLAG_HIDDEN)&&lv_obj_has_flag(view.detail_,LV_OBJ_FLAG_HIDDEN));
 int black=0;
 for(int y=0;y<224;y++)for(int x=0;x<224;x++){
  bool bit=code[(y*280/224)*35+(x*280/224)/8]&(0x80>>((x*280/224)%8));
  assert(frame[(8+y)*240+8+x]==(bit?0:65535));
  if(bit){++black;assert((2*(8+x)-239)*(2*(8+x)-239)+(2*(8+y)-239)*(2*(8+y)-239)<=240*240);}
 }
 printf("PASS 50176 exact RGB565 pixels; 224x224 image at 8,8; %d black synthetic pixels inside physical round mask; no label overlay\n",black);save(0);
 // PHASES
 const char* titles[]={"正在连接 Wi-Fi","Wi-Fi 连接失败","正在同步时间","时间同步失败","正在确认家庭绑定","添加成功","配网已超时","设备码未准备好","配网启动失败"};
 const char* details[]={"请保持设备通电，稍候片刻","检查网络密码重试","已连上网络，正在准备安全连接","请检查网络，按设备按钮重新配网","请保持设备和手机联网","设备已绑定家庭，即将进入机器人","请按设备按钮，重新扫码配网","请联系设备管理员检查出厂配置","请重启设备后重试"};
 for(int i=0;i<9;i++){
  assert(view.Update(titles[i],details[i],nullptr,0));refresh(disp);
  assert(lv_obj_has_flag(view.card_,LV_OBJ_FLAG_HIDDEN));
  for(auto* label:{view.title_,view.detail_}){
   assert(!lv_obj_has_flag(label,LV_OBJ_FLAG_HIDDEN));
   lv_point_t measured;lv_text_get_size(&measured,lv_label_get_text(label),&family_setup_font,0,0,360,LV_TEXT_FLAG_NONE);
   assert(measured.y<=lv_obj_get_height(label));
   printf("phase %d %s logical text %d,%d; visible half scale\n",i+1,label==view.title_?"title":"detail",measured.x,measured.y);
  }
  int ink=0;const auto background=frame[0];
  for(int y=0;y<240;y++)for(int x=0;x<240;x++)if(frame[y*240+x]!=background){
   ++ink;assert((2*x-239)*(2*x-239)+(2*y-239)*(2*y-239)<=240*240);
  }
  assert(ink>100); // Visible text, and none of its rendered pixels are outside the round screen.
  save(i+1);
 }
 for(int i=0;i<100;i++){
  assert(view.Update("微信扫码添加设备","",code.data(),code.size()));refresh(disp);
  assert(view.Update(nullptr,nullptr,nullptr,0));refresh(disp);
 }
 }
 refresh(disp);lv_display_delete(disp);lv_deinit();puts("PASS 9 statuses; 100 show/hide cycles; destruction/refresh");
}
