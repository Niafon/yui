# AI-воркеры

Каждый воркер — отдельный процесс с одной задачей и узким контрактом
(SRS 4.1, ADR-005). Воркер **не имеет доступа к базе, к памяти и к
инструментам**: он получает payload задачи и возвращает результат (VIS-009).

```bash
python -m yui_worker --kind stt        --port 8802 --model small
python -m yui_worker --kind tts        --port 8803
python -m yui_worker --kind vision     --port 8804
python -m yui_worker --kind embeddings --port 8801 --model all-MiniLM-L6-v2
python -m unittest discover -s tests
```

## Контракт

| Метод | Путь | Запрос → ответ |
|---|---|---|
| GET | `/healthz` | `{id, kind, ready, detail}` — 503, пока модель грузится |
| POST | `/v1/stt` | `{audio_b64, sample_rate, channels, format, language, final}` → `{text, confidence, final, language, speaker_is_owner?}` |
| POST | `/v1/tts` | `{text, voice, emotion, speed, format}` → `{sample_rate, chunks:[{seq, audio_b64}]}` |
| POST | `/v1/vision` | `{image_b64, mime, question, want_ocr}` → `{description, objects, ocr_text, confidence}` |
| POST | `/v1/embeddings` | `{texts:[…]}` → `{vectors:[[…]], dim, backend}` |

Имена полей совпадают с `contracts/proto/yui/v1/worker.proto`, поэтому переход
на gRPC не потребует менять логику воркеров.

## Без моделей

По умолчанию воркеры работают в placeholder-режиме и честно сообщают об этом в
`/healthz.detail`. Это сделано намеренно: весь путь голоса, памяти и аватара
можно пройти и замерить до того, как выбраны модели (SRS 22.1). Ветка загрузки
настоящего бэкенда уже написана в каждом воркере — достаточно передать
`--model`.

Если модель не загрузилась, воркер **не падает молча**: он переходит на
placeholder и пишет причину в `detail`, а ядро показывает это в статусе
(AI-007, NFR-010).

## Embeddings

Fallback — детерминированные hash-эмбеддинги (FNV-1a, 256 измерений). Тот же
алгоритм реализован в ядре (`internal/provider/mock.go`), поэтому векторы
совместимы. Тест `tests/test_hashing.py` проверяет хеш по эталонным значениям
FNV-1a — это и есть проверка совместимости с Go.
