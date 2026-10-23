#!/usr/bin/env python3
"""Check unknown-position clarification and choice ownership; not heard-word accuracy.

The final generated text still requires independent semantic review. A user-supplied
start is an explicit instruction, never an oracle for actual device playback.
"""
import argparse
import json
import unicodedata
from pathlib import Path
from check_resume_ownership import check_snapshot_requests

QUESTION = "我还不能确认刚才停在哪儿。请告诉我从哪里继续，或从头再来。"


def normalize(text):
    return "".join(c for c in text if not c.isspace() and not unicodedata.category(c).startswith("P"))


def check(result, trace):
    errors = []
    if (result.get("scenario") != "clarify-choice" or result.get("status") != "transport_completed"
            or result.get("simulated_receipts") is not True or result.get("synthetic_audio_only") is not True):
        errors.append("invalid_or_incomplete_synthetic_scenario")
    if result.get("user_supplied_resume_point") is not True:
        errors.append("user_choice_provenance_missing")
    replies = {r.get("phase"): r for r in result.get("replies", [])}
    question, final, count = (replies.get(p, {}) for p in ("continue", "specified_start", "count"))
    ids = [r.get("playback_id") for r in (count, question, final)]
    if (any(not isinstance(i, int) or isinstance(i, bool) or i <= 0 for i in ids)
            or len(set(ids)) != 3 or any(r.get("frames", 0) <= 0 for r in (count, question, final))):
        errors.append("invalid_playback_identity_or_audio")
    if count.get("reason") != "interrupted":
        errors.append("missing_interruption")
    if normalize(question.get("generated_text", "")) != normalize(QUESTION):
        errors.append("unknown_position_not_clarified")
    pauses = [e for e in trace if e.get("event") in ("paused_created", "paused_resumed")]
    task = pauses[0].get("paused_task_id") if pauses else None
    if not task or any(e.get("paused_task_id") != task for e in pauses):
        errors.append("original_task_replaced")
    if sum(e.get("event") == "task_resume_bound" for e in trace) < 2:
        errors.append("missing_question_or_choice_binding")
    if not any(e.get("event") == "finished" and e.get("playback_id") == ids[2]
               and e.get("paused_playback_id") == ids[2] and e.get("paused_task_id") == task for e in trace):
        errors.append("user_choice_not_owned_by_original_task")
    verified = [i for i, e in enumerate(trace) if e.get("event") == "clarification_verified"]
    if len(verified) != 1:
        errors.append("missing_unique_clarification_verification")
    else:
        v = verified[0]
        requests = [i for i, e in enumerate(trace[:v]) if e.get("event") == "response_requested"]
        if (not requests or any(e.get("event") == "tts_start" for e in trace[requests[-1] + 1:v])
                or not any(e.get("event") == "tts_start" for e in trace[v + 1:])):
            errors.append("clarification_played_before_verification")
    snapshot_errors, requests = check_snapshot_requests(trace)
    errors.extend(snapshot_errors)
    return {"status": "failed" if errors else "passed", "errors": sorted(set(errors)),
            "scope": "synthetic_unknown_position_question_and_task_ownership",
            "gated_response_requests": requests, "final_text_must_be_reviewed": True,
            "exact_text_resume_validated": False, "acoustic_playback_validated": False}


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--result", required=True)
    p.add_argument("--trace", required=True)
    args = p.parse_args()
    try:
        r = json.loads(Path(args.result).read_text())
        t = [json.loads(line) for line in Path(args.trace).read_text().splitlines() if line.strip()]
        verdict = check(r, t)
    except (OSError, ValueError, TypeError, KeyError):
        verdict = {"status": "failed", "errors": ["invalid_input"], "exact_text_resume_validated": False}
    print(json.dumps(verdict, ensure_ascii=False, indent=2))
    return 0 if verdict["status"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
