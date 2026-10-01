# Трассировка требований

Только требования, которые реализованы или сознательно закрыты частично.
Полный каталог — в Приложении A SRS. Колонка «Проверка» — файл теста или
ручная процедура.

## Core и сессии

| ID | Где | Проверка |
|---|---|---|
| CORE-001 | `cmd/yui-core/main.go`, `internal/api/server.go` (`/healthz`, `/v1/status`) | `curl /healthz` |
| CORE-002 | `internal/provider/registry.go` — Core знает только интерфейсы | подмена на `mock` в конфиге |
| CORE-003 | `internal/session/session.go` | ручной прогон 30 мин (AC-03) |
| CORE-004 | `session.SetOutputDevice`, подписка нескольких клиентов на одну сессию | две сцены на одной сессии |
| CORE-005 | `internal/supervisor/supervisor.go` — рестарт с backoff, Core не падает | kill воркера (AC-13) |
| CORE-006 | частично: `session.BeginTurn` заменяет предыдущий turn; idempotency-key для команд ещё нет | — |
| CORE-008 | клиенты ходят только через API; прямого доступа к БД нет | `internal/store` не импортируется клиентами |
| CORE-009 | `memstore` снапшот, embedded SQLite | `memstore_test.go: TestSnapshotSurvivesReopen` |

## Голос

| ID | Где | Проверка |
|---|---|---|
| VOICE-001 | `internal/api/socket.go` (data plane, бинарные кадры) | — |
| VOICE-002 | команда `audio.end` | — |
| VOICE-004 | сессия живёт между репликами | `TestSnapshotSurvivesReopen` |
| VOICE-005 | `Transcript.SpeakerIsOwner`, `runAudioTurn` игнорирует чужую речь | контракт воркера |
| VOICE-006 | кадры `transcript.partial` / `transcript.final` | — |
| VOICE-007 | `agent.speak` синтезирует по предложениям | `agent/context_test.go: TestEndsSentence` |
| VOICE-008 | команда `mic.state`, индикатор в сцене и на телефоне | — |
| VOICE-009 | чужая речь не превращается в turn | `socket.go: runAudioTurn` |
| VOICE-010 | `Session.OutputDevice`, TTS-кадр адресован устройству | — |
| VOICE-011 | `Synthesize` отменяется по ctx; `Manager.CancelTurn` | — |

## Vision

| ID | Где | Проверка |
|---|---|---|
| VIS-001 | `POST /v1/sessions/{id}/vision`, кадр `vision.frame` | `workers/tests/test_workers.py: TestVision` |
| VIS-003, VIS-004 | контракт `Vision.Analyze` (description + ocr_text + confidence) | там же |
| VIS-009 | воркер получает только payload задачи | `worker.proto`, `provider/worker.go` |

## Память

| ID | Где | Проверка |
|---|---|---|
| MEM-002 | `model.MemoryItem` (источник, уверенность, статус) | — |
| MEM-003 | `memory.Service.Retrieve` + `agent.ContextBuilder` | `memory/rank_test.go` |
| MEM-004 | ранжирование поверх выборки из БД; вектор удаляем | `rank_test.go` работает без векторов |
| MEM-005 | пространства памяти, проверка прав по категории | `permission_test.go` |
| MEM-006 | `memory.Extractor.FromTurn` | `memory/extract_test.go` |
| MEM-007 | статус `needs_confirmation`, вес в ранжировании | `TestNeedsConfirmationRanksBelowConfirmed` |
| MEM-008 | `Remember` → `Supersede` | `memstore_test.go: TestMemoryCorrectionKeepsHistory` |
| MEM-009 | `memory_versions`, `valid_to`, `replaced_by` | там же |
| MEM-011 | `Pin`, бонус в ранжировании, TTL не трогает pinned | `TestPinnedMemoryOutranksNoise` |
| MEM-012, MEM-013 | soft delete + корзина + restore | `TestTrashAndRestore` |
| MEM-014 | `Sensitivity` на каждой записи, фильтр провайдера | `permission_test.go` |
| MEM-017 | TTL в `ApplyRetention`; квота медиа — не реализована | — |

