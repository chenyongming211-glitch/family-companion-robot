import unittest
from check_clarify_choice import check

QUESTION = "我还不能确认刚才停在哪儿。请告诉我从哪里继续，或从头再来。"

class ClarifyChoiceTests(unittest.TestCase):
    def fixtures(self):
        r = {"scenario": "clarify-choice", "status": "transport_completed", "simulated_receipts": True,
             "synthetic_audio_only": True, "user_supplied_resume_point": True,
             "replies": [{"phase": "count", "playback_id": 2, "reason": "interrupted", "frames": 90},
                         {"phase": "continue", "playback_id": 5, "generated_text": QUESTION, "frames": 110},
                         {"phase": "specified_start", "playback_id": 6, "generated_text": "三十七，三十八，直到五十。", "frames": 100}]}
        t = [{"event": "paused_created", "playback_id": 2, "paused_task_id": "task-1"},
             {"event": "task_resume_bound"},
             {"event": "snapshot_acknowledged", "upstream_generation": 2, "snapshot_revision": 5},
             {"event": "response_requested", "upstream_generation": 2, "snapshot_revision": 5},
             {"event": "clarification_verified"}, {"event": "tts_start"},
             {"event": "task_resume_bound"},
             {"event": "finished", "playback_id": 6, "paused_playback_id": 6, "paused_task_id": "task-1"}]
        return r,t

    def test_valid_question_is_not_measured_word_resume(self):
        r,t=self.fixtures();v=check(r,t)
        self.assertEqual(v["status"],"passed")
        self.assertFalse(v["exact_text_resume_validated"])
        self.assertTrue(v["final_text_must_be_reviewed"])

    def test_raw_restart_instead_of_question_fails(self):
        r,t=self.fixtures();r["replies"][1]["generated_text"]="一二三四五六七八九十"
        self.assertIn("unknown_position_not_clarified",check(r,t)["errors"])

    def test_release_before_verification_fails(self):
        r,t=self.fixtures();t[4],t[5]=t[5],t[4]
        self.assertIn("clarification_played_before_verification",check(r,t)["errors"])

    def test_unbound_user_choice_fails(self):
        r,t=self.fixtures();t[-1]["paused_playback_id"]=2
        self.assertIn("user_choice_not_owned_by_original_task",check(r,t)["errors"])

    def test_external_choice_cannot_pretend_to_be_measured(self):
        r,t=self.fixtures();r.pop("user_supplied_resume_point")
        self.assertIn("user_choice_provenance_missing",check(r,t)["errors"])

    def test_stale_ack_fails(self):
        r,t=self.fixtures();t[3]["snapshot_revision"]=4
        self.assertIn("response_without_current_snapshot_ack",check(r,t)["errors"])

if __name__=="__main__":unittest.main()
