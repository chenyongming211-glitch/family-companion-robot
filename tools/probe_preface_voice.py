#!/usr/bin/env python3
"""Prove the pool is the same speaker as the answer, without listening.

The pool is only usable if a listener cannot tell it came from a different mouth
than the reply that follows. The first version of this script compared a full
sentence against the single syllable "嗯" and reported a 58Hz F0 gap -- which
measured the text, not the speaker: a nasal hum has its energy in the low
hundreds of Hz and no consonant noise, a whole sentence has both. Comparing
different utterances cannot answer a question about voices.

This version controls the text. One sentence, spoken twice on the same
connection, once as the assistant (how production speaks) and once as a
repeater (how the pool is made). If the voice changes with the role, these two
diverge. If they match, the role is not a variable, and the pool differs from
the answer only in what it says -- exactly as one person differs from themselves
across two sentences.

Also reports the level production actually speaks at, so the pool can be
normalised to it instead of to full scale. Matching peak but not loudness is how
the same voice ends up sounding like a louder stranger.

  answer    -- assistant role, sentence S
  repeated  -- repeater role, sentence S
  pooled    -- a shipped pool file (different words, checked for level only)

Usage:
  PYTHONPATH=/opt/family-robot/probe-libs python3 tools/probe_preface_voice.py \
      --env /etc/family-robot/server.env --pool /tmp/preface-out/a01.pcm
"""
import argparse
import base64
import json
import re

import numpy as np
import websocket

RATE = 24000
SENTENCE = "今天天气挺好的，咱们出去走走吧。"


def load_env(path):
    env = {}
    with open(path) as fh:
        for line in fh:
            m = re.match(r"^([A-Z_]+)=(.*)$", line.strip())
            if m:
                env[m.group(1)] = m.group(2).strip().strip('"').strip("'")
    return env


def connect(url, key, voice, instructions):
    ws = websocket.create_connection(url, header={"Authorization": "Bearer " + key}, timeout=30)
    ws.send(json.dumps({"type": "session.update", "session": {
        "modalities": ["text", "audio"], "instructions": instructions,
        "turn_detection": None, "input_audio_transcription": None,
        "audio": {
            "input": {"format": {"type": "pcm", "sample_rate": 16000, "sample_format": "s16le",
                                 "channels": 1, "packing": "interleaved", "channel_layout": "mono"}},
            "output": {"voice": voice, "format": {"type": "pcm", "sample_rate": RATE}},
        }}}))
    while True:
        event = json.loads(ws.recv())
        if event.get("type") == "session.updated":
            return ws
        if event.get("type") == "error":
            raise SystemExit("session.update rejected: %s" % json.dumps(event)[:300])


def turn(ws, text):
    ws.send(json.dumps({"type": "conversation.item.create", "item": {
        "type": "message", "role": "user", "content": [{"type": "input_text", "text": text}]}}))
    ws.send(json.dumps({"type": "response.create"}))
    chunks, said = [], ""
    while True:
        event = json.loads(ws.recv())
        kind = event.get("type", "")
        if kind == "response.audio.delta":
            chunks.append(base64.b64decode(event.get("delta") or ""))
        elif kind == "response.audio_transcript.done":
            said = event.get("transcript") or ""
        elif kind == "response.done":
            break
        elif kind == "error":
            raise SystemExit("turn failed: %s" % json.dumps(event)[:300])
    return b"".join(chunks), said


