"""Optional prepared speech, independent of the main SDK generation queue."""
import asyncio

from livekit import rtc


async def play_preparation(output, revision, index, pcm, settle, *, task_id, allow_start):
    finished = asyncio.get_running_loop().create_future()
    binding = None
    def observed(complete):
        settle(complete)
        if not finished.done():
            finished.set_result(complete)
    try:
        binding = output.bind_unit(revision, index, len(pcm) // 2, observed,
                                   task_id=task_id, allow_start=allow_start)
        for offset in range(0, len(pcm), 960):
            chunk = pcm[offset:offset + 960]
            await output.capture_frame(rtc.AudioFrame(chunk, 24000, 1, len(chunk) // 2))
        output.flush()
        await asyncio.shield(finished)
    except BaseException:
        # Retire the sender and seal any admitted PCM. Device receipts retain
        # the binding's independent lifetime, including a late full drain.
        if binding and binding.block_id is not None:
            output.clear_buffer()
        raise
    finally:
        if binding is None:
            observed(False)
        else:
            output.cancel_binding(binding)
