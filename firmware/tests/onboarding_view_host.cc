#include <cstdint>
#include <cstddef>
#include <cstring>
#include <vector>
#include <string>
#include <cassert>
struct lv_font_t {};
enum {LV_OBJ_FLAG_SCROLLABLE=1,LV_OBJ_FLAG_HIDDEN=2,LV_OBJ_FLAG_CLICKABLE=4,
      LV_OPA_COVER=255,LV_TEXT_ALIGN_CENTER=0,LV_LABEL_LONG_WRAP=1,
      LV_IMAGE_HEADER_MAGIC=25,LV_COLOR_FORMAT_I1=7};
struct lv_image_dsc_t {
    struct {int magic=0,cf=0,w=0,h=0,stride=0;} header;
    uint32_t data_size=0;const uint8_t* data=nullptr;
};
struct lv_obj_t {
    lv_obj_t* parent=nullptr;std::vector<lv_obj_t*> children;
    int x=0,y=0,w=0,h=0,flags=0;std::string text;const lv_image_dsc_t* image=nullptr;
};
lv_obj_t top;
lv_obj_t* lv_layer_top(){return &top;}
lv_obj_t* lv_obj_create(lv_obj_t* p){auto* o=new lv_obj_t;o->parent=p;p->children.push_back(o);return o;}
lv_obj_t* lv_label_create(lv_obj_t* p){return lv_obj_create(p);}
lv_obj_t* lv_image_create(lv_obj_t* p){return lv_obj_create(p);}
void lv_obj_delete(lv_obj_t* p){for(auto* c:p->children)lv_obj_delete(c);delete p;}
void lv_obj_set_size(lv_obj_t* p,int w,int h){p->w=w;p->h=h;}
void lv_obj_set_pos(lv_obj_t* p,int x,int y){p->x=x;p->y=y;}
void lv_obj_remove_flag(lv_obj_t* p,int f){p->flags&=~f;}
void lv_obj_add_flag(lv_obj_t* p,int f){p->flags|=f;}
void lv_obj_set_style_transform_pivot_x(lv_obj_t*,int,int){}
void lv_obj_set_style_transform_pivot_y(lv_obj_t*,int,int){}
void lv_obj_set_style_transform_scale_x(lv_obj_t*,int,int){}
void lv_obj_set_style_transform_scale_y(lv_obj_t*,int,int){}
void lv_obj_set_style_radius(lv_obj_t*,int,int){}
void lv_obj_set_style_border_width(lv_obj_t*,int,int){}
void lv_obj_set_style_pad_all(lv_obj_t*,int,int){}
void lv_obj_set_style_bg_opa(lv_obj_t*,int,int){}
void lv_obj_set_style_bg_color(lv_obj_t*,uint32_t,int){}
void lv_obj_set_style_text_color(lv_obj_t*,uint32_t,int){}
void lv_obj_set_style_text_align(lv_obj_t*,int,int){}
void lv_obj_set_style_text_font(lv_obj_t*,const lv_font_t*,int){}
uint32_t lv_color_hex(uint32_t c){return c;}
void lv_label_set_long_mode(lv_obj_t*,int){}
void lv_label_set_text(lv_obj_t* p,const char* t){p->text=t;}
void lv_image_set_src(lv_obj_t* p,const void* d){p->image=static_cast<const lv_image_dsc_t*>(d);}
void lv_image_set_antialias(lv_obj_t*,bool){}
void lv_image_cache_drop(const void*){}
void lv_obj_move_foreground(lv_obj_t*){}
#include "family_setup_view.h"
#include "family_setup_state.h"
// PRODUCTION_VIEW
int main(){
    FamilyOnboarding::SetupView view;
    lv_font_t font;
    view.Initialize(&font,480,480);
    auto* root=top.children.back();assert(root->flags&LV_OBJ_FLAG_HIDDEN);
    std::vector<uint8_t> bits(9800,0);bits[0]=0x81;bits.back()=0x40;
    assert(view.Update("微信扫码添加设备","请在小程序中完成登录和配网",bits.data(),bits.size()));
    assert(!(root->flags&LV_OBJ_FLAG_HIDDEN));assert(root->w==480&&root->h==480);
    auto* title=root->children[0];auto* card=root->children[1];auto* detail=root->children[2];
    auto* image=card->children[0];const auto* d=image->image;
    assert(title->text=="微信扫码添加设备" && detail->text=="请在小程序中完成登录和配网");
    assert(card->x==88&&card->y==100&&card->w==304&&card->h==304);
    assert(d&&d->header.w==280&&d->header.h==280&&d->header.stride==36&&d->header.cf==LV_COLOR_FORMAT_I1);
    assert(d->data_size==10088&&d->data[0]==255&&d->data[7]==255);
    assert(d->data[4]==0&&d->data[5]==0&&d->data[6]==0);
    assert(d->data[8]==0x81&&d->data[8+279*36+34]==0x40);
    for(int y=0;y<280;++y){assert(d->data[8+y*36+35]==0);}
    bits.assign(9800,255);assert(d->data[8]==0x81); // caller can release its bitmap
    assert(!view.Update("bad","bad",bits.data(),1));
    assert(view.Update("连接 Wi-Fi","请稍候",nullptr,0));
    assert(card->flags&LV_OBJ_FLAG_HIDDEN);assert(title->y==160);
    assert(view.Update(nullptr,nullptr,nullptr,0));assert(root->flags&LV_OBJ_FLAG_HIDDEN);
    assert(view.Update("添加成功","设备已绑定家庭",nullptr,0));
    assert(!(root->flags&LV_OBJ_FLAG_HIDDEN)&&title->text=="添加成功");
    FamilyOnboarding::SetupView small;
    small.Initialize(&font,240,240);
    auto* round=top.children.back();
    auto* round_title=round->children[0];auto* round_card=round->children[1];auto* round_detail=round->children[2];
    // A synthetic circular code has no secret or actual claim. Check every sampled bit.
    std::vector<uint8_t> circle(9800,0);
    for(int y=0;y<280;++y)for(int x=0;x<280;++x)
        if((x-140)*(x-140)+(y-140)*(y-140)<130*130 && (x+y)%3==0)circle[y*35+x/8]|=0x80>>(x%8);
    assert(small.Update("微信扫码添加设备","请在小程序中完成登录和配网",circle.data(),circle.size()));
    assert(round_card->x==8&&round_card->y==8&&round_card->w==224&&round_card->h==224);
    assert(round_title->flags&LV_OBJ_FLAG_HIDDEN);assert(round_detail->flags&LV_OBJ_FLAG_HIDDEN);
    auto* round_image=round_card->children[0];auto* rd=round_image->image;
    assert(round_image->x==0&&round_image->y==0);
    assert(rd&&rd->header.w==224&&rd->header.h==224&&rd->header.stride==28&&rd->data_size==6280);
    for(int y=0;y<224;++y)for(int x=0;x<224;++x){
        int sx=x*280/224,sy=y*280/224;
        assert(((rd->data[8+y*28+x/8]>>(7-x%8))&1)==((circle[sy*35+sx/8]>>(7-sx%8))&1));
    }
    for(const char* status:{"连接 Wi-Fi","正在校时","等待云端确认","添加失败","添加超时","添加成功"}){
        assert(small.Update(status,"请稍候",nullptr,0));
        assert(round_card->flags&LV_OBJ_FLAG_HIDDEN);
        assert(!(round_title->flags&LV_OBJ_FLAG_HIDDEN)&&!(round_detail->flags&LV_OBJ_FLAG_HIDDEN));
        assert(round_title->x>=30&&round_title->y>=40&&round_title->w/2<=180&&round_title->h/2<=60);
        assert(round_detail->x>=30&&round_detail->y>=100&&round_detail->w/2<=180&&round_detail->y+round_detail->h/2<=200);
    }
    assert(small.Update(nullptr,nullptr,nullptr,0));assert(round->flags&LV_OBJ_FLAG_HIDDEN);
}
