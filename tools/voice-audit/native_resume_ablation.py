"""Native provider history ablation with synthetic facts; no device or audio sink."""

import argparse, asyncio, base64, json, os, pathlib, re, shlex, time, wave
import aiohttp

DEFAULT = "你是友善、耐心的中文家庭语音助手。自然简洁地交流，每次先用一到三句话回答。用户插话时立即让出话轮，根据新的问题继续交流。不要声称完成了你没有执行的操作。"


async def case(http, env, plan, name, history, trial):
    start = time.monotonic()
    events = []
    text = []
    calls = []
    audio_bytes = 0
    transcripts = []
    result = {
        "variant": name,
        "trial": trial,
        "seed": 20261023 + trial,
        "synthetic_history_replay": True,
        "heard_prefix_is_oracle_fixture": name == "synthetic_oracle",
        "audio_saved": False,
        "audio_played": False,
        "history": history,
    }
    async with http.ws_connect(
        env["QWEN_REALTIME_URL"],
        headers={"Authorization": "Bearer " + env["DASHSCOPE_API_KEY"]},
        max_msg_size=2 << 20,
        timeout=aiohttp.ClientWSTimeout(ws_receive=35),
    ) as ws:

        async def read():
            msg = await ws.receive()
            if msg.type != aiohttp.WSMsgType.TEXT:
                raise RuntimeError("closed")
            ev = json.loads(msg.data)
            typ = ev.get("type", "unknown")
            events.append({"ms": round((time.monotonic() - start) * 1000), "type": typ})
            if typ == "error":
                raise RuntimeError("provider_rejected_protocol")
            return ev

        assert (await read())["type"] == "session.created"
        instructions = (
            env.get("ROBOT_INSTRUCTIONS", DEFAULT)
            + plan["no_device_controls"]
            + plan["task_instructions"]
            + plan["fulfillment"]
            + plan["history_header"]
            + json.dumps(history, ensure_ascii=False)
        )
        await ws.send_json(
            {
                "type": "session.update",
                "session": {
                    "modalities": ["text", "audio"],
                    "instructions": instructions,
                    "turn_detection": None,
                    "input_audio_transcription": {"model": "qwen3-asr-flash-realtime"},
                    "seed": 20261023 + trial,
                    "tools": plan["tools"],
                    "audio": {
                        "input": {
                            "format": {
                                "type": "pcm",
                                "sample_rate": 16000,
                                "sample_format": "s16le",
                                "channels": 1,
                                "packing": "interleaved",
                                "channel_layout": "mono",
                            }
                        },
                        "output": {
                            "voice": env.get("QWEN_VOICE", "Tina"),
                            "format": {"type": "pcm", "sample_rate": 24000},
                        },
                    },
                },
            }
        )
        while (await read())["type"] != "session.updated":
            pass
        with wave.open(
            str(pathlib.Path(plan["_fixtures"]) / "continue.wav"), "rb"
        ) as f:
            assert (f.getframerate(), f.getnchannels(), f.getsampwidth()) == (
                16000,
                1,
                2,
            )
            pcm = f.readframes(f.getnframes())
        for offset in range(0, len(pcm), 6400):
            await ws.send_json(
                {
                    "type": "input_audio_buffer.append",
                    "audio": base64.b64encode(pcm[offset : offset + 6400]).decode(),
                }
            )
        await ws.send_json({"type": "input_audio_buffer.commit"})
        while True:
            ev = await read()
            if ev["type"] == "conversation.item.input_audio_transcription.completed":
                transcripts.append(ev.get("transcript", ""))
            if ev["type"] == "input_audio_buffer.committed":
                break
        await ws.send_json({"type": "response.create"})
        pending = []
        rounds = 0
        while time.monotonic() - start < 100:
            ev = await read()
            typ = ev["type"]
            if typ == "response.audio.delta":
                audio_bytes += len(base64.b64decode(ev["delta"]))
            elif typ in ("response.audio_transcript.done", "response.text.done"):
                text.append(ev.get("transcript", ev.get("text", "")))
            elif typ == "conversation.item.input_audio_transcription.completed":
                transcripts.append(ev.get("transcript", ""))
            elif typ == "response.function_call_arguments.done":
                pending.append(ev)
                calls.append({"name": ev.get("name"), "arguments": ev.get("arguments")})
            elif typ == "response.done":
                status = ev.get("response", {}).get("status", "unknown")
                if status != "completed":
                    raise RuntimeError("response_" + status)
                if not pending:
                    result["status"] = status
                    break
                rounds += 1
                if rounds > 4:
                    raise RuntimeError("too_many_tool_rounds")
                for call in pending:
                    output = (
                        dict(
                            history.get("paused_task", plan["paused_unknown"]), ok=True
                        )
                        if call.get("name") == "conversation_get_paused_task"
                        else {
                            "ok": False,
                            "error": "mutations_disabled_in_synthetic_audit",
                        }
                    )
                    await ws.send_json(
                        {
                            "type": "conversation.item.create",
                            "item": {
                                "type": "function_call_output",
                                "call_id": call["call_id"],
                                "output": json.dumps(output, ensure_ascii=False),
                            },
                        }
                    )
                pending = []
                await ws.send_json({"type": "response.create"})
        else:
            raise RuntimeError("case_deadline")
    result.update(
        input_transcripts=transcripts,
        replies=text,
        tool_calls=calls,
        audio_bytes=audio_bytes,
        events=events,
        elapsed_ms=round((time.monotonic() - start) * 1000),
    )
    return result


async def main(args):
    os.umask(0o077)
    out = pathlib.Path(args.out)
    out.mkdir(mode=0o700, exist_ok=False)
    plan = json.loads(pathlib.Path(args.plan).read_text())
    env = {}
    assert all(re.fullmatch(r"[A-Za-z0-9_-]{1,64}", name) for name in plan["variants"])
    plan["_fixtures"] = args.fixtures
    env.update(os.environ)
    for line in (
        pathlib.Path(args.env_file).read_text().splitlines() if args.env_file else []
    ):
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        k, _, v = line.partition("=")
        env[k] = shlex.split(v)[0] if shlex.split(v) else ""
    failed = False
    async with aiohttp.ClientSession() as http:
        for trial in range(args.trials):
            for name, history in plan["variants"].items():
                try:
                    r = await case(http, env, plan, name, history, trial)
                except Exception as e:
                    r = {
                        "variant": name,
                        "trial": trial,
                        "probe_error": type(e).__name__,
                    }
                    failed = True
                (out / (name + "-" + str(trial) + ".json")).write_text(
                    json.dumps(r, ensure_ascii=False, indent=2) + "\n"
                )
                print(
                    json.dumps(
                        {
                            k: r.get(k)
                            for k in (
                                "variant",
                                "trial",
                                "status",
                                "replies",
                                "tool_calls",
                                "probe_error",
                            )
                        },
                        ensure_ascii=False,
                    ),
                    flush=True,
                )
    if failed:
        raise SystemExit(1)


parser = argparse.ArgumentParser()
parser.add_argument("--plan", required=True)
parser.add_argument("--out", required=True)
parser.add_argument("--trials", type=int, default=2)
parser.add_argument("--fixtures", required=True)
parser.add_argument(
    "--env-file",
    help="Optional protected server env file; otherwise use inherited environment",
)
args = parser.parse_args()
assert 1 <= args.trials <= 5
asyncio.run(main(args))
