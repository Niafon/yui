"""Worker entrypoint.

    python -m yui_worker --kind embeddings --port 8801
    python -m yui_worker --kind stt --port 8802 --model small
    python -m yui_worker --kind tts --port 8803
    python -m yui_worker --kind vision --port 8804

The core's supervisor starts these; running one by hand is only for debugging.
"""

from __future__ import annotations

import argparse
import logging
import signal
import sys
import threading

from .base import serve
from .workers.embeddings import EmbeddingsWorker
from .workers.stt import STTWorker
from .workers.tts import TTSWorker
from .workers.vision import VisionWorker
from .workers.computer import ComputerWorker

KINDS = {
    "computer": ComputerWorker,
    "embeddings": EmbeddingsWorker,
    "stt": STTWorker,
    "tts": TTSWorker,
    "vision": VisionWorker,
}


def build(kind: str, model: str, device: str, unload_after: int, embedding_dim: int = 0):
    """Vision is the one worker that must give VRAM back.

    On a 10 GB card the LLM stays resident, so the vision model is loaded on
    demand and unloaded after idle (ADR-016).
    """
    if kind not in KINDS:
        raise SystemExit(f"unknown worker kind: {kind} (expected one of {', '.join(KINDS)})")
    if kind == "embeddings":
        return EmbeddingsWorker(model_name=model, embedding_dim=embedding_dim, device=device)
    if kind == "stt":
        return STTWorker(model_name=model, device=device)
    if kind == "computer":
        return ComputerWorker(model_name=model or "microsoft/Fara1.5-4B")
    if kind == "vision":
        return VisionWorker(model_name=model, device=device, unload_after=unload_after or 120)
    return KINDS[kind](model_name=model)


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(prog="yui_worker")
    parser.add_argument("--kind", required=True, choices=sorted(KINDS))
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=0)
    parser.add_argument("--model", default="")
    parser.add_argument("--device", default="auto", choices=["auto", "cpu", "cuda"])
    parser.add_argument(
        "--embedding-dim", type=int, default=0,
        help="Matryoshka output dimension for embedding models (0 = native dimension)",
    )
    parser.add_argument(
        "--unload-after",
        type=int,
        default=0,
        help="выгрузить модель после N секунд простоя (0 — держать загруженной)",
    )
    parser.add_argument("--log-level", default="info")
    parser.add_argument("--stt-language", default="ru", help="empty string enables language detection")
    parser.add_argument("--stt-beam-size", type=int, default=5)
    parser.add_argument("--stt-hotwords", default="Юи, Yui")
    parser.add_argument("--stt-compute-type", default="auto")
    parser.add_argument("--computer-endpoint", default="http://127.0.0.1:11441/v1")
    parser.add_argument("--computer-max-rounds", type=int, default=20)
    args = parser.parse_args(argv)

    logging.basicConfig(
        level=getattr(logging, args.log_level.upper(), logging.INFO),
        format="%(asctime)s %(levelname)s %(name)s %(message)s",
    )

    worker = build(args.kind, args.model, args.device, args.unload_after, args.embedding_dim)
    if args.kind == "computer":
        worker.endpoint = args.computer_endpoint
        worker.max_rounds = max(1, min(50, args.computer_max_rounds))
    if args.kind == "stt":
        worker.language = args.stt_language
        worker.beam_size = max(1, min(10, args.stt_beam_size))
        worker.hotwords = args.stt_hotwords[:500]
        worker.compute_type = args.stt_compute_type
    httpd = serve(worker, args.host, args.port)
    host, port = httpd.server_address[:2]
    print(f"yui worker {worker.worker_id} ready on http://{host}:{port} ({worker.detail})", flush=True)

    stop = threading.Event()
    signal.signal(signal.SIGINT, lambda *_: stop.set())
    signal.signal(signal.SIGTERM, lambda *_: stop.set())
    stop.wait()
    httpd.shutdown()
    return 0


if __name__ == "__main__":
    sys.exit(main())
