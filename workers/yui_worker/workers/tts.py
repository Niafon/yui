"""Text to speech worker.

The placeholder backend synthesises a short, quiet tone per sentence so the
client audio path (buffering, device routing, cancellation) can be tested
without a voice model. Chunks are returned in order; the core streams them to
the selected output device (VOICE-007, VOICE-010).
"""

from __future__ import annotations

import math
import html
import re
import struct
import threading
from typing import Any, Dict, List

from ..base import Worker
from ..protocol import AudioReply, SpeakRequest

SAMPLE_RATE = 16000
CHUNK_MS = 200


def tone_pcm16(duration_s: float, freq: float = 196.0, amplitude: float = 0.08) -> bytes:
    frames = int(SAMPLE_RATE * duration_s)
    out = bytearray()
    for i in range(frames):
        # Fade in and out so the placeholder does not click.
        env = min(1.0, i / 400.0, (frames - i) / 400.0)
        value = amplitude * env * math.sin(2 * math.pi * freq * i / SAMPLE_RATE)
        out += struct.pack("<h", int(value * 32767))
    return bytes(out)


def split_chunks(pcm: bytes, chunk_ms: int = CHUNK_MS, sample_rate: int = SAMPLE_RATE) -> List[bytes]:
    size = int(sample_rate * 2 * chunk_ms / 1000)
    return [pcm[i:i + size] for i in range(0, len(pcm), size)] or [b""]


class TTSWorker(Worker):
    def __init__(self, model_name: str = "") -> None:
        super().__init__("tts", "tts")
        self.model_name = model_name
        self.model = None
        self.backend = "placeholder"
        self.synthesis_lock = threading.Lock()
        self.op("tts")(self.handle)

    def load(self) -> None:
        if self.model_name:
            try:
                if self.model_name.endswith(".pt"):
                    import torch
                    torch.set_num_threads(4)
                    self.model = torch.package.PackageImporter(self.model_name).load_pickle("tts_models", "model")
                    self.model.to(torch.device("cpu"))
                    self.backend = "silero"
                    self.detail = f"silero:xenia:{self.model_name}"
                    self.ready = True
                    return
                if self.model_name.endswith(".onnx"):
                    from piper import PiperVoice
                    self.model = PiperVoice.load(self.model_name, use_cuda=False)
                    self.backend = "piper"
                    self.detail = f"piper:{self.model_name}"
                    self.ready = True
                    return
                from TTS.api import TTS  # type: ignore

                self.model = TTS(self.model_name)
                self.detail = f"coqui:{self.model_name}"
            except Exception as exc:  # noqa: BLE001
                self.model = None
                self.ready = False
                self.detail = f"voice unavailable ({exc.__class__.__name__}: {exc})"
                return
        else:
            self.detail = "placeholder tone (no voice model configured)"
        self.ready = True

    def handle(self, payload: Dict[str, Any]) -> Dict[str, Any]:
        req = SpeakRequest.parse(payload)
        if not self.ready:
            raise RuntimeError(self.detail)
        if self.model is not None and self.backend == "silero":
            # The configured Russian Silero voice rejects fragments whose
            # normalisation leaves no Cyrillic speech (emoji, Markdown, Latin).
            # Such fragments are common at streamed sentence boundaries.
            text = html.unescape(req.text).replace("ё", "е").replace("Ё", "Е")
            if not re.search(r"[а-яА-Я]", text):
                return AudioReply(chunks=[], sample_rate=48000).to_json()
            import torch
            speaker = "xenia" if req.voice == "default" else req.voice
            if speaker not in self.model.speakers:
                speaker = "xenia"
            with self.synthesis_lock:
                wav = self.model.apply_tts(text=text, speaker=speaker, sample_rate=48000)
            pcm = (wav.clamp(-1, 1) * 32767).to(dtype=torch.int16).numpy().tobytes()
            return AudioReply(chunks=split_chunks(pcm, sample_rate=48000), sample_rate=48000).to_json()
        if self.model is not None and self.backend == "piper":
            from piper import SynthesisConfig
            config = SynthesisConfig(length_scale=1.0 / max(0.5, min(2.0, req.speed)))
            audio = list(self.model.synthesize(req.text, syn_config=config))
            rate = self.model.config.sample_rate
            pcm = b"".join(chunk.audio_int16_bytes for chunk in audio)
            return AudioReply(chunks=split_chunks(pcm, sample_rate=rate), sample_rate=rate).to_json()
        if self.model is not None:
            wav = self.model.tts(text=req.text, speaker=req.voice or None)
            pcm = b"".join(struct.pack("<h", int(max(-1.0, min(1.0, s)) * 32767)) for s in wav)
            rate = int(self.model.synthesizer.output_sample_rate)
            return AudioReply(chunks=split_chunks(pcm, sample_rate=rate), sample_rate=rate).to_json()

        # Emotion shifts the placeholder pitch, so the client can hear that the
        # emotional state actually reaches synthesis.
        freq = {"joy": 262.0, "warm": 220.0, "concern": 165.0, "sad": 147.0}.get(req.emotion, 196.0)
        seconds = max(0.4, min(6.0, len(req.text) / 14.0 / max(0.5, req.speed)))
        pcm = tone_pcm16(seconds, freq=freq)
        return AudioReply(chunks=split_chunks(pcm), sample_rate=SAMPLE_RATE).to_json()
