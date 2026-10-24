import asyncio
import base64
import json
import tempfile
import unittest

from aiohttp import web

from family_voice.config import Settings
from family_voice.runtime import VoiceSession


class RuntimeTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.events = []
        self.hold_final = False
        self.final_requested = asyncio.Event()
        self.release_final = asyncio.Event()
        self.old_asr_closed = asyncio.Event()
        self.asr_count = 0
        app = web.Application()
        app.router.add_get('/asr', self.asr)
        app.router.add_get('/tts', self.tts)
        app.router.add_post('/v1/chat/completions', self.llm)
        self.runner = web.AppRunner(app)
        await self.runner.setup()
        site = web.TCPSite(self.runner, '127.0.0.1', 0)
        await site.start()
        port = site._server.sockets[0].getsockname()[1]
        cfg = Settings.from_env({'DASHSCOPE_API_KEY': 'synthetic-key', 'VOICE_ASR_URL': f'ws://127.0.0.1:{port}/asr', 'VOICE_TTS_URL': f'ws://127.0.0.1:{port}/tts', 'VOICE_LLM_URL': f'http://127.0.0.1:{port}/v1', 'VOICE_AUDIT_DIR': self.temp.name})
        async def send(event):
            self.events.append(event)
            if event['type'] == 'audio.end':
                self.voice.receipt({**event, 'played_samples': event['sent_samples'], 'valid': True, 'finished': True})
        self.voice = VoiceSession(cfg, send)
        await self.voice.start()

    async def asyncTearDown(self):
        await self.voice.close()
        await self.runner.cleanup()
        self.temp.cleanup()

    async def asr(self, request):
        self.asr_count += 1
        number = self.asr_count
        ws = web.WebSocketResponse()
        await ws.prepare(request)
        task = (await ws.receive_json())['header']['task_id']
        await ws.send_json({'header': {'event': 'task-started', 'task_id': task}})
        async for message in ws:
            if message.type == web.WSMsgType.TEXT:
                self.final_requested.set()
                if self.hold_final and number == 1:
                    closed = asyncio.create_task(ws.receive())
                    released = asyncio.create_task(self.release_final.wait())
                    done, pending = await asyncio.wait((closed, released), return_when=asyncio.FIRST_COMPLETED)
                    for job in pending:
                        job.cancel()
                    await asyncio.gather(*pending, return_exceptions=True)
                    if closed in done:
                        self.old_asr_closed.set()
                        await ws.close()
                        return ws
                await ws.send_json({'header': {'event': 'result-generated', 'task_id': task}, 'payload': {'output': {'sentence': {'text': '一加一等于几？', 'sentence_end': True}}}})
                await ws.send_json({'header': {'event': 'task-finished', 'task_id': task}})
                break
        await ws.close()
        return ws

    async def tts(self, request):
        ws = web.WebSocketResponse()
        await ws.prepare(request)
        await ws.send_json({'type': 'session.created'})
        await ws.receive_json()
        await ws.send_json({'type': 'session.updated'})
        await ws.receive_json()
        await ws.receive_json()
        await ws.send_json({'type': 'response.audio.delta', 'response_id': 'r', 'delta': base64.b64encode(b'\x01\x00' * 480).decode()})
        await ws.send_json({'type': 'response.done', 'response': {'id': 'r', 'status': 'completed'}})
        await ws.close()
        return ws

    async def llm(self, request):
        body = await request.json()
        self.assertEqual(body['model'], 'qwen-plus')
        response = web.StreamResponse(headers={'Content-Type': 'text/event-stream'})
        await response.prepare(request)
        for delta, finish in (({'role': 'assistant', 'content': '2'}, None), ({}, 'stop')):
            data = {'id': 'chat-test', 'created': 1, 'model': 'qwen-plus', 'object': 'chat.completion.chunk', 'choices': [{'index': 0, 'delta': delta, 'finish_reason': finish}]}
            await response.write(('data: ' + json.dumps(data) + '\n\n').encode())
        await response.write(b'data: [DONE]\n\n')
        return response

    async def test_real_agent_session_connects_all_three_provider_boundaries(self):
        await self.voice.begin_input()
        self.voice.feed(b'\0' * 640)
        await self.voice.commit()
        async with asyncio.timeout(5):
            while not any(e['type'] == 'reply' for e in self.events):
                await asyncio.sleep(.01)
        self.assertTrue(any(e['type'] == 'transcript' and e.get('final') for e in self.events), [(e['type'], e.get('text'), e.get('final')) for e in self.events])
        self.assertTrue(any(e['type'] == 'audio' for e in self.events))
        self.assertTrue(any(e['type'] == 'reply' and '2' in e['text'] for e in self.events))
        await asyncio.wait_for(self.voice.interrupt(), .5)

    async def test_cancel_while_asr_final_is_pending_cannot_generate_old_answer(self):
        self.hold_final = True
        await self.voice.begin_input()
        self.voice.feed(b'\0' * 640)
        commit = asyncio.create_task(self.voice.commit())
        try:
            try:
                await asyncio.wait_for(self.final_requested.wait(), 2)
            except TimeoutError:
                from pathlib import Path
                details = [json.loads(p.read_text()) for p in Path(self.temp.name).glob('*.json')]
                self.fail(f'ASR finish not reached; audit={details}; commit_done={commit.done()}')
            await self.voice.interrupt()
            self.assertTrue(commit.cancelled(), 'cancel must retire the pending input commit')
            await asyncio.wait_for(self.old_asr_closed.wait(), .5)
            # The first provider terminal is never released. A fresh turn must
            # still reach ASR/LLM/TTS on the same AgentSession.
            await self.voice.begin_input()
            self.voice.feed(b'\0' * 640)
            await asyncio.wait_for(self.voice.commit(), 3)
            async with asyncio.timeout(3):
                while not any(e['type'] == 'reply' for e in self.events):
                    await asyncio.sleep(.01)
            self.assertTrue(any(e['type'] == 'reply' and '2' in e['text'] for e in self.events))
        finally:
            self.release_final.set()
            if not commit.done():
                commit.cancel()
            await asyncio.gather(commit, return_exceptions=True)


if __name__ == '__main__':
    unittest.main()
