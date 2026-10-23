#!/usr/bin/env python3
"""Offline official Qwen forced-aligner probe and conservative boundary contract.

No ASR, recording, playback, server calls, or gateway integration. The CLI only
emits candidate timestamps; verified boundary projection requires independently
supplied evidence and a calibrated playback coordinate mapping.
"""
import argparse
import hashlib
import importlib.metadata
import json
import math
import os
import platform
import resource
import time
import unicodedata
from pathlib import Path


def _unknown(reason):
    return {"status": "unknown", "reason": reason}


def _finite(value):
    return isinstance(value, (int, float)) and not isinstance(value, bool) and math.isfinite(value)


def _units(text):
    # Explicitly discard punctuation and spacing only; do not silently lose symbols.
    return "".join(c for c in text if not c.isspace() and not unicodedata.category(c).startswith("P"))


def validate_alignment(data, expected_identity):
    """Validate structural candidates, never acoustic truth or transcript coverage.

    audio_complete is a provenance assertion that the whole output PCM was saved.
    Even true does not establish that every supplied word was spoken.
    """
    required = ("generation", "response_id", "item_id", "content_index", "audio_sha256")
    if (not isinstance(expected_identity, dict) or
            any(k not in expected_identity for k in required) or
            data.get("identity") != expected_identity or
            not expected_identity.get("response_id") or not expected_identity.get("item_id") or
            len(str(expected_identity.get("audio_sha256", ""))) != 64):
        return _unknown("identity_mismatch")
    duration = data.get("audio_duration")
    if not _finite(duration) or duration <= 0:
        return _unknown("invalid_audio_duration")
    if data.get("audio_complete") is not True:
        return _unknown("incomplete_audio")
    text, spans = data.get("text"), data.get("spans")
    if not isinstance(text, str) or not _units(text) or not isinstance(spans, list) or not spans:
        return _unknown("missing_coverage")
    previous_end = 0.
    for span in spans:
        if not isinstance(span, dict) or not isinstance(span.get("text"), str) or not _units(span["text"]):
            return _unknown("invalid_span")
        start, end = span.get("start"), span.get("end")
        if (not _finite(start) or not _finite(end) or start < previous_end or
                end <= start or end > duration):
            return _unknown("invalid_span")
        previous_end = end
    if _units("".join(s["text"] for s in spans)) != _units(text):
        return _unknown("missing_coverage")
    return {"status": "candidate_valid", "reason": "structure_only_acoustic_coverage_unproven"}


def group_character_spans(data, units, expected_identity):
    """Explicit alternate granularity; preserve every original constituent.

    Zero-length constituents may belong to a positive whole-word span, but no
    wrong-order, overlapping, out-of-range or split-token span is repaired.
    This supplies another structural candidate, never acoustic validation.
    """
    original = validate_alignment(data, expected_identity)
    if original["status"] != "candidate_valid" and original["reason"] != "invalid_span":
        return {"validation": original}
    if (not isinstance(units, list) or not units or
            any(not isinstance(u, str) or not _units(u) for u in units) or
            _units("".join(units)) != _units(data["text"])):
        return {"validation": _unknown("group_units_mismatch")}
    previous_end = 0.
    for span in data["spans"]:
        if (not isinstance(span, dict) or not isinstance(span.get("text"), str) or
                not _units(span["text"]) or not _finite(span.get("start")) or
                not _finite(span.get("end")) or span["start"] < previous_end or
                span["end"] < span["start"] or span["end"] > data["audio_duration"]):
            return {"validation": _unknown("invalid_constituent")}
        previous_end = span["end"]
    groups, cursor = [], 0
    for unit in units:
        constituents, joined = [], ""
        target = _units(unit)
        while cursor < len(data["spans"]) and len(joined) < len(target):
            span = data["spans"][cursor]
            constituents.append(dict(span))
            joined += _units(span["text"])
            cursor += 1
        if joined != target:
            return {"validation": _unknown("cannot_split_or_reassign_constituent")}
        groups.append({"text": unit, "start": constituents[0]["start"],
                       "end": constituents[-1]["end"], "constituent_spans": constituents})
    if cursor != len(data["spans"]):
        return {"validation": _unknown("group_units_mismatch")}
    grouped = {k: data[k] for k in ("identity", "text", "audio_duration", "audio_complete")}
    grouped["spans"] = groups
    return {"validation": validate_alignment(grouped, expected_identity), "data": grouped,
            "zero_duration_constituents": sum(s["start"] == s["end"] for s in data["spans"])}


