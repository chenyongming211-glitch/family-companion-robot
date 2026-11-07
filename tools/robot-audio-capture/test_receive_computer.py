"""Cloud-only synthetic receiver tests. No microphone, model or real audio."""
import hashlib
import io
import json
import math
import os
from pathlib import Path
import stat
import struct
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from unittest.mock import patch
import wave

try:
    import receive_computer as c
except ModuleNotFoundError:
    c = None

CLIP = "ab" * 16


def header(rate=8000, channels=1):
    return dict(schema_version=1, source_kind="private_human", source_device="mac_microphone", sample_format="f32le_interleaved", sample_rate=rate, channels=channels, frames=rate * 4, pcm_bytes=rate * 4 * channels * 4)


def payload(rate=8000, channels=1):
    return b"".join(struct.pack("<f", 0.15 * math.sin(2 * math.pi * (220 if channel == 0 else 660) * frame / rate)) for frame in range(rate * 4) for channel in range(channels))


def framed(h, pcm, tail=b""):
    raw = h if isinstance(h, bytes) else json.dumps(h).encode()
    return struct.pack("!I", len(raw)) + raw + pcm + tail


class ReceiverTests(unittest.TestCase):
    def setUp(self):
        self.assertIsNotNone(c, "computer receiver implementation required")
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.root.chmod(0o700)

    def bad(self, reason, fn):
        with self.assertRaises(c.ReceiveError) as caught:
            fn()
        self.assertEqual(caught.exception.reason, reason)
        self.assertEqual(str(caught.exception), reason)

    def receive(self, raw, clip=CLIP, converter=None):
        fd, writer = os.pipe()
        def feed():
            try:
                with os.fdopen(writer, "wb") as stream:
                    stream.write(raw)
            except BrokenPipeError:
                pass
        thread = threading.Thread(target=feed)
        thread.start()
        events = []
        try:
            result = c.run_receiver(str(self.root), clip, "M01", fd, events.append, converter=converter)
        finally:
            os.close(fd)
            thread.join(2)
        return result, events

    def test_strict_header_accepts_exact_native_format(self):
        self.assertEqual(c.parse_header(json.dumps(header(44100, 2)).encode()), header(44100, 2))

    def test_header_rejects_duplicate_keys(self):
        raw = json.dumps(header()).encode()[:-1] + b',"sample_rate":8000}'
        self.bad("header_invalid", lambda: c.parse_header(raw))

    def test_header_rejects_extra_missing_and_wrong_fixed_fields(self):
        variants = [dict(header(), extra=1), {k: v for k, v in header().items() if k != "channels"}, dict(header(), source_device="waveshare"), dict(header(), sample_format="s16le"), dict(header(), source_kind="public")]
        for h in variants:
            with self.subTest(h=h):
                self.bad("header_invalid", lambda: c.parse_header(json.dumps(h).encode()))

    def test_header_rejects_boolean_fraction_and_out_of_bounds_integers(self):
        for key, value in [("schema_version", True), ("sample_rate", True), ("channels", True), ("frames", True), ("pcm_bytes", True), ("sample_rate", 8000.0), ("sample_rate", 7999), ("sample_rate", 96001), ("channels", 0), ("channels", 9), ("frames", 31999), ("pcm_bytes", 1)]:
            with self.subTest(key=key, value=value):
                self.bad("header_invalid", lambda: c.parse_header(json.dumps(dict(header(), **{key: value})).encode()))

    def test_header_rejects_arrays_invalid_utf8_and_non_json(self):
        for raw in [b"[]", b"null", b"\xff", b"{", b'{"x":NaN}']:
            self.bad("header_invalid", lambda: c.parse_header(raw))

    def test_header_lengths_zero_and_oversize(self):
        for n in [0, 2049, 0xffffffff]:
            clip = f"{n:032x}"
            result, events = self.receive(struct.pack("!I", n), clip)
            self.assertEqual(result["reason"], "header_length")
            self.assertEqual(events[0], dict(schema_version=1, event="ready", clip=clip))

    def test_truncated_length_header_and_payload(self):
        for raw in [b"\0", struct.pack("!I", 10) + b"{}", framed(header(), b"\0" * 4)]:
            result, _ = self.receive(raw)
            self.assertEqual(result["reason"], "truncated")
            self.assertFalse((self.root / CLIP / "utterance.wav").exists())
            (self.root / CLIP).rename(self.root / ("old" + str(len(raw))))

    def test_payload_requires_eof_and_retains_complete_original_on_failure(self):
        pcm = payload()
        result, _ = self.receive(framed(header(), pcm, b"unexpected"))
        self.assertEqual(result["reason"], "trailing_data")
        self.assertEqual((self.root / CLIP / "source.f32le").read_bytes(), pcm)
        self.assertFalse((self.root / CLIP / "utterance.wav").exists())

    def test_read_timeout_is_total_deadline(self):
        fd, writer = os.pipe()
        try:
            start = time.monotonic()
            result = c.run_receiver(str(self.root), CLIP, "M01", fd, lambda _: None, read_timeout=0.05)
            self.assertEqual(result["reason"], "timeout")
            self.assertLess(time.monotonic() - start, 1)
        finally:
            os.close(fd); os.close(writer)

    def test_nonfinite_and_excessive_float_retain_original_without_wav(self):
        for i, value in enumerate([float("nan"), float("inf"), -float("inf"), 16.1]):
            pcm = struct.pack("<f", value) + b"\0" * (128000 - 4)
            clip = f"{i:032x}"
            result, _ = self.receive(framed(header(), pcm), clip)
            self.assertEqual(result["reason"], "sample_invalid")
            self.assertEqual((self.root / clip / "source.f32le").read_bytes(), pcm)
            self.assertFalse((self.root / clip / "utterance.wav").exists())

    def test_real_ffmpeg_preserves_original_all_channels_selects_first_and_canonical_wav(self):
        h = header(44100, 2)
        pcm = payload(44100, 2)
        result, events = self.receive(framed(h, pcm))
        self.assertEqual(result["event"], "stored")
        self.assertEqual(set(result), {"schema_version", "event", "clip", "samples", "duration_seconds", "rms"})
        self.assertEqual(events, [dict(schema_version=1, event="ready", clip=CLIP), result])
        folder = self.root / CLIP
        self.assertEqual((folder / "source.f32le").read_bytes(), pcm)
        wav = (folder / "utterance.wav").read_bytes()
        self.assertEqual(len(wav), 128044)
        with wave.open(io.BytesIO(wav)) as f:
            self.assertEqual((f.getnchannels(), f.getsampwidth(), f.getframerate(), f.getnframes()), (1, 2, 16000, 64000))
            samples = struct.unpack("<64000h", f.readframes(64000))
        first_energy = abs(sum(samples[i] * complex(math.cos(2 * math.pi * 220 * i / 16000), math.sin(2 * math.pi * 220 * i / 16000)) for i in range(64000)))
        other_energy = abs(sum(samples[i] * complex(math.cos(2 * math.pi * 660 * i / 16000), math.sin(2 * math.pi * 660 * i / 16000)) for i in range(64000)))
        self.assertGreater(first_energy, other_energy * 100)
        meta = json.loads((folder / "metadata.json").read_text())
        self.assertEqual(meta["source_format"], h)
        self.assertEqual(meta["selected_channel"], 0)
        self.assertEqual(meta["source_sha256"], hashlib.sha256(pcm).hexdigest())
        self.assertEqual(meta["wav_sha256"], hashlib.sha256(wav).hexdigest())
        self.assertEqual(meta["source_device_basis"], "operator_declared_direct_avfoundation")
        self.assertTrue(meta["raw_source_all_channels_retained"])
        for key in ["human_acceptance", "production_acceptance", "identity_acceptance"]:
            self.assertFalse(meta[key])
        self.assertEqual(meta["stage"], "computer_enrollment_candidate")
        self.assertEqual(stat.S_IMODE(folder.stat().st_mode), 0o700)
        for p in folder.iterdir():
            self.assertEqual(stat.S_IMODE(p.stat().st_mode), 0o600)

    def test_existing_clip_and_symlink_or_public_root_rejected_without_overwrite(self):
        (self.root / CLIP).mkdir()
        marker = self.root / CLIP / "marker"
        marker.write_text("keep")
        result, events = self.receive(b"")
        self.assertEqual(result["reason"], "output_unsafe")
        self.assertEqual(len(events), 1)
        self.assertEqual(marker.read_text(), "keep")
        (self.root / "link").symlink_to(self.root, target_is_directory=True)
        for path in [str(self.root / "link"), str(self.root / ".." / self.root.name)]:
            result = c.run_receiver(path, "cd" * 16, "M01", -1, lambda _: None)
            self.assertEqual(result["reason"], "output_unsafe")
        self.root.chmod(0o755)
        self.assertEqual(c.run_receiver(str(self.root), "ef" * 16, "M01", -1, lambda _: None)["reason"], "output_unsafe")

    def test_root_must_be_exactly_0700(self):
        self.root.chmod(0o600)
        self.assertEqual(c.run_receiver(str(self.root), CLIP, "M01", -1, lambda _: None)["reason"], "output_unsafe")
        self.root.chmod(0o700)

    def test_cli_term_writes_fixed_incomplete_and_exits(self):
        child = subprocess.Popen([sys.executable, str(Path(c.__file__)), "--root", str(self.root), "--clip", CLIP, "--participant", "M01"], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        ready = json.loads(child.stdout.readline())
        self.assertEqual(ready["event"], "ready")
        child.terminate()
        stdout, stderr = child.communicate(timeout=3)
        self.assertEqual(child.returncode, 2)
        self.assertEqual(stderr, b"")
        self.assertEqual(json.loads(stdout)["reason"], "signal")
        self.assertEqual(json.loads((self.root / CLIP / "metadata.json").read_text())["status"], "incomplete")

    def test_clip_and_participant_allowlist(self):
        for clip, participant in [("../a", "M01"), ("AB" * 16, "M01"), (CLIP, "Name"), (CLIP, "M06"), (CLIP, "M01\n")]:
            self.assertEqual(c.run_receiver(str(self.root), clip, participant, -1, lambda _: None)["reason"], "arguments_invalid")
        self.assertEqual(list(self.root.iterdir()), [])

    def test_converter_exception_and_invalid_output_are_sanitized_and_original_retained(self):
        def fail(*args):
            raise OSError("SECRET_AUDIO_OR_PATH")
        for i, converter in enumerate([fail, lambda *args: (b"bad", "ffmpeg version 1")]):
            clip = f"{20+i:032x}"
            result, events = self.receive(framed(header(), payload()), clip, converter)
            self.assertEqual(result["reason"], "conversion_failed")
            self.assertNotIn("SECRET", json.dumps(events))
            self.assertTrue((self.root / clip / "source.f32le").exists())
            self.assertFalse((self.root / clip / "utterance.wav").exists())

    def test_process_cli_stdout_is_fixed_json_only(self):
        proc = subprocess.run([sys.executable, str(Path(c.__file__)), "--root", str(self.root), "--clip", CLIP, "--participant", "M01"], input=framed(header(), payload()), stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=15)
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertEqual(proc.stderr, b"")
        lines = proc.stdout.splitlines()
        self.assertEqual(len(lines), 2)
        self.assertEqual([json.loads(line)["event"] for line in lines], ["ready", "stored"])
        self.assertLess(len(proc.stdout), 512)

    def test_converter_timeout_terminates_and_reaps_child(self):
        children = []
        def start(*args, **kwargs):
            child = subprocess.Popen(*args, **kwargs)
            children.append(child)
            return child
        self.bad("conversion_failed", lambda: c.run_command(["/bin/sh", "-c", "trap '' TERM; exec sleep 30"], b"", 0.05, popen=start))
        self.assertIsNotNone(children[0].poll())

    def test_converter_output_is_bounded_and_failure_sanitized(self):
        self.bad("conversion_failed", lambda: c.run_command(["/bin/sh", "-c", "head -c 200000 /dev/zero"], b"", 1))
        self.bad("conversion_failed", lambda: c.run_command(["/bin/sh", "-c", "printf SECRET >&2; exit 4"], b"", 1))

    def test_storage_failure_has_incomplete_metadata_and_no_successful_wav(self):
        original = c.PrivateOutput.write
        def fail_wav(output, name, raw):
            if name == "utterance.wav":
                raise OSError("private-path")
            return original(output, name, raw)
        with patch.object(c.PrivateOutput, "write", fail_wav):
            result, _ = self.receive(framed(header(), payload()))
        self.assertEqual(result["reason"], "storage_failed")
        meta = json.loads((self.root / CLIP / "metadata.json").read_text())
        self.assertEqual(meta["status"], "incomplete")
        self.assertFalse((self.root / CLIP / "utterance.wav").exists())


if __name__ == "__main__":
    unittest.main()
