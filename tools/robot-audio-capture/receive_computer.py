#!/usr/bin/env python3
"""Cloud-only, single-use native Mac PCM receiver. Never runs a model."""
import argparse
import hashlib
import io
import json
import math
import os
from pathlib import Path
import re
import resource
import selectors
import signal
import stat
import struct
import subprocess
import sys
import time
import wave

from capture import PrivateOutput, CaptureError

# Wire constants are mirrored by computer-voice-capture/State.swift.
MAX_HEADER = 2048
MAX_PCM = 16 * 1024 * 1024
READ_SECONDS = 60
REASONS = frozenset("arguments_invalid output_unsafe header_length header_invalid truncated trailing_data timeout sample_invalid conversion_failed signal storage_failed internal_error".split())
KEYS = frozenset("schema_version source_kind source_device sample_format sample_rate channels frames pcm_bytes".split())


class ReceiveError(Exception):
    def __init__(self, reason):
        self.reason = reason if reason in REASONS else "internal_error"
        super().__init__(self.reason)


def require(ok, reason):
    if not ok:
        raise ReceiveError(reason)


def parse_header(raw):
    def unique(pairs):
        obj = {}
        for key, value in pairs:
            require(key not in obj, "header_invalid")
            obj[key] = value
        return obj
    try:
        h = json.loads(raw, object_pairs_hook=unique, parse_constant=lambda _: None)
        require(type(h) is dict and set(h) == KEYS, "header_invalid")
        require(all(type(h[k]) is int for k in ("schema_version", "sample_rate", "channels", "frames", "pcm_bytes")), "header_invalid")
        require(h["schema_version"] == 1 and h["source_kind"] == "private_human" and h["source_device"] == "mac_microphone" and h["sample_format"] == "f32le_interleaved", "header_invalid")
        require(8000 <= h["sample_rate"] <= 96000 and 1 <= h["channels"] <= 8, "header_invalid")
        require(h["frames"] == h["sample_rate"] * 4 and h["pcm_bytes"] == h["frames"] * h["channels"] * 4 and 0 < h["pcm_bytes"] <= MAX_PCM, "header_invalid")
        return h
    except (ValueError, TypeError, UnicodeError, RecursionError):
        raise ReceiveError("header_invalid") from None


class DeadlineReader:
    def __init__(self, fd, seconds):
        self.fd = fd
        self.deadline = time.monotonic() + seconds
        self.selector = selectors.DefaultSelector()
        self.selector.register(fd, selectors.EVENT_READ)

    def read(self, count):
        left = self.deadline - time.monotonic()
        require(left > 0 and self.selector.select(left), "timeout")
        return os.read(self.fd, count)

    def exact(self, count):
        data = bytearray()
        while len(data) < count:
            chunk = self.read(min(65536, count - len(data)))
            require(chunk, "truncated")
            data.extend(chunk)
        return bytes(data)

    def close(self):
        self.selector.close()


def stop_child(child):
    if child.poll() is None:
        child.terminate()
    try:
        child.wait(timeout=0.5)
    except subprocess.TimeoutExpired:
        child.kill()
        child.wait(timeout=1)