def alignment_fingerprint(data):
    """Bind an external attestation to this exact candidate and transcript."""
    fields = {k: data.get(k) for k in ("identity", "text", "spans", "audio_duration", "audio_complete")}
    return hashlib.sha256(json.dumps(fields, sort_keys=True, ensure_ascii=False,
                                     allow_nan=False).encode()).hexdigest()


def project_boundary(data, expected_identity, played_samples, mapping, evidence):
    """Project only independently verified spans into an explicitly calibrated map.

    This API treats evidence as an external attestation, not a measurement made by
    this script. Tests use labelled fixtures; the actual model runner never issues
    that attestation. Mapping is source PCM sample position =
    (played_samples - playback_offset_samples) * source_rate / playback_rate.
    """
    validation = validate_alignment(data, expected_identity)
    if validation["status"] != "candidate_valid":
        return validation
    if (not isinstance(evidence, dict) or evidence.get("coverage_validated") is not True or
            evidence.get("boundaries_validated") is not True or
            evidence.get("identity") != expected_identity or not evidence.get("reference") or
            evidence.get("alignment_sha256") != alignment_fingerprint(data)):
        return _unknown("unvalidated_acoustic_evidence")
    if not isinstance(mapping, dict):
        return _unknown("invalid_coordinate_mapping")
    sr, pr, offset = mapping.get("source_rate"), mapping.get("playback_rate"), mapping.get("playback_offset_samples")
    if (mapping.get("validated") is not True or mapping.get("identity") != expected_identity or
            not mapping.get("calibration_id") or
            not all(_finite(v) for v in (sr, pr, offset, played_samples)) or
            sr <= 0 or pr <= 0 or offset < 0 or played_samples < offset):
        return _unknown("invalid_coordinate_mapping")
    source_sample = (played_samples - offset) * sr / pr
    seconds = source_sample / sr
    if seconds > data["audio_duration"]:
        return _unknown("playback_out_of_bounds")
    completed = []
    for span in data["spans"]:
        if span["start"] < seconds < span["end"]:
            return _unknown("cut_inside_span")
        if span["end"] <= seconds:
            completed.append(span["text"])
    return {"status": "known", "completed_text": "".join(completed),
            "source_pcm_seconds": seconds, "evidence_reference": evidence["reference"]}


