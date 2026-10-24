from dataclasses import dataclass, field
from pathlib import Path
from urllib.parse import urlsplit


@dataclass(frozen=True)
class Settings:
    api_key: str = field(repr=False)
    asr_url: str
    llm_url: str
    tts_url: str
    asr_model: str = 'fun-asr-realtime'
    llm_model: str = 'qwen-plus'
    tts_model: str = 'qwen3-tts-flash-realtime'
    voice: str = 'Cherry'
    client_token: str = field(default='', repr=False)
    audit_dir: Path = Path('/var/lib/family-robot-livekit-dev/audit')
    test_keep_open: bool = False

    @classmethod
    def from_env(cls, env):
        key = env.get('DASHSCOPE_API_KEY', '').strip()
        if not key:
            raise ValueError('DASHSCOPE_API_KEY is required')
        native = urlsplit(env.get('QWEN_REALTIME_URL', 'wss://dashscope.aliyuncs.com/api-ws/v1/realtime'))
        if native.scheme != 'wss' or not native.hostname or native.username or native.password:
            raise ValueError('invalid protected native endpoint')
        host = native.hostname
        model = env.get('VOICE_TTS_MODEL', 'qwen3-tts-flash-realtime')
        cfg = cls(
            api_key=key,
            asr_url=env.get('VOICE_ASR_URL', f'wss://{host}/api-ws/v1/inference'),
            llm_url=env.get('VOICE_LLM_URL', f'https://{host}/compatible-mode/v1'),
            tts_url=env.get('VOICE_TTS_URL', f'wss://{host}/api-ws/v1/realtime?model={model}'),
            asr_model=env.get('VOICE_ASR_MODEL', 'fun-asr-realtime'),
            llm_model=env.get('VOICE_LLM_MODEL', 'qwen-plus'),
            tts_model=model,
            voice=env.get('VOICE_TTS_VOICE', 'Cherry'),
            client_token=env.get('VOICE_CLIENT_TOKEN', ''),
            audit_dir=Path(env.get('VOICE_AUDIT_DIR', '/var/lib/family-robot-livekit-dev/audit')),
            test_keep_open=env.get('VOICE_TEST_KEEP_OPEN', '0') == '1',
        )
        for url, secure, local in ((cfg.asr_url, 'wss', 'ws'), (cfg.tts_url, 'wss', 'ws'), (cfg.llm_url, 'https', 'http')):
            u = urlsplit(url)
            if u.username or u.password or not u.hostname or not (u.scheme == secure or (u.scheme == local and u.hostname in ('127.0.0.1', '::1', 'localhost'))):
                raise ValueError('model endpoints require TLS except on loopback')
        return cfg
