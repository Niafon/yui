# Adaptive inference runtime

Yui 0.3.1 can keep several local model profiles and choose between them per
request. The choice is made in Core, so one setting applies to Desktop and
Mobile sessions alike.

## Modes

- `auto` — quality/resource score, task complexity and gaming guardrails.
- `max_quality` — strongly favors the highest quality profile that fits.
- `balanced` — quality with moderate resource penalties.
- `gaming` — always applies gaming-safe scoring even if no known game process
  is detected.
- `manual` — requires `locked_model` or `preferred_provider`; the selected
  model is never silently replaced.

A session-level provider set through `/v1/sessions/{id}/provider` has the
highest priority. It bypasses Auto completely.

## Model family vs backend

Several provider entries may describe the same `model_family`:

```json
{
  "id": "qwen35-9b-gpu",
  "model_family": "qwen3.5-9b",
  "execution_backend": "gpu"
}
```

```json
{
  "id": "qwen35-9b-cpu",
  "model_family": "qwen3.5-9b",
  "execution_backend": "cpu"
}
```

With `locked_model=qwen3.5-9b`, Yui may move between these providers without
changing the model. It cannot switch to `qwen3.5-4b` until the lock is removed.

## Resource telemetry

Core polls at a low frequency (3 seconds by default):

- Windows CPU and RAM through WinAPI;
- foreground process through WinAPI;
- NVIDIA GPU utilization and VRAM through one `nvidia-smi` query;
- Linux CPU/RAM through `/proc` and NVIDIA data through `nvidia-smi`.

FPS and frametime are optional telemetry from a game/desktop integration:

```http
POST /v1/inference/telemetry
```

```json
{
  "fps": 141,
  "baseline_fps": 144,
  "frame_time_ms": 7.1,
  "game_active": true,
  "foreground_process": "dota2.exe"
}
```

If the drop from `baseline_fps` exceeds `max_fps_impact_percent`, GPU
candidates are rejected before inference begins.

## Managed llama.cpp profiles

`yui.config.example.json` contains optional cold-started llama.cpp workers.
They are `autostart=false`; Model Manager starts the selected worker and waits
for its health endpoint. A missing optional worker is skipped in Auto, while a
manual/explicit selection returns an error.

Expected files in `./models` are documented in `models/README.md`. The project
does not bundle model weights.

## API

`GET /v1/inference` returns preferences, current telemetry, last decision and
all provider/model profiles.

`PATCH /v1/inference/preferences` accepts any subset of the current
preferences. Example:

```json
{
  "mode": "manual",
  "locked_model": "qwen3.5-9b",
  "allow_auto_downgrade": false,
  "minimum_quality": 70
}
```

Every automatic or explicit selection is also emitted to the session stream as
`inference.decision`, including the provider, model family, backend and human
readable reason.