## Личность

| ID | Где | Проверка |
|---|---|---|
| PER-001 | `Identity` и `Presentation` разделены | `identity_test.go` |
| PER-002 | `PATCH /v1/identities/{id}` | — |
| PER-003 | `identity.DefaultPreset` (18+, аниме, Live2D) | `TestDefaultPresetIsAdultAndPersisted` |
| PER-004 | `EmotionalState` в хранилище | — |
| PER-005 | кадр `avatar.expression` + текст промпта | — |
| PER-006, PER-012 | `DevelopmentMode`, лимит дрейфа, заморозка | `TestEvolveIsRateLimited`, `TestFrozenIdentityDoesNotDrift` |
| PER-007 | `Checkpoint` | — |
| PER-009 | правила поведения в `identity.Snapshot` | `TestSnapshotStatesTheImitationBoundary` |

## Провайдеры

| ID | Где | Проверка |
|---|---|---|
| AI-001 | интерфейсы `LLM/STT/TTS/Vision/Embeddings` | mock и openai реализуют одно и то же |
| AI-002 | отдельный провайдер на каждый тип в конфиге | `config.Validate` |
| AI-003, AI-004 | `provider/openai.go` (SSE-стриминг) | — |
| AI-005 | `FilterForProvider` — единственный выход наружу | `TestFilterForProviderDropsForbiddenBlocks` |
| AI-007 | ошибка провайдера → кадр `error`, без fallback | `agent.fail` |
| AI-011 | `permission.Fit` по бюджету токенов | `TestFitKeepsCurrentTextWithinBudget` |
| AI-012 | `audit.ProviderCall` пишет manifest категорий | `/v1/audit` |

## Клиенты

| ID | Где | Проверка |
|---|---|---|
| CL-001 | `mobile/lib` | — |
| CL-002 | `VoiceSessionService.kt` | — |
| CL-003 | индикаторы микрофона и внешнего провайдера в обоих клиентах | — |
| CL-004 | одноразовый код, TTL 24 часа, повтор отклоняется | `api/pairing.go` |
| CL-006, CL-007 | `desktop/` (Tauri 2 + TS, canvas-аватар) | `npm run build` |
| CL-011 | кадр `memory.indicator` → журнал памяти | — |
| CL-012 | при недоступном ядре — явная ошибка, не имитация | `home_page.dart` |

## Безопасность

| ID | Где | Проверка |
|---|---|---|
| SEC-001 | один владелец, `UserID` в Deps | — |
| SEC-002 | `ConfirmationFor`: high/critical → биометрия/PIN, не голос | `permission.go` |
| SEC-003 | `Grant` = subject × категория × действие | `permission_test.go` |
| SEC-004 | новая категория для внешнего провайдера → `ask` | `TestRemoteProviderIsAskedBeforeFirstTransfer` |
| SEC-005 | не-loopback без TLS отклоняется | `api.authenticated` |
| SEC-006 | хранится только хеш токена; секреты не логируются | `HashToken` |
| SEC-007 | `audit.Service` | `/v1/audit` |
| SEC-010 | отзыв устройства | `TestDeviceRevocationIsVisible` |
| SEC-013 | инструмента экстренного вызова нет вовсе | отсутствие кода — это и есть реализация |

## Нефункциональные

| ID | Где | Проверка |
|---|---|---|
| NFR-003 | `core/migrations/0001_init.sql`, таблица `schema_migrations` | — |
| NFR-004 | `protocol_version` в конверте | `contracts/proto` |
| NFR-005 | `X-Trace-Id`, `logging.WithTrace` | — |
| NFR-006 | планировщик отменяет задачу при активном turn | `scheduler.runOne` |
| NFR-007 | супервизор поднимает воркеры сам | `supervisor.StartAll` |
| NFR-008 | нет абсолютных путей и Windows-специфики в ядре | — |
| NFR-010 | кадр `error` вместо тихого отказа | `agent.fail` |


