"""Read-only product identity and facts for this isolated voice entry."""
import copy
import json
from pathlib import Path
from urllib.parse import parse_qs, urlsplit

from state import RECENT_TURN_LIMIT


_PRODUCT = json.loads((Path(__file__).resolve().parents[2] / 'config/product-identity.json').read_text())


def capability_snapshot(cfg, context, *, connected):
    model = cfg.get('model') or parse_qs(urlsplit(cfg.get('QWEN_REALTIME_URL', '')).query).get('model', [None])[0]
    provider = '阿里云通义千问' if isinstance(model, str) and model.casefold().startswith('qwen') else None
    voice_status = 'available' if connected else 'temporarily_unavailable'
    caps = {key: {'status': voice_status, 'description': description} for key, description in (
        ('voice_chat', '自然语音聊天'), ('general_qa', '一般知识问答和学习交流'),
        ('storytelling', '讲故事'), ('language_exchange', '中英文交流'))}
    caps.update({
        'recent_conversation': {'status': 'testing', 'description': '当前会话近期已播文字；识别和回忆可能有误'},
        'story_resume': {'status': 'testing', 'description': '插话后结合已播内容语义续讲，非精确逐字原稿恢复'},
    })
    caps.update({key: {'status': 'not_connected', 'description': description} for key, description in (
        ('full_history_search', '查询全部通话档案'), ('cross_session_memory', '跨会话长期记忆'),
        ('reminders', '设置并执行提醒或闹钟'), ('image_analysis', '当前没有拍照、图片上传、查看或分析通道'),
        ('live_information', '联网检索、实时天气或新闻查询'),
        ('device_control', '控制音量、亮度、家电等设备'), ('physical_actions', '移动、拿取等实体动作'))})
    if cfg.get('enable_search') is True:
        caps['live_information'] = {'status': voice_status, 'description': '使用内置联网搜索查询天气、新闻等最新信息'}
    return {'product': copy.deepcopy(_PRODUCT), 'entry': 'realtime-output-live', 'input_channels': ['audio'],
        'model': {'id': model, 'provider': provider}, 'capabilities': caps,
        'memory': {'max_recent_turns': RECENT_TURN_LIMIT,
            'included_turns': len(context['recent_turns']),
            'older_turns_omitted': context['older_turns_omitted'], 'scope': 'current_session_played_text',
            'server_persists_text': True, 'can_query_prior_sessions': False, 'server_saves_raw_audio': False}}
