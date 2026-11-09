"""Cloud checks for shared S3 config and actual pinned Bluedroid allocator macros."""
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
ROOT=Path(__file__).resolve().parents[1]
FLAG='CONFIG_BT_ALLOCATION_FROM_SPIRAM_FIRST=y'
def module():
    spec=importlib.util.spec_from_file_location('prepare',ROOT/'tools/prepare_onboarding.py')
    result=importlib.util.module_from_spec(spec);spec.loader.exec_module(result);return result
class S3BleMemory(unittest.TestCase):
    def config(self,tree):
        path=tree/'main/boards/m5stack/stopwatch/config.json';path.parent.mkdir(parents=True)
        data={'type':'m5stack-stopwatch','target':'esp32s3','builds':[{'name':n,'sdkconfig_append':['CONFIG_SPIRAM=y','CONFIG_KEEP=y']} for n in ['m5stack-stopwatch','m5stack-stopwatch-software-aec','m5stack-stopwatch-xiaoman']]}
        path.write_text(json.dumps(data));return path,data
    def test_common_onboarding_default_preserves_both_board_profiles(self):
        with tempfile.TemporaryDirectory() as d:
            tree=Path(d);path,data=self.config(tree)
            wave=tree/'main/boards/waveshare/config.json';wave.parent.mkdir(parents=True);wave.write_text('wave sentinel')
            defaults=tree/'sdkconfig.defaults';defaults.write_text('defaults sentinel\n')
            prepare=module();self.assertTrue(hasattr(prepare,'prepare_s3_ble_memory'),'missing shared S3 host-allocation policy')
            prepare.prepare_s3_ble_memory(tree)
            self.assertEqual(json.loads(path.read_text()),data)
            self.assertEqual(wave.read_text(),'wave sentinel')
            self.assertTrue(defaults.read_text().startswith('defaults sentinel\n'))
            self.assertEqual(defaults.read_text().count(FLAG),1)
    @unittest.skipUnless(os.getenv('IDF_PATH'),'requires actual cloud SDK header')
    def test_actual_sdk_allocator_prefers_psram_and_retains_internal_fallback(self):
        with tempfile.TemporaryDirectory() as d:
            tree=Path(d);path,_=self.config(tree);prepare=module()
            defaults=tree/'sdkconfig.defaults';defaults.write_text('')
            if hasattr(prepare,'prepare_s3_ble_memory'):prepare.prepare_s3_ble_memory(tree)
            enabled=FLAG in defaults.read_text()
            fake=tree/'fake';fake.mkdir();(fake/'esp_log.h').write_text('')
            (fake/'esp_heap_caps.h').write_text('''
#define MALLOC_CAP_DEFAULT 1
#define MALLOC_CAP_SPIRAM 2
#define MALLOC_CAP_INTERNAL 4
void* heap_caps_malloc_prefer(size_t,int,...);
void* heap_caps_calloc_prefer(size_t,size_t,int,...);
''')
            host=r'''
#include <assert.h>
#include <stdint.h>
#include <stdlib.h>
#include <stdarg.h>
static unsigned ordinary,preferred,internal,external;static int external_available=1;static char token;
void* ordinary_malloc(size_t n){assert(n==64);++ordinary;++internal;return &token;}
void* ordinary_calloc(size_t n,size_t size){assert(n==1);return ordinary_malloc(size);}
#define malloc ordinary_malloc
#define calloc ordinary_calloc
#include "osi/allocator.h"
void* heap_caps_malloc_prefer(size_t size,int count,...){
 assert(size==64&&count==2);va_list args;va_start(args,count);
 assert(va_arg(args,int)==(MALLOC_CAP_DEFAULT|MALLOC_CAP_SPIRAM));
 assert(va_arg(args,int)==(MALLOC_CAP_DEFAULT|MALLOC_CAP_INTERNAL));va_end(args);++preferred;
 if(external_available)++external;else ++internal;return &token;
}
void* heap_caps_calloc_prefer(size_t n,size_t size,int count,...){assert(n==1);va_list args;va_start(args,count);assert(va_arg(args,int)==3&&va_arg(args,int)==5);va_end(args);return heap_caps_malloc_prefer(size,count,3,5);}
int main(){
 assert(osi_malloc_base(64)==&token);assert(osi_calloc_base(64)==&token);
#if EXPECT_PREFER
 assert(preferred==2&&external==2&&internal==0&&ordinary==0);
 external_available=0;assert(osi_malloc_base(64)==&token);assert(osi_calloc_base(64)==&token);
 assert(preferred==4&&external==2&&internal==2&&ordinary==0);
#else
 assert(ordinary==2&&internal==2&&preferred==0&&external==0);
#endif
}
'''
            (tree/'test.c').write_text(host)
            sdk=Path(os.environ['IDF_PATH'])/'components/bt'
            for name,actual,expected in [('m5',enabled,1),('stock',False,0)]:
                (fake/'sdkconfig.h').write_text('#define CONFIG_BT_ALLOCATION_FROM_SPIRAM_FIRST 1\n' if actual else '')
                result=subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror',f'-DEXPECT_PREFER={expected}','-I',str(fake),'-I',str(sdk/'common/include'),'-I',str(sdk/'common/osi/include'),str(tree/'test.c'),'-o',str(tree/name)],capture_output=True,text=True)
                self.assertEqual(result.returncode,0,result.stderr)
                result=subprocess.run([str(tree/name)],capture_output=True,text=True)
                self.assertEqual(result.returncode,0,result.stderr)
