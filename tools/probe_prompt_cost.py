#!/usr/bin/env python3
"""How much of the first-packet delay is prefill of the system prompt?

Production sends about 3200 tokens of identity and rules before any audio comes
back. This measures TTFT against four prompt sizes on fresh sessions, so the cost
per token is measured rather than assumed.
"""
import json, re, statistics, time
import websocket

env = {}
for line in open("/etc/family-robot/realtime-test.env"):
    m = re.match(r"^([A-Z_]+)=(.*)$", line.strip())
    if m:
        env[m.group(1)] = m.group(2).strip('"').strip("'")
URL, KEY, VOICE = env["QWEN_REALTIME_URL"], env["DASHSCOPE_API_KEY"], env.get("QWEN_VOICE", "Tina")

BASE = "你是一个家庭语音陪伴助手，说话简洁口语化。回答控制在两句话以内。"
FILLER = "家里有老人和孩子，说话要温和、具体、不要夸张，遇到不确定的事情要说清楚自己不确定。"

def instructions(units):
    return BASE + (FILLER * units)

def one_turn(units):
    ws = websocket.create_connection(URL, header={"Authorization": "Bearer " + KEY}, timeout=30)
    try:
        ws.send(json.dumps({"type": "session.update", "session": {
            "modalities": ["text", "audio"], "instructions": instructions(units),
            "enable_search": False, "turn_detection": None, "input_audio_transcription": None,
            "audio": {"input": {"format": {"type": "pcm", "sample_rate": 16000, "sample_format": "s16le",
                                            "channels": 1, "packing": "interleaved", "channel_layout": "mono"}},
                      "output": {"voice": VOICE, "format": {"type": "pcm", "sample_rate": 24000}}}}}))
        while True:
            e = json.loads(ws.recv())
            if e.get("type") == "session.updated":
                break
            if e.get("type") == "error":
                raise SystemExit(json.dumps(e)[:300])
        ws.send(json.dumps({"type": "conversation.item.create", "item": {"type": "message", "role": "user",
                  "content": [{"type": "input_text", "text": "用一句话说说你今天怎么样。"}]}}))
        ws.send(json.dumps({"type": "response.create"}))
        started = time.time()
        ttft = None
        tokens = None
        while True:
            e = json.loads(ws.recv())
            if e.get("type") == "response.audio.delta" and ttft is None:
                ttft = (time.time() - started) * 1000
            elif e.get("type") == "response.done":
                tokens = ((e.get("response") or {}).get("usage") or {}).get("input_tokens")
                break
        return ttft, tokens
    finally:
        ws.close()

print("units  chars  input_tokens  ttft_ms (3 runs)")
rows = []
for units in (1, 15, 48, 95):
    runs = []
    tokens = None
    for _ in range(3):
        ttft, tokens = one_turn(units)
        runs.append(ttft)
    rows.append((units, len(instructions(units)), tokens, statistics.median(runs)))
    print("%5d  %5d  %12s  %s -> median %.0f" % (units, len(instructions(units)), tokens,
          ["%.0f" % r for r in runs], statistics.median(runs)))

if len(rows) >= 2:
    (u1, c1, t1, m1), (u2, c2, t2, m2) = rows[0], rows[-1]
    if t1 and t2 and t2 > t1:
        print("\nper 1000 tokens: %.0f ms" % ((m2 - m1) / (t2 - t1) * 1000))
        print("production prompt ~3200 tokens would cost about %.0f ms of prefill"
              % ((m2 - m1) / (t2 - t1) * 3200))
