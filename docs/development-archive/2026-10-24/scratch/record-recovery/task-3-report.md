# Task 3 completion report

Status: offline experiment completed; automatic accurate resume remains unknown.

Public-safe report draft: `.superpowers/record-recovery/offline/report-public.md`. Parent should copy it to `docs/verification/2026-10-23-record-recovery-alignment.md` when integrating; no tracked files were edited by this worker.

Actual runs: 30 Qwen3-ForcedAligner MPS inferences plus 30 independent official Whisper tiny CPU/DTW inferences. 12 constructed unit-clip conditions + 3 native unlabelled continuous-output conditions, repeated twice. Both repeats deterministic. Constructed truth from synthesis-unit frame coordinates, not either model. No mic, physical device, playback, production dependency or source change.

Important evidence: all source/clip SHA and crop origins in offline/manifest.json; qwen-results.json and whisper-results.json retain exact text/spans/resources; scores.json and score.py explicitly distinguish unsafe diagnostic last-end scoring from strict unknown production eligibility. Full report contains setup/source hashes/resources/recognition errors/zero and out-of-range spans/caveats.

Independent Whisper initially native-exited139; isolated official dtw_cpu(zeros(3,4)) reproduced exit139, NUMBA_DISABLE_JIT=1 minimum diagnostic succeeds and all model runs then succeed using unchanged official Python DTW function. Logs: dtw-native.txt / dtw-nojit.txt / whisper-stderr.txt / whisper-nojit-stderr.txt. Precise LLVM/JIT internals unproven, no library patch.

Early full-draft counterexample: .2s inserted silence -> Qwen all10 spans beyond file end; quarter-first-unit crop .468375s -> all10 spans beyond file end. Whisper empty on both. Mid-five and resumed-mid-seven -> both models claim current incomplete source unit as completed under unsafe last-end rule. 3s silence -> Qwen fake tenth positive span0.64–.80s, Whisper empty. Native continuous Whisper output has substantial number repetition/substitution; no independent timing truth, do not score it as verified.

Measurements: full constructed sequences Qwen17/17 matching units end MAE25.8ms/max56.5ms; Whisper16/17 matching units end MAE48.8ms/max183.6ms, with 虹桥->红桥 mismatch. These are synthetic clip supports, not natural phonetic boundary accuracy. Qwen diagnostic last8/12 vs Whisper9/12 are not production success rates. Physical device coordinate map absent. Strict status remains unknown.

All assets, weights, source-copy diagnostics and runtime data remain in ignored offline/. No commit or push performed, per assignment.

Parent-requested conservative follow-up: conservative_policy.py reuses existing ASR outputs without new inference. Require exact unique normalized draft prefix, withhold last intersected constructed unit. 6/12 construction candidates, 6unknown; 19 source units advanced, 0observed false source-unit completions, 4already complete source units repeated. Mid-five repaired in source-fragment terms; resumed-mid-seven and proper-word mismatch reject unknown. No acoustic/global guarantee, all device mappings unknown. See appended public report section and conservative-policy.json. Cut signal energy checks in cut-signal-diagnostics.json establish continued synthesized waveform only, not phoneme truth.
