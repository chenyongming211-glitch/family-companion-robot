"""Conservative experiment candidates, never certified word-level hearing."""
import math
import unicodedata


def normalized(text):
    return ''.join(c.casefold() for c in text if not unicodedata.category(c).startswith(('P', 'Z', 'S')))


def select_prefix(sentences, generated, played_ms, *, margin_ms=120, total_audio_ms=None):
    unknown = {'status': 'unknown', 'prefix': None}
    if not math.isfinite(played_ms) or played_ms < 0 or margin_ms < 0:
        return unknown
    words, previous = [], 0
    for sentence in sentences:
        if sentence.get('sentence_end') is not True:
            continue
        for word in sentence.get('words', []):
            begin, end = word.get('begin_time'), word.get('end_time')
            if (type(begin) not in (int, float) or type(end) not in (int, float)
                    or not math.isfinite(begin) or not math.isfinite(end)
                    or not previous <= begin <= end or not isinstance(word.get('text'), str)
                    or (total_audio_ms is not None and end > total_audio_ms)):
                return unknown
            previous = end
            words.append(word)
    selected = []
    for word in words:
        if word['end_time'] > max(0, played_ms - margin_ms):
            break
        selected.append(word)
    recognized = normalized(''.join(word['text'] for word in selected))
    if not recognized or not normalized(generated).startswith(recognized):
        return unknown
    count = end_index = 0
    for index, char in enumerate(generated):
        count += len(normalized(char))
        if count == len(recognized):
            end_index = index + 1
            break
    while end_index < len(generated) and not normalized(generated[end_index]):
        end_index += 1
    return {'status': 'candidate', 'prefix': generated[:end_index],
            'word_end_ms': selected[-1]['end_time'], 'margin_ms': margin_ms,
            'selected_words': len(selected), 'played_ms': played_ms}


def validate_receipt(receipt, identity, sent_samples):
    completed, accepted = receipt.get('completed_samples'), receipt.get('accepted_samples')
    if (receipt.get('type') not in ('cut', 'played') or receipt.get('id') != identity
            or receipt.get('basis') != 'CoreAudio_dataPlayedBack'
            or receipt.get('microphone') is not False
            or type(completed) is not int or type(accepted) is not int
            or not 0 <= completed <= accepted <= sent_samples or completed % 1440):
        raise ValueError('invalid_playback_receipt')
    return completed * 1000 / 24000


class PrefixTracker:
    def __init__(self, generated, total_audio_ms):
        self.generated, self.total_audio_ms = generated, total_audio_ms
        self.sentences, self.arrivals = {}, []

    def add(self, sentence, received_ms):
        identity = sentence.get('sentence_id')
        if sentence.get('sentence_end') is not True or type(identity) is not int or not 1 <= identity <= 256:
            return
        self.sentences[identity] = sentence
        self.arrivals.append((received_ms, dict(self.sentences)))

    def ordered(self, values=None):
        values = self.sentences if values is None else values
        if sorted(values) != list(range(1, len(values) + 1)):
            return []
        return [values[key] for key in sorted(values)]

    def candidate(self, played_ms):
        return select_prefix(self.ordered(), self.generated, played_ms, total_audio_ms=self.total_audio_ms)

    def covers(self, played_ms, values=None):
        ordered = self.ordered(values)
        result = select_prefix(ordered, self.generated, played_ms, total_audio_ms=self.total_audio_ms)
        return (result['status'] == 'candidate'
                and max((word['end_time'] for sentence in ordered for word in sentence.get('words', [])), default=0)
                >= played_ms - 120)

    def first_usable_ms(self, played_ms):
        return next((at for at, values in self.arrivals if self.covers(played_ms, values)), None)
