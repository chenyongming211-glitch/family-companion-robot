#pragma once
#include <cstdlib>
enum {MALLOC_CAP_INTERNAL=1,MALLOC_CAP_8BIT=2};
inline bool heap_fail=false;
inline void* heap_caps_aligned_alloc(size_t align,size_t size,int caps) {
    if(heap_fail)return nullptr;
    if(caps!=(MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT))std::abort();
    void* result=nullptr;return posix_memalign(&result,align,size)==0?result:nullptr;
}
inline void heap_caps_free(void* value) {std::free(value);}
