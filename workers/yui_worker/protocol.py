"""Worker wire contract.

The transport is JSON over loopback HTTP (SRS 18.3). Field names match the
protobuf messages in contracts/proto so the transport can be swapped for gRPC
without changing the workers.
"""

from __future__ import annotations

import base64
from dataclasses import dataclass, field
from typing import Any, Dict, List, Optional

PROTOCOL_VERSION = 1


class ProtocolError(ValueError):
    """Raised when a request cannot be understood. Answered with HTTP 400."""


def _require(payload: Dict[str, Any], key: str) -> Any:
    if key not in payload:
        raise ProtocolError(f"missing field: {key}")
    return payload[key]


def decode_audio(payload: Dict[str, Any], key: str = "audio_b64") -> bytes:
    raw = payload.get(key) or ""
    try:
        return base64.b64decode(raw)
    except Exception as exc:  # noqa: BLE001 - surfaced to the caller as 400
        raise ProtocolError(f"{key} is not valid base64") from exc


@dataclass
class TranscribeRequest:
    audio: bytes
    sample_rate: int = 16000
    channels: int = 1
    fmt: str = "pcm16"
    language: str = ""
    final: bool = True
    session_id: str = ""

    @classmethod
    def parse(cls, payload: Dict[str, Any]) -> "TranscribeRequest":
        return cls(
            audio=decode_audio(payload),
            sample_rate=int(payload.get("sample_rate") or 16000),
            channels=int(payload.get("channels") or 1),
            fmt=payload.get("format") or "pcm16",
            language=payload.get("language") or "",
            final=bool(payload.get("final", True)),
            session_id=payload.get("session_id") or "",
        )

    @property
    def duration_seconds(self) -> float:
        frame = max(1, self.sample_rate * self.channels * 2)
        return len(self.audio) / frame


@dataclass
class Transcript:
    text: str
    confidence: float = 0.0
    final: bool = True
    language: str = ""
    speaker_is_owner: Optional[bool] = None

    def to_json(self) -> Dict[str, Any]:
        body: Dict[str, Any] = {
            "text": self.text,
            "confidence": self.confidence,
            "final": self.final,
            "language": self.language,
        }
        if self.speaker_is_owner is not None:
            body["speaker_is_owner"] = self.speaker_is_owner
        return body


@dataclass
class SpeakRequest:
    text: str
    voice: str = "default"
    emotion: str = "neutral"
    speed: float = 1.0
    fmt: str = "pcm16"

    @classmethod
    def parse(cls, payload: Dict[str, Any]) -> "SpeakRequest":
        text = str(_require(payload, "text"))
        if not text.strip():
            raise ProtocolError("text is empty")
        return cls(
            text=text,
            voice=payload.get("voice") or "default",
            emotion=payload.get("emotion") or "neutral",
            speed=float(payload.get("speed") or 1.0),
            fmt=payload.get("format") or "pcm16",
        )


@dataclass
class AudioReply:
    chunks: List[bytes] = field(default_factory=list)
    sample_rate: int = 16000

    def to_json(self) -> Dict[str, Any]:
        return {
            "sample_rate": self.sample_rate,
            "chunks": [
                {"seq": i, "audio_b64": base64.b64encode(c).decode("ascii")}
                for i, c in enumerate(self.chunks)
            ],
        }


@dataclass
class VisionRequest:
    image: bytes
    mime: str = "image/jpeg"
    question: str = ""
    want_ocr: bool = False

    @classmethod
    def parse(cls, payload: Dict[str, Any]) -> "VisionRequest":
        image = decode_audio(payload, "image_b64")
        if not image:
            raise ProtocolError("image_b64 is empty")
        return cls(
            image=image,
            mime=payload.get("mime") or "image/jpeg",
            question=payload.get("question") or "",
            want_ocr=bool(payload.get("want_ocr", False)),
        )


@dataclass
class VisionResult:
    description: str
    objects: List[str] = field(default_factory=list)
    ocr_text: str = ""
    confidence: float = 0.0

    def to_json(self) -> Dict[str, Any]:
        return {
            "description": self.description,
            "objects": self.objects,
            "ocr_text": self.ocr_text,
            "confidence": self.confidence,
        }


@dataclass
class EmbedRequest:
    texts: List[str]
    purpose: str = ""

    @classmethod
    def parse(cls, payload: Dict[str, Any]) -> "EmbedRequest":
        texts = _require(payload, "texts")
        if not isinstance(texts, list):
            raise ProtocolError("texts must be an array")
        purpose = str(payload.get("purpose") or "")
        if purpose not in {"", "query", "document"}:
            raise ProtocolError("purpose must be query or document")
        return cls(texts=[str(t) for t in texts], purpose=purpose)
