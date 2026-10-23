"""Run unchanged TEN source with fake transport/runtime only; no network or keys.
Native TEN C runtime, ASR/LLM/TTS vendors, and speakers are NOT exercised.
"""

import argparse, asyncio, base64, hashlib, importlib, json, pathlib, sys, types

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument(
    "--runtime-root",
    type=pathlib.Path,
    required=True,
    help="Pinned sparse upstream/ and packages/ layout described in reference manifest",
)
parser.add_argument("--out", type=pathlib.Path, required=True)
parser.add_argument("--suite", choices=["chained", "realtime"], required=True)
args = parser.parse_args()
ROOT = args.runtime_root
SUITE = args.suite
BASE_VERSION = "0.7.41" if SUITE == "chained" else "0.7.22"
assert (
    json.loads((ROOT / "download_manifest.json").read_text())["commit"]
    == "1493ed2394c1c972dc79c0f4d567579954f9addf"
)
reference = json.loads(
    pathlib.Path(__file__).with_name("resume-reference-lock.json").read_text()
)["upstreams"]["ten"]
for entry in reference["source_files"]:
    assert (
        hashlib.sha256((ROOT / "upstream" / entry["path"]).read_bytes()).hexdigest()
        == entry["sha256"]
    ), entry["path"]
assert (
    hashlib.sha256(
        (ROOT / ("ten_ai_base-" + BASE_VERSION + ".tpkg")).read_bytes()
    ).hexdigest()
    == reference["package_archive_sha256"][BASE_VERSION]
)
OUT = args.out
OUT.mkdir(parents=True, exist_ok=False)


class Msg:
    def __init__(self, name="", payload=None, final=False):
        self.name = name
        self.payload = payload or {}
        self.final = final
        self.dests = []

    @classmethod
    def create(cls, name, *args):
        return cls(name)

    def get_name(self):
        return self.name

    def set_dests(self, dests):
        self.dests = dests

    def set_property_from_json(self, key, s):
        self.payload = json.loads(s)

    def get_property_to_json(self, key):
        return json.dumps(self.payload), None

    def get_status_code(self):
        return 0

    def is_final(self):
        return self.final


class Extension:
    def __init__(self, name):
        self.name = name


class Loc:
    def __init__(self, *args):
        self.extension_name = args[-1]


class Frame(Msg):
    def __init__(self, name=""):
        super().__init__(name)
        self.buf = bytearray()

    def __getattr__(self, n):
        if n.startswith("set_"):
            return lambda *a: None
        raise AttributeError(n)

    def alloc_buf(self, n):
        self.buf = bytearray(n)

    def lock_buf(self):
        return memoryview(self.buf)

    def unlock_buf(self, b):
        pass


rt = types.ModuleType("ten_runtime")
for k, v in {
    "AsyncExtension": Extension,
    "AsyncTenEnv": object,
    "Cmd": Msg,
    "CmdResult": Msg,
    "Data": Msg,
    "Loc": Loc,
    "TenError": Exception,
    "StatusCode": types.SimpleNamespace(OK=0, ERROR=1),
    "AudioFrame": Frame,
    "AudioFrameDataFmt": types.SimpleNamespace(INTERLEAVE=1),
}.items():
    setattr(rt, k, v)
sys.modules["ten_runtime"] = rt
sys.modules["ten_runtime.async_ten_env"] = types.SimpleNamespace(AsyncTenEnv=object)


def namespace(n, p):
    m = types.ModuleType(n)
    m.__path__ = [str(p)]
    sys.modules[n] = m


namespace(
    "ten_ai_base",
    ROOT
    / (
        "packages/ten_ai_base-0.7.22/interface/ten_ai_base"
        if BASE_VERSION == "0.7.22"
        else "packages/ten_ai_base/interface/ten_ai_base"
    ),
)
mainpath = (
    ROOT
    / "upstream/ai_agents/agents/examples/voice-assistant/tenapp/ten_packages/extension/main_python"
)
namespace("ten_chained", mainpath)
realtimepath = (
    ROOT / "upstream/ai_agents/agents/ten_packages/extension/openai_mllm_python"
)
namespace("ten_realtime", realtimepath)
from ten_chained.extension import MainControlExtension
from ten_chained.agent.events import ASRResultEvent
from ten_ai_base.struct import MLLMClientMessageItem
from ten_realtime import extension as rext
from ten_realtime.realtime import struct as rs

