# Shared S3 ordinary-allocation policy and verification

Scope: onboarding candidates for BOTH supported ESP32-S3 boards. No stock firmware, model, wake threshold, VAD, protocol/security, Wi-Fi power-save, controller/DMA or task-stack capability changes. Hardware installation and field acceptance remain root-owned.

## Evidence and hypothesis

13d physical cleanup succeeded (internal216823→229523, reclaimed12700), yet its first boot aborted in PHY PLL timer creation with ESP_ERR_NO_MEM; its subsequent boot remained running. Bluetooth reclamation alone did not eliminate the transient failure.

Actual linked esp-sr libfst.a has two allocation policies: core FST nodes explicitly request PSRAM, while linked determinization/minimization objects also issue ordinary malloc8/12/16/20 and calloc(n,4). Pinned SDK heap_caps_malloc_default prefers INTERNAL for sizes≤1024; its reserved102400-byte pool is consumable by internal/DMA/task allocations and normal fallback, not a guaranteed free-memory floor. esp_timer_create explicitly allocates INTERNAL|8BIT. This supports testing PSRAM preference for all ordinary positive sizes; it does not prove this change fixes the physical startup peak.

One common preparation helper now replaces upstream sdkconfig.defaults.esp32s3 threshold1024→0. Existing BT host PSRAM preference stays enabled. Explicit required internal allocations retain their capabilities; ordinary small allocations now incur PSRAM access costs, and can still fall back to internal RAM if PSRAM allocation fails. This is a broader ordinary-allocation policy, not a vendor FST allocator patch.

## Fresh tests and clean configuration

Cloud root `/home/ubuntu/m5-shared-heap-20261110`; no Mac runtime/tests/builds.

`heap-red.log`: old1024 fails the preserved-reservoir assertion. `target-precedence-red.log`: an initial global-default append is overridden by the later target1024, also fails; final implementation replaces the target default itself. `heap-green-final.log`:3/3 pass, executing the actual pinned SDK default allocator body and cap definitions with bounded mock heaps. Many simultaneously live small FST-sized allocations exhaust the old internal reservoir, while0 preserves it for explicit INTERNAL|8BIT timer-like, DMA and stack-like requests. PSRAM exhaustion fallback and total failure remain covered. The modeled32-byte request is a budget check, not a measured physical timer object size.

`prove_fresh_config.py` invokes locked clean prepare_experiment + prepare_onboarding, then actual SDK kconfgen with an ABSENT existing sdkconfig. Defaults follow pinned kconfig.cmake order: global→target→generated board defaults. BOTH profiles resolve threshold0 and BT_ALLOCATION_FROM_SPIRAM_FIRST=y; ALL effective CONFIG values match their native cache. Fresh common default files are byte-identical to caches. Per-profile proof/config/log are under `fresh-config/`. Target defaults are included in frozen derived/common source rows.

Initial full-suite setup errors (AppleDouble patch sidecars and missing public fixture documents) are retained in supported-host.log; corrected workspace then required a fresh stable-source run after the target-precedence correction because already-loaded test fixtures still reflected the earlier helper. No failed run is counted as acceptance. Final stable-source full-suite/native/artifact results are recorded below when available.

Pinned actual SDK heap_caps.c SHA256 `6576a54104902164e726a0a6149e01cbbbe1aa1c5a388125bc77cb587ff911b4`; esp_heap_caps.h `926eb9eada9d675c0302b34e2ed6c4977a9267963dca39e50550ee90efadad97`.

## Final verification

Source commit `624addd1d2c98709dfb7d62923201894bd349d85` (main, no push by worker). Only three firmware/test files committed; root-owned AGENTS/handoff/plan/verification edits excluded.

Final exact-source full supported suite: `supported-host-exact.log`,154/154 PASS in189.466s, zero errors/failures/skips. Cloud command uses supply venv with ONBOARDING_OFFICIAL, IDF_PATH, M5_ORIGINAL_SOURCE, ONBOARDING_CANDIDATE and FIRMWARE_HOST_ASAN_PRELOAD, invoking `firmware/tools/run_host_tests.py --archive /home/ubuntu/onboarding-screen-20261109/source/.superpowers/firmware-baseline/xiaozhi-v2.5.2.tar.gz`. Retired C3 sources remain historical inputs; no C3 runtime/build gate was used.

M5 `idf.py build` + `idf.py merge-bin` exit0 (`build-m5.log`, `merge-m5.log`). `verify_candidate.py` PASS: official binary partition parser/MD5, security/provenance/source equality, app/asset fit, all merged slices and ZIP round-trip. Effective CONFIG delta exactly `CONFIG_SPIRAM_MALLOC_ALWAYSINTERNAL:1024→0`; all four nonapp regions byte-identical frozen13d. No layout or state migration.

