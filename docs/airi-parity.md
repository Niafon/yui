# AIRI parity

Сравнение сделано с [moeru-ai/airi](https://github.com/moeru-ai/airi). Уже
есть: потоковый голос, зрение, память, Live2D, смена провайдера, локальный
режим и адаптивный выбор модели. В этот комплект добавлена поддержка VRM в
desktop stage; модель берётся из `desktop/public/assets/vrm/default.vrm`.

Следующие возможности AIRI подключаются через изолированные интеграции и не
получают обхода разрешений Yui:

| Возможность | Состояние | Подключение |
|---|---|---|
| VRM, выражения, lip-sync | реализовано | переключатель «Аватар» в stage |
| несколько голосовых провайдеров | готово на уровне контракта | `ProviderConfig` для каждого TTS |
| MCP-инструменты | готово на уровне реестра | capability-плагины Yui |
| Discord bot | каркас интеграции | `integrations/discord` |
| Telegram bot | каркас интеграции | `integrations/telegram` |
| Minecraft | каркас игрового bridge | `integrations/minecraft` |
| Factorio | каркас игрового bridge | `integrations/factorio` |

Игровые и сетевые интеграции намеренно не включаются без endpoint/token:
каждая команда проходит через реестр инструментов, риск и подтверждение.