## Инструменты и подтверждения (ADR-026)

| ID | Где | Проверка |
|---|---|---|
| AI-008 | `tools.Tool.Validate` — строгая схема, выдуманный аргумент отклоняется | `tools_test.go: TestValidateRejectsUnknownAndMissingArguments` |
| AI-009 | `agent.runToolCall` — permission по категории инструмента | — |
| AI-010 | подтверждение до выполнения, парковка в `tools.Pending` | `TestPendingInvocationsExpire` |
| SEC-002 | голос не проходит выше низкого риска | `agent/tools_test.go: TestVoiceNeverSatisfiesHighRisk` |
| SEC-013 | опасных инструментов не существует в реестре | `TestDangerousToolsAreAbsent` |
| NFR-010 | недоступная capability возвращает честную ошибку | `TestUnavailableCapabilityFailsHonestly` |

## Шифрование (ADR-024, ADR-025)

| ID | Где | Проверка |
|---|---|---|
| SEC-009 | `crypto.Store`: AES-256-GCM, Argon2id, DEK в keyfile | `crypto_test.go` |
| TBD-03 → ADR-024 | восстановление recovery-ключом без пароля | `TestRecoveryKeyOpensTheStore` |
| — | смена пароля не требует перешифровки | `TestChangePasswordKeepsData` |
| — | шифротекст привязан к записи | `TestCiphertextIsBoundToItsRecord` |

## Голос и зрение: режимы (ADR-020, ADR-021, ADR-028)

| ID | Где | Проверка |
|---|---|---|
| VOICE-003 | `config.Voice.WakeWord.Mode`: off/device/core | — |
| VOICE-011 | `api.handleBargeIn` + `agent.BargeIn`; режим решает ядро, не клиент | — |
| VIS-005 | `config.Vision.EventMode`: off/manual/motion/periodic/smart | — |
| R-03 | vision выгружается после простоя, STT и embeddings на CPU | `workers/yui_worker/workers/vision.py` |

## Semantic index (ADR-018)

| ID | Где | Проверка |
|---|---|---|
| MEM-003 | гибридный поиск: exact semantic + FTS5/BM25 в одном ранжировании | `memory/rank_test.go` |
| MEM-004 | сбой индекса логируется и не ломает выдачу | `memory.Service.Retrieve` |


## Обновления августа 2026 (ADR-032…036)

| ID | Где | Проверка |
|---|---|---|
| SEC-006 | `crypto.zero` затирает DEK, KEK и пароль; `Scrubbed` + `runtime/secret` под тегом | `crypto_test.go: TestUnlockClearsTheCallerSecret` |
| SEC-009 | идентификатор ключа больше не выводится из ключа | `TestKeyIDDoesNotLeakKeyMaterial` |
| SEC-005 | явный постквантовый гибрид в TLS | `api/tls.go` |
| NFR-002 | профиль утечки горутин, фоновая проверка | `api/debug.go`, `leak_go127.go` |
| NFR-006, AC-14 | планировщик уступает разговору, включая отмену запущенного задания | `scheduler_test.go` (5 тестов на synctest) |
| MEM-003 | итеративные сканы: селективный фильтр больше не «недобирает» | `0004_vector_tuning.sql`, `postgres.VectorSearch` |
| VIS-003, VIS-004 | зрение через резидентную LLM; описание и OCR разделены | `provider/vision_test.go` |
| VIS-008, R-12 | промпт зрения запрещает выводы о личности и настроении | `provider/vision_openai.go` |
| R-03 | vision больше не конкурирует за VRAM: отдельная модель не грузится | ADR-036 |
