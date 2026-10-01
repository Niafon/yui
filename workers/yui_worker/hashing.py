"""Deterministic hash embeddings.

This is the fallback embedding used before a real model is configured. The Go
core implements the identical algorithm in internal/provider/mock.go, so
vectors produced on either side are comparable. Any change here must be made
there too.
"""

from __future__ import annotations

import math
from typing import List

FNV_OFFSET_32 = 0x811C9DC5
FNV_PRIME_32 = 0x01000193
MASK_32 = 0xFFFFFFFF


def fnv1a32(text: str) -> int:
    """FNV-1a over the UTF-8 bytes, matching Go's hash/fnv New32a."""
    h = FNV_OFFSET_32
    for byte in text.encode("utf-8"):
        h ^= byte
        h = (h * FNV_PRIME_32) & MASK_32
    return h


def _is_letter_or_digit(ch: str) -> bool:
    return ("a" <= ch <= "z") or ("0" <= ch <= "9") or ("а" <= ch <= "я") or ch == "ё"


def tokenize(text: str) -> List[str]:
    """Lowercase and split on anything that is not a letter or digit.

    Tokens of a single character are dropped, exactly as in the Go core.
    """
    tokens: List[str] = []
    current: List[str] = []
    for ch in text.lower():
        if _is_letter_or_digit(ch):
            current.append(ch)
        elif current:
            tokens.append("".join(current))
            current = []
    if current:
        tokens.append("".join(current))
    return [t for t in tokens if len(t) > 1]


def hash_vector(text: str, dim: int = 256) -> List[float]:
    """Map text to a normalised sparse vector."""
    vec = [0.0] * dim
    for token in tokenize(text):
        h = fnv1a32(token)
        idx = h % dim
        vec[idx] += 1.0 if h % 2 == 0 else -1.0
    norm = math.sqrt(sum(v * v for v in vec))
    if norm == 0:
        return vec
    return [v / norm for v in vec]


def cosine(a: List[float], b: List[float]) -> float:
    if len(a) != len(b) or not a:
        return 0.0
    dot = sum(x * y for x, y in zip(a, b))
    na = math.sqrt(sum(x * x for x in a))
    nb = math.sqrt(sum(y * y for y in b))
    if na == 0 or nb == 0:
        return 0.0
    return dot / (na * nb)
