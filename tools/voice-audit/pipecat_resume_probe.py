"""Offline real-Pipecat frame/context probe; no credentials/network/device access.

Pipecat classes are unmodified. The harness replaces only external I/O and
processor wiring, using the same setup approach as upstream output tests.
The sink accepts eight 100 ms synthetic PCM chunks and blocks chunk nine.
This proves frame/context behavior, not physical audibility or model behavior.
"""

import argparse
import subprocess
import asyncio
import copy
import json
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import AsyncMock, patch

from loguru import logger

from pipecat.clocks.system_clock import SystemClock
from pipecat.frames.frames import (
    AggregatedTextFrame,
    CancelFrame,
    InterruptionFrame,
    LLMFullResponseEndFrame,
    LLMFullResponseStartFrame,
    LLMTextFrame,
    StartFrame,
    TTSAudioRawFrame,
    TTSTextFrame,
)
from pipecat.processors.aggregators.llm_context import LLMContext
from pipecat.processors.aggregators.llm_response_universal import LLMAssistantAggregator
from pipecat.processors.frame_processor import FrameDirection, FrameProcessorSetup
from pipecat.services.openai.realtime.llm import (
    CurrentAudioResponse,
    OpenAIRealtimeLLMService,
)
from pipecat.services.tts_service import TTSService
from pipecat.transports.base_output import BaseOutputTransport
from pipecat.transports.base_transport import TransportParams
from pipecat.utils.asyncio.task_manager import TaskManager
from pipecat.utils.context.aggregated_frame_sequencer import AggregatedFrameSequencer
from pipecat.utils.context.word_timestamp_tracker import WordTimestampTracker
from pipecat.utils.text.base_text_aggregator import TextType

OUT = None
FULL = " ".join(str(i) for i in range(1, 51))
DOWN = FrameDirection.DOWNSTREAM


class OfflineProviderTTS(TTSService):
    """Only fills the abstract external-provider hook; never synthesizes."""

    async def run_tts(self, text, context_id):
        raise AssertionError("No provider call is allowed")
        yield  # pragma: no cover


async def wait_until(predicate, timeout=3.0):
    async with asyncio.timeout(timeout):
        while not predicate():
            await asyncio.sleep(0.002)


