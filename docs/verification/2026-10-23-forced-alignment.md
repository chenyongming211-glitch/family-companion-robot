# Forced alignment experiment — 2026-10-23

## Scope and primary sources

Task 2 of `docs/superpowers/plans/2026-10-23-resume-state-and-alignment.md` evaluates the official **Qwen3-ForcedAligner-0.6B** offline, with supplied Chinese text. No auxiliary ASR, microphone, audible playback, firmware, device connection, or gateway integration is used by this probe. The parent task separately supplied machine-generated native-provider output for offline alignment.

Primary references read for this experiment:

- [Official direct ForcedAligner API](https://github.com/QwenLM/Qwen3-ASR#forcedaligner-usage).
- [Official model card](https://huggingface.co/Qwen/Qwen3-ForcedAligner-0.6B).
- [Official ModelScope model repository](https://modelscope.cn/models/Qwen/Qwen3-ForcedAligner-0.6B/files).
- Official source inspected at Git revision `7c6daf77a2421100f5fb066495372c00129d39ff`, including `qwen_asr/inference/qwen3_forced_aligner.py` and `pyproject.toml`.

Only `Qwen3ForcedAligner.from_pretrained(...).align(...)` is invoked; importing its `qwen-asr` package does not mean an ASR model was loaded. Weights are local and network access is disabled during inference.

## Conservative validation and projection contract

`tools/voice-audit/forced_alignment_probe.py` has two deliberately distinct operations:

1. `validate_alignment` checks matching generation/response/item/content identity and source-file SHA-256, known complete audio, finite positive audio duration, nonempty exact transcript-unit coverage, monotonic non-overlapping positive spans, and bounds within the source audio. Punctuation and spaces are ignored when checking units; arbitrary symbols are retained. Passing returns **candidate_valid**, which proves structural consistency only. A full list of words emitted by a forced aligner does not establish that the words exist acoustically.
2. `project_boundary` requires an external attestation of acoustic coverage and boundaries, bound to the exact candidate identity/text/spans/duration via `alignment_fingerprint`, plus a documented, validated sample-coordinate mapping bound to that same playback identity. A cut inside a span returns **unknown**, without a confident text endpoint. Missing evidence, incomplete audio with a full transcript, wrong identity, invalid spans, and invalid/out-of-range coordinates return **unknown**.

The official API returns **postprocessed** model candidates. In the inspected source, `fix_timestamp` selects an increasing subsequence and replaces/interpolates anomalous timestamp values; the configured timestamp bins are 80 ms. Consequently, monotonicity of returned spans is partly enforced by the library and is not an independent acoustic check. The probe retains exactly the public API output; it does not invent intermediate word positions.

The experiment runner never manufactures the external attestation and always emits `boundary_projection.status=unknown`. Unit tests use deliberately labelled numeric fixtures, which exercise the contract and are not model or acoustic evidence. `audio_complete=true` asserts the source capture ended normally; it does not assert transcript correspondence. In particular, silence can have a complete file and a mismatched transcript.

Mapping contract, before any downstream use:

```text
source_pcm_sample = (device_played_samples - playback_offset_samples)
                    * source_rate / playback_rate
source_pcm_seconds = source_pcm_sample / source_rate
```

A rate ratio alone does not validate this map. The offset and calibration must establish which generated PCM, resampler output, codec delay/padding, queued packets and I2S counter belong to the identity. The gateway currently has no measured mapping from these offline files to a physical device. I2S delivery still does not prove audible reception. This experiment supplies no such calibration and cannot claim exact heard words.

## Synthetic input provenance

All input files remain under ignored `.superpowers/resume-state-alignment/`; no audio or weights are tracked. macOS `say` writes directly to mono PCM16 WAV files at 16 kHz, using `Tingting`, rate 160. No sound is played.

- Counting: `一，二，三，四，五，六，七，八，九，十。`
- Ordinary speech: `今天阳光很好，我们一起去公园散步，然后回家吃晚饭。`
- A 43% frame-prefix of each file is paired intentionally with the **full** original text and marked `audio_complete=false`.
- Three seconds of all-zero PCM is paired with `一，二，三，四，五。`.

The parent task also supplied one actual native-provider output file: 20.960 seconds of 24 kHz PCM, counting 1..50, file SHA-256 `8c83603eacd3464a8a7986eaeaad2c09bf4667c65057d7a039149e398463cc3a`. The exact provider transcript is Arabic digits joined by `、`. This is run unchanged, then separately with an explicitly labelled experimental conversion of numbers 1..50 to Chinese numeral characters. Each condition is also run with a 43% PCM prefix and its full transcript. The conversion is an experimental condition, not a silently corrected provider transcript, and does not prove those words were spoken. Arrival times and transcript lengths are never used to label the audio.

These inputs provide controlled complete/truncated/silent files. `say` supplies no independently validated per-word boundaries; a cut at 43% is not labelled as a measured word boundary. Candidate model timestamps cannot be reused as ground truth for this same experiment.

## Reproduction

Use a fresh Python 3.12 virtual environment under the ignored experiment directory. The probe supports a local pinned model directory only. Obtain files from the official model repository, pin the revisions below, and verify registry SHA-256 values before running. Direct Hugging Face access timed out at both 30 and 60 seconds on this host; the official ModelScope download succeeded. The inherited local proxy endpoint was unavailable, so public downloads were retried without proxy environment variables. No credentials were read.

```sh
uv venv --python python3.12 .superpowers/resume-state-alignment/venv
uv pip install --python .superpowers/resume-state-alignment/venv/bin/python \
  -r tools/voice-audit/forced_alignment_requirements.txt

say -v Tingting -r 160 -o .superpowers/resume-state-alignment/counting.wav \
  --data-format=LEI16@16000 '一，二，三，四，五，六，七，八，九，十。'
say -v Tingting -r 160 -o .superpowers/resume-state-alignment/ordinary.wav \
  --data-format=LEI16@16000 '今天阳光很好，我们一起去公园散步，然后回家吃晚饭。'

HF_HUB_OFFLINE=1 TRANSFORMERS_OFFLINE=1 \
.superpowers/resume-state-alignment/venv/bin/python \
  tools/voice-audit/forced_alignment_probe.py \
  --model .superpowers/resume-state-alignment/model \
  --model-revision modelscope:6f4d7c9606feb7adf282c9e4b139f28e8695d867 \
  --manifest .superpowers/resume-state-alignment/manifest.json \
  --output .superpowers/resume-state-alignment/cpu-results.json \
  --device cpu --threads 4 --repeats 2

python3 -m unittest discover -s tools/voice-audit \
  -p test_forced_alignment_probe.py -v
```

Use `--device mps` and a distinct output path for the MPS run. The additional numeric conditions use `extra-manifest.json` with the same command and separate `cpu-extra-results.json` / `mps-extra-results.json` outputs.

The manifest is JSON `{ "cases": [...] }`. Each case contains `name`, local `audio` path relative to the manifest, `text`, `audio_complete`, and `identity` with `generation`, `response_id`, `item_id`, `content_index`. Optional `transcript_condition` records explicit text transformations. Optional `group_units` is the exact supplied whole-number list for alternate-granularity validation; grouping preserves all constituents. The probe hashes the source file, derives duration from frames/sample rate, records every returned span and per-case error, and checkpoints the JSON report after each inference. See the private experiment directory for this run's exact manifest, dependency freeze and detailed outputs.

To regenerate truncated files without guessed speech timing, copy exactly `floor(original_frame_count * 0.43)` PCM frames from each full WAV and preserve sample rate/channel/width. Generate silence with 48,000 zero PCM16 mono samples at 16 kHz. Neither operation assigns a word label to the cut.

## Verification evidence

Initial contract implementation was a deliberate `unknown/not_implemented` stub. The first test run produced **5 assertion failures in 9 tests** (no import failures), covering the positive projection and specific rejection reasons. After implementation, **9 tests passed**. A separate regression test then reproduced reuse of evidence on changed spans (**1 failure in 10 tests**); binding evidence to the candidate fingerprint fixed it. A third TDD cycle added explicit whole-number grouping: **1 failure in 12 tests** before implementation, then **12 tests passed**. A fourth regression cycle reproduced reuse of a coordinate mapping for another generation (**1 failure in 13 tests**) and fixed it by binding the mapping to playback identity. A final regression for absent evidence/mapping reproduced a missing explicit outcome (**1 failure in 14 tests**); it now returns unknown. A local-model CLI regression also failed before implementation and now rejects remote/nonexistent model paths before runtime imports; the runner enforces offline environment flags itself. The latest run passes **15 tests**. RED/GREEN logs are retained privately.

These Python results validate the conservative contract. They do not validate the model's word accuracy, native-provider behavior or physical playback boundary.

## Runtime and pinned model identity

Host: Apple M4 Mac mini, 10 CPU cores, 16 GB unified memory, macOS 26.3.1 (25D771280a), arm64. Isolated Python is CPython 3.12.7. Tested runtime pins are `qwen-asr==0.0.6`, `torch==2.14.1`, `transformers==4.57.6`, `numpy==2.5.4`, `soundfile==0.14.0`; the full resolved dependency freeze remains in private evidence. Torch reports MPS built and available. CPU baseline uses float32, eager attention, four Torch threads. These are experimental settings, not a tuned deployment recommendation.

Model identity is the official **ModelScope** repository's files at revision `6f4d7c9606feb7adf282c9e4b139f28e8695d867` (latest model-card revision `cf1c50164ea3ac48240d12bef5ead74aee0720cc`). No Hugging Face commit SHA was resolved; the ModelScope SHA must not be represented as a Hugging Face revision.

- `model.safetensors`: 1,835,544,544 bytes, SHA-256 `47831d0e82f96b20e9034dba01a075ee06436654719f6a68289e49f1b65ce0e7`.
- `config.json`: SHA-256 `d616c65d46c4b90bdc651b0a0963ea932732241140f337f9bb6b0335a9c8ef09`.
- Every downloaded tokenizer/processor/model file was checked against the official registry's SHA-256 metadata.
- Installed `qwen3_forced_aligner.py` and the inspected Git source both hash to `fb586127507c5980d11c6d848b530e652afad3ae0707cba6eb0c90377b2a2111`. Thus the inspected processor behavior matches the installed implementation.

The initial slow PyPI installation was cancelled before completion, then the same pinned packages were installed into the same otherwise empty isolated environment using the Aliyun PyPI mirror. Model weights were unaffected. Nothing was installed into the global Python environment.

## Actual model results

Both CPU and MPS executed all nine original conditions twice: **36 real model inferences**, with no inference exceptions. Both backends returned identical public timestamp spans for all original cases. The table records the second invocation of each case within its resident process; these are individual observations, not latency percentiles or a load test.

| Condition | Audio seconds | CPU repeat seconds | MPS repeat seconds | Structural result |
|---|---:|---:|---:|---|
| `say` counting, complete | 5.715813 | 0.326928 | 0.144152 | 10 positive character candidates |
| `say` counting, truncated/full text | 2.457750 | 0.242387 | 0.093256 | unknown: incomplete audio |
| `say` ordinary, complete | 5.717313 | 0.776722 | 0.159870 | 22 positive character candidates |
| `say` ordinary, truncated/full text | 2.458438 | 0.346201 | 0.122974 | unknown: incomplete audio |
| Silence/full text | 3.000000 | 0.254651 | 0.097735 | unknown: four zero-duration spans |
| Native raw Arabic transcript, complete | 20.960000 | 1.146239 | 0.442605 | one giant digit token; no number granularity |
| Native raw Arabic, truncated/full text | 9.012792 | 0.453328 | 0.227324 | unknown: incomplete audio |
| Native explicit Chinese numerals, complete | 20.960000 | 1.353026 | 0.548744 | 117 chars, three zero-duration spans |
| Native explicit Chinese numerals, truncated/full text | 9.012792 | 0.789238 | 0.369135 | unknown: incomplete audio |

Every row's **physical word-boundary projection is unknown**, including structurally valid complete samples.

Initialization and resource observations:

- First CPU process: 69.181 seconds from just before importing `Qwen3ForcedAligner` through model initialization, then first counting inference 1.053 seconds. Full process wall time 99.64 seconds. Peak process RSS 4.066 GiB (`/usr/bin/time -l` agrees with Python `resource`). This initialization includes first use of the installed Python dependencies; it excludes earlier Torch/NumPy imports, package download, and weight download. It must not be described as pure weight-load latency.
- Subsequent fresh MPS process with already warm OS/package caches: 8.115 seconds for that same initialization region, first counting inference 2.738 seconds, total process wall time 18.82 seconds. Peak process RSS 1.908 GiB; observed MPS driver allocation up to 3.949 GiB. Unified-memory accounting can overlap, so those counters must not be added as independent resident memory pools. This is not an equal-cache cold-start CPU/MPS comparison.
- CPU first 24 kHz native inference 7.705 seconds versus repeat 1.146 seconds also demonstrates one-time preprocessing/runtime cost. These observed outliers matter for worker warm-up; no response-stop path should wait for them.
- First CPU import emitted upstream `nagisa` Python 3.12 `SyntaxWarning`s for invalid escape sequences. No library source was patched. Focused contract tests have clean output.

### Failure behavior and numeric granularity

The model is a forced aligner and emits supplied transcript units even when the corresponding speech is absent. On 2.457750-second truncated counting it still emitted all ten supplied characters, including four zero-duration spans and three spans extending beyond the file end. Truncated ordinary speech emitted timestamps as far as 3.52 seconds for a 2.458438-second file. Native normalized truncation emitted 117 characters, 61 zero-duration spans and 57 out-of-range spans, reaching 20.48 seconds for a 9.012792-second file. Silence also produced nonempty timestamps. A full returned token list therefore cannot be treated as acoustic coverage.

The installed official `encode_timestamp` uses `tokenize_space_lang` for Chinese. It removes punctuation inside a whitespace-delimited token before splitting CJK characters. Therefore the raw `1、2、…、50` transcript becomes one concatenated Arabic digit token, whose candidate span is 18.00–20.48 seconds. It passes structural unit concatenation checks but cannot answer which individual number was played. This is precisely why `candidate_valid` does not mean validated coverage, granularity or accuracy.

A separate explicit **space-separated Arabic** condition changes only the separators to spaces (`1 2 … 50`). It yields 50 original-number tokens, 16 of which have zero duration; the complete case is structurally invalid. The unmodified raw transcript remains a separate retained case.

A further explicit Chinese **whole-number grouping** uses the supplied numeral list, not acoustic inference, to combine the aligner's 117 character constituents into 50 number units. The helper `group_character_spans` retains every original constituent inside its parent group. It permits a zero-length constituent only inside an overall positive number span; it never splits a returned token, changes a time, bridges an overlap/wrong-order/out-of-range constituent, discards a character, or repairs incomplete audio. The three zero-length characters lie inside the numbers 22, 31 and 42; all 50 whole-number spans are positive, ordered and bounded in the complete sample, so the grouping is a **structurally valid alternate-granularity candidate**. It supplies no independently measured acoustic truth and its word position remains unknown. Truncated/full-transcript grouping remains unknown.


Supplemental conditions were retained in separate CPU/MPS reports: four conditions × two invocations × two backends = **16 additional retained results**, for 52 retained model results overall. An initial supplemental CPU diagnostic pass was superseded after an evidence-schema cleanup. Both final supplemental backends returned identical constituent spans and grouped validation outcomes.

| Supplemental condition | CPU repeat seconds | MPS repeat seconds | Outcome |
|---|---:|---:|---|
| Explicit spaced Arabic, complete | 1.429901 | 0.485012 | unknown: 16 zero-duration whole-number spans |
| Explicit spaced Arabic, truncated/full text | 0.951170 | 0.265246 | unknown: incomplete audio |
| Explicit Chinese, complete, grouped by number | 1.847560 | 0.604264 | 50 whole-number candidates; retains three zero-duration chars |
| Explicit Chinese, truncated/full text, grouped | 0.742525 | 0.354445 | unknown: incomplete audio |

All these supplementary boundary projections remain unknown. The final extra-run report records probe source SHA-256 `bcf61262c4576989f88c16ff19651b2dafe45e4d95b43ebbdb69810a65e22c05`. A subsequent contract-only change additionally bound the unused projection mapping to playback identity and made absent evidence explicitly unknown, and enforced the already-tested offline/local-model invocation; the measured model invocation, timestamps and grouping logic were unchanged, and its new regression test passes. The final contract does not loosen any measured result.

## Feasibility decision

**Local offline inference is demonstrated on this M4 for CPU and MPS. Accurate physical playback text position remains unproven.** The resident model is fast enough in this small experiment to justify evaluating asynchronous alignment of complete audio/text blocks. There is no demonstrated streaming/causal alignment contract here: the direct API consumes a buffered audio/text pair, supplying full future text against an incomplete audio prefix fails, and first-use initialization/inference costs are much larger than warm calls.

For the current task, alignment must remain an offline or asynchronous diagnostic that never blocks immediate interruption/reconnect. It cannot decide heard words from this evidence. Before any future production projection, independently label representative word/number boundaries, validate transcript normalization and unit granularity, establish coverage/error rules and timing uncertainty, and calibrate codec/resampler/I2S coordinates for the matching generation. No device acoustic acceptance, exact-resume pass, or online deployment claim is made by this experiment.
