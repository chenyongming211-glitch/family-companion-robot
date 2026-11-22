#!/usr/bin/env python3
"""Measure TTFT drift on a persistent realtime session versus a fresh one.

Two groups, same ten questions, text input so the only variable is the session:

  A. one connection, ten turns on it   -> does TTFT grow with the context?
  B. ten connections, one turn each    -> what production does today

The prompt is short on purpose. TTFT drift comes from prefill cost, and a short
base prompt exaggerates the relative effect rather than hiding it: if ten turns
of history do not move TTFT here, a longer prompt will not either. Absolute
numbers are optimistic against production's ~3200 token identity prompt; the
comparison between A and B is what this is for.
"""
import base64
import json
import os
import re
import statistics
import time

import websocket

env = {}
with open("/etc/family-robot/realtime-test.env") as fh:
    for line in fh:
        m = re.match(r"^([A-Z_]+)=(.*)$", line.strip())
        if m:
            env[m.group(1)] = m.group(2).strip('"').strip("'")

URL = env.get("QWEN_REALTIME_URL", "")
KEY = env.get("DASHSCOPE_API_KEY", "")
VOICE = env.get("QWEN_VOICE", "Tina")
if not URL or not KEY:
    raise SystemExit("missing QWEN_REALTIME_URL or DASHSCOPE_API_KEY")

INSTRUCTIONS = (
    "你是一个家庭语音陪伴助手，说话简洁口语化。回答控制在两句话以内，不要说废话。"
)

QUESTIONS = [
    "你叫什么名字？",
    "讲一个很短的故事。",
    "长江流经哪几个省？",
    "珠穆朗玛峰有多高？",
    "为什么天是蓝色的？",
    "推荐一部适合全家看的电影。",
    "人为什么会做梦？",
    "茶叶是怎么做的？",
    "地球到月亮有多远？",
    "说一句鼓励人的话。",
]


def session_update():
    return {
        "type": "session.update",
        "session": {
            "modalities": ["text", "audio"],
            "instructions": INSTRUCTIONS,
            "enable_search": False,
            "turn_detection": None,
            "input_audio_transcription": None,
            "audio": {
                "input": {"format": {"type": "pcm", "sample_rate": 16000,
                                     "sample_format": "s16le", "channels": 1,
                                     "packing": "interleaved", "channel_layout": "mono"}},
                "output": {"voice": VOICE,
                           "format": {"type": "pcm", "sample_rate": 24000}},
            },
        },
    }


def connect():
    ws = websocket.create_connection(
        URL, header={"Authorization": "Bearer " + KEY}, timeout=30
    )
    ws.send(json.dumps(session_update()))
    while True:
        event = json.loads(ws.recv())
        if event.get("type") in ("session.updated", "session.created"):
            if event.get("type") == "session.updated":
                return ws
        elif event.get("type") == "error":
            raise SystemExit("session.update rejected: %s" % json.dumps(event)[:300])


def ask(ws, text):
    """Send one turn and return (ttft_ms, input_tokens, output_chars)."""
    ws.send(json.dumps({
        "type": "conversation.item.create",
        "item": {"type": "message", "role": "user",
                 "content": [{"type": "input_text", "text": text}]},
    }))
    ws.send(json.dumps({"type": "response.create"}))
    started = time.time()
    ttft = None
    tokens = None
    chars = 0
    while True:
        event = json.loads(ws.recv())
        kind = event.get("type", "")
        if kind == "response.audio.delta" and ttft is None:
            ttft = (time.time() - started) * 1000
        elif kind == "response.audio_transcript.delta":
            chars += len(event.get("delta") or "")
        elif kind == "response.done":
            usage = (event.get("response") or {}).get("usage") or {}
            tokens = usage.get("input_tokens")
            break
        elif kind == "error":
            raise SystemExit("turn failed: %s" % json.dumps(event)[:300])
    return ttft, tokens, chars


def group_persistent():
    rows = []
    ws = connect()
    try:
        for q in QUESTIONS:
            ttft, tokens, chars = ask(ws, q)
            rows.append((ttft, tokens, chars))
    finally:
        ws.close()
    return rows


def group_fresh():
    rows = []
    for q in QUESTIONS:
        ws = connect()
        try:
            ttft, tokens, chars = ask(ws, q)
            rows.append((ttft, tokens, chars))
        finally:
            ws.close()
    return rows


def report(name, rows):
    print("\n== %s ==" % name)
    print("  turn  ttft_ms  input_tokens  out_chars")
    for i, (ttft, tokens, chars) in enumerate(rows, 1):
        print("  %4d  %7.0f  %12s  %9d" % (i, ttft or -1, tokens, chars))
    ttfts = [r[0] for r in rows if r[0]]
    toks = [r[1] for r in rows if r[1]]
    if ttfts:
        print("  ttft: first=%.0f last=%.0f median=%.0f max=%.0f"
              % (ttfts[0], ttfts[-1], statistics.median(ttfts), max(ttfts)))
    if toks:
        print("  tokens: first=%d last=%d growth=+%d"
              % (toks[0], toks[-1], toks[-1] - toks[0]))
    return ttfts, toks


a_ttft, a_tok = report("A persistent session, 10 turns", group_persistent())
b_ttft, b_tok = report("B fresh session per turn, 10 turns", group_fresh())

print("\n== verdict ==")
if a_ttft and b_ttft:
    drift = a_ttft[-1] - a_ttft[0]
    saved = statistics.median(b_ttft) - statistics.median(a_ttft)
    print("  A drift first->last: %+.0f ms" % drift)
    print("  median B - median A: %+.0f ms (positive means persistent is faster)" % saved)
    print("  A tokens grew by %+d over 9 turns" % (a_tok[-1] - a_tok[0]) if a_tok else "")
    if drift > 300:
        print("  VERDICT: drift is real, a persistent session needs a rebuild policy")
    else:
        print("  VERDICT: no meaningful drift at this prompt size")
