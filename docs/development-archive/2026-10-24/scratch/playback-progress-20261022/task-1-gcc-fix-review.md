# Narrow independent review — GCC fixture warning fix

Scope: immutable `101cb4ed662138dbea22991ae3f5d50f29fdf77f` -> `17bb1e001f9f25926071a6a8871426ae52235e4a`, checked against `task-1-gcc-fix.diff` and Git's actual commit diff.

- **Specification compliance: PASS.** Only the two native fixture files change; warning/sanitizer policy and test behavior are retained.
- **Code quality: PASS.** The explicit guard braces and separate statements remove misleading layout without changing control flow. No actionable finding.

Checked changes:

1. `firmware/tests/playback_adapter_test.cc`, `heap_caps_malloc`: increments `alloc_index` once; returns null on the same failure index; otherwise allocates, records, and returns the allocation exactly as before.
2. Same file, `i2s_channel_register_event_callback`: retains the disabled-channel assertion; increments registration index once; returns -1 on the same injected failure; otherwise marks callback ownership and returns ESP_OK.
3. `firmware/tests/playback_websocket_test.cc`, `cJSON_GetObjectItem`: retains null-node early return and the same map lookup/result. The declaration is now visibly outside the guard.
4. Same file, close-notification case: retains the initial close, one conditional callback when deferred, a second unconditional callback, and repeated explicit close, in the same order. Assertions and case loops are unchanged.

The commit contains no production source or runner changes. At HEAD, `firmware/tests/test_playback_progress.py` still passes `-Wall -Wextra -Werror -pedantic` and `-fsanitize=address,undefined` to all three native executables. There is no suppression, weaker assertion, removed case, or changed failure injection.

The updated report records Apple-clang/macOS three-test success. This review did not repeat that suite or the controller's Linux GCC 13.3 run. Linux result and target build outcome remain separate execution evidence; neither is inferred from this source-only verdict.

No product edits, device access, or production access performed. The previous Task 1 source-review approval is unaffected by this fixture-only change.
