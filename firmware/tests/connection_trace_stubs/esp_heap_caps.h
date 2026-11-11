#pragma once
#include <cstddef>
constexpr int MALLOC_CAP_INTERNAL=1, MALLOC_CAP_8BIT=2;
inline size_t heap_caps_get_free_size(int) { return 130000; }
inline size_t heap_caps_get_largest_free_block(int) { return 65536; }