FULL = "，".join(map(str, range(1, 51))) + "。"


class Env:
    def __init__(self):
        self.requests = []
        self.cmds = []
        self.data = []
        self.audio = []
        self.errors = []

    def log_error(self, s):
        self.errors.append(s)

    def __getattr__(self, n):
        if n.startswith("log_"):
            return lambda *a: None
        raise AttributeError(n)

    async def get_property_to_json(self, key):
        return '{"greeting":""}', None

    async def send_cmd(self, c):
        self.cmds.append(
            {
                "name": c.name,
                "payload": c.payload,
                "dest": [d.extension_name for d in c.dests],
            }
        )
        return Msg(), None

    async def send_cmd_ex(self, c):
        self.requests.append(c.payload)
        text = c.payload["messages"][-1]["content"]
        answer = (
            FULL
            if len(self.requests) == 1
            else ("2。" if len(self.requests) == 2 else "[NO MODEL CALLED]")
        )
        for typ, delta in [
            ("message_content_delta", answer),
            ("message_content_done", None),
        ]:
            p = {
                "type": typ,
                "role": "assistant",
                "response_id": str(len(self.requests)),
                "content": answer,
            }
            if delta is not None:
                p["delta"] = delta
            yield Msg(payload=p), None
        yield Msg(final=True), None

    async def send_data(self, d):
        self.data.append(
            {
                "name": d.name,
                "payload": d.payload,
                "dest": [x.extension_name for x in d.dests],
            }
        )

    async def send_audio_frame(self, f):
        self.audio.append(len(f.buf))

    async def return_result(self, r):
        pass


async def settle(test):
    for _ in range(100):
        if test():
            return
        await asyncio.sleep(0.005)
    raise AssertionError("condition timed out")


async def chained():
    env = Env()
    ext = MainControlExtension("main")
    await ext.on_init(env)
    await ext._on_asr_result(
        ASRResultEvent(text="从1数到50", final=True, metadata={"session_id": "100"})
    )
    await settle(
        lambda: any(
            d["name"] == "tts_text_input" and d["payload"]["text_input_end"]
            for d in env.data
        )
    )
    before = [m.model_dump() for m in ext.agent.llm_exec.contexts]
    # A harness-side listener/speaker reports only 8 heard. TEN does not define played_count;
    # feeding it through an existing tts_audio_end event demonstrates this path is ignored.
    await ext.agent.on_data(
        Msg(
            "tts_audio_end",
            {
                "request_id": "tts-request-1",
                "reason": 2,
                "metadata": {"harness_heard_through": 8},
            },
        )
    )
    await ext._on_asr_result(
        ASRResultEvent(text="1加1等于几", final=True, metadata={"session_id": "100"})
    )
    await settle(
        lambda: len(env.requests) >= 2 and ext.agent.llm_exec.current_text is None
    )
    await ext._on_asr_result(
        ASRResultEvent(text="继续刚才数数", final=True, metadata={"session_id": "100"})
    )
    await settle(lambda: len(env.requests) >= 3)
    result = {
        "level": "unchanged upstream Python Agent/MainControl/LLMExec + real ten_ai_base helper/struct; fake native runtime and provider transport; no live LLM/audio",
        "generated_through": 50,
        "harness_heard_through": 8,
        "context_before_interrupt": before,
        "math_request_messages": env.requests[1]["messages"],
        "continue_request_messages": env.requests[2]["messages"],
        "abort_commands": [c for c in env.cmds if c["name"] == "abort"],
        "rtc_flush_commands": [c for c in env.cmds if c["name"] == "flush"],
        "tts_flush_count": sum(d["name"] == "tts_flush" for d in env.data),
        "resume_state_keys": [
            k
            for k in vars(ext.agent.llm_exec)
            if any(
                s in k
                for s in [
                    "context",
                    "task",
                    "text",
                    "request",
                    "play",
                    "resume",
                    "cursor",
                ]
            )
        ],
        "errors": env.errors,
    }
    assert result["math_request_messages"][1]["content"] == FULL
    assert result["continue_request_messages"][1]["content"] == FULL
    assert not env.errors
    await ext.on_stop(env)
    return result


class Conn:
    messages = []
    instance = None

    def __init__(self, **kw):
        self.sent = []
        Conn.instance = self

    async def connect(self):
        pass

    async def listen(self):
        for m in self.messages:
            yield m

    async def send_request(self, m):
        self.sent.append(rs.to_json(m))

    async def close(self):
        pass


