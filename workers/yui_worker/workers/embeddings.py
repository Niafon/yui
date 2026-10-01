"""Embeddings worker.

Default backend is the dependency-free hash embedding shared with the core.
If sentence-transformers is installed, it is used instead and the dimension
changes accordingly — the index is rebuildable, so a model swap costs nothing
but a re-index (MEM-004).
"""

from __future__ import annotations

from typing import Any, Dict, List

from ..base import Worker
from ..hashing import hash_vector
from ..protocol import EmbedRequest

HASH_DIM = 256


class EmbeddingsWorker(Worker):
    def __init__(self, model_name: str = "", embedding_dim: int = 0, device: str = "auto") -> None:
        super().__init__("embeddings", "embeddings")
        self.model_name = model_name
        self.target_dim = max(0, int(embedding_dim))
        self.device = device
        self.model = None
        self.dim = self.target_dim or HASH_DIM
        self.op("embeddings")(self.handle)

    def load(self) -> None:
        if self.model_name:
            try:
                from sentence_transformers import SentenceTransformer  # type: ignore

                kwargs = {}
                if self.target_dim:
                    # SentenceTransformers exposes Matryoshka truncation here;
                    # Qwen3-Embedding is trained for variable output dimensions.
                    kwargs["truncate_dim"] = self.target_dim
                if self.device in {"cpu", "cuda"}:
                    kwargs["device"] = self.device
                self.model = SentenceTransformer(self.model_name, **kwargs)
                self.dim = int(self.model.get_sentence_embedding_dimension())
                if self.target_dim:
                    self.dim = self.target_dim
                self.detail = f"sentence-transformers:{self.model_name}"
            except Exception as exc:  # noqa: BLE001
                # Falling back is visible, not silent.
                self.detail = f"hash fallback ({exc.__class__.__name__}: {exc})"
        else:
            self.detail = "hash embeddings (no model configured)"
        self.ready = True

    def handle(self, payload: Dict[str, Any]) -> Dict[str, Any]:
        req = EmbedRequest.parse(payload)
        if self.model is not None:
            encode_kwargs = {"normalize_embeddings": True}
            # Qwen3-Embedding ships a query prompt in SentenceTransformers.
            # Documents intentionally remain unprompted, as recommended by
            # the model authors for retrieval. Other models simply ignore
            # this branch if they expose no named query prompt.
            prompts = getattr(self.model, "prompts", {}) or {}
            if req.purpose == "query" and "query" in prompts:
                encode_kwargs["prompt_name"] = "query"
            vectors: List[List[float]] = [
                [float(v) for v in row] for row in self.model.encode(req.texts, **encode_kwargs)
            ]
        else:
            vectors = [hash_vector(t, self.dim) for t in req.texts]
        return {"vectors": vectors, "dim": self.dim, "backend": self.detail}
