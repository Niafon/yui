# Yui integrations

Интеграции AIRI-style работают как внешние bridges. Bridge не вызывает модель
напрямую: он отправляет события в core и получает разрешённые команды через
протокол инструмента. Это сохраняет аудит, redaction и подтверждения.

Переменные окружения для будущих адаптеров:

```text
YUI_DISCORD_TOKEN=
YUI_TELEGRAM_TOKEN=
YUI_MINECRAFT_ENDPOINT=ws://127.0.0.1:25575
YUI_FACTORIO_ENDPOINT=ws://127.0.0.1:34197
```
