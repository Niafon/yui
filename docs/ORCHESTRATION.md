# Локальная оркестрация моделей

Профиль RTX 3080 создаётся командой:

```powershell
python scripts/download-orchestration.py --fara --runtime cuda
python scripts/configure-orchestration.py --computer --apply
```

`configure-orchestration.py` сохраняет резервную копию `yui.config.json` перед заменой. Текущий процесс Yui не перезапускается автоматически.

После следующего запуска:

- LFM2.5-2.6B работает как быстрый CPU-профиль для коротких команд и обычного диалога.
- Bonsai 2 27B работает как reasoning-профиль на GPU или CPU. GPU-вариант использует сборку llama.cpp от PrismML; обычная llama.cpp для ternary GGUF не подходит.
- Jev выбирает между доступными локальными профилями только для текущего запроса. Ключ читается из `OPENROUTER_API_KEY`; история и память в Jev не отправляются. При отсутствии ключа работает детерминированный локальный скоринг.
- Fara1.5-4B запускается отдельным worker через официальный Microsoft Fara harness. Браузер временный, домены задаются на каждый вызов, скачивания и локальные адреса блокируются, критические точки требуют ответа владельца.

Модели не держатся одновременно в видеопамяти: GPU-workers помечены как exclusive, а inference manager останавливает предыдущий worker и ждёт его завершения перед запуском следующего. Видеопамять игры учитывается отдельно; fallback на облако не выполняется.

Память получает ограниченную выдачу: для LFM используется до четырёх релевантных записей и меньший бюджет контекста, дубликаты удаляются, просроченные и удалённые записи не возвращаются. Подтверждённые пользовательские процедуры сохраняются через `memory.learn_procedure` и остаются под общим контуром разрешений.

STT использует faster-whisper с VAD, русским языком по умолчанию, hotwords `Юи, Yui`, beam search только для финальных фрагментов, фильтрацией повторов и оценкой уверенности по принятым сегментам. Тишина не запускает модель, а сбой загрузки больше не превращается в фиктивную расшифровку.

Проверки:

```powershell
python scripts/check-orchestration.py
python scripts/check-computer.py
cd core; go test -tags sqlite_fts5 ./... -race -count=1
cd ..\workers; ..\.venv\Scripts\python.exe -m unittest discover -s tests -v
```

Источники моделей: [LiquidAI/LFM2.5-2.6B](https://huggingface.co/LiquidAI/LFM2.5-2.6B), [PrismML Ternary Bonsai 2](https://huggingface.co/prism-ml/Ternary-Bonsai-2-27B-gguf), [Microsoft Fara1.5-4B](https://huggingface.co/microsoft/Fara1.5-4B), [Microsoft Fara harness](https://github.com/microsoft/fara).
