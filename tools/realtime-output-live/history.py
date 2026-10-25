# DEPRECATED_PRODUCT_PATH (2026-10-25): replaced in the default voice path by Go.
# Go implementation: internal/realtimestate/history.go
# Retained only for temporary Python rollback and differential-test reference.
# Do not add product features here; see docs/CODE_LIFECYCLE.md.
"""Retain played ASR observations without certifying an exact acoustic stop."""
import math
import re
import unicodedata


NUMBERS = re.compile(r'[0-9０-９〇零一二两三四五六七八九十百千]+')
DIGITS = {c: str(i) for i, c in enumerate('零一二三四五六七八九')}
DIGITS.update({'〇': '0', '两': '2'})
UNITS = {'十': 10, '百': 100, '千': 1000}


def number_key(raw):
    raw = unicodedata.normalize('NFKC', raw)
    if raw.isascii() and raw.isdecimal():
        return 'cardinal', raw
    if all(c in DIGITS for c in raw):
        return ('cardinal' if len(raw) == 1 else 'digit_sequence', ''.join(DIGITS[c] for c in raw))
    total, pending, previous = 0, None, 10000
    for c in raw:
        if c in DIGITS:
            if pending not in (None, 0):
                return 'literal', raw
            pending = int(DIGITS[c])
        elif c in UNITS and UNITS[c] < previous:
            total += (1 if pending is None else pending) * UNITS[c]
            pending, previous = None, UNITS[c]
        else:
            return 'literal', raw
    if pending and previous >= 100 and not (len(raw) >= 2 and raw[-2] in ('零', '〇')):
        # 二百五 may mean 250 rather than 205; keep this observation unaligned.
        return 'literal', raw
    return 'cardinal', str(total + (pending or 0))


def tokens(text):
    """Keep number boundaries: '一、二' must not match the cardinal '12'."""
    result, offset = [], 0
    for match in NUMBERS.finditer(text):
        for index in range(offset, match.start()):
            for char in unicodedata.normalize('NFKC', text[index]).casefold():
                if not unicodedata.category(char).startswith(('P', 'Z', 'S')):
                    result.append((('char', char), index + 1))
        result.append((number_key(match.group()), match.end()))
        offset = match.end()
    for index in range(offset, len(text)):
        for char in unicodedata.normalize('NFKC', text[index]).casefold():
            if not unicodedata.category(char).startswith(('P', 'Z', 'S')):
                result.append((('char', char), index + 1))
    return result


def equivalent_prefix(generated, observed):
    draft, heard = tokens(generated), tokens(observed)
    if not heard or len(heard) > len(draft) or any(a[0] != b[0] for a, b in zip(draft, heard)):
        return None
    end = draft[len(heard) - 1][1]
    while end < len(generated) and unicodedata.category(generated[end]).startswith(('P', 'Z', 'S')):
        end += 1
    return generated[:end]


def observations(turn, margin_ms=120):
    selected, previous, expected, length = [], 0, 1, 0
    played_ms, sent_ms = turn.completed / 24, turn.sent / 24
    boundary = 'no_recognition'
    for identity, sentence in sorted(turn.tracker.sentences.items()):
        if identity != expected or sentence.get('sentence_end') is not True:
            return selected, 'recognition_gap'
        expected += 1
        for word in sentence.get('words', []):
            start, end, text = word.get('begin_time'), word.get('end_time'), word.get('text')
            if (type(start) not in (int, float) or type(end) not in (int, float)
                    or not math.isfinite(start) or not math.isfinite(end)
                    or not previous <= start <= end <= sent_ms or not isinstance(text, str)):
                return selected, 'invalid_timing'
            if end > max(0, played_ms - margin_ms):
                return selected, 'playback_guard'
            if length + len(text) > 2048 or len(selected) >= 512:
                return selected, 'context_limit'
            length += len(text)
            selected.append({'text': text, 'begin_ms': start, 'end_ms': end})
            previous, boundary = end, 'conservative_end'
    return selected, boundary


def playback_evidence(turn):
    words, boundary = observations(turn)
    observed = ''.join(w['text'] for w in words).strip() or None
    aligned = equivalent_prefix(turn.generated, observed) if observed else None
    complete = bool(turn.ended and turn.sent > 0 and turn.completed == turn.sent)
    # Normal generation end means all source audio, including codec lookahead,
    # was released. Counters alone on an interrupted draft cannot prove this.
    spoken = turn.generated if complete else (aligned or observed)
    source = ('native_transcript_full_playback' if complete else 'output_asr' if observed else 'none')
    return {'spoken_text': spoken or None, 'observed_text': observed,
        'text_source': source, 'alignment': 'equivalent_prefix' if aligned else 'unmatched',
        'aligned_prefix': aligned, 'completed_ms': turn.completed / 24, 'sent_ms': turn.sent / 24,
        'boundary': 'complete_response' if complete else boundary,
        'exact_stop_known': complete, 'word_end_ms': words[-1]['end_ms'] if words else None,
        'tail_words': words[-6:], 'margin_ms': 120, 'claim_scope': 'this_response_only',
        'recognition_may_differ': True}
