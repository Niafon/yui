# Yui Companion

Подготовленный локальный комплект, установленные модели и подключение телефона:
**[docs/LOCAL-SETUP.md](docs/LOCAL-SETUP.md)**. Запуск на этом ПК — `Start-Yui.cmd`.

Персональный локальный AI-компаньон с голосом, зрением, долговременной памятью
и переносимой личностью. Реализация по SRS v0.1.

Лицензия — Apache-2.0. Все решения по открытым вопросам SRS зафиксированы в
[docs/decisions.md](docs/decisions.md) (ADR-037 storage, ADR-038 adaptive inference).

## Что уже работает

| Подсистема | Состояние |
|---|---|
| Yui Core (Go) | сессии, turn-пайплайн со стримингом, разрешения, аудит, планировщик, супервизор процессов |
| Долговременная память | структурированные факты, версии, исправление, корзина, TTL, гибридный поиск (exact semantic + FTS5/BM25), извлечение фактов из речи |
| Личность и эмоции | пресет 18+, черты, ограниченный дрейф характера, VAD-эмоции, выражения аватара |
| Разрешения | subject × категория × действие, redaction контекста, manifest каждого вызова модели |
| Инструменты | реестр с оценкой риска, подтверждения по SRS 16.5, встроенный набор: время, память, напоминания, таймеры, заметки, задачи, музыка, ссылки, поиск в интернете |
| Шифрование | envelope encryption, Argon2id, восстановление по паролю **или** recovery-ключу; явное затирание ключей, опционально `runtime/secret` |
| Провайдеры | единый интерфейс LLM/STT/TTS/Vision/Embeddings; драйверы openai-compatible, worker, mock |
| Adaptive inference | Model Registry + Resource Monitor + Task Classifier + Model/Backend Scheduler; Manual lock имеет приоритет над Auto |
| Транспорт | HTTP + собственная реализация WebSocket (RFC 6455); control/data планы разделены |
| Хранилище | **Embedded SQLite + WAL + FTS5**, exact in-process vector cache; PostgreSQL оставлен только как legacy backend |
| Голос | режимы wake word (`off/device/core`) и barge-in (`off/button/wake_word/any_speech`) |
| AI-воркеры (Python) | HTTP-контракт, hash-embeddings, placeholder STT/TTS/Vision, точки подключения реальных моделей |
| Desktop Stage | Tauri 2.11 + TypeScript: сцена, presence-лента, диалог, журнал памяти, журнал передачи данных |
| Android | Flutter: сопряжение по коду, сессия, control/data сокеты; Kotlin foreground service |

Трассировка «требование → код → тест»: [docs/traceability.md](docs/traceability.md).
Что дальше: [ROADMAP.md](ROADMAP.md). Архитектура: [ARCHITECTURE.md](ARCHITECTURE.md).

Пошаговая установка на Windows: [docs/INSTALL.md](docs/INSTALL.md).

## Быстрый старт

### Поиск в интернете

В рабочем `yui.config.json` включён `web_search.enabled`. Когда вопрос требует
свежих сведений, Юи может вызвать `web.search`. Перед отправкой поискового
запроса в DuckDuckGo приложение показывает его и просит подтверждение владельца.
После подтверждения результаты с адресами источников появляются в диалоге.
Для другой установки добавьте `"web_search": {"enabled": true}` в конфигурацию
и перезапустите ядро. Поиск использует публичный RSS Bing и требует подключения
к интернету; если источник недоступен, Юи покажет ошибку вместо выдуманных
актуальных данных.

> С v0.3 PostgreSQL и pgvector **не нужны** для обычной установки. `yui-core`
> сам создаёт `data/yui.db`; FTS5 индекс и exact vector cache являются
> производными и могут быть перестроены из таблицы памяти.

### Вариант 1: без БД и без моделей (проверить, что всё живо)

```bash
cd core && go mod tidy && go test ./...
YUI_DB_DRIVER=memory go run ./cmd/yui-core     # напечатает токен для сцены
```

На Windows то же самое делают скрипты:

```powershell
powershell -ExecutionPolicy Bypass -File scripts\install.ps1
powershell -ExecutionPolicy Bypass -File scripts\start.ps1 -Dev
```

### Вариант 2: рабочая конфигурация

```bash
cp yui.config.example.json yui.config.json
make migrate                    # схема встроена: отдельный сервер БД не нужен

export YUI_PASSWORD='ваш пароль'   # при первом запуске создаст ключ
make run                           # ЗАПИШИТЕ recovery-ключ из лога
make run-workers                   # четыре воркера
cd desktop && npm install && npm run dev
```

Recovery-ключ печатается **один раз** и нигде не сохраняется. Без пароля и без
него зашифрованные воспоминания не восстановить — это и есть смысл строгого
режима.

### Проверка сквозного диалога