def analyse(pcm):
    """F0 spread, spectral centroid and level over voiced frames only."""
    x = np.frombuffer(pcm, dtype="<i2").astype(np.float32)
    if x.size < RATE // 20:
        return None
    n, hop = 600, 240  # 25ms window, 10ms hop
    frames = [x[i:i + n] for i in range(0, x.size - n, hop)]
    rms = np.array([np.sqrt(np.mean(f ** 2)) for f in frames])
    if rms.size == 0 or np.max(rms) <= 0:
        return None
    loud = rms > 0.15 * np.max(rms)
    frames = [f for f, k in zip(frames, loud) if k]
    if len(frames) < 5:
        return None

    f0s, cents, levels = [], [], []
    for f in frames:
        f = f - np.mean(f)
        if not np.any(f):
            continue
        corr = np.correlate(f, f, mode="full")[len(f) - 1:]
        lo, hi = RATE // 400, min(len(corr) - 1, RATE // 70)
        if hi <= lo:
            continue
        lag = lo + int(np.argmax(corr[lo:hi]))
        if corr[lag] > 0.35 * corr[0]:
            f0s.append(RATE / lag)
            levels.append(20 * np.log10(max(1e-9, np.sqrt(np.mean(f ** 2)) / 32768.0)))
        spec = np.abs(np.fft.rfft(f * np.hanning(len(f))))
        freqs = np.fft.rfftfreq(len(f), 1 / RATE)
        if spec.sum() > 0:
            cents.append(float(np.sum(freqs * spec) / np.sum(spec)))
    if len(f0s) < 5:
        return None
    peak = 20 * np.log10(max(1e-9, np.max(np.abs(x)) / 32768.0))
    return {
        "f0_median": round(float(np.median(f0s)), 1),
        "f0_p10": round(float(np.percentile(f0s, 10)), 1),
        "f0_p90": round(float(np.percentile(f0s, 90)), 1),
        "centroid_hz": round(float(np.median(cents)), 1),
        "peak_dbfs": round(float(peak), 1),
        "voiced_rms_dbfs": round(float(np.median(levels)), 1),
        "voiced_frames": len(f0s),
    }


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--env", default="/etc/family-robot/server.env")
    ap.add_argument("--pool", default="", help="a shipped clip, e.g. /var/lib/.../preface/Tina/a01.pcm")
    args = ap.parse_args()

    env = load_env(args.env)
    url, key = env.get("QWEN_REALTIME_URL", ""), env.get("DASHSCOPE_API_KEY", "")
    voice = env.get("QWEN_VOICE", "Tina")

    ws = connect(url, key, voice,
                 "你是友善、耐心的中文家庭语音助手。自然简洁地交流，每次先用一到两句话回答。"
                 "用户让你说什么，你就原样说那句话，不要改字。")
    answer, answer_text = turn(ws, SENTENCE)
    ws.close()

    ws = connect(url, key, voice,
                 "你是一台复读机。用户给你一句话，你只用同样的语气把这句话原样念一遍，"
                 "一个字都不加，一个字都不减，不要回应它的内容，不要解释，不要加语气词。")
    repeated, said = turn(ws, "念：%s" % SENTENCE)
    ws.close()

    rows = [("answer", answer, answer_text), ("repeated", repeated, said)]
    if args.pool:
        with open(args.pool, "rb") as fh:
            rows.append(("pooled", fh.read(), ""))
    out = {}
    for name, pcm, text in rows:
        out[name] = analyse(pcm) or {"error": "no_voiced_frames", "bytes": len(pcm)}
        print("%-9s %s  %s" % (name, json.dumps(out[name], ensure_ascii=False), text[:36]))

    a, r = out["answer"], out["repeated"]
    if "error" not in a and "error" not in r:
        print("\nsame sentence, two roles -- if the role changed the speaker, these diverge:")
        print("  F0 median    %+6.1f Hz  (%+.1f%%)" % (
            r["f0_median"] - a["f0_median"],
            100 * (r["f0_median"] - a["f0_median"]) / a["f0_median"]))
        print("  F0 range     p10-p90 answer %.0f-%.0f, repeated %.0f-%.0f" % (
            a["f0_p10"], a["f0_p90"], r["f0_p10"], r["f0_p90"]))
        print("  centroid     %+6.1f Hz" % (r["centroid_hz"] - a["centroid_hz"]))
        print("  loudness     %+6.1f dB  (voiced RMS)" % (r["voiced_rms_dbfs"] - a["voiced_rms_dbfs"]))
        print("\nproduction level to match: peak %.1f dBFS, voiced RMS %.1f dBFS" % (
            a["peak_dbfs"], a["voiced_rms_dbfs"]))
    print("\n" + json.dumps(out, ensure_ascii=False))


if __name__ == "__main__":
    main()
