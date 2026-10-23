"""Execute upstream chat/abort/history methods with explicit synthetic dependencies."""

import argparse, ast, asyncio, hashlib, importlib.util, json, pathlib, queue, sys, types, uuid

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument(
    "--source-root",
    type=pathlib.Path,
    required=True,
    help="Flat directory with pinned dialogue.py, dto.py, abortHandle.py, connection.py, test_dialogue.py",
)
parser.add_argument("--out", type=pathlib.Path, required=True)
parser.add_argument("--upstream-tests", action="store_true")
args = parser.parse_args()
base = args.source_root
reference = json.loads(
    pathlib.Path(__file__).with_name("resume-reference-lock.json").read_text()
)["upstreams"]["xiaozhi"]
for name, expected in reference["files"].items():
    assert hashlib.sha256((base / name).read_bytes()).hexdigest() == expected, name
out = args.out
out.mkdir(parents=True, exist_ok=False)


def load(name, file):
    spec = importlib.util.spec_from_file_location(name, base / file)
    mod = importlib.util.module_from_spec(spec)
    sys.modules[name] = mod
    spec.loader.exec_module(mod)
    return mod


sys.modules["core"] = types.ModuleType("core")
sys.modules["core.utils"] = types.ModuleType("core.utils")
dialogue = load("core.utils.dialogue", "dialogue.py")
dto = load("audit_dto", "dto.py")
abort = load("audit_abort", "abortHandle.py")
source = (base / "connection.py").read_text()
tree = ast.parse(source)
original = next(
    n
    for n in tree.body
    if isinstance(n, ast.ClassDef) and n.name == "ConnectionHandler"
)
methods = [
    n
    for n in original.body
    if isinstance(n, (ast.FunctionDef, ast.AsyncFunctionDef))
    and n.name in ("chat", "clear_queues", "clearSpeakStatus")
]
klass = ast.ClassDef(
    name="ProductionMethods", bases=[], keywords=[], body=methods, decorator_list=[]
)
module = ast.fix_missing_locations(ast.Module(body=[klass], type_ignores=[]))
namespace = dict(
    asyncio=asyncio,
    uuid=uuid,
    queue=queue,
    json=json,
    TAG="isolated-audit",
    Message=dialogue.Message,
    **{k: getattr(dto, k) for k in ("TTSMessageDTO", "SentenceType", "ContentType")}
)
exec(compile(module, str(base / "connection.py"), "exec"), namespace)


class Logger:
    def bind(self, **kw):
        return self

    def info(self, *a, **kw):
        pass

    def debug(self, *a, **kw):
        pass

    def error(self, *a, **kw):
        raise AssertionError(str(a))


class Sink:
    def __init__(self):
        self.accepted = []


class TextQueue(queue.Queue):
    def __init__(self, sink):
        super().__init__()
        self.sink = sink

    def put(self, item, *a, **kw):
        if (
            item.sentence_type == dto.SentenceType.MIDDLE
            and len(self.sink.accepted) < 8
        ):
            self.sink.accepted.append(item.content_detail)
        else:
            super().put(item, *a, **kw)


class TTS:
    def __init__(self, sink):
        self.tts_text_queue = TextQueue(sink)
        self.tts_audio_queue = queue.Queue()
        self.text = []

    def store_tts_text(self, sentence, text):
        self.text.append(text)


class WS:
    def __init__(self):
        self.sent = []

    async def send(self, data):
        self.sent.append(json.loads(data))


class LLM:
    def __init__(self, conn, limit):
        self.conn = conn
        self.limit = limit

    def response(self, session, history):
        for i in range(1, 51):
            if i == self.limit + 1:
                self.conn.client_abort = True
            yield str(i) + "、"


class Conn(namespace["ProductionMethods"]):
    pass


results = []
for label, limit in [
    ("generation_complete_played8", 50),
    ("generation_interrupted_after12_played8", 12),
]:
    c = Conn()
    sink = Sink()
    c.logger = Logger()
    c.session_id = "synthetic"
    c.sentence_id = None
    c.dialogue = dialogue.Dialogue()
    c.tts = TTS(sink)
    c.memory = None
    c.current_speaker = None
    c.system_introduced_speakers = set()
    c.features = {"emoji": False}
    c.config = {}
    c.intent_type = "nointent"
    c.client_abort = False
    c.report_queue = queue.Queue()
    c.websocket = WS()
    c.client_is_speaking = True
    c.close_after_chat = True
    c.llm = LLM(c, limit)
    assert c.chat("从一完整数到五十")
    before = c.dialogue.get_llm_dialogue()
    queued = c.tts.tts_text_queue.qsize()
    asyncio.run(abort.handleAbortMessage(c))
    after = c.dialogue.get_llm_dialogue()
    assert before == after
    assert (
        c.client_abort
        and c.tts.tts_text_queue.empty()
        and c.tts.tts_audio_queue.empty()
        and not c.client_is_speaking
    )
    assert c.websocket.sent == [
        {"type": "tts", "state": "stop", "session_id": "synthetic"}
    ]
    assert sink.accepted == [str(i) + "、" for i in range(1, 9)]
    assert after[-1]["content"] == "".join(str(i) + "、" for i in range(1, limit + 1))
    c.dialogue.put(dialogue.Message(role="user", content="停一下，一加一等于几"))
    c.dialogue.put(dialogue.Message(role="assistant", content="二。"))
    c.dialogue.put(dialogue.Message(role="user", content="继续数"))
    results.append(
        {
            "case": label,
            "upstream_chat_ast_verbatim": True,
            "upstream_abort_imported": True,
            "synthetic_word_sink_no_audio": True,
            "simulated_played_prefix": sink.accepted,
            "queued_text_before_abort": queued,
            "queued_text_after_abort": c.tts.tts_text_queue.qsize(),
            "context_before_abort": before,
            "context_after_abort": after,
            "context_before_continue": c.dialogue.get_llm_dialogue(),
            "actual_model_api_called": False,
            "full_server_started": False,
        }
    )
summary = {
    "upstream_commit": "fdf280f84aaa84211f680936cbfd98bf9d7be01b",
    "cases": results,
    "source_sha256": {
        p.name: hashlib.sha256(p.read_bytes()).hexdigest()
        for p in base.glob("*.py")
        if p.name != "probe.py"
    },
}
(out / "synthetic-method-results.json").write_text(
    json.dumps(summary, ensure_ascii=False, indent=2) + "\n"
)
print(
    json.dumps(
        {
            "cases": len(results),
            "result": "PASS; observe generation prefix retained, not playback prefix",
            "full_server_started": False,
        }
    )
)
if args.upstream_tests:
    import pytest

    raise SystemExit(
        pytest.main(
            ["-q", "-p", "pytest_asyncio.plugin", str(base / "test_dialogue.py")]
        )
    )
