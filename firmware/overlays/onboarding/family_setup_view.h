#pragma once
#include <lvgl.h>
#include <cstddef>
#include <cstdint>
#include <vector>

namespace FamilyOnboarding {
// Owned by the board's Display. All methods run under its existing LVGL lock.
class SetupView {
public:
    ~SetupView();
    void Initialize(const lv_font_t* font,int width,int height);
    bool Update(const char* title,const char* detail,const uint8_t* packed,std::size_t size);
private:
    lv_obj_t *root_=nullptr,*title_=nullptr,*card_=nullptr,*image_=nullptr,*detail_=nullptr;
    lv_image_dsc_t descriptor_{};
    std::vector<uint8_t> pixels_;
    int width_=0;
};
}
