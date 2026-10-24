"""Single-segment SDK contract probe; not a production audio adapter."""

from livekit import rtc
from livekit.agents.voice import io


class ReceiptProbe(io.AudioOutput):
    """No network, PCM retention, timers or device access."""

    def __init__(self):
        super().__init__(
            label="device-receipt-probe",
            capabilities=io.AudioOutputCapabilities(pause=False),
            sample_rate=24000,
        )
        self.block_id = 0
        self.sent_samples = 0
        self._live = False
        self._sealed = False
        self._interrupted = False
        self._invalid = False

    async def capture_frame(self, frame: rtc.AudioFrame):
        if frame.sample_rate != 24000 or frame.num_channels != 1:
            raise ValueError("expected mono 24kHz frame")
        if self._live and self._sealed:
            raise RuntimeError("probe supports only one pending segment")
        if not self._live:
            self.block_id += 1
            self.sent_samples = 0
            self._live = True
            self._sealed = self._interrupted = self._invalid = False
        await super().capture_frame(frame)
        self.sent_samples += frame.samples_per_channel

    def flush(self):
        super().flush()
        if self._live:
            self._sealed = True

    def clear_buffer(self):
        if self._live:
            self._interrupted = True
            self.flush()

    def receipt(self, block_id, played_samples, *, valid=True):
        if not self._live or block_id != self.block_id:
            return False
        if not valid:
            self._invalid = True
            return False
        if self._invalid or not self._sealed:
            return False
        if type(played_samples) is not int or not 0 <= played_samples <= self.sent_samples:
            return False
        if not self._interrupted and played_samples != self.sent_samples:
            return False
        self._live = False
        self.on_playback_finished(
            playback_position=played_samples / 24000,
            interrupted=self._interrupted,
            synchronized_transcript=None,
        )
        return True