def run_command(args, data, timeout, popen=subprocess.Popen, output_limit=128000):
    """Bound stdin/out/err and total child lifetime; reap on every exit."""
    child = None
    selector = selectors.DefaultSelector()
    output, errors = bytearray(), bytearray()
    offset = 0
    deadline = time.monotonic() + timeout
    try:
        child = popen(args, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env={"PATH": "/usr/bin:/bin", "LC_ALL": "C"}, start_new_session=True)
        for stream in (child.stdin, child.stdout, child.stderr):
            os.set_blocking(stream.fileno(), False)
        if data:
            selector.register(child.stdin, selectors.EVENT_WRITE)
        else:
            child.stdin.close()
        selector.register(child.stdout, selectors.EVENT_READ)
        selector.register(child.stderr, selectors.EVENT_READ)
        while selector.get_map():
            left = deadline - time.monotonic()
            require(left > 0, "conversion_failed")
            for key, _ in selector.select(min(left, 0.2)):
                if key.fileobj is child.stdin:
                    offset += os.write(key.fd, data[offset:offset + 65536])
                    if offset == len(data):
                        selector.unregister(child.stdin); child.stdin.close()
                else:
                    chunk = os.read(key.fd, 65536)
                    if not chunk:
                        selector.unregister(key.fileobj)
                    else:
                        target, limit = (output, output_limit) if key.fileobj is child.stdout else (errors, 65536)
                        require(len(target) + len(chunk) <= limit, "conversion_failed")
                        target.extend(chunk)
        require(child.wait(timeout=max(0.001, deadline - time.monotonic())) == 0, "conversion_failed")
        return bytes(output)
    except (OSError, subprocess.SubprocessError):
        raise ReceiveError("conversion_failed") from None
    finally:
        selector.close()
        if child:
            stop_child(child)
            for stream in (child.stdin, child.stdout, child.stderr):
                stream.close()


def convert(pcm, h):
    raw_version = run_command(["/usr/bin/ffmpeg", "-version"], b"", 3, output_limit=16384).splitlines()[0]
    require(re.fullmatch(rb"ffmpeg version [0-9A-Za-z.+~:_ -]{1,180}(?: Copyright \(c\) [0-9-]{4,9} the FFmpeg developers)?", raw_version), "conversion_failed")
    output = run_command(["/usr/bin/ffmpeg", "-hide_banner", "-loglevel", "error", "-nostdin", "-f", "f32le", "-ar", str(h["sample_rate"]), "-ac", str(h["channels"]), "-i", "pipe:0", "-af", "pan=mono|c0=c0", "-ar", "16000", "-ac", "1", "-f", "s16le", "pipe:1"], pcm, 10)
    require(len(output) == 128000, "conversion_failed")
    return output, raw_version.decode("ascii")