async def realtime(done_before_interrupt=False):
    # Feed the upstream OpenAI event decoder's dataclasses, with full transcript ahead of speaker.
    rid = "r1"
    item = "i1"
    common = dict(
        event_id="e1", response_id=rid, item_id=item, output_index=0, content_index=0
    )
    ms = [
        rs.ResponseCreated(event_id="created", response=rs.Response(id=rid)),
        rs.ResponseAudioTranscriptDelta(**common, delta=FULL),
        rs.ResponseAudioDelta(**common, delta=base64.b64encode(bytes(4800)).decode()),
    ]
    if done_before_interrupt:
        ms += [
            rs.ResponseAudioTranscriptDone(**common, transcript=FULL),
            rs.ResponseAudioDone(**common),
            rs.ResponseDone(
                event_id="done", response=rs.Response(id=rid, status="completed")
            ),
        ]
    ms += [
        rs.InputAudioBufferSpeechStarted(
            event_id="speech", audio_start_ms=8000, item_id="u2"
        )
    ]
    if not done_before_interrupt:
        ms += [
            rs.ResponseAudioDelta(
                **common, delta=base64.b64encode(bytes(4800)).decode()
            )
        ]
    Conn.messages = ms
    rext.RealtimeApiConnection = Conn
    env = Env()
    ext = rext.OpenAIRealtime2Extension("v2v")
    ext.ten_env = env
    ext.stopped = True
    ext.config = types.SimpleNamespace(
        base_url="wss://not-used.invalid",
        path="/v1/realtime",
        api_key="NOT_A_KEY",
        model="fake",
        vendor="",
        server_vad=True,
        sample_rate=24000,
    )
    await ext.start_connection()
    result = {
        "level": "unchanged upstream OpenAIRealtime2Extension event loop + actual ten_ai_base mllm; fake websocket/runtime; no provider history claims",
        "generated_through": 50,
        "harness_heard_through": 8,
        "response_done_before_interrupt": done_before_interrupt,
        "protocol_requests_sent": Conn.instance.sent,
        "emitted_events": env.data,
        "audio_frame_byte_lengths": env.audio,
        "local_message_context": ext.message_context,
        "errors": env.errors,
    }
    assert not Conn.instance.sent, "Unexpected server truncate/cancel request"
    assert any(d["name"] == "mllm_server_interrupted" for d in env.data)
    if done_before_interrupt:
        assert not env.errors
        assert len(env.audio) == 1
    else:
        assert any("delta" in e and "valid string" in e for e in env.errors), env.errors
        assert (
            len(env.audio) == 2
        ), "Pinned upstream transcript-validation failure bypasses flushed.add"
        result["observed_upstream_error"] = (
            "MLLMServerOutputTranscript.delta is str in pinned ten_ai_base, but realtime interruption passes None; caught before flushed.add(response_id)"
        )
    result["truncate_request_count"] = sum(
        "conversation.item.truncate" in s for s in Conn.instance.sent
    )
    return result


async def main():
    results = (
        {"chained": await chained()}
        if SUITE == "chained"
        else {
            "realtime_in_progress": await realtime(),
            "realtime_generated_done_speaker_behind": await realtime(True),
        }
    )
    results["environment"] = {
        "ten_ai_base_version": BASE_VERSION,
        "suite": SUITE,
        "python": sys.version,
        "native_runtime": "test double",
        "provider": "test double",
        "heard_through_8": "external fixture, not device playback",
        "loaded_upstream_modules": {
            n: str(getattr(m, "__file__", ""))
            for n, m in sys.modules.items()
            if n.startswith(("ten_chained.", "ten_realtime.", "ten_ai_base."))
            and getattr(m, "__file__", None)
        },
    }
    (OUT / f"actual-{SUITE}-{BASE_VERSION}-results.json").write_text(
        json.dumps(results, ensure_ascii=False, indent=2)
    )
    print(json.dumps(results, ensure_ascii=False, indent=2))
    # Upstream LLMExec creates an untracked queue consumer; explicitly cancel remaining harness tasks.
    current = asyncio.current_task()
    pending = [t for t in asyncio.all_tasks() if t is not current]
    for t in pending:
        t.cancel()
    await asyncio.gather(*pending, return_exceptions=True)


asyncio.run(main())
