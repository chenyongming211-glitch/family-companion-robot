"""Bounded pre-play check for explicit playback-history questions only."""
import re
import time

from history import NUMBERS, number_key, playback_evidence, is_reference_answer


PROGRESS = re.compile(r'[数念读讲说播]到|停在')
CLAIM = re.compile(r'(?:[数念读讲说播]到|停在)了?\s*(?:第)?([0-9０-９〇零一二两三四五六七八九十百千]+)')
HEDGE = re.compile(r'大概|大约|左右|附近|至少|不确定|不能确定|无法确定|不太确定|不能精确|可能|差不多|开头|开始的部分|这一段|那一段|还没|尚未|未完|没完全|未结束|没结束|刚开始')
NO_RECORD = re.compile(r'没有[任何]*记录|没记录|没有保存|没(?:有)?记下')


def verdict(turns, text):
    evidence = [playback_evidence(t) for t in turns[-16:] if not is_reference_answer(t.user)]
    available = [e for e in evidence if e['spoken_text']]
    reasons, anchors = [], set()
    if available and NO_RECORD.search(text):
        reasons.append('record_exists')
    for item in available:
        tail = ''.join(w['text'] for w in item['tail_words'])
        if item['tail_words']:
            last = item['tail_words'][-1]['text'].strip()
            if NUMBERS.fullmatch(last):
                anchors.add(number_key(last))
        if item['boundary'] == 'complete_response':
            tail = item['spoken_text']
        matches = list(NUMBERS.finditer(tail))
        if matches:
            anchors.add(number_key(matches[-1].group()))
    claims = list(CLAIM.finditer(text))
    if PROGRESS.search(text) and (not available or any(not e['exact_stop_known'] for e in available)) and not HEDGE.search(text):
        reasons.append('conservative_boundary_requires_qualification')
    for claim in claims:
        if number_key(claim.group(1)) not in anchors:
            reasons.append('unsupported_numeric_progress')
    return {'allowed': not reasons, 'reasons': sorted(set(reasons))}


async def guarded_stream(native, factory, instructions, pcm, user_text, prior, rejected, observe=None):
    """No retry on provider failure. One semantic rewrite after a known rejection."""
    protected, current, owned, started = None, native, False, time.monotonic()
    try:
        for attempt in range(2):
            audio, transcript, size = [], '', 0
            async for kind, value in current.stream(pcm):
                if protected is None:
                    query = user_text()
                    protected = is_reference_answer(query)
                    if observe:
                        observe({'phase': 'selected', 'protected': protected, 'transcription_available': bool(query)})
                if not protected:
                    yield kind, value
                    continue
                if kind == 'text':
                    transcript = value
                else:
                    size += len(value)
                    if size > 24000 * 2 * 60:
                        raise ValueError('recall_audio_limit')
                    audio.append(value)
            if not protected:
                return
            result = verdict(prior, transcript)
            if result['allowed'] and transcript and audio:
                if observe:
                    observe({'phase': 'accepted', 'rewrites': attempt,
                        'buffer_ms': round((time.monotonic() - started) * 1000)})
                yield 'text', transcript
                for chunk in audio:
                    yield 'pcm', chunk
                return
            rejected({'attempt': attempt + 1, 'candidate': transcript, **result})
            if attempt == 1:
                raise ValueError('recall_grounding_unconfirmed')
            # The rejected response completed normally and has never been sent.
            # Closing it and asking for a rewrite is not an unknown-outcome retry.
            await current.close()
            current, owned = factory(), True
            await current.open(instructions + '\n本轮候选回答未获播放权限，原因：' + ','.join(result['reasons'])
                + '。现在重新组织一条简短自然的回答：只依据源内容播放记录；边界保守必须明确说大致或附近，'
                '不重复未经记录支持的进度数字；有记录不能说没有记录。不能引用之前对停点的回答作证据。')
    finally:
        if owned:
            await current.close()
