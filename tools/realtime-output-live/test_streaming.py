"""Reply-delivery regressions; synthetic context, no provider or microphone."""
import asyncio
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from server import Session
from state import Turn


def source(text='一二三四五六七八', last_word='七八'):
    turn = Turn(1)
    turn.user, turn.generated = '从一数到五十', text + '九十十一十二'
    turn.sent = turn.completed = turn.accepted = 9120 * 24
    turn.cut = True
    turn.drain_target = turn.sent
    turn.drain_done.set()
    turn.tracker.add({'sentence_id': 1, 'sentence_end': True, 'words': [
        {'text': text[:-len(last_word)], 'begin_time': 0, 'end_time': 6000},
        {'text': last_word, 'begin_time': 6000, 'end_time': 8160}]}, 9200)
    return turn


class Native:
    def __init__(self, answer, gate=None, failure=False):
        self.answer, self.gate, self.failure = answer, gate, failure
        self.instructions, self.closed = '', False

    async def open(self, instructions):
        self.instructions = instructions

    async def stream(self, pcm):
        if self.failure:
            raise ValueError('synthetic_provider_failure')
        yield 'pcm', bytes(2880)
        if self.gate:
            await self.gate.wait()
        yield 'text', self.answer

    async def close(self):
        self.closed = True


class ASR:
    def __init__(self, *_):
        self.done = asyncio.Event()
        self.done.set()
        self.input_samples = 0

    def feed(self, pcm):
        self.input_samples += len(pcm) // 2

    def finish(self):
        pass


class Socket:
    def __init__(self):
        self.events, self.packets = [], []
        self.first_packet = asyncio.Event()

    async def send_json(self, event):
        self.events.append(event)

    async def send_bytes(self, packet):
        self.packets.append(packet)
        self.first_packet.set()


class StreamingTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.socket = Socket()
        self.session = Session(self.socket, {}, Path(self.directory.name))
        self.session.history.append(source())

    async def asyncTearDown(self):
        for task in list(self.session.tasks):
            task.cancel()
        await asyncio.gather(*self.session.tasks, return_exceptions=True)
        self.session.input_codec.close()
        self.directory.cleanup()

    async def reply(self, question, factory):
        turn = Turn(len(self.session.history) + 1)
        turn.user, turn.stopped_at = question, asyncio.get_running_loop().time()
        self.session.history.append(turn)
        self.session.output = turn
        with patch('server.StreamingNative', factory), patch('server.OutputASR', ASR):
            await self.session.reply(turn, bytes(640))
        return turn

    async def test_history_audio_is_released_before_generation_finishes(self):
        gate = asyncio.Event()
        native = Native('刚才数到八了。', gate)
        task = asyncio.create_task(self.reply('刚才数数数到几了？', lambda *_: native))
        try:
            await asyncio.wait_for(self.socket.first_packet.wait(), .3)
            self.assertFalse(task.done(), 'provider is still generating')
        finally:
            gate.set()
            await asyncio.wait_for(task, 1)

    async def test_prior_rejected_answers_are_delivered_without_rewrite(self):
        cases = [
            ('呃，刚才数数是数到几呢？', '刚才数到了九。', source('1234567八九', '九')),
            ('你刚才数数数到几了？我记不太清。', '刚才数到八了。', source()),
            ('嗯，刚才你数数过数吗？', '数过，刚才从一数到了八。', source()),
        ]
        for question, answer, previous in cases:
            with self.subTest(question=question):
                self.session.history = [previous]
                self.socket.events.clear()
                self.socket.packets.clear()
                native = Native(answer)
                with patch('server.StreamingNative', return_value=native) as factory:
                    turn = await self.reply(question, factory)
                self.assertEqual(factory.call_count, 1)
                self.assertTrue(self.socket.packets, 'no semantic veto may silence this turn')
                self.assertTrue(turn.ended)
                self.assertEqual(turn.generated, answer)
                self.assertNotIn({'type': 'status', 'code': 'turn_failed'}, self.socket.events)

    async def test_context_retains_whole_observation_not_only_tail(self):
        self.session.history[0] = source('好我慢慢数给你听一二三四五六七八')
        native = Native('你说的是刚才数数，还是另一个问题？')
        await self.reply('那后来呢？', lambda *_: native)
        context = json.loads(native.instructions.split('背景：', 1)[1])
        evidence = context['recent_turns'][0]['playback_evidence']
        self.assertEqual(evidence['spoken_text'], '好我慢慢数给你听一二三四五六七八')
        self.assertEqual(context['recent_turns'][0]['assistant_spoken_prefix'], evidence['spoken_text'])
        self.assertNotIn('九十十一十二', native.instructions)
        self.assertTrue(self.socket.packets)

    async def test_provider_failure_is_not_retried_and_next_turn_can_answer(self):
        with patch('server.StreamingNative', return_value=Native('', failure=True)) as factory:
            turn = await self.reply('刚才讲到哪里了？', factory)
        self.assertEqual(factory.call_count, 1)
        self.assertFalse(turn.ended)
        self.assertIn({'type': 'status', 'code': 'turn_failed'}, self.socket.events)
        self.assertFalse(self.session.closed)
        following = await self.reply('二加三等于几？', lambda *_: Native('等于五。'))
        self.assertTrue(following.ended)
        self.assertEqual(following.generated, '等于五。')


if __name__ == '__main__':
    unittest.main()
