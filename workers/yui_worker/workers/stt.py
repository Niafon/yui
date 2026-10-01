"""Speech to text worker.

Backends, in order of preference:
  1. faster-whisper, when installed and a model is configured
  2. a placeholder transcript that describes the audio it received

The placeholder exists so the whole voice path — capture, streaming, turn
handling, memory write-back — can be exercised before any model is downloaded.
"""

from __future__ import annotations

import io
import math
import threading
import wave
from typing import Any, Dict

from ..base import Worker
from ..protocol import ProtocolError, Transcript, TranscribeRequest


def pcm16_to_wav(pcm: bytes, sample_rate: int, channels: int) -> bytes:
    buf = io.BytesIO()
    with wave.open(buf, "wb") as wav:
        wav.setnchannels(max(1, channels))
        wav.setsampwidth(2)
        wav.setframerate(sample_rate)
        wav.writeframes(pcm)
    return buf.getvalue()


def rms_level(pcm: bytes) -> float:
    """Rough loudness, used by the placeholder to tell speech from silence."""
    if len(pcm) < 2:
        return 0.0
    total = 0
    count = 0
    for i in range(0, len(pcm) - 1, 2):
        sample = int.from_bytes(pcm[i:i + 2], "little", signed=True)
        total += sample * sample
        count += 1
    if count == 0:
        return 0.0
    return (total / count) ** 0.5 / 32768.0


class STTWorker(Worker):
    def __init__(self, model_name: str = "", device: str = "auto", *, language: str = "ru",
                 beam_size: int = 5, hotwords: str = "Юи, Yui", compute_type: str = "auto") -> None:
        super().__init__("stt", "stt")
        self.model_name = model_name
        self.device = device
        self.model = None
        self.language = language
        self.beam_size = max(1, min(10, beam_size))
        self.hotwords = hotwords[:500]
        self.compute_type = compute_type
        self._lock = threading.Lock()
        self.op("stt")(self.handle)

    def load(self) -> None:
        if self.model_name:
            try:
                from faster_whisper import WhisperModel  # type: ignore

                device = self.device
                if device == "auto":
                    import ctranslate2
                    device = "cuda" if ctranslate2.get_cuda_device_count() else "cpu"
                compute = self.compute_type
                if compute == "auto":
                    compute = "int8" if device == "cpu" else "int8_float16"
                try:
                    self.model = WhisperModel(self.model_name, device=device,
                        compute_type=compute, cpu_threads=4)
                except Exception:
                    if self.device != "auto" or device == "cpu":
                        raise
                    device = "cpu"
                    self.model = WhisperModel(self.model_name, device="cpu", compute_type="int8", cpu_threads=4)
                self.detail = f"faster-whisper:{self.model_name} ({device}, VAD)"
            except Exception as exc:  # noqa: BLE001
                self.detail = f"model unavailable ({exc.__class__.__name__})"
                self.ready = False
                return
        else:
            self.detail = "placeholder (no model configured)"
        self.ready = True

    def handle(self, payload: Dict[str, Any]) -> Dict[str, Any]:
        req = TranscribeRequest.parse(payload)
        if req.fmt != "pcm16" or req.sample_rate not in (8000, 16000, 22050, 24000, 32000, 44100, 48000) or req.channels not in (1, 2):
            raise ProtocolError("STT requires pcm16, 8–48 kHz, mono or stereo")
        if len(req.audio) % (2 * req.channels) or req.duration_seconds > 120:
            raise ProtocolError("invalid PCM frame length or audio longer than 120 seconds")
        if self.model_name and self.model is None:
            raise RuntimeError("configured STT model is unavailable")
        language = req.language or self.language or None
        if req.duration_seconds < 0.08 or rms_level(req.audio) < 0.0001:
            return Transcript(text="", final=req.final, language=language or "").to_json()
        if self.model is not None:
            wav = pcm16_to_wav(req.audio, req.sample_rate, req.channels)
            # A generator performs inference while consumed: keep the lock for
            # the whole decode so simultaneous clients cannot oversubscribe CPU.
            with self._lock:
                segments, info = self.model.transcribe(
                    io.BytesIO(wav), language=language, beam_size=self.beam_size if req.final else 1,
                    temperature=0.0, vad_filter=True,
                    vad_parameters={"min_silence_duration_ms": 350, "speech_pad_ms": 200},
                    condition_on_previous_text=False, hotwords=self.hotwords or None,
                    no_speech_threshold=0.6, log_prob_threshold=-1.0,
                    compression_ratio_threshold=2.4,
                )
                accepted = [seg for seg in segments if seg.text.strip()
                            and seg.no_speech_prob < 0.6 and seg.avg_logprob >= -1.0
                            and seg.compression_ratio <= 2.4]
            text = " ".join(seg.text.strip() for seg in accepted).strip()
            weights = [max(0.01, seg.end - seg.start) for seg in accepted]
            confidence = (sum(math.exp(min(0, seg.avg_logprob)) * weight for seg, weight in zip(accepted, weights))
                          / sum(weights)) if weights else 0.0
            return Transcript(
                text=text,
                confidence=confidence,
                final=req.final,
                language=getattr(info, "language", req.language),
            ).to_json()

        level = rms_level(req.audio)
        text = ""
        if level > 0.01:
            text = f"[stt placeholder] речь {req.duration_seconds:.1f}с, уровень {level:.2f}"
        return Transcript(
            text=text,
            confidence=0.2 if text else 0.0,
            final=req.final,
            language=req.language or "ru",
            # Owner verification is a separate model; unknown means the core
            # treats the speech as owner input only in single user MVP.
            speaker_is_owner=None,
        ).to_json()
