#!/usr/bin/env python3
"""Run one bounded audio round trip without logging API credentials."""

import argparse
import base64
import collections
import json
import pathlib
import re
import time
import wave

import websocket


def run(args):
    config = dict(
        line.split("=", 1)
        for line in args.config.read_text().splitlines()
        if line.strip() and not line.lstrip().startswith("#")
    )
    key = config["DASHSCOPE_API_KEY"].strip()
    url = config["QWEN_REALTIME_URL"].strip()
    if not key or not url.startswith("wss://"):
        raise ValueError("Missing API key or secure realtime URL")
    with wave.open(str(args.input), "rb") as source:
        if (
            source.getnchannels(),
            source.getsampwidth(),
            source.getframerate(),
            source.getcomptype(),
        ) != (1, 2, 16000, "NONE"):
            raise ValueError("Input must be mono 16 kHz, 16-bit PCM WAV")
        seconds = source.getnframes() / 16000
        if not 0 < seconds <= 30:
            raise ValueError("Input must contain 0-30 seconds of audio")
        pcm = source.readframes(source.getnframes())

    args.output.mkdir(parents=True, exist_ok=False, mode=0o750)
    counts = collections.Counter()
    started = time.monotonic()
    deadline = started + 60
    websocket.enableTrace(False)
    ws = websocket.create_connection(
        url, header=["Authorization: Bearer " + key], timeout=15
    )
    result = {
        "model": "qwen3.8-omni-flash-realtime",
        "test": "synthetic_audio_input_to_audio_output",
        "input_seconds": round(seconds, 3),
        "connect_ms": round((time.monotonic() - started) * 1000, 1),
    }

    def send(event):
        ws.send(json.dumps(event, ensure_ascii=False))

    def receive():
        while time.monotonic() < deadline:
            ws.settimeout(min(5, max(0.1, deadline - time.monotonic())))
            try:
                data = ws.recv()
            except websocket.WebSocketTimeoutException:
                continue
            if not data:
                raise RuntimeError("Connection closed before test completion")
            event = json.loads(data)
            counts[event.get("type", "unknown")] += 1
            if event.get("type") == "error":
                raise RuntimeError(json.dumps(event.get("error", {}), ensure_ascii=False))
            if event.get("type") == "conversation.item.input_audio_transcription.completed":
                result["input_transcript"] = event.get("transcript", "")
            return event
        raise TimeoutError("Probe exceeded its 60-second deadline")

    def wait_for(event_type):
        while True:
            event = receive()
            if event.get("type") == event_type:
                return event

    try:
        wait_for("session.created")
        send({
            "type": "session.update",
            "session": {
                "modalities": ["text", "audio"],
                "instructions": "你是家庭助理语音连接测试助手，按用户要求回答一句简短中文。",
                "turn_detection": None,
                "input_audio_transcription": {"model": "qwen3-asr-flash-realtime"},
                "audio": {
                    "input": {"format": {
                        "type": "pcm", "sample_rate": 16000,
                        "sample_format": "s16le", "channels": 1,
                        "packing": "interleaved", "channel_layout": "mono",
                    }},
                    "output": {
                        "voice": "Tina",
                        "format": {"type": "pcm", "sample_rate": 24000},
                    },
                },
            },
        })
        wait_for("session.updated")
        for offset in range(0, len(pcm), 3200):
            send({
                "type": "input_audio_buffer.append",
                "audio": base64.b64encode(pcm[offset:offset + 3200]).decode("ascii"),
            })
        send({"type": "input_audio_buffer.commit"})
        wait_for("input_audio_buffer.committed")
        submitted = time.monotonic()
        send({"type": "response.create"})
        audio = bytearray()
        text = []
        first_audio = None
        while True:
            event = receive()
            kind = event.get("type")
            if kind == "response.audio.delta":
                if first_audio is None:
                    first_audio = time.monotonic()
                audio.extend(base64.b64decode(event["delta"], validate=True))
            elif kind == "response.audio_transcript.delta":
                text.append(event.get("delta", ""))
            elif kind == "response.audio_transcript.done":
                result["output_transcript"] = event.get("transcript", "")
            elif kind == "conversation.item.input_audio_transcription.completed":
                result["input_transcript"] = event.get("transcript", "")
            elif kind == "response.done":
                response = event.get("response", {})
                result["status"] = response.get("status")
                result["usage"] = response.get("usage")
                if result["status"] != "completed":
                    raise RuntimeError("Incomplete response: " + json.dumps(response.get("status_details")))
                break
        if not audio or len(audio) % 2:
            raise RuntimeError("Response did not contain valid PCM audio")
        result.setdefault("output_transcript", "".join(text))
        result["first_audio_ms_after_request"] = round((first_audio - submitted) * 1000, 1)
        result["response_ms"] = round((time.monotonic() - submitted) * 1000, 1)
        result["output_seconds"] = round(len(audio) / 48000, 3)
        result["audio_bytes"] = len(audio)
        result["events"] = dict(counts)
        with wave.open(str(args.output / "reply.wav"), "wb") as target:
            target.setnchannels(1)
            target.setsampwidth(2)
            target.setframerate(24000)
            target.writeframes(audio)
        (args.output / "result.json").write_text(json.dumps(result, ensure_ascii=False, indent=2))
        print(json.dumps(result, ensure_ascii=False), flush=True)
    finally:
        ws.close(timeout=2)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--config", type=pathlib.Path, default=pathlib.Path("/etc/family-robot/server.env"))
    parser.add_argument("--input", type=pathlib.Path, required=True)
    parser.add_argument("--output", type=pathlib.Path, required=True)
    args = parser.parse_args()
    try:
        run(args)
    except Exception as error:
        safe = re.sub(r"sk-[A-Za-z0-9._-]+", "[REDACTED]", str(error))
        print(json.dumps({"status": "failed", "error": safe}, ensure_ascii=False), flush=True)
        raise SystemExit(1)
