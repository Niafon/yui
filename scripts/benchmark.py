#!/usr/bin/env python3
"""Замер задержки голосового цикла на целевом ПК.

Это первый шаг плана из docs/decisions.md: пока цифр нет, выбор моделей
остаётся предположением. Скрипт прогоняет реальные turn-ы через API ядра и
показывает разброс, а не среднее — среднее скрывает именно те задержки,
которые слышны в разговоре.

    python scripts/benchmark.py --token <токен> --runs 20
"""

from __future__ import annotations

import argparse
import json
import statistics
import time
import urllib.request

PHRASES = [
    "Привет, как дела?",
    "Напомни, что я говорил про проект на прошлой неделе",
    "Который час и что у меня сегодня?",
    "Расскажи коротко, что ты умеешь",
    "Запомни, что я пью чай без сахара",
]


def call(url: str, token: str, payload: dict | None) -> tuple[dict, float]:
    data = json.dumps(payload).encode() if payload is not None else None
    req = urllib.request.Request(
        url,
        data=data,
        headers={"Content-Type": "application/json", "Authorization": f"Bearer {token}"},
        method="POST" if data is not None else "GET",
    )
    started = time.perf_counter()
    with urllib.request.urlopen(req, timeout=120) as resp:
        body = json.loads(resp.read())
    return body, (time.perf_counter() - started) * 1000


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--base", default="http://127.0.0.1:8765")
    parser.add_argument("--token", required=True)
    parser.add_argument("--runs", type=int, default=20)
    args = parser.parse_args()

    status, _ = call(f"{args.base}/v1/status", args.token, None)
    defaults = {
        p["info"]["kind"]: p["info"].get("model") or p["info"]["id"]
        for p in status.get("providers", [])
        if p.get("is_default")
    }
    print("Провайдеры:", ", ".join(f"{k}={v}" for k, v in sorted(defaults.items())))

    session, _ = call(f"{args.base}/v1/sessions", args.token, {})
    session_id = session["id"]

    latencies: list[float] = []
    for i in range(args.runs):
        phrase = PHRASES[i % len(PHRASES)]
        _, ms = call(
            f"{args.base}/v1/sessions/{session_id}/message",
            args.token,
            {"text": phrase},
        )
        latencies.append(ms)
        print(f"  {i + 1:2d}. {ms:7.0f} мс  {phrase[:40]}")

    latencies.sort()
    p50 = statistics.median(latencies)
    p95 = latencies[int(len(latencies) * 0.95) - 1]
    print(
        f"\nмедиана {p50:.0f} мс, p95 {p95:.0f} мс, минимум {latencies[0]:.0f}, "
        f"максимум {latencies[-1]:.0f}"
    )
    # Цель SRS 19.1: 1-2 с до начала ответа при прогретых локальных моделях.
    if p50 <= 2000:
        print("Медиана укладывается в целевой диапазон SRS 19.1.")
    else:
        print("Медиана выше цели SRS 19.1 — смотрите время до первого токена.")
    print("\nЭто полный turn, а не время до первого звука. Для разреза по этапам")
    print("смотрите latency_ms в кадре turn.done и метки в structured logs.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