Frozen M5 manifest `evidence/m5/manifest.json` SHA256 `0f4159021956b965ab275b776f7301946c8875b65547891ea4917626e1121fae`; app `evidence/m5/build/xiaozhi.bin`,3483952 bytes SHA256 `6fcccb91fb77596ed43d0011712be972077f6645915c576f8e6ffcf1334dd5d6`; ELF `bb6d62e5c1cbfe6956e3b8f297632ed64a98314c7603d13335f7235b7028885b`; merged `ff77f9a6956eba16a09d1bd73435e3a6795f556527254919fdd362af0239bf19`. App fits4128768 slot with644816 free.

All257 source-input paths independently SHA-equal current Mac files and committed source; frozen under `frozen-source/`, firmware root `frozen-source/firmware`. Source-inputs.json SHA256 `6fd2b29ebd01071be6b75e241694ce19900e5cf7e0acecf227630fd1f2ad37a4`. M5 derived14/common11 rows include S3 target defaults. All9 manifest files,5 flashed regions, derived sources and source-inputs map copied into evidence. No generated firmware/private data entered Git.

Wave `idf.py build` + `idf.py merge-bin` exit0 (`build-wave.log`, `merge-wave.log`). The same verifier PASS: effective config delta ONLY1024→0, all four nonapp regions byte-identical13d,16 derived/11 common rows. All11 common derived source hashes independently match M5. No Wave hardware operation occurred.

Wave manifest `evidence/waveshare/manifest.json` SHA256 `03fef1e7a4aaeabdbd42a328589c3e58db27ae8223f43b3cf88932276385557d`; app3470480 bytes SHA256 `1e41711e55122d55cbf9f4634016899c9941f9bd9040fc15401f8c215171992d`; ELF `5fa253bc81cd41cb310773742cbd2d13f1278e359142626e17204d5d7657beee`; merged `ece7db10a8ee252f2a75898637304834a891f60123ecf2956070fae4c26b746a`. Slot3604480 leaves134000 bytes. Both profiles assets5376385 bytes.

The final frozen native sources match clean preparation for both profiles. M5 frozen evidence stayed unchanged while Wave compiled. Existing caches were reused; old13d/105/00bb/744 artifacts were not overwritten or removed. Physical validation of either new app remains outside the worker result: host allocation routing and native builds do not establish a measured startup minimum or absence of the real PHY abort. Root owns bound-state-preserving installation, rollback and field acceptance.


## Root installation and startup observation

Root pushed source624addd and installed only M5, preserving its confirmed family-home binding/version1/From and owner-user body grant. Fresh current snapshot f549 combined36864 actual runtime bytes with verified immutable regions/whole hardwareMD5; app-slot update completed02:35:47 Beijing. The complete4128768-byte slot readback equals expected10e0aebeba0f04008e033eb25835e3a2e4fe1206e05876c822b00e5062b9f47c; all nonapp regions and expected whole8763460493264a49da3f023f227f836c502ccfad68c68934ac20aea95a7de2f8 matched hardware. No factory/layout/state resupply.

After one reviewed controlled boot at02:38:06, the complete300-second passive capture contains50243 bytes,30 heap samples, two FST builds and no NO_MEM/abort/backtrace/reboot/brownout/watchdog message. Logged heap timestamps remain monotonic10352→299392ms; wall-clock capture duration is300seconds, not an exact conversion from those log ticks. Last internal free98643 bytes, reported minimum33468; prior13d minimum16 with a real first-boot PHY abort remains a failure record. Cleanup succeeded and reclaimed12700 bytes. This establishes this startup observation, not long-duration or acoustic acceptance.

Root verified original config projection,315 old source hashes,11 protected files, other servicePIDs and Go3256763/df5386 unchanged. Both devices' authenticated bootstrap returns ownWSS/version1; invalid-protocol requests stop at400 without opening testWS/model. Native relay/serial and reverse42217 have been closed. Raw logs, images, NVS and private proofs remain cloudroot0700/0600 under m5-shared-heap-20261110; no firmware binaries or private data were copied to Mac/Git.

Exact624 CI stock42673686993 and AEC42673686679 passed on attempt2; AEC covers all four supported S3 build variants. The initial missing-job/internal-server-error infrastructure failure and retries are retained, with no product/workflow changes for rerun. User listening/popping/natural-interruption feedback is still pending; the existing question asks for normal long speech and an interruption, without re-registration.
