"""Synthetic ONNX tests; run only in the isolated cloud Python environment."""
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

import onnx
from onnx import TensorProto, helper


URL = "https://huggingface.co/Wespeaker/wespeaker-cnceleb-resnet34-LM/resolve/ed25c1ecbd9e8d3c7944df95430da02bd3cdcf7a/cnceleb_resnet34_LM.onnx"
SCRIPT = Path(__file__).with_name("prepare_wespeaker.py")


class PreparationTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)

    def model(self):
        # A real checker-valid graph with a fixed weight, no speech or vectors.
        weight = helper.make_tensor("weight", TensorProto.FLOAT, [80, 256], [0.125] * (80 * 256))
        nodes = [helper.make_node("ReduceMean", ["features"], ["mean"], axes=[1], keepdims=0),
                 helper.make_node("MatMul", ["mean", "weight"], ["embedding"])]
        graph = helper.make_graph(nodes, "synthetic", [helper.make_tensor_value_info("features", TensorProto.FLOAT, ["B", "T", 80])],
                                  [helper.make_tensor_value_info("embedding", TensorProto.FLOAT, ["B", 256])], [weight])
        return helper.make_model(graph, opset_imports=[helper.make_opsetid("", 13)])

    def run_prepare(self, model=None, *, name="output.onnx", sha=None, size=None, language="Chinese", url=URL):
        raw = (model or self.model()).SerializeToString(deterministic=True)
        source = self.root / "input.onnx"
        source.write_bytes(raw)
        digest = hashlib.sha256(raw).hexdigest()
        args = [sys.executable, str(SCRIPT), "--input", str(source), "--output", str(self.root / name),
                "--sha256", sha or digest, "--bytes", str(len(raw) if size is None else size), "--language", language, "--url", url]
        return subprocess.run(args, capture_output=True, text=True), raw

    def test_verified_metadata_preserves_graph_weights_and_private_output(self):
        result, raw = self.run_prepare()
        self.assertEqual(result.returncode, 0, result.stderr)
        report = json.loads(result.stdout)
        derived = (self.root / "output.onnx").read_bytes()
        before, after = onnx.load_model_from_string(raw), onnx.load_model_from_string(derived)
        self.assertEqual(before.graph.SerializeToString(), after.graph.SerializeToString())
        self.assertEqual(report["raw_sha256"], hashlib.sha256(raw).hexdigest())
        self.assertEqual(report["model_sha256"], hashlib.sha256(derived).hexdigest())
        self.assertEqual(report["raw_graph_sha256"], report["model_graph_sha256"])
        self.assertEqual(report["model_bytes"], len(derived))
        metadata = dict((p.key, p.value) for p in after.metadata_props)
        self.assertEqual(metadata["feature_normalize_type"], "global-mean")
        self.assertEqual(metadata["output_dim"], "256")
        self.assertEqual(metadata["url"], URL)
        self.assertIn("frontend", metadata["comment"])
        self.assertEqual((self.root / "output.onnx").stat().st_mode & 0o777, 0o600)
        for secret in [str(self.root), "raw_data", "float_data", '"weight"', '"embedding"']:
            self.assertNotIn(secret, result.stdout + result.stderr)

    def test_deterministic_metadata_and_preserves_unrelated_metadata(self):
        model = self.model()
        helper.set_model_props(model, {"retained": "research source", "framework": "wespeaker"})
        one, _ = self.run_prepare(model, name="one.onnx")
        two, _ = self.run_prepare(model, name="two.onnx")
        self.assertEqual(one.returncode, 0, one.stderr)
        self.assertEqual(two.returncode, 0, two.stderr)
        self.assertEqual((self.root / "one.onnx").read_bytes(), (self.root / "two.onnx").read_bytes())
        self.assertEqual(json.loads(one.stdout)["metadata"]["retained"], "research source")

    def test_refuses_overwrite_without_mutation(self):
        output = self.root / "output.onnx"
        output.write_bytes(b"preserve prior result")
        result, _ = self.run_prepare()
        self.assertEqual(result.returncode, 1)
        self.assertEqual(output.read_bytes(), b"preserve prior result")
        self.assertEqual(result.stderr, "preparation_failed\n")

    def test_rejects_wrong_digest_and_size_before_parsing(self):
        for changes in [{"sha": "0" * 64}, {"size": 1}, {"size": 128 * 1024 * 1024 + 1}]:
            with self.subTest(changes=changes):
                result, _ = self.run_prepare(**changes)
                self.assertEqual(result.returncode, 1)
                self.assertEqual(result.stdout, "")
                self.assertEqual(result.stderr, "preparation_failed\n")
                self.assertFalse((self.root / "output.onnx").exists())

    def test_rejects_external_data_even_without_external_file(self):
        model = self.model()
        tensor = model.graph.initializer[0]
        tensor.ClearField("float_data")
        tensor.data_location = TensorProto.EXTERNAL
        item = tensor.external_data.add(); item.key = "location"; item.value = "private-weight.bin"
        result, _ = self.run_prepare(model)
        self.assertEqual(result.returncode, 1)
        self.assertEqual(result.stderr, "preparation_failed\n")

    def test_rejects_contract_mismatches(self):
        changes = [lambda m: setattr(m.graph.input[0].type.tensor_type, "elem_type", TensorProto.DOUBLE),
                   lambda m: setattr(m.graph.input[0].type.tensor_type.shape.dim[2], "dim_value", 40),
                   lambda m: m.graph.input[0].type.tensor_type.shape.dim.pop(),
                   lambda m: setattr(m.graph.output[0].type.tensor_type, "elem_type", TensorProto.DOUBLE),
                   lambda m: setattr(m.graph.output[0].type.tensor_type.shape.dim[1], "dim_value", 512),
                   lambda m: m.graph.output[0].type.tensor_type.shape.dim.pop(),
                   lambda m: m.graph.input.add().CopyFrom(m.graph.input[0])]
        for change in changes:
            with self.subTest(change=change):
                model = self.model(); change(model)
                result, _ = self.run_prepare(model)
                self.assertEqual(result.returncode, 1)
                self.assertFalse((self.root / "output.onnx").exists())

    def test_rejects_duplicate_or_conflicting_metadata(self):
        for metadata in [[("framework", "wespeaker"), ("framework", "wespeaker")],
                         [("sample_rate", "8000")], [("feature_normalize_type", "none")],
                         [("unrelated", "one"), ("unrelated", "two")]]:
            with self.subTest(metadata=metadata):
                model = self.model()
                for key, value in metadata:
                    p = model.metadata_props.add(); p.key = key; p.value = value
                result, _ = self.run_prepare(model)
                self.assertEqual(result.returncode, 1)
                self.assertFalse((self.root / "output.onnx").exists())

    def test_rejects_unpinned_or_mismatched_source_and_language(self):
        for changes in [{"url": URL.replace("ed25c1ecbd9e8d3c7944df95430da02bd3cdcf7a", "main")},
                        {"url": "https://untrusted.invalid/model"}, {"language": "English"}]:
            with self.subTest(changes=changes):
                result, _ = self.run_prepare(**changes)
                self.assertEqual(result.returncode, 1)
                self.assertFalse((self.root / "output.onnx").exists())

    def test_cli_errors_do_not_echo_private_paths(self):
        result = subprocess.run([sys.executable, str(SCRIPT), "--input", "/private/secret-model"], capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("/private/secret-model", result.stderr)


if __name__ == "__main__":
    unittest.main()