def run_probe(args):
    """Load only the forced aligner; persist per-case errors as experiment evidence."""
    os.environ["HF_HUB_OFFLINE"] = "1"
    os.environ["TRANSFORMERS_OFFLINE"] = "1"
    import numpy as np
    import soundfile as sf
    import torch
    torch.set_num_threads(args.threads)
    started = time.perf_counter()
    from qwen_asr import Qwen3ForcedAligner
    model = Qwen3ForcedAligner.from_pretrained(
        args.model, dtype=torch.float32, device_map=args.device,
        attn_implementation="eager", local_files_only=True)
    if args.device == "mps":
        torch.mps.synchronize()
    init = time.perf_counter() - started
    manifest_path = Path(args.manifest).resolve()
    manifest = json.loads(manifest_path.read_text())
    with open(Path(args.model) / "model.safetensors", "rb") as weights:
        weights_sha = hashlib.file_digest(weights, "sha256").hexdigest()
    report = {"model": args.model, "model_revision": args.model_revision,
              "runtime": {"python": platform.python_version(), "platform": platform.platform(),
                          "packages": {p: importlib.metadata.version(p) for p in
                                       ("qwen-asr", "torch", "transformers", "numpy", "soundfile")}},
              "backend": args.device, "dtype": "float32", "attention": "eager",
              "torch_num_threads": torch.get_num_threads(),
              "manifest_sha256": hashlib.sha256(manifest_path.read_bytes()).hexdigest(),
              "probe_source_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
              "weights_sha256": weights_sha,
              "cold_model_initialization_seconds": init,
              "peak_process_rss_bytes_after_init": resource.getrusage(resource.RUSAGE_SELF).ru_maxrss,
              "cases": []}
    # ru_maxrss is bytes on macOS, KiB on Linux.
    rss_multiplier = 1 if platform.system() == "Darwin" else 1024
    report["peak_process_rss_bytes_after_init"] *= rss_multiplier
    for case in manifest["cases"]:
        path = (manifest_path.parent / case["audio"]).resolve()
        samples, sr = sf.read(path, dtype="float32")
        if samples.ndim != 1:
            raise ValueError("probe requires mono PCM")
        digest = hashlib.sha256(path.read_bytes()).hexdigest()
        identity = dict(case["identity"], audio_sha256=digest)
        duration = len(samples) / sr
        for iteration in range(args.repeats):
            result = {"name": case["name"], "iteration": iteration,
                      "identity": identity, "audio_duration": duration,
                      "sample_rate": sr, "text": case["text"],
                      "transcript_condition": case.get("transcript_condition", "supplied unchanged"),
                      "audio_complete": case.get("audio_complete") is True,
                      "boundary_projection": _unknown("no_independent_acoustic_or_device_validation")}
            t0 = time.perf_counter()
            try:
                aligned = model.align(audio=(np.asarray(samples), sr), text=case["text"], language="Chinese")
                if args.device == "mps":
                    torch.mps.synchronize()
                result["spans"] = [{"text": s.text, "start": float(s.start_time), "end": float(s.end_time)}
                                   for s in aligned[0]]
                result["validation"] = validate_alignment(result, identity)
                if case.get("group_units"):
                    result["grouped_candidate"] = group_character_spans(result, case["group_units"], identity)
            except Exception as exc:
                result["error"] = f"{type(exc).__name__}: {exc}"
                result["validation"] = _unknown("model_error")
            result["inference_seconds"] = time.perf_counter() - t0
            result["real_time_factor"] = result["inference_seconds"] / duration
            result["peak_process_rss_bytes"] = resource.getrusage(resource.RUSAGE_SELF).ru_maxrss * rss_multiplier
            if args.device == "mps":
                result["mps_driver_allocated_bytes"] = torch.mps.driver_allocated_memory()
            report["cases"].append(result)
            Path(args.output).write_text(json.dumps(report, ensure_ascii=False, indent=2, allow_nan=False) + "\n")
            print(json.dumps({k: result[k] for k in ("name", "iteration", "validation", "inference_seconds")}), flush=True)
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--model", required=True, help="Local pinned official model snapshot directory")
    parser.add_argument("--model-revision", required=True, help="Resolved upstream model SHA")
    parser.add_argument("--manifest", required=True, help="JSON cases with local mono PCM paths")
    parser.add_argument("--output", required=True, help="Ignored local JSON evidence path")
    parser.add_argument("--device", choices=("cpu", "mps"), default="cpu")
    parser.add_argument("--threads", type=int, default=4)
    parser.add_argument("--repeats", type=int, default=2)
    args = parser.parse_args()
    if not Path(args.model).is_dir():
        parser.error("model must be a local directory")
    if args.repeats < 1 or args.threads < 1:
        parser.error("--repeats and --threads must be positive")
    run_probe(args)


if __name__ == "__main__":
    main()
