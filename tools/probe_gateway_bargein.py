#!/usr/bin/env python3
"""Bounded synthetic Xiaozhi Opus test: interrupt by voice, never send abort."""

import argparse
import ctypes
import ctypes.util
import json
import pathlib
import threading
import time
import wave

import websocket


def opus_api():
    lib = ctypes.CDLL(ctypes.util.find_library("opus"))
    short_p = ctypes.POINTER(ctypes.c_int16)
    byte_p = ctypes.POINTER(ctypes.c_ubyte)
    lib.opus_encoder_create.argtypes = [ctypes.c_int, ctypes.c_int, ctypes.c_int, ctypes.POINTER(ctypes.c_int)]
    lib.opus_encoder_create.restype = ctypes.c_void_p
    lib.opus_encoder_destroy.argtypes = [ctypes.c_void_p]
    lib.opus_encode.argtypes = [ctypes.c_void_p, short_p, ctypes.c_int, byte_p, ctypes.c_int]
    lib.opus_encode.restype = ctypes.c_int
    lib.opus_decoder_create.argtypes = [ctypes.c_int, ctypes.c_int, ctypes.POINTER(ctypes.c_int)]
    lib.opus_decoder_create.restype = ctypes.c_void_p
    lib.opus_decoder_destroy.argtypes = [ctypes.c_void_p]
    lib.opus_decode.argtypes = [ctypes.c_void_p, byte_p, ctypes.c_int, short_p, ctypes.c_int, ctypes.c_int]
    lib.opus_decode.restype = ctypes.c_int
    return lib


def read_wav(path):
    with wave.open(str(path)) as src:
        if (src.getframerate(), src.getnchannels(), src.getsampwidth()) != (16000, 1, 2):
            raise ValueError("expected mono 16kHz PCM WAV")
        if src.getnframes() > 16000 * 15:
            raise ValueError("test input too long")
        return src.readframes(src.getnframes()) + bytes(32000 * 2)


