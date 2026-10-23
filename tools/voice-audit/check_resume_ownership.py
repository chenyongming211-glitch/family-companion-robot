#!/usr/bin/env python3
"""Check synthetic resume-twice ownership; transport success alone is insufficient.

Does not establish acoustic playback, word alignment or accurate continuation.
Input files may contain synthetic transcripts; output omits conversation text.
"""
import argparse
import json
from pathlib import Path


def check_snapshot_requests(trace):
    errors = []
    last_ack = None
    request_count = 0
    for event in trace:
        if event.get("event") == "snapshot_acknowledged":
            last_ack = event
        if event.get("event") == "response_requested":
            request_count += 1
            if (last_ack is None or event.get("upstream_generation") != last_ack.get("upstream_generation")
                    or event.get("snapshot_revision") != last_ack.get("snapshot_revision")):
                errors.append("response_without_current_snapshot_ack")
            last_ack = None
    if request_count == 0:
        errors.append("missing_generation_gate_evidence")
    return errors, request_count


def check(result, trace):
    errors = []
    if (result.get("scenario") != "resume-twice" or
            result.get("status") != "transport_completed" or
            result.get("simulated_receipts") is not True or
            result.get("synthetic_audio_only") is not True):
        errors.append("invalid_or_incomplete_synthetic_scenario")
    replies = {r.get("phase"): r for r in result.get("replies", [])}
    phases = ("count", "resume_first", "resume_final")
    if any(p not in replies for p in phases):
        errors.append("missing_reply_phase")
        return {"status": "failed", "errors": errors,
                "exact_text_resume_validated": False}
    ids = [replies[p].get("playback_id") for p in phases]
    if any(not isinstance(i, int) or isinstance(i, bool) or i <= 0 for i in ids) or len(set(ids)) != 3:
        errors.append("invalid_playback_identities")
    if any(replies[p].get("reason") != "interrupted" for p in phases[:2]):
        errors.append("missing_two_interruptions")
    pauses = [e for e in trace if e.get("event") in ("paused_created", "paused_resumed")]
    task = pauses[0].get("paused_task_id") if pauses else None
    if not task or any(e.get("paused_task_id") != task for e in pauses):
        errors.append("original_task_replaced")
    if not all(any(e.get("playback_id") == i for e in pauses) for i in ids[:2]):
        errors.append("paused_latest_segment_not_advanced")
    bindings = sum(e.get("event") == "task_resume_bound" for e in trace)
    if bindings < 2:
        errors.append("missing_successful_resume_binding")
    if not any(e.get("event") == "finished" and e.get("playback_id") == ids[2]
               and e.get("paused_playback_id") == ids[2] and e.get("paused_task_id") == task
               for e in trace):
        errors.append("final_resume_not_owned_by_original_task")
    snapshot_errors, request_count = check_snapshot_requests(trace)
    errors.extend(snapshot_errors)
    return {"status": "failed" if errors else "passed", "errors": sorted(set(errors)),
            "scope": "synthetic_task_ownership_and_snapshot_order",
            "paused_playback_ids": ids[:2], "final_playback_id": ids[2],
            "successful_resume_bindings": bindings, "gated_response_requests": request_count,
            "exact_text_resume_validated": False, "acoustic_playback_validated": False}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--result", required=True)
    parser.add_argument("--trace", required=True)
    args = parser.parse_args()
    try:
        result = json.loads(Path(args.result).read_text())
        trace = [json.loads(line) for line in Path(args.trace).read_text().splitlines() if line.strip()]
        verdict = check(result, trace)
    except (OSError, ValueError, TypeError, KeyError):
        verdict = {"status": "failed", "errors": ["invalid_input"], "exact_text_resume_validated": False}
    print(json.dumps(verdict, ensure_ascii=False, indent=2))
    return 0 if verdict["status"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
