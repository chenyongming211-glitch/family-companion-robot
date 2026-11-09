"""Execute the pinned SDK default allocator with a bounded internal reservoir."""
import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest
from test_s3_ble_memory import module


class S3DefaultHeap(unittest.TestCase):
    @unittest.skipUnless(os.getenv('IDF_PATH'), 'requires actual cloud SDK source')
    def test_actual_sdk_small_allocations_preserve_internal_timer_budget(self):
        sdk = Path(os.environ['IDF_PATH']) / 'components/heap'
        source = (sdk / 'heap_caps.c').read_text()
        start = source.index('HEAP_IRAM_ATTR void *heap_caps_malloc_default(')
        opening = source.index('{', start)
        depth = 1
        end = opening + 1
        while depth:
            depth += (source[end] == '{') - (source[end] == '}')
            end += 1
        allocator = source[start:end]
        header = (sdk / 'include/esp_heap_caps.h').read_text()
        caps = '\n'.join(re.findall(r'^#define MALLOC_CAP_(?:DEFAULT|SPIRAM|INTERNAL|8BIT|DMA)\s+[^\n]+', header, re.M))
        self.assertEqual(len(caps.splitlines()), 5)
        with tempfile.TemporaryDirectory() as directory:
            tree = Path(directory)
            (tree / 'sdkconfig.defaults').write_text('CONFIG_SPIRAM_MALLOC_ALWAYSINTERNAL=1024\n')
            (tree / 'sdkconfig.defaults.esp32s3').write_text('CONFIG_SPIRAM_MALLOC_ALWAYSINTERNAL=1024\n')
            module().prepare_s3_memory_policy(tree)
            settings = dict(line.split('=', 1) for line in ((tree / 'sdkconfig.defaults').read_text() + (tree / 'sdkconfig.defaults.esp32s3').read_text()).splitlines() if line.startswith('CONFIG_'))
            limit = int(settings['CONFIG_SPIRAM_MALLOC_ALWAYSINTERNAL'])
            harness = r'''
#include <assert.h>
#include <stddef.h>
#include <stdint.h>
#define HEAP_IRAM_ATTR
#define MALLOC_DISABLE_EXTERNAL_ALLOCS -1
CAPS
static int malloc_alwaysinternal_limit;
static size_t internal_left, external_left;
static unsigned failures, last_caps;
static char token;
void *heap_caps_malloc_base(size_t size, unsigned caps) {
 last_caps=caps;
 if(caps&MALLOC_CAP_SPIRAM) {if(external_left<size)return NULL;external_left-=size;return &token;}
 if(internal_left<size){if(caps==MALLOC_CAP_DEFAULT&&external_left>=size){external_left-=size;return &token;}return NULL;}internal_left-=size;return &token;
}
void *heap_caps_malloc(size_t size,unsigned caps){return heap_caps_malloc_base(size,caps);}
void heap_caps_alloc_failed(size_t size,unsigned caps,const char *where){(void)size;(void)caps;(void)where;++failures;}
ALLOCATOR
static void reset(int threshold){malloc_alwaysinternal_limit=threshold;internal_left=4096;external_left=65536;failures=0;}
static void fst_peak(void){for(int n=0;n<80;++n){assert(heap_caps_malloc_default(8));assert(heap_caps_malloc_default(12));assert(heap_caps_malloc_default(16));assert(heap_caps_malloc_default(20));}}
int main(void){
 // Old policy reproduces starvation by many live small FST allocations.
 reset(1024);fst_peak();assert(internal_left<32);assert(!heap_caps_malloc(32,MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT));
 // The generated common policy must preserve that reservoir.
 reset(POLICY);fst_peak();assert(internal_left==4096);assert(heap_caps_malloc(32,MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT));assert(last_caps==(MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT));
 assert(heap_caps_malloc(64,MALLOC_CAP_INTERNAL|MALLOC_CAP_DMA));assert(last_caps==(MALLOC_CAP_INTERNAL|MALLOC_CAP_DMA));
 assert(heap_caps_malloc(512,MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT));
 // SDK fallback remains available when PSRAM is exhausted.
 external_left=0;size_t before=internal_left;assert(heap_caps_malloc_default(20));assert(last_caps==MALLOC_CAP_DEFAULT);assert(internal_left==before-20);assert(failures==0);
 internal_left=0;assert(!heap_caps_malloc_default(20));assert(failures==1);
}
'''.replace('CAPS', caps).replace('ALLOCATOR', allocator).replace('POLICY', str(limit))
            (tree / 'test.c').write_text(harness)
            result = subprocess.run(['cc', '-std=c11', '-Wall', '-Wextra', '-Werror', '-Wno-misleading-indentation', str(tree / 'test.c'), '-o', str(tree / 'test')], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            result = subprocess.run([str(tree / 'test')], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
