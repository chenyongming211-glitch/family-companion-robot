#include "family_setup_view.h"
#include "family_setup_state.h"
#include <cstring>

namespace FamilyOnboarding {
SetupView::~SetupView() {
    if(root_)lv_obj_delete(root_);
    lv_image_cache_drop(&descriptor_);
}
void SetupView::Initialize(const lv_font_t* font,int width,int height) {
    if(root_)return;
    width_=width;
    root_=lv_obj_create(lv_layer_top());
    lv_obj_set_size(root_,width,height);lv_obj_set_pos(root_,0,0);
    lv_obj_set_style_radius(root_,0,0);lv_obj_set_style_border_width(root_,0,0);
    lv_obj_set_style_pad_all(root_,0,0);lv_obj_set_style_bg_opa(root_,LV_OPA_COVER,0);
    lv_obj_set_style_bg_color(root_,lv_color_hex(0xf4f6ff),0);
    lv_obj_remove_flag(root_,LV_OBJ_FLAG_SCROLLABLE);lv_obj_remove_flag(root_,LV_OBJ_FLAG_CLICKABLE);
    title_=lv_label_create(root_);
    card_=lv_obj_create(root_);
    lv_obj_set_size(card_,304,304);lv_obj_set_pos(card_,(width-304)/2,100);
    lv_obj_set_style_radius(card_,12,0);lv_obj_set_style_border_width(card_,0,0);
    lv_obj_set_style_pad_all(card_,0,0);lv_obj_set_style_bg_opa(card_,LV_OPA_COVER,0);
    lv_obj_set_style_bg_color(card_,lv_color_hex(0xffffff),0);
    lv_obj_remove_flag(card_,LV_OBJ_FLAG_SCROLLABLE);
    image_=lv_image_create(card_);lv_obj_set_pos(image_,12,12);lv_image_set_antialias(image_,false);
    detail_=lv_label_create(root_);
    for(auto* label:{title_,detail_}) {
        lv_obj_set_style_text_font(label,font,0);
        lv_obj_set_style_text_align(label,LV_TEXT_ALIGN_CENTER,0);
        lv_label_set_long_mode(label,LV_LABEL_LONG_WRAP);
    }
    lv_obj_set_style_text_color(title_,lv_color_hex(0x4d58ef),0);
    lv_obj_set_style_text_color(detail_,lv_color_hex(0x555d75),0);
    lv_obj_add_flag(root_,LV_OBJ_FLAG_HIDDEN);
}
bool SetupView::Update(const char* title,const char* detail,const uint8_t* packed,std::size_t size) {
    if(!root_ || (packed && size!=CodePixels) || (!packed && size))return false;
    if(!title) {lv_obj_add_flag(root_,LV_OBJ_FLAG_HIDDEN);return true;}
    const bool code=packed!=nullptr;
    lv_label_set_text(title_,title);lv_label_set_text(detail_,detail?detail:"");
    lv_obj_set_size(title_,330,48);lv_obj_set_pos(title_,(width_-330)/2,code?44:160);
    lv_obj_set_size(detail_,320,64);lv_obj_set_pos(detail_,(width_-320)/2,code?410:236);
    if(code) {
        // Native indexed image keeps the exact 1-bit code and uses ~10KiB;
        // palette0=opaque white, palette1=opaque black. Rows align to four bytes.
        lv_image_set_src(image_,nullptr);lv_image_cache_drop(&descriptor_);
        pixels_.assign(8+36*CodeWidth,0);
        for(int i=0;i<4;++i)pixels_[i]=255;
        pixels_[7]=255;
        for(std::size_t y=0;y<CodeWidth;++y)std::memcpy(pixels_.data()+8+y*36,packed+y*35,35);
        descriptor_.header.magic=LV_IMAGE_HEADER_MAGIC;
        descriptor_.header.cf=LV_COLOR_FORMAT_I1;
        descriptor_.header.w=CodeWidth;descriptor_.header.h=CodeWidth;
        descriptor_.header.stride=36;
        descriptor_.data_size=pixels_.size();descriptor_.data=pixels_.data();
        lv_image_set_src(image_,&descriptor_);lv_obj_remove_flag(card_,LV_OBJ_FLAG_HIDDEN);
    } else lv_obj_add_flag(card_,LV_OBJ_FLAG_HIDDEN);
    lv_obj_remove_flag(root_,LV_OBJ_FLAG_HIDDEN);lv_obj_move_foreground(root_);
    return true;
}
}
