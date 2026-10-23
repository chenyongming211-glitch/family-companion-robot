import unittest
from check_resume_ownership import check


class ResumeOwnershipTests(unittest.TestCase):
    def fixtures(self):
        result = {"scenario": "resume-twice", "status": "transport_completed",
                  "simulated_receipts": True, "synthetic_audio_only": True,
                  "replies": [{"phase": p, "playback_id": i, "reason": reason}
                              for p, i, reason in [("count", 2, "interrupted"),
                                                   ("resume_first", 4, "interrupted"),
                                                   ("resume_final", 7, "")]]}
        trace = [{"event": "paused_created", "playback_id": 2, "paused_task_id": "task-1"},
                 {"event": "task_resume_bound"},
                 {"event": "paused_resumed", "playback_id": 4, "paused_task_id": "task-1"},
                 {"event": "snapshot_acknowledged", "upstream_generation": 3, "snapshot_revision": 10},
                 {"event": "response_requested", "upstream_generation": 3, "snapshot_revision": 10},
                 {"event": "task_resume_bound"},
                 {"event": "finished", "playback_id": 7, "paused_playback_id": 7, "paused_task_id": "task-1"}]
        return result, trace

    def test_transport_success_cannot_hide_second_unbound_resume(self):
        result, trace = self.fixtures()
        trace.pop(5)
        trace[-1]["paused_playback_id"] = 4
        self.assertEqual(check(result, trace)["status"], "failed")

    def test_stale_ack_is_rejected(self):
        result, trace = self.fixtures()
        trace[4]["snapshot_revision"] = 11
        self.assertIn("response_without_current_snapshot_ack", check(result, trace)["errors"])

    def test_bound_normal_block_is_not_exact_text_resume(self):
        result, trace = self.fixtures()
        verdict = check(result, trace)
        self.assertEqual(verdict["status"], "passed")
        self.assertIs(verdict["exact_text_resume_validated"], False)

    def test_side_answer_cannot_replace_original_task(self):
        result, trace = self.fixtures()
        trace[2]["paused_task_id"] = "task-2"
        self.assertIn("original_task_replaced", check(result, trace)["errors"])


if __name__ == "__main__":
    unittest.main()
