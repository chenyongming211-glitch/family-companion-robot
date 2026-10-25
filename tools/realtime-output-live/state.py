"""Bounded sample timelines and conservative, played-only text context."""
import asyncio
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'realtime-output-probe'))
from alignment import PrefixTracker


class AudioRing:
    def __init__(self, capacity=16000 * 35):
        self.capacity, self.end, self.data = capacity, 0, bytearray()

    def append(self, pcm):
        if len(pcm) % 2:
            raise ValueError('unaligned_pcm')
        self.data.extend(pcm)
        self.end += len(pcm) // 2
        if len(self.data) > self.capacity * 2:
            del self.data[:len(self.data) - self.capacity * 2]

    def slice(self, start, end):
        first = self.end - len(self.data) // 2
        if not first <= start < end <= self.end:
            return None
        return bytes(self.data[(start - first) * 2:(end - first) * 2])


class Turn:
    def __init__(self, identity):
        self.id = identity
        self.user = self.generated = ''
        self.sent = self.completed = self.accepted = 0
        self.cut = False
        self.begun = self.ended = False
        self.drain_target = None
        self.drain_done = asyncio.Event()
        self.tracker = PrefixTracker('', 0)

    def can_send(self):
        return not self.cut and self.sent - self.completed < 7200

    def seal_for_drain(self):
        if self.drain_target is None:
            self.cut = True
            self.drain_target = self.sent

    def receipt(self, event):
        completed, accepted = event.get('completed'), event.get('accepted')
        terminal = event.get('terminal', 'progress')
        if (type(event.get('id')) is not int or event.get('id') != self.id or event.get('basis') != 'CoreAudio_dataPlayedBack'
                or type(completed) is not int or type(accepted) is not int
                or not self.completed <= completed <= accepted <= self.sent
                or accepted < self.accepted or completed % 1440 or accepted % 1440
                or terminal not in ('progress', 'played', 'drained')
                or (terminal == 'drained' and (self.drain_target is None
                    or completed != self.drain_target or accepted != self.drain_target))
                or (terminal == 'played' and (not self.ended or completed != self.sent or accepted != self.sent))):
            return False
        self.completed, self.accepted = completed, accepted
        if terminal == 'drained':
            self.drain_done.set()
        return True

    def prefix(self):
        self.tracker.generated = self.generated
        self.tracker.total_audio_ms = self.sent / 24
        return self.tracker.candidate(self.completed / 24)


def context_for(turns):
    result = []
    for turn in turns[-16:]:
        candidate = turn.prefix()
        result.append({'user': turn.user[:2048] or '[原始语音已交给模型，文字转写尚未就绪]',
            'assistant_spoken_prefix': candidate['prefix'],
            'position': candidate['status'], 'interrupted': turn.cut})
    return json.dumps({'recent_turns': result, 'older_turns_omitted': len(turns) > 16}, ensure_ascii=False)
