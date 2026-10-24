# Natural barge-in firmware build foundation

Main baseline 9cbe153 was pushed to private GitHub repository chenyongming211-glitch/family-companion-robot. Tag pre-barge-in-20261021 pushed and main verified equal. Private device data stays only in the primary checkout artifacts directory.

Current worktree is isolated, branch codex/natural-barge-in. Do not redispatch old controls tasks in the primary checkout ledger. User has approved the natural-barge-in design and firmware test route. Build foundation is the first prerequisite; AEC integration and hardware acceptance remain separate work.

Task 1: pending plan/implementation. Deliver locked upstream source and stock StopWatch build CI with no device or server changes.

Plan committed a3d08ad. Baseline Go tests passed in isolated worktree. Canonical task-brief helper needed /bin/bash plus explicit output path because bundled Homebrew bash/readline is broken; do not alter user runtime. Task 1 implementation dispatch next.

Task 1 code complete: commits a3d08ad..69b44f9; spec and quality reviews clean after controlled gzip-error fix. 19 preparation tests and 110 stock upstream host tests pass. Branch pushed, exact-head Linux stock CI pending. No AEC/firmware/server changes.
User confirmed tonight's onsite availability. Still give a timely notice before any serial open/reset/flash. Normal conversation may be active; do not interrupt silently. Firmware prep only so far. Exact-head baseline CI was pushed via existing GitHub SSH authentication (HTTPS PAT lacks workflow scope). Repo origin remains HTTPS; no global auth/proxy changes.

First Linux run39630448069 at69b44f9 failed before CMake configuration: stale upstream README used board argument m5stack-stopwatch, but canonical current --list-boards maps board directory m5stack/stopwatch, name m5stack-stopwatch, target esp32s3. SDK assertion, owned19 tests and upstream110 tests passed on Linux. Complete failed log is .superpowers/sdd/ci-first-failed.log. Same implementer is correcting lock/workflow/docs/plan and checking actual variant selection; do not re-run unrelated suites. This is not a hardware failure.

2026-10-22 user correction: use main directly, never master; no future feature branches/worktrees. Existing clean commits fast-forwarded to primary main at8437b16. All ignored worktree scratch preserved in primary .superpowers/firmware-development-20261022/. Future task paths must use primary checkout. Legacy worktree will be archived after final supplemental review. Stock Linux build still pending; no AEC or hardware acceptance.

Consolidation verified: primary main634eca1 equals origin/main; GitHub default main and private=true. Remote/local temporary feature branch deleted only after ancestor check. Native managed worktree archived; supplemental review report preserved. Main-only policy tracked in AGENTS.md. Build run39632225982 at634eca1 in progress.

Optional interruption metadata task complete: fe99184..353f715, task spec/quality review clean, full macOS race/vet/build passed (nonfatal LC_DYSYMTAB linker warning disclosed). Final combined review9cbe153..0734591 clean after scoped local-build doc fix; latest source main0734591. No deployment or firmware flash. AEC/reference integration and acoustic acceptance remain required. Offline existing backup verified16MiB and partition MD5/table match; private evidence artifacts/firmware-preflight-20261022/backup-integrity.json.

Main32df47e6e1bb13ccada454741540c521aa7cb26a pushed; independent GitHub Actions39634154317 reports samehead running. Only main local/remote head remains. Exact82item cleanup proposal1.47GiB awaits explicit user reply; none deleted. Existing firmware is untouched; natural AEC remains unimplemented.

User chose to make family-companion-robot public instead of authorizing deletion of other repositories artifacts. API verified isPrivate=false, default main. No old artifact deletion authorized or performed. Build39634154317 at32df47e is running; its upload result will prove whether visibility change resolves storage block. Official GitHub docs checked; standard public-runner usage free, do not claim all storage unlimited before runtime verification.

Manual public-context artifactprobe39636384744 at8953c44 SUCCESS. It contains only sha/runid text. Public upload path now demonstrated; no other project artifacts need deletion. Full stock firmware manual build dispatched frommain8953c44 in public context to recover full baseline evidence. Previous32dfcompile passed but its uploadfailed; do not treat previous binary as downloaded.

