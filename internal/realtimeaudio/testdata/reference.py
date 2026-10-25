"""Test-only differential oracle; production Go never invokes Python."""
import audioop
import base64
import ctypes
import importlib.util
import json
import os
import sys


def encoded(chunks):
    return [base64.b64encode(chunk).decode("ascii") for chunk in chunks]


request = json.load(sys.stdin)
chunks = [base64.b64decode(chunk) if chunk is not None else b"" for chunk in request["chunks"]]
if request["operation"] == "resample":
    state, result = None, []
    for chunk in chunks:
        converted, state = audioop.ratecv(chunk, 2, 1, 24000, 16000, state)
        result.append(converted)
    json.dump({"converted": encoded(result)}, sys.stdout)
else:
    # Read the unchanged baseline. The cloud validation explicitly supplies its
    # frozen checkout; do not copy baseline modules into the running service.
    path = os.environ.get("REALTIME_AUDIO_PY", "../../tools/realtime-output-live/audio.py")
    spec = importlib.util.spec_from_file_location("baseline_audio", path)
    baseline = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(baseline)
    codec = baseline.Opus(request["rate"], request["frame"])
    try:
        lookahead = ctypes.c_int()
        codec.lib.opus_encoder_ctl.argtypes = [ctypes.c_void_p, ctypes.c_int, ctypes.POINTER(ctypes.c_int)]
        if codec.lib.opus_encoder_ctl(codec.encoder, 4027, ctypes.byref(lookahead)) != 0:
            raise RuntimeError("lookahead query failed")
        codec.lib.opus_get_version_string.restype = ctypes.c_char_p
        if request["operation"] == "tail":
            pending, total, frames = bytearray(), 0, []
            for chunk in chunks:
                pending.extend(chunk)
                total += len(chunk) // 2
                while len(pending) >= 2880:
                    frames.append(bytes(pending[:2880]))
                    del pending[:2880]
            # This is the frozen server.py producer's tail formula, including
            # its behavior for zero input; never substitute queried lookahead.
            target = (total + 312 + 1439) // 1440
            while len(frames) < target:
                frames.append(bytes(pending[:2880]).ljust(2880, b"\0"))
                del pending[:2880]
            chunks = frames
        packets = [codec.encode(chunk) for chunk in chunks]
        decoded = [codec.decode(packet) for packet in packets]
        json.dump({"packets": encoded(packets), "decoded": encoded(decoded),
                   "lookahead": lookahead.value,
                   "version": codec.lib.opus_get_version_string().decode("ascii")}, sys.stdout)
    finally:
        codec.close()
