import copy
import math
import subprocess
import sys
import unittest
from pathlib import Path

from forced_alignment_probe import validate_alignment, project_boundary, alignment_fingerprint, group_character_spans


class AlignmentContractTests(unittest.TestCase):
    def setUp(self):
        self.identity = {"generation": 1, "response_id": "r1", "item_id": "i1",
                         "content_index": 0, "audio_sha256": "a" * 64}
        self.data = {"identity": self.identity.copy(), "text": "你好，世界。",
                     "audio_duration": 2.0, "audio_complete": True,
                     "spans": [{"text": "你好", "start": .1, "end": .8},
                               {"text": "世界", "start": 1., "end": 1.8}]}
        self.evidence = {"coverage_validated": True, "boundaries_validated": True,
                         "identity": self.identity.copy(), "reference": "test-only hand-labelled fixture"}
        self.evidence["alignment_sha256"] = alignment_fingerprint(self.data)
        self.mapping = {"validated": True, "identity": self.identity.copy(),
                        "calibration_id": "test-only PCM identity",
                        "source_rate": 16000, "playback_rate": 24000,
                        "playback_offset_samples": 0}

    def test_group_number_retains_zero_duration_constituents(self):
        self.data["text"] = "十一，十二"
        self.data["spans"] = [{"text": "十", "start": .1, "end": .3},
                              {"text": "一", "start": .3, "end": .3},
                              {"text": "十", "start": .5, "end": .5},
                              {"text": "二", "start": .5, "end": .9}]
        r = group_character_spans(self.data, ["十一", "十二"], self.identity)
        self.assertEqual(r["validation"]["status"], "candidate_valid")
        self.assertEqual(r["data"]["spans"][0]["text"], "十一")
        self.assertEqual(r["data"]["spans"][0]["constituent_spans"], self.data["spans"][:2])
        self.assertEqual(r["zero_duration_constituents"], 2)

    def test_grouping_cannot_repair_bad_order_bounds_or_wrong_units(self):
        for spans, units in [
                ([{"text": "你好世界", "start": .3, "end": .1}], ["你好世界"]),
                ([{"text": "你好世界", "start": .1, "end": 3}], ["你好世界"]),
                (self.data["spans"], ["你好地球"]),
                ([{"text": "你好世界", "start": .1, "end": .1}], ["你好世界"])]:
            self.data["spans"] = spans
            self.assertEqual(group_character_spans(self.data, units, self.identity)["validation"]["status"], "unknown")

    def test_absent_evidence_or_mapping_is_explicit_unknown(self):
        for evidence, mapping in ((None, self.mapping), (self.evidence, None)):
            try:
                result = project_boundary(self.data, self.identity, 24000, mapping, evidence)
            except (AttributeError, TypeError):
                result = {"status": "invalid_contract_exception"}
            self.assertEqual(result["status"], "unknown")

    def test_cli_requires_local_model_before_importing_runtime(self):
        result = subprocess.run([sys.executable, str(Path(__file__).with_name("forced_alignment_probe.py")),
                                 "--model", "/nonexistent-forced-aligner-probe", "--model-revision", "test",
                                 "--manifest", "unused", "--output", "unused"], capture_output=True, text=True)
        self.assertIn("model must be a local directory", result.stderr)
        self.assertEqual(result.returncode, 2)

    def test_valid_completed_prefix(self):
        self.assertEqual(validate_alignment(self.data, self.identity)["status"], "candidate_valid")
        r = project_boundary(self.data, self.identity, 24000, self.mapping, self.evidence)
        self.assertEqual(r["status"], "known")
        self.assertEqual(r["completed_text"], "你好")

    def test_cut_inside_word_is_unknown(self):
        r = project_boundary(self.data, self.identity, 12000, self.mapping, self.evidence)
        self.assertEqual(r["status"], "unknown")
        self.assertEqual(r["reason"], "cut_inside_span")
        self.assertNotIn("completed_text", r)

    def test_bad_spans_are_unknown(self):
        for spans in ([{"text": "你好世界", "start": .8, "end": .2}],
                      [{"text": "你好世界", "start": -.1, "end": 1.}],
                      [{"text": "你好世界", "start": .1, "end": 2.1}],
                      [{"text": "你好世界", "start": math.nan, "end": 1.}],
                      [{"text": "你好", "start": .1, "end": 1.2},
                       {"text": "世界", "start": 1., "end": 1.8}],
                      [{"text": "你好世界", "start": .1, "end": .1}]):
            with self.subTest(spans=spans):
                d = copy.deepcopy(self.data)
                d["spans"] = spans
                self.assertEqual(validate_alignment(d, self.identity)["status"], "unknown")

    def test_identity_mismatch(self):
        d = copy.deepcopy(self.data)
        d["identity"]["generation"] = 2
        self.assertEqual(validate_alignment(d, self.identity)["reason"], "identity_mismatch")

    def test_incomplete_audio_full_text_even_plausible_spans_unknown(self):
        self.data["audio_complete"] = False
        self.assertEqual(validate_alignment(self.data, self.identity)["reason"], "incomplete_audio")
        self.assertEqual(project_boundary(self.data, self.identity, 24000, self.mapping,
                                         self.evidence)["status"], "unknown")

    def test_missing_or_wrong_text_coverage(self):
        for text in ("你好", "你好地球", ""):
            self.data["spans"] = [{"text": text, "start": .1, "end": 1.}]
            self.assertEqual(validate_alignment(self.data, self.identity)["status"], "unknown")

    def test_candidate_times_without_external_evidence_cannot_project(self):
        for evidence in ({}, {**self.evidence, "coverage_validated": False},
                         {**self.evidence, "boundaries_validated": False},
                         {**self.evidence, "identity": {"generation": 2}}):
            self.assertEqual(project_boundary(self.data, self.identity, 24000, self.mapping,
                                             evidence)["status"], "unknown")

    def test_unvalidated_or_out_of_range_device_coordinate_unknown(self):
        for played, mapping in ((24000, {}), (-1, self.mapping), (50000, self.mapping),
                                (math.nan, self.mapping),
                                (24000, {**self.mapping, "playback_rate": 0})):
            self.assertEqual(project_boundary(self.data, self.identity, played, mapping,
                                             self.evidence)["status"], "unknown")

    def test_evidence_cannot_be_reused_for_different_candidate_spans(self):
        self.data["spans"][0]["end"] = .6
        self.assertEqual(project_boundary(self.data, self.identity, 24000, self.mapping,
                                         self.evidence)["status"], "unknown")

    def test_coordinate_mapping_must_match_playback_identity(self):
        self.mapping["identity"] = {**self.identity, "generation": 2}
        self.assertEqual(project_boundary(self.data, self.identity, 24000, self.mapping,
                                         self.evidence)["status"], "unknown")

    def test_coordinate_offset_and_rate_are_applied(self):
        self.mapping["playback_offset_samples"] = 24000
        self.assertEqual(project_boundary(self.data, self.identity, 48000, self.mapping,
                                         self.evidence).get("completed_text"), "你好")


if __name__ == "__main__":
    unittest.main()
