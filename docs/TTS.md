# Голоса (TTS)

Ядро синтезирует речь через провайдеров `kind: "tts"`. Все драйверы отдают
сырой PCM 16 бит, поэтому сцена начинает играть и двигать губами уже с первых
100 мс ответа.

| Драйвер | Сервис | Что нужно |
|---|---|---|
| `worker` | локальный Silero/Piper через `yui_worker` | ничего (по умолчанию) |
| `openai` | OpenAI `/v1/audio/speech` и совместимые локальные серверы (Kokoro-FastAPI, openedai-speech) | `OPENAI_API_KEY` для облака; для локального сервера ключ не нужен |
| `elevenlabs` | ElevenLabs streaming (`pcm_24000`) | `ELEVENLABS_API_KEY`, ID голоса |
| `azure` | Azure Speech REST (`raw-24khz-16bit-mono-pcm`, SSML) | `AZURE_SPEECH_KEY`, региональный endpoint, нейроголос |

## Добавить из интерфейса

Настройки → Модели → «Добавить» → назначение «Голос». Для облака выберите
«Внешний провайдер» и сервис; адрес и имя переменной с ключом подставятся.
Сам ключ задаётся в окружении ядра и в интерфейс не вводится. Затем выберите
модель в фильтре «Голос» и нажмите «Использовать».

## Конфигурация вручную

```json
{
  "providers": [
    { "id": "eleven", "kind": "tts", "driver": "elevenlabs", "model": "eleven_multilingual_v2",
      "voice": "21m00Tcm4TlvDq8ikWAM", "api_key_env": "ELEVENLABS_API_KEY", "local": false },
    { "id": "azure-ru", "kind": "tts", "driver": "azure",
      "endpoint": "https://westeurope.tts.speech.microsoft.com/cognitiveservices/v1",
      "voice": "ru-RU-SvetlanaNeural", "api_key_env": "AZURE_SPEECH_KEY", "local": false },
    { "id": "openai-voice", "kind": "tts", "driver": "openai", "model": "gpt-4o-mini-tts",
      "voice": "nova", "api_key_env": "OPENAI_API_KEY", "local": false },
    { "id": "kokoro", "kind": "tts", "driver": "openai", "endpoint": "http://127.0.0.1:8880/v1",
      "model": "kokoro", "voice": "af_heart", "local": true }
  ],
  "default_providers": { "tts": "azure-ru" }
}
```

`voice` у провайдера важнее профиля голоса персонажа. Если `voice` пуст,
используется профиль персонажа, когда сервис его понимает (имена локальных
голосов вроде `xenia` пропускаются), иначе голос сервиса по умолчанию.
Модели `gpt-4o*` получают эмоцию реплики как инструкцию к подаче.

## Приватность

Облачный голос получает текст ответа — категорию `conversation`. Как и
облачная LLM, он делает это только после разрешения владельца: первый ответ
звучит без голоса, а в интерфейсе появляется запрос «Отправить данные
наружу?». «Разрешить один раз» действует на следующий ответ, «Разрешить
всегда» сохраняет постоянное разрешение (его можно отозвать в разделе
«Приватность»). Каждая передача попадает в журнал.
