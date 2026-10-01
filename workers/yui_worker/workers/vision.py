"""Vision worker.

The placeholder reads image dimensions straight from the file header, so it
reports something true about the frame without any dependency. A real backend
(a VLM plus an OCR engine) plugs in behind the same contract.
"""

from __future__ import annotations

import struct
import time
from typing import Any, Dict, Optional, Tuple

from ..base import Worker
from ..protocol import VisionRequest, VisionResult


def image_size(data: bytes) -> Optional[Tuple[int, int]]:
    """Return (width, height) for PNG, GIF and JPEG without an image library."""
    if data[:8] == b"\x89PNG\r\n\x1a\n" and len(data) >= 24:
        width, height = struct.unpack(">II", data[16:24])
        return int(width), int(height)
    if data[:6] in (b"GIF87a", b"GIF89a") and len(data) >= 10:
        width, height = struct.unpack("<HH", data[6:10])
        return int(width), int(height)
    if data[:2] == b"\xff\xd8":  # JPEG: walk the segment markers
        i = 2
        while i + 9 < len(data):
            if data[i] != 0xFF:
                i += 1
                continue
            marker = data[i + 1]
            if marker in (0xC0, 0xC1, 0xC2, 0xC3):
                height, width = struct.unpack(">HH", data[i + 5:i + 9])
                return int(width), int(height)
            if i + 4 > len(data):
                break
            length = struct.unpack(">H", data[i + 2:i + 4])[0]
            i += 2 + length
    return None


class VisionWorker(Worker):
    """Vision is loaded on demand and released after idle.

    On a 10 GB card the LLM is resident, so a vision model that stays loaded
    would push the whole pipeline into shared memory and collapse throughput
    (ADR-016, risk R-03).
    """

    def __init__(self, model_name: str = "", device: str = "auto", unload_after: int = 120) -> None:
        super().__init__("vision", "vision")
        self.model_name = model_name
        self.device = device
        self.unload_after = unload_after
        self.model = None
        self._last_used = 0.0
        self.op("vision")(self.handle)

    def load(self) -> None:
        # Nothing is loaded at startup on purpose; readiness means "can accept
        # a frame", not "weights are in VRAM".
        self.detail = (
            f"on-demand ({self.model_name}, unload after {self.unload_after}s)"
            if self.model_name
            else "placeholder (no vision model configured)"
        )
        self.ready = True

    def _ensure_model(self) -> None:
        if not self.model_name or self.model is not None:
            return
        try:
            from transformers import AutoModelForVision2Seq, AutoProcessor  # type: ignore

            self.processor = AutoProcessor.from_pretrained(self.model_name)
            self.model = AutoModelForVision2Seq.from_pretrained(self.model_name)
            self.detail = f"loaded {self.model_name}"
        except Exception as exc:  # noqa: BLE001
            self.model = None
            self.detail = f"placeholder ({exc.__class__.__name__}: {exc})"

    def release(self) -> None:
        """Called by the idle sweep and after a burst of frames."""
        if self.model is None:
            return
        self.model = None
        self.detail = f"unloaded after {self.unload_after}s idle"

    def maybe_release(self) -> None:
        if self.unload_after and self._last_used and time.time() - self._last_used > self.unload_after:
            self.release()

    def handle(self, payload: Dict[str, Any]) -> Dict[str, Any]:
        req = VisionRequest.parse(payload)
        self._last_used = time.time()
        self._ensure_model()
        size = image_size(req.image)
        if size:
            description = f"[vision placeholder] кадр {size[0]}x{size[1]}, {len(req.image)} байт"
        else:
            description = f"[vision placeholder] кадр {len(req.image)} байт, формат {req.mime}"
        if req.question:
            description += f"; вопрос: {req.question}"
        return VisionResult(description=description, confidence=0.1).to_json()