def run_receiver(root, clip, participant, fd, emit, read_timeout=READ_SECONDS, converter=None):
    output = reader = None
    h = pcm = wav = None
    reason = None
    summary = None
    report = dict(schema_version=1, stage="computer_enrollment_candidate", source_kind="private_human", source_device="mac_microphone", source_device_basis="operator_declared_direct_avfoundation", human_acceptance=False, production_acceptance=False, identity_acceptance=False, raw_source_all_channels_retained=False, selected_channel=0, context_saved=False)
    try:
        require(isinstance(clip, str) and re.fullmatch(r"[0-9a-f]{32}", clip) and isinstance(participant, str) and re.fullmatch(r"M0[1-5]", participant), "arguments_invalid")
        require(isinstance(root, str) and Path(root).is_absolute() and ".." not in Path(root).parts and "\0" not in root and "\n" not in root and "\r" not in root, "output_unsafe")
        try:
            root_stat = os.stat(root, follow_symlinks=False)
            require(stat.S_ISDIR(root_stat.st_mode) and stat.S_IMODE(root_stat.st_mode) == 0o700 and root_stat.st_uid == os.geteuid(), "output_unsafe")
        except OSError:
            raise ReceiveError("output_unsafe") from None
        output = PrivateOutput(str(Path(root) / clip))
        report.update(clip=clip, participant=participant)
        emit(dict(schema_version=1, event="ready", clip=clip))
        reader = DeadlineReader(fd, min(read_timeout, READ_SECONDS))
        size = struct.unpack("!I", reader.exact(4))[0]
        require(0 < size <= MAX_HEADER, "header_length")
        h = parse_header(reader.exact(size))
        pcm = reader.exact(h["pcm_bytes"])
        output.write("source.f32le", pcm)
        report.update(source_format=h, source_bytes=len(pcm), source_sha256=hashlib.sha256(pcm).hexdigest(), raw_source_all_channels_retained=True)
        require(reader.read(1) == b"", "trailing_data")
        count = 0
        source_square = source_peak = 0.0
        for (value,) in struct.iter_unpack("<f", pcm):
            require(math.isfinite(value) and abs(value) <= 16, "sample_invalid")
            source_square += value * value
            source_peak = max(source_peak, abs(value))
            count += abs(value) >= 1
        report.update(source_rms=math.sqrt(source_square / (len(pcm) // 4)), source_peak=source_peak, source_clipped_samples=count)
        try:
            canonical, version = (converter or convert)(pcm, h)
            require(isinstance(canonical, bytes) and len(canonical) == 128000 and isinstance(version, str), "conversion_failed")
        except (OSError, ValueError, TypeError, subprocess.SubprocessError):
            raise ReceiveError("conversion_failed") from None
        square = peak = clipped = 0
        for (sample,) in struct.iter_unpack("<h", canonical):
            square += sample * sample
            peak = max(peak, abs(sample))
            clipped += sample in {-32768, 32767}
        rms = math.sqrt(square / 64000) / 32768
        buffer = io.BytesIO()
        with wave.open(buffer, "wb") as f:
            f.setnchannels(1); f.setsampwidth(2); f.setframerate(16000); f.writeframes(canonical)
        wav = buffer.getvalue()
        require(len(wav) == 128044, "conversion_failed")
        report.update(ffmpeg_version=version, wav_format=dict(sample_rate=16000, channels=1, sample_format="s16le", samples=64000), samples=64000, duration_seconds=4, wav_bytes=len(wav), wav_sha256=hashlib.sha256(wav).hexdigest(), rms=rms, peak=peak / 32768, clipped_samples=clipped)
        report.update(status="complete", reason=None)
        output.write("utterance.wav", wav)
        output.write("metadata.json", (json.dumps(report, sort_keys=True, indent=2) + "\n").encode())
        os.fsync(output.fd)
        summary = dict(schema_version=1, event="stored", clip=clip, samples=64000, duration_seconds=4, rms=rms)
    except ReceiveError as error:
        reason = error.reason
    except CaptureError:
        reason = "output_unsafe"
    except (OSError, ValueError, TypeError, OverflowError, struct.error):
        reason = "storage_failed"
    except Exception:
        reason = "internal_error"
    finally:
        if reader:
            reader.close()
        if reason:
            summary = dict(schema_version=1, event="incomplete", clip=clip if isinstance(clip, str) and re.fullmatch(r"[0-9a-f]{32}", clip) else None, reason=reason)
            if output and output.fd is not None:
                report.update(status="incomplete", reason=reason, wav_bytes=0, wav_sha256=None)
                try:
                    # This directory is exclusively owned by this invocation;
                    # delete only its fixed successful-output names on failure.
                    for name in ("utterance.wav", "metadata.json"):
                        try:
                            os.unlink(name, dir_fd=output.fd)
                        except FileNotFoundError:
                            pass
                    output.write("metadata.json", (json.dumps(report, sort_keys=True, indent=2) + "\n").encode())
                except OSError:
                    pass
        if output and output.fd is not None:
            os.close(output.fd); output.fd = None
    emit(summary)
    return summary


def main(argv=None):
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", required=True)
    parser.add_argument("--clip", required=True)
    parser.add_argument("--participant", required=True)
    args = parser.parse_args(argv)
    def interrupted(signum, frame):
        for sig in (signal.SIGTERM, signal.SIGHUP):
            signal.signal(sig, signal.SIG_IGN)
        raise ReceiveError("signal")
    for sig in (signal.SIGTERM, signal.SIGHUP):
        signal.signal(sig, interrupted)
    signal.signal(signal.SIGPIPE, signal.SIG_IGN)
    def emit(value):
        try:
            print(json.dumps(value, allow_nan=False, separators=(",", ":")), flush=True)
        except BrokenPipeError:
            pass
    result = run_receiver(args.root, args.clip, args.participant, sys.stdin.fileno(), emit)
    return 0 if result["event"] == "stored" else 2


if __name__ == "__main__":
    sys.exit(main())
