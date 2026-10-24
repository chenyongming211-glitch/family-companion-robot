# Task 2 completion report

Commit: `6a1dd70e6e8393a147cc44d30263bd29d18dd74e` on main. No push. Only four owned files committed:

- `tools/voice-audit/forced_alignment_probe.py`
- `tools/voice-audit/test_forced_alignment_probe.py`
- `tools/voice-audit/forced_alignment_requirements.txt`
- `docs/verification/2026-10-23-forced-alignment.md`

## Implementation and verification

Conservative standalone offline candidate validator checks complete-audio provenance, exact normalized text-unit coverage, matching generation/response/item/content/audio-file SHA identity, finite positive source duration, monotonic non-overlapping positive spans and bounds. `candidate_valid` is explicitly structural only. Boundary projector additionally requires an external independent acoustic attestation bound to exact text/spans/duration/identity fingerprint, a calibrated mapping bound to the same identity, valid device sample coordinates and no cut inside a word. Failing or absent evidence returns explicit unknown. The real runner never creates an acoustic attestation and always outputs unknown boundary projection.

Explicit optional `group_units` grouping preserves every source constituent, including zero-duration chars. It cannot split a model token, change times, discard chars, bridge overlapping/wrong-order/out-of-range spans, or repair incomplete audio. Whole-number candidate granularity is separate from individual character validity.

Latest verification: `python3 -m unittest discover -s tools/voice-audit -p test_forced_alignment_probe.py -v` — 15 tests pass, 0.043 sec. `python3 -m py_compile` passes. `git diff --cached --check` passed before commit. Owned files clean after commit. Ignored model/audio paths confirmed with `git check-ignore`.

Genuine TDD RED logs:

- `python-red.txt`: 5 assertion failures in 9 tests on deliberately unimplemented contract stub.
- `python-evidence-red.txt`: 1 failure/10 tests proving candidate evidence replay across changed spans.
- `python-grouping-red.txt`: 1 failure/12 tests before grouping implementation.
- `python-mapping-red.txt`: 1 failure/13 tests proving coordinate map reuse across generations.
- `python-absent-red.txt`: 1 failure/14 tests for missing evidence/map explicit outcome.
- `python-offline-red.txt`: 1 failure/15 tests before CLI local-model rejection.
- `python-green.txt`: final 15-test pass.

## Actual official model and runtime

Hardware: M4 Mac mini, 10 CPU cores, 16GB unified memory, macOS26.3.1/25D771280a arm64.

CPython3.12.7 isolated venv. qwen-asr0.0.6, torch2.14.1, transformers4.57.6, numpy2.5.4, soundfile0.14.0. All94resolved dependencies in `requirements-lock.txt`. No global Python installation. PyPI assets were slow (~0.35MB/s); cancelled initial incomplete owned install and successfully installed same pins via Aliyun mirror (~5.4MB/s observed).

Primary references read: official GitHub ForcedAligner usage, HuggingFace Qwen/Qwen3-ForcedAligner-0.6B model card, official ModelScope files. Official Git inspected revision `7c6daf77a2421100f5fb066495372c00129d39ff`. Installed and inspected `qwen3_forced_aligner.py` both SHA256 `fb586127507c5980d11c6d848b530e652afad3ae0707cba6eb0c90377b2a2111`.

HuggingFace direct download failed timeout30s and60s. Official ModelScope successful. Weight file revision `6f4d7c9606feb7adf282c9e4b139f28e8695d867`; registry model-card latest `cf1c50164ea3ac48240d12bef5ead74aee0720cc`. These are ModelScope revisions, not HF SHA. Weight bytes1,835,544,544 SHA256 `47831d0e82f96b20e9034dba01a075ee06436654719f6a68289e49f1b65ce0e7`; every downloaded model/processor/tokenizer file matches official registry SHA. Files under `model/`, registry response `modelscope-files.json`.

All inference uses the direct official `Qwen3ForcedAligner.align` API with local weights, float32/eager attention/four Torch threads and explicit HF_HUB_OFFLINE=1/TRANSFORMERS_OFFLINE=1. No auxiliary ASR weights/model, remote API calls by this subagent, microphone, sound playback, device connection, firmware flash or server access.

## Actual inference evidence