```bash
TOKEN=<токен ядра>
SESSION=$(curl -s -X POST localhost:8765/v1/sessions -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["id"])')

curl -s -X POST localhost:8765/v1/sessions/$SESSION/message -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"text":"Меня зовут Антон, запомни это"}'

curl -s localhost:8765/v1/memory -H "Authorization: Bearer $TOKEN"   # факт сохранён
curl -s localhost:8765/v1/audit  -H "Authorization: Bearer $TOKEN"   # что ушло в модель
curl -s localhost:8765/v1/tools  -H "Authorization: Bearer $TOKEN"   # инструменты и их риск
```

## Модели и adaptive inference под RTX 3080 10 ГБ

С v0.3.1 модель больше не обязана быть одной и постоянно сидеть на GPU. Core
держит реестр профилей и перед каждым inference строит план из двух сигналов:
**сложность задачи** + **текущая нагрузка CPU/GPU/RAM/VRAM/игровой процесс**.
На Windows CPU/RAM/foreground process читаются через WinAPI, NVIDIA GPU/VRAM —
через `nvidia-smi`. FPS/frametime может дополнительно присылать desktop/game
integration; при падении FPS выше `max_fps_impact_percent` GPU-кандидаты
блокируются.

Базовый набор профилей в `yui.config.example.json`:

| Профиль | Роль | Backend | Смысл |
|---|---|---|---|
| `qwen35-9b-gpu` | LLM Qwen 3.5 9B Q4 | GPU | максимум качества, когда GPU свободен |
| `qwen35-9b-cpu` | та же Qwen 3.5 9B Q4 | CPU | сохраняет model lock 9B во время игры |
| `qwen35-4b-cpu` | Qwen 3.5 4B Q4 | CPU | gaming/low-latency профиль |
| `stt-worker` | Parakeet TDT 0.6B | CPU | речь без конкуренции за VRAM |
| `tts-worker` | CosyVoice2-0.5B | GPU | стриминговый голос; можно добавить CPU-профиль |
| `vision-worker` | Qwen3-VL 4B | GPU, on-demand | отдельный vision fallback |

Управление имеет строгий приоритет: **session provider lock → global model
lock → user limits → gaming/resource guardrails → task complexity → scoring**.
`locked_model=qwen3.5-9b` не разрешает перейти на 4B, но позволяет выбрать
`qwen35-9b-gpu` или `qwen35-9b-cpu`. В `Auto` при разрешённом downgrade простой
игровой вопрос может уйти на 4B CPU, а сложный анализ — на 9B.

Managed llama.cpp profiles запускаются Supervisor'ом только при выборе. Если
опциональный worker/вес отсутствует, Auto пробует следующий профиль и не
переключается в облако. В Manual ошибка показывается пользователю — выбранная
модель никогда не подменяется молча.

Подробности — ADR-016, ADR-017, ADR-036 и ADR-038.

## Структура

```
core/          Go: ядро, единственный авторитетный writer (ADR-004, ADR-009)
  internal/    model, store, crypto, permission, memory, identity, session, agent, tools, provider, api
  internal/store/sqlite/  production SQLite schema + FTS5 + exact vector layer
  migrations/  legacy PostgreSQL migrations for v0.2/export
workers/       Python: STT, TTS, Vision, Embeddings за узким контрактом (ADR-005)
contracts/     protobuf: control plane, data plane, память, воркеры
desktop/       Tauri 2 + TypeScript: VRM/Live2D-сцена, чат, настройки, окна аватара и чата
mobile/        Flutter + Kotlin: сенсоры, наушники, уведомления
docs/          архитектура, решения, трассировка требований
```

## Принципы, зафиксированные в коде

- **Локально по умолчанию.** Категория данных не покидает машину без явного решения владельца. Единственная точка выхода — `permission.FilterForProvider`.
- **Политика — это код, а не системный промпт.** Разрешения, риск и подтверждения проверяются до формирования запроса.
- **Голос не подтверждает опасное.** Выше низкого риска нужен PIN или биометрия — проверяется тестом, а не соглашением.
- **Индекс — не источник истины.** Вектор можно удалить и перестроить, воспоминание не теряется.
- **Сбой виден.** Нет тихого fallback на облако, нет молчаливых ошибок.
- **Чего нет — то и есть защита.** Инструментов отправки сообщений, звонков, платежей и экстренного вызова не существует в коде.

## Лицензия

Apache License 2.0 — см. [LICENSE](LICENSE) и [NOTICE](NOTICE).

Внимание при публикации: **Live2D Cubism SDK не Apache-2.0** и в репозитории
используется вместе с локальной моделью Haru; условия и настройка описаны в
[docs/live2d.md](docs/live2d.md). **Parakeet под CC-BY-4.0** и требует указания авторства. Лицензии
моделей перечислены в `NOTICE`.