def run(args):
    cfg = dict(line.split("=", 1) for line in args.config.read_text().splitlines() if line.strip() and not line.startswith("#"))
    lib = opus_api()
    code = ctypes.c_int()
    enc = lib.opus_encoder_create(16000, 1, 2048, ctypes.byref(code))
    if code.value:
        raise RuntimeError("encoder initialization failed")
    dec = None
    ws = websocket.create_connection(
        "ws://127.0.0.1:8000/xiaozhi/v1/", timeout=1, enable_multithread=True, suppress_origin=True,
        header=["Authorization: Bearer " + cfg["ROBOT_DEVICE_TOKEN"], "Device-Id: " + cfg["ROBOT_ALLOWED_DEVICE"], "Protocol-Version: 1"],
    )
    errors = []
    first_done = threading.Event()
    second_started = threading.Event()
    second_done = threading.Event()
    second_start_time = []
    stop_time = None
    frames = [0, 0]
    audio = [bytearray(), bytearray()]
    transcripts = []
    started = time.monotonic()

    def send_audio(pcm, done, second=False):
        try:
            if second:
                second_start_time.append(time.monotonic())
                second_started.set()
            for pos in range(0, len(pcm), 1920):
                samples = (ctypes.c_int16 * 960).from_buffer_copy(pcm[pos:pos + 1920].ljust(1920, b"\0"))
                out = (ctypes.c_ubyte * 4096)()
                n = lib.opus_encode(enc, samples, 960, out, len(out))
                if n <= 0:
                    raise RuntimeError("encoding failed")
                ws.send_binary(bytes(out[:n]))
                time.sleep(0.06)
        except Exception as error:
            errors.append(type(error).__name__)
        finally:
            done.set()

    try:
        args.output.mkdir(mode=0o750)
        ws.send(json.dumps({"type": "hello", "version": 1, "transport": "websocket", "audio_params": {"format": "opus", "sample_rate": 16000, "channels": 1, "frame_duration": 60}}))
        hello = json.loads(ws.recv())
        if hello.get("type") != "hello":
            raise RuntimeError("invalid hello")
        ws.send(json.dumps({"type": "listen", "state": "start", "mode": "realtime", "session_id": hello["session_id"]}))
        first = threading.Thread(target=send_audio, args=(read_wav(args.input), first_done), daemon=True)
        first.start()
        followup = read_wav(args.followup)
        round_index = 0
        playing = False
        sender = None
        while time.monotonic() - started < 70:
            if errors:
                raise RuntimeError("input writer failed")
            try:
                data = ws.recv()
            except websocket.WebSocketTimeoutException:
                continue
            if not data:
                raise RuntimeError("gateway disconnected")
            if isinstance(data, bytes):
                if not playing or dec is None:
                    raise RuntimeError("audio arrived outside playback")
                packet = (ctypes.c_ubyte * len(data)).from_buffer_copy(data)
                pcm = (ctypes.c_int16 * 1920)()
                n = lib.opus_decode(dec, packet, len(data), pcm, 1920, 0)
                if n <= 0:
                    raise RuntimeError("invalid Opus reply")
                audio[round_index].extend(bytes(pcm)[:n * 2])
                frames[round_index] += 1
                if round_index == 0 and frames[0] >= 10 and first_done.is_set() and sender is None:
                    # No listen.start or abort: only new microphone Opus frames.
                    sender = threading.Thread(target=send_audio, args=(followup, second_done, True), daemon=True)
                    sender.start()
                continue
            ev = json.loads(data)
            if ev.get("type") == "alert":
                raise RuntimeError("gateway reported an error")
            if ev.get("type") == "tts" and ev.get("state") == "start":
                if dec:
                    lib.opus_decoder_destroy(dec)
                dec = lib.opus_decoder_create(16000, 1, ctypes.byref(code))
                playing = True
            if ev.get("type") == "tts" and ev.get("state") == "sentence_start":
                transcripts.append(ev.get("text", ""))
            if ev.get("type") == "tts" and ev.get("state") == "stop":
                playing = False
                if round_index == 0:
                    if not second_started.is_set():
                        raise RuntimeError("first reply ended before voice interruption")
                    stop_time = time.monotonic() - second_start_time[0]
                    if stop_time > 2 or frames[0] > 45:
                        raise RuntimeError("reply did not stop promptly on voice interruption")
                    round_index = 1
                else:
                    break
        else:
            raise TimeoutError("barge-in test deadline")
        if not frames[1] or not transcripts or "收到" not in transcripts[-1]:
            raise RuntimeError("follow-up answer was not received")
        if sender:
            sender.join(timeout=8)
            if sender.is_alive():
                raise RuntimeError("input writer did not stop")
        result = {"status": "passed", "test": "native_vad_voice_interruption_without_abort", "audio_frames": frames, "stop_ms_after_followup_audio_started": round(stop_time * 1000, 1), "output_transcripts": transcripts, "total_ms": round((time.monotonic() - started) * 1000, 1)}
        for i, pcm in enumerate(audio):
            with wave.open(str(args.output / f"reply-{i + 1}.wav"), "wb") as target:
                target.setnchannels(1)
                target.setsampwidth(2)
                target.setframerate(16000)
                target.writeframes(pcm)
        (args.output / "result.json").write_text(json.dumps(result, ensure_ascii=False, indent=2))
        print(json.dumps(result, ensure_ascii=False), flush=True)
    finally:
        ws.close()
        first_done.wait(timeout=3)
        second_done.wait(timeout=3) if second_started.is_set() else None
        if dec:
            lib.opus_decoder_destroy(dec)
        lib.opus_encoder_destroy(enc)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--config", type=pathlib.Path, default=pathlib.Path("/etc/family-robot/server.env"))
    parser.add_argument("--input", type=pathlib.Path, required=True)
    parser.add_argument("--followup", type=pathlib.Path, required=True)
    parser.add_argument("--output", type=pathlib.Path, required=True)
    args = parser.parse_args()
    try:
        run(args)
    except Exception as error:
        print(json.dumps({"status": "failed", "error_type": type(error).__name__, "reason": str(error) if isinstance(error, (ValueError, RuntimeError, TimeoutError)) else "test transport failure"}), flush=True)
        raise SystemExit(1)