`manifest.json` contains9original conditions. Silent local `say` Tingting rate160 PCM16/16kHz generated counting1..10 and ordinary Chinese sentence (~5.716s each).43% frame prefixes paired with complete text intentionally;3s zero PCM silence paired with nonempty text.

Parent supplied native provider output file at `artifacts/resume-state-alignment-20261023/native-output/output.wav`; file SHA `8c83603eacd3464a8a7986eaeaad2c09bf4667c65057d7a039149e398463cc3a`,20.96s24kHzPCM. Exact raw Arabic1..50 transcript preserved. Explicit experimental Chinese numeral normalization tested separately. Each full-text condition also tested against43%audio9.012792s. No provider calls by this subagent.

CPU and MPS each ran all9conditions twice:36actual retained results in `cpu-results.json`,`mps-results.json`; detailed stdout/stderr and /usr/bin/time -l counters alongside. Both backends return identical public spans. CPU initialization(import Qwen + weight/model init)69.181s; first counting1.053s; warm counting0.327s, ordinary0.777s, native raw1.146s, native Chinese1.353s. CPU peakRSS4.066GiB, whole-process wall99.64s. First24kHz native preprocessing/inference7.705s versus repeat1.146s.

MPS available+built. Subsequent fresh MPS process with warm OS/package caches initialization8.115s, first counting2.738s, warm counting0.144s, ordinary0.160s, raw native0.443s, native Chinese0.549s; whole-process wall18.82s. RSS1.908GiB and separate MPS driver allocation max3.949GiB. Do not add counters or claim equal-cache cold CPU/MPS comparison.

`extra-manifest.json` explicitly tests whitespace-separated Arabic1..50 and Chinese grouping into original50number units, each complete/truncated and twice on each backend.16additional retained results in `cpu-extra-results.json`,`mps-extra-results.json`, total52retained results. An earlier8-inference supplemental CPU diagnostic pass was superseded after schema cleanup. Final extra CPU/MPS public spans and grouped outcomes match exactly. Source SHA recorded in extra results `bcf61262c4576989f88c16ff19651b2dafe45e4d95b43ebbdb69810a65e22c05`; final script SHA `d330713d3d9515fd8e6bf09a06489ac50e4096e037ed7a97759528e9281b4b83` adds stricter unused projector identity/missing-evidence checks and enforces the already-used offline/local-model settings. Model calls, output timestamps and grouping logic unchanged; final contract tests verify changes. This distinction is documented publicly.

Final supplemental warm times CPU/MPS seconds: spacedArabic full1.429901/0.485012; truncated0.951170/0.265246; groupedChinese full1.847560/0.604264; truncated0.742525/0.354445.

## Findings and acceptance boundary

1. Local offline execution is proved CPU+MPS. Bounded asynchronous complete-block alignment deserves future evaluation; streaming/causal correctness is not demonstrated and immediate stop must never wait for model startup/alignment.
2. Official aligner postprocesses predicted timestamps using increasing subsequence plus replacement/interpolation;80ms bins. Monotonicity is therefore not independent correctness evidence.
3. Incomplete audio paired with full text still returns all supplied units, including zero/out-of-range times. Truncated ordinary2.458438s yields maxend3.52s. Native normalized truncation9.012792s yields117chars,61zero spans,57out-of-range spans, maxend20.48s. All validation/projection unknown.
4. Silence returns5spans,4zero. Unknown.
5. Official Chinese path uses tokenize_space_lang and removes punctuation before CJK splitting. Raw Arabic1、2…50 collapses into ONE giant digit token18.00–20.48s. Structurally valid concatenation, unusable per-number granularity, unknown acoustic accuracy/projection.
6. Explicit space-separated Arabic yields50tokens with16zero-duration numbers. Invalid full-case structure.
7. Explicit Chinese has117chars,3zero spans in22/31/42. Retaining every constituent and grouping by original numbers yields50positive monotonic in-bounds number candidates. Structure is feasible at whole-number granularity, but not an accurate-resume pass. All real boundaries remain unknown.
8. No manual/human word ground truth, codec/resampler-delay calibration, queued packet/I2S mapping or measured auditory receipt. `say`/provider transcripts and model timestamps cannot serve as their own truth. Native provider integration and physical-device acceptance belong to parent Task3 and remain separate.

No credentials/weights/raw audio tracked. All detailed evidence/venv/model/audio under ignored directories. Worktree changes outside four owned files belong to other agents and were untouched.
