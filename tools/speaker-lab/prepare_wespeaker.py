#!/usr/bin/env python3
"""Verify a local fixed WeSpeaker ONNX and add sherpa research metadata only.

Run in the private cloud environment. This tool performs no network access and
refuses existing output files. Raw weights and adapted weights remain separate.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import sys

import onnx
from onnx import TensorProto


MAX_MODEL_BYTES = 128 * 1024 * 1024
SOURCES = {
    "https://huggingface.co/Wespeaker/wespeaker-voxceleb-resnet293-LM/resolve/6e6bffe5bf3d772a1f143dc6dbfea58a0799ea83/voxceleb_resnet293_LM.onnx": "English",
    "https://huggingface.co/Wespeaker/wespeaker-cnceleb-resnet34-LM/resolve/ed25c1ecbd9e8d3c7944df95430da02bd3cdcf7a/cnceleb_resnet34_LM.onnx": "Chinese",
}
COMMENT = ("Isolated research only; metadata adaptation preserves the graph and weights. "
           "sherpa v2.0.1 frontend defaults differ from WeSpeaker reference; "
           "not strict reference equivalence or product approval.")


def digest(content):
    return hashlib.sha256(content).hexdigest()


def canonical_path(value, *, existing):
    path = Path(value)
    if not path.is_absolute() or os.path.normpath(value) != value:
        raise ValueError("invalid path")
    for part in [path, *path.parents] if existing else [path.parent, *path.parent.parents]:
        if part.is_symlink() or not part.exists():
            raise ValueError("invalid path")
    return path


def reject_external_data(message):
    # Walk nested attributes/subgraphs as well as graph initializers. Never ask
    # ONNX to resolve external files, including malformed location-only tensors.
    if isinstance(message, TensorProto) and (message.data_location == TensorProto.EXTERNAL or message.external_data):
        raise ValueError("external data")
    for field, value in message.ListFields():
        if field.type != field.TYPE_MESSAGE:
            continue
        if field.is_repeated:
            for child in value:
                reject_external_data(child)
        else:
            reject_external_data(value)


def check_contract(model):
    graph = model.graph
    if len(graph.input) != 1 or len(graph.output) != 1:
        raise ValueError("invalid interface")
    for value, rank, last in [(graph.input[0], 3, 80), (graph.output[0], 2, 256)]:
        tensor = value.type.tensor_type
        if tensor.elem_type != TensorProto.FLOAT or len(tensor.shape.dim) != rank:
            raise ValueError("invalid tensor")
        dims = tensor.shape.dim
        if not dims[-1].HasField("dim_value") or dims[-1].dim_value != last:
            raise ValueError("invalid dimension")
        for dim in dims[:-1]:
            if not ((dim.HasField("dim_value") and dim.dim_value > 0) or
                    (dim.HasField("dim_param") and dim.dim_param)):
                raise ValueError("invalid dynamic dimension")
    input_batch, output_batch = graph.input[0].type.tensor_type.shape.dim[0], graph.output[0].type.tensor_type.shape.dim[0]
    if input_batch.HasField("dim_value") and output_batch.HasField("dim_value") and input_batch.dim_value != output_batch.dim_value:
        raise ValueError("batch mismatch")


def prepare(input_path, output_path, sha256, expected_bytes, language, url):
    if not re.fullmatch(r"[0-9a-f]{64}", sha256) or not 1 <= expected_bytes <= MAX_MODEL_BYTES or SOURCES.get(url) != language:
        raise ValueError("invalid provenance")
    source = canonical_path(input_path, existing=True)
    output = canonical_path(output_path, existing=False)
    if not source.is_file() or source.stat().st_size != expected_bytes or output.exists() or output.is_symlink():
        raise ValueError("invalid file")
    with source.open("rb") as stream:
        raw = stream.read(expected_bytes + 1)
    if len(raw) != expected_bytes or digest(raw) != sha256:
        raise ValueError("unverified bytes")
    # Parse only after the trusted expected size and digest have matched.
    model = onnx.load_model_from_string(raw)
    reject_external_data(model)
    check_contract(model)
    onnx.checker.check_model(model)
    raw_graph_sha = digest(model.graph.SerializeToString(deterministic=True))
    metadata = {}
    for prop in model.metadata_props:
        if prop.key in metadata:
            raise ValueError("duplicate metadata")
        metadata[prop.key] = prop.value
    additions = {"framework": "wespeaker", "sample_rate": "16000", "output_dim": "256",
                 "normalize_samples": "0", "feature_normalize_type": "global-mean",
                 "language": language, "url": url, "comment": COMMENT}
    for key, value in additions.items():
        if key in metadata and metadata[key] != value:
            raise ValueError("conflicting metadata")
    metadata.update(additions)
    del model.metadata_props[:]
    for key, value in sorted(metadata.items()):
        prop = model.metadata_props.add()
        prop.key, prop.value = key, value
    model_graph_sha = digest(model.graph.SerializeToString(deterministic=True))
    if model_graph_sha != raw_graph_sha:
        raise ValueError("graph changed")
    onnx.checker.check_model(model)
    adapted = model.SerializeToString(deterministic=True)
    if len(adapted) > MAX_MODEL_BYTES:
        raise ValueError("adapted model oversize")
    # Exclusive creation is the overwrite policy, including concurrent callers.
    fd = os.open(output, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    try:
        with os.fdopen(fd, "wb") as stream:
            stream.write(adapted)
    except BaseException:
        output.unlink(missing_ok=True)
        raise
    return {"raw_sha256": sha256, "raw_bytes": len(raw), "model_sha256": digest(adapted),
            "model_bytes": len(adapted), "raw_graph_sha256": raw_graph_sha,
            "model_graph_sha256": model_graph_sha, "metadata": metadata}


class PrivateArgumentParser(argparse.ArgumentParser):
    def error(self, message):
        raise ValueError("invalid arguments")


def main(argv=None):
    try:
        parser = PrivateArgumentParser(description=__doc__)
        parser.add_argument("--input", required=True)
        parser.add_argument("--output", required=True)
        parser.add_argument("--sha256", required=True)
        parser.add_argument("--bytes", required=True, type=int, dest="expected_bytes")
        parser.add_argument("--language", required=True, choices=("English", "Chinese"))
        parser.add_argument("--url", required=True)
        args = vars(parser.parse_args(argv))
        args["input_path"] = args.pop("input")
        args["output_path"] = args.pop("output")
        print(json.dumps(prepare(**args), sort_keys=True))
        return 0
    except Exception:
        print("preparation_failed", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
