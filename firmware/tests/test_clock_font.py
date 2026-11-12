"""Actual offline date font callbacks cover every clock character, no SDK required."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

FIRMWARE = Path(__file__).resolve().parents[1]


class ClockFontTest(unittest.TestCase):
    def test_actual_date_glyphs_and_bitmap_bounds(self):
        self.check_font('desktop_clock_font', 'family_robot_desktop_clock_font',
                        '0123456789 年月日周星期一二三四五六', 44)

    def test_actual_digit_glyphs_and_bitmap_bounds(self):
        self.check_font('desktop_clock_digits_font', 'family_robot_desktop_clock_digits_font',
                        '0123456789:-', 48)

    def check_font(self, stem, symbol, characters, line_height):
        source = FIRMWARE / 'overlays/main/display' / (stem + '.c')
        self.assertTrue(source.is_file(), 'missing offline glyph subset')
        header = r'''
#pragma once
#include <stdbool.h>
#include <stdint.h>
#include <stddef.h>
typedef struct {int unused;} lv_draw_buf_t;
typedef struct {unsigned adv_w,box_w,box_h;int ofs_x,ofs_y;unsigned stride,format;union{unsigned index;}gid;} lv_font_glyph_dsc_t;
typedef struct lv_font_t lv_font_t;
struct lv_font_t {
 bool (*get_glyph_dsc)(const lv_font_t*,lv_font_glyph_dsc_t*,uint32_t,uint32_t);
 const void* (*get_glyph_bitmap)(lv_font_glyph_dsc_t*,lv_draw_buf_t*);
 int line_height,base_line;unsigned static_bitmap;
};
#define LV_FONT_GLYPH_FORMAT_A8 8
'''
        codes = list(map(ord, characters))
        program = '#include "'+stem+'.h"\n#include <assert.h>\n' + r'''
int main(void){
 const lv_font_t* font=&FONT_SYMBOL;
 const uint32_t required[]={''' + ','.join(map(str,codes)) + r'''};
 assert(font->line_height==FONT_LINE_HEIGHT && font->base_line==9 && font->static_bitmap);
 for(size_t i=0;i<sizeof(required)/sizeof(required[0]);++i){
  lv_font_glyph_dsc_t glyph={0};
  assert(font->get_glyph_dsc(font,&glyph,required[i],0));
  assert(glyph.adv_w>0 && glyph.adv_w<=(unsigned)font->line_height && glyph.box_w<=(unsigned)font->line_height && glyph.box_h<=(unsigned)font->line_height);
  assert(glyph.stride==glyph.box_w && glyph.format==LV_FONT_GLYPH_FORMAT_A8);
  const uint8_t* pixels=font->get_glyph_bitmap(&glyph,NULL);assert(pixels);
  unsigned lit=0;
  for(size_t n=0;n<(size_t)glyph.box_w*glyph.box_h;++n)lit+=pixels[n]!=0;
  if(required[i]!=32)assert(lit>0);
 }
 lv_font_glyph_dsc_t missing={0};
 assert(!font->get_glyph_dsc(font,&missing,0x10ffff,0));
}
'''
        program=program.replace('FONT_SYMBOL',symbol).replace('FONT_LINE_HEIGHT',str(line_height))
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work/'lvgl.h').write_text(header)
            (work/'main.c').write_text(program)
            binary = work/'font'
            built = subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror',
                '-fsanitize=address,undefined','-fno-sanitize-recover=all',
                '-I',str(work),'-I',str(source.parent),str(source),str(work/'main.c'),
                '-o',str(binary)],capture_output=True,text=True)
            self.assertEqual(built.returncode,0,built.stderr)
            env = dict(os.environ, ASAN_OPTIONS='halt_on_error=1:abort_on_error=1',
                       UBSAN_OPTIONS='halt_on_error=1:print_stacktrace=1')
            if os.environ.get('FIRMWARE_HOST_ASAN_PRELOAD'):
                env['LD_PRELOAD']=os.environ['FIRMWARE_HOST_ASAN_PRELOAD']
            ran = subprocess.run([str(binary)],capture_output=True,text=True,env=env,timeout=10)
            self.assertEqual(ran.returncode,0,ran.stdout+ran.stderr)


if __name__ == '__main__':
    unittest.main()
