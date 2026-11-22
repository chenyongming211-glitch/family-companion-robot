#!/usr/bin/env python3
"""Same session, same prompt, same question: text input versus audio input.

Production submits pcm and gets its first packet around 1100ms. A text turn on the
same provider answers in about 600ms at a short prompt, so this isolates how much
of the difference belongs to the audio path rather than to the model.
"""
import base64, json, re, statistics, time
import websocket

env = {}
for line in open("/etc/family-robot/realtime-test.env"):
    m = re.match(r"^([A-Z_]+)=(.*)$", line.strip())
    if m:
        env[m.group(1)] = m.group(2).strip('"').strip("'")
URL, KEY, VOICE = env["QWEN_REALTIME_URL"], env["DASHSCOPE_API_KEY"], env.get("QWEN_VOICE", "Tina")
PCM = open("/tmp/probe.pcm", "rb").read()
PROMPT = "你是一个家庭语音陪伴助手，说话简洁口语化。" + "家里有老人和孩子，说话要温和、具体。" * 40

def settings():
    return {"type": "session.update", "session": {
        "modalities": ["text", "audio"], "instructions": PROMPT,
        "enable_search": False, "turn_detection": None, "input_audio_transcription": None,
        "audio": {"input": {"format": {"type": "pcm", "sample_rate": 16000, "sample_format": "s16le",
                                        "channels": 1, "packing": "interleaved", "channel_layout": "mono"}},
                  "output": {"voice": VOICE, "format": {"type": "pcm", "sample_rate": 24000}}}}}

def connect():
    ws = websocket.create_connection(URL, header={"Authorization": "Bearer " + KEY}, timeout=30)
    ws.send(json.dumps(settings()))
    while True:
        e = json.loads(ws.recv())
        if e.get("type") == "session.updated":
            return ws
        if e.get("type") == "error":
            raise SystemExit(json.dumps(e)[:300])

def wait_first(ws, started, label):
    ttft, tokens = None, None
    while True:
        e = json.loads(ws.recv())
        k = e.get("type", "")
        if k == "response.audio.delta" and ttft is None:
            ttft = (time.time() - started) * 1000
        elif k == "response.done":
            tokens = ((e.get("response") or {}).get("usage") or {}).get("input_tokens")
            return ttft, tokens
        elif k == "error":
            raise SystemExit("%s failed: %s" % (label, json.dumps(e)[:300]))

def text_turn(text):
    ws = connect()
    try:
        ws.send(json.dumps({"type": "conversation.item.create", "item": {"type": "message", "role": "user",
                  "content": [{"type": "input_text", "text": text}]}}))
        started = time.time()
        ws.send(json.dumps({"type": "response.create"}))
        return wait_first(ws, started, "text")
    finally:
        ws.close()

def audio_turn(pcm):
    ws = connect()
    try:
        started = time.time()
        for i in range(0, len(pcm), 3200):
            chunk = pcm[i:i + 3200]
            ws.send(json.dumps({"type": "input_audio_buffer.append",
                                "audio": base64.b64encode(chunk).decode()}))
        append_done = time.time()
        ws.send(json.dumps({"type": "input_audio_buffer.commit"}))
        ws.send(json.dumps({"type": "response.create"}))
        ttft, tokens = wait_first(ws, started, "audio")
        return ttft, tokens, (append_done - started) * 1000
    finally:
        ws.close()

print("prompt chars=%d (about %d tokens)" % (len(PROMPT), len(PROMPT) // 2))
print("audio pcm bytes=%d (%.0f ms at 16k mono)" % (len(PCM), len(PCM) / 32))

texts, audios, uploads = [], [], []
for _ in range(3):
    t, _tok = text_turn("明天杭州天气怎么样")
    texts.append(t)
for _ in range(3):
    a, _tok, up = audio_turn(PCM)
    audios.append(a)
    uploads.append(up)

print("text  ttft: %s -> median %.0f ms" % (["%.0f" % x for x in texts], statistics.median(texts)))
print("audio ttft: %s -> median %.0f ms" % (["%.0f" % x for x in audios], statistics.median(audios)))
print("audio upload part: %s -> median %.0f ms" % (["%.0f" % x for x in uploads], statistics.median(uploads)))
print("")
print("audio path costs %+.0f ms over text at equal prompt" % (statistics.median(audios) - statistics.median(texts)))