Task1foundation COMPLETE: stock run39636558961 at8953c44 public-context SUCCESS; artifact12363385545 downloaded6,418,334bytes ZIP SHA/CRC valid;10exactfiles/2binaryhashes/source+componentlocks/partitionMD5+existinggeometry/app+assetsfit verified. Local artifacts/firmware-baseline-20261022-8953c44/verification.json. Firmware source still unmodified stock; notAEC. User question latest clarified: latestmain checkout, stock buildtarget, notcustombarge-in, Go separate.
User resting 2026-10-22: continue remaining confirmed code/build/automated tests quietly, NO progress chatter/questions. No flash, serial open/reset, production gateway restart/deploy or field microphone/voice testing while user rests. Save code and review/build evidence on main for tomorrow. Do not schedule notifications or wakeups. AEC integration remains required; resume next prerequisite then integration.

Portable reference history COMPLETE: 0bf3361..9010fc3; 20/20 Python discovery tests including real C++ behavior, object32024B, task spec+quality approved. Not integrated AEC yet. Next atomic experimental source prep, then actual DMA/AFE integration.
User additionally reports slight piercing sound only USB-C-to-C connected to Mac AND watch awake, absent battery-only or standby. Tracked docs/verification/2026-10-22-usb-awake-noise.md with exact observations, unconfirmed hypotheses, source evidence and future single-variable matrix. No hardware action/parameter fix authorized while user rests; offline investigation can proceed.

Experiment preparation implemented d54991c: 18/18 focused +38/38 full, exact real archive/dependency/provenance verified. Review found one minor reserved-overlay provenance mismatch (root dependencies.lock overwritten after hashing); implementer fixing with regression before marking complete. Native SDK lock corrected to exact YAML bytes+SHA pin, no parser dependency. Integration plan/brief ready; next strongest-model DMA/AFE task after prep fix review.

Experimental preparation COMPLETE: d54991c +04f0b94 +ad5d513; 38/38 initial full suite, final focused23/23 including native YAML digest, atomic fixture failures and patch-created symlink alias rejection. Final task spec+quality review clean; no open findings. Controller now dispatches integrated DMA/AFE/diagnostic/policy implementation from approved plan. No hardware/production actions.

Main fcf6465f9a3c7f87c4ff7be91cf7387df24e6a30 pushed via existing SSH override (origin stays HTTPS). Stock CI39641720601 in progress; new experiment-prep+reference host tests on Linux, still unmodified stock target. No need download another identical-source stock baseline. Strongest-model implementer /root/implement_stopwatch_software_aec active on actual DMA/AFE integration; exact brief aec-integration-brief.md and planned report aec-integration-report.md. Task1 only; collector/experimental CI/field package Task2 remains after review.

Stock CI39641720601 on exactfcf6465 SUCCESS: new owned43 tests +upstream110 passed on Linux, stock compile+all evidence+artifact upload passed. Artifact12364444755 ZIP6,418,328B, SHA f43150ea16028b9b4405da89313da4d6b1ed17699c6b24629a7cf4c79df06994; full closed-job log ci-main-fcf6465.log. Still unmodified firmware target, not AEC/physical acceptance. Existing original baseline remains downloaded evidence.

2026-10-22 Task1 source/host complete: commit3f433d0,47owned+110upstream, independent fix review approves all4. Target/acoustic pending. Task2 base740947f (plus plan-checkpoint commit); fresh standard implementer next, brief aec-delivery-brief.md. Userquiet/nohardware persists.

2026-10-22 FINALsoftwaredelivery c6fc6c2docs/main pushed; runtime source15fbc73bothpatchedtargetCI39655807535green64+110each, stock39655807410green, both25fileartifactsdownload/crypto/image/config/geometryverified; privatefullrollbackinputsready. Noiseinvestigationrecordedunknownrootcause/preflashbaselinepending. Allimplementation/buildplanssoftwarecomplete; nohardware/productionchange, acousticfieldpending. Seecontroller-current FINALblock/buildverificationdoc.