async def scenario(mode):
    clock = SystemClock()
    clock.start()
    tasks = TaskManager()
    setup = FrameProcessorSetup(
        clock=clock,
        task_manager=tasks,
        audio_out_sample_rate=16000,
        pipeline_worker=SimpleNamespace(app_resources=None),
    )
    context = LLMContext([{"role": "user", "content": "从1数到50"}])
    agg = LLMAssistantAggregator(context)
    agg.push_frame = AsyncMock()
    await agg.setup(setup)
    delivered_text = []
    accepted_chunks = []
    ninth_entered = asyncio.Event()
    sink_gate = asyncio.Event()
    transport = BaseOutputTransport(
        TransportParams(
            audio_out_enabled=True,
            audio_out_sample_rate=16000,
            audio_out_10ms_chunks=10,
        )
    )

    async def forward(frame, direction=DOWN):
        if direction == DOWN:
            if isinstance(frame, TTSTextFrame):
                delivered_text.append(frame.raw_text or frame.text)
            await agg.process_frame(frame, direction)

    async def write_audio(frame):
        number = int.from_bytes(frame.audio[:2], "little")
        if number >= 9:
            ninth_entered.set()
            await sink_gate.wait()
        await asyncio.sleep(0.2 if mode == "word_timestamps_sink_lag" else 0.1)
        accepted_chunks.append(number)
        return True

    transport.push_frame = forward
    transport.write_audio_frame = write_audio
    tts = OfflineProviderTTS()

    async def append_to_audio_context(context_id, frame):
        await transport.process_frame(frame, DOWN)

    tts.append_to_audio_context = append_to_audio_context
    await transport.setup(setup)
    start = StartFrame(audio_out_sample_rate=16000)
    await transport.process_frame(start, DOWN)
    await transport.set_transport_ready(start)
    await transport.process_frame(LLMFullResponseStartFrame(), DOWN)
    await wait_until(lambda: bool(agg._assistant_turn_start_timestamp))

    seq = AggregatedFrameSequencer()
    tracker = WordTimestampTracker()
    baseline = clock.get_time()
    tracker.start(baseline)
    if mode.startswith("word_timestamps"):
        src = AggregatedTextFrame(FULL, TextType.SENTENCE)
        await seq.register_spoken(src, "count", FULL, append_to_context=True)
        # Real framework tracker + sequencer create the timed word frames.
        for i in range(1, 51):
            for frame in seq.process_word(str(i), tracker.pts(i * 0.1), "count"):
                await transport.process_frame(frame, DOWN)
    elif mode == "realtime_transcript_ahead_without_timestamps":
        # This is OpenAI's upstream service, NOT a Qwen adapter or model call.
        service = OpenAIRealtimeLLMService(api_key="unused-offline-placeholder")

        # The actual upstream method calls push_frame(frame) with no direction.
        async def service_forward(frame, direction=DOWN):
            await transport.process_frame(frame, direction)

        service.push_frame = service_forward
        await service._push_output_transcript_text_frames(FULL)

    # Synthetic PCM contains labels rather than speech; no raw audio is saved.
    for i in range(1, 51):
        pcm = i.to_bytes(2, "little") * 1600
        await transport.process_frame(TTSAudioRawFrame(pcm, 16000, 1), DOWN)
        if mode == "sentence_5_without_timestamps" and i % 5 == 0:
            text = " ".join(str(n) for n in range(i - 4, i + 1))
            src = AggregatedTextFrame(text, TextType.SENTENCE)
            await tts._push_spoken_text(src, text, f"count-{i}", True)
    if mode == "sentence_50_without_timestamps":
        # Upstream non-word TTS queues the segment text after segment audio.
        src = AggregatedTextFrame(FULL, TextType.SENTENCE)
        await tts._push_spoken_text(src, FULL, "count", True)
    await transport.process_frame(LLMFullResponseEndFrame(), DOWN)
    try:
        await asyncio.wait_for(ninth_entered.wait(), timeout=3)
        if mode.startswith("word_timestamps"):
            expected_words = 16 if mode == "word_timestamps_sink_lag" else 8
            await wait_until(lambda: len(delivered_text) >= expected_words)
        assert accepted_chunks == list(range(1, 9)), accepted_chunks
        await transport.process_frame(InterruptionFrame(), DOWN)
        before_math = copy.deepcopy(context.get_messages())
        # Inputs and math answer are deterministic frames, never a model call.
        context.add_message({"role": "user", "content": "一加一等于几？"})
        await agg.process_frame(LLMFullResponseStartFrame(), DOWN)
        await agg.process_frame(LLMTextFrame("2"), DOWN)
        await agg.process_frame(LLMFullResponseEndFrame(), DOWN)
        context.add_message({"role": "user", "content": "继续"})
        result = {
            "mode": mode,
            "generated_text": FULL,
            "simulated_sink_accepted_chunks": accepted_chunks[:],
            "delivered_tts_text_before_interrupt": delivered_text[:],
            "context_after_interruption": before_math,
            "context_at_continue": copy.deepcopy(context.get_messages()),
            "physical_audio_or_model_call": False,
        }
        if mode.startswith("word_timestamps"):
            expected = " ".join(str(i) for i in range(1, expected_words + 1))
            assert before_math[-1]["content"] == expected, before_math
            seq.clear()
            assert seq.process_word("9", tracker.pts(0.9), "count") == []
            result["late_word_after_sequencer_clear"] = []
        elif mode == "sentence_50_without_timestamps":
            assert not any(m["role"] == "assistant" for m in before_math)
        elif mode == "sentence_5_without_timestamps":
            assert before_math[-1]["content"] == "1 2 3 4 5", before_math
        else:
            assert before_math[-1]["content"] == FULL, before_math
        return result
    finally:
        await transport.cancel(CancelFrame())
        await transport.cleanup()
        await agg.cleanup()


async def upstream_truncation():
    service = OpenAIRealtimeLLMService(api_key="unused-offline-placeholder")
    service.send_client_event = AsyncMock()
    service._current_audio_response = CurrentAudioResponse(
        item_id="simulated-item",
        content_index=0,
        start_time_ms=1000,
        total_size=24000 * 2 * 5,
    )
    with patch("pipecat.services.openai.realtime.llm.time.time", return_value=1.8):
        await service._truncate_current_audio_response()
    event = service.send_client_event.call_args.args[0]
    assert event.audio_end_ms == 800
    assert service._current_audio_response is None
    return {
        "client_event": event.model_dump(),
        "remote_server_ack_or_remote_context_verified": False,
        "method_rewrites_local_llm_context": False,
    }


async def main():
    logger.remove()
    results = [
        await scenario(mode)
        for mode in (
            "word_timestamps",
            "word_timestamps_sink_lag",
            "sentence_50_without_timestamps",
            "sentence_5_without_timestamps",
            "realtime_transcript_ahead_without_timestamps",
        )
    ]
    payload = {
        "upstream_commit": "3dfcd17a112fba71c66f5b8ed5929eb2a07e74e9",
        "cases": results,
        "openai_realtime_truncation": await upstream_truncation(),
    }
    OUT.mkdir(parents=True, exist_ok=True)
    (OUT / "frame-context-results.json").write_text(
        json.dumps(payload, ensure_ascii=False, indent=2)
    )
    print(json.dumps(payload, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source-root", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    commit = subprocess.check_output(
        ["git", "-C", str(args.source_root), "rev-parse", "HEAD"], text=True
    ).strip()
    assert commit == "3dfcd17a112fba71c66f5b8ed5929eb2a07e74e9"
    import pipecat

    assert Path(pipecat.__file__).resolve().is_relative_to(args.source_root.resolve())
    assert not args.out.exists()
    OUT = args.out
    asyncio.run(main())
