# Live2D

The desktop stage renders Cubism 3/4 `.model3.json` packages with PixiJS 6 and
pixi-live2d-display. Bundled official samples from Live2D/CubismWebSamples,
release `4-r.7` (© Live2D Inc.):

| Model | Notes |
|---|---|
| Hiyori | nine idle motions, no expression files (parameter emotion layer); AIRI's default |
| Natori | named expressions Smile / Sad / Angry / Surprised |
| Haru | eight expressions (F01…F08) |
| Mao | Cubism 4.2 model with vowel mouth parameters `ParamMouthA…O` |

Assets are local in `desktop/public/assets/live2d/` and copied into `desktop/dist`
by Vite. No CDN or network download is needed when opening the application.
The model loads independently of the core connection. Motion sound files are
not downloaded and sound playback is disabled so they cannot overlap Yui's TTS.

To restore or extend the assets, run `npm run live2d:download [haru hiyori natori mao]`
from `desktop`. `provenance.json` records source URLs, file sizes and SHA-256
checksums. Model resources are pinned to a sample release; the official Core
download URL is vendor-maintained, so its checksum can change on a later
download (the script keeps the local Core when the vendor host is unreachable).

Users can also import their own Cubism 3/4 package as a `.zip` in Settings →
Avatar; it stays in the browser's IndexedDB.

## Animation layers

`desktop/src/avatar/live2d.ts` adds per-frame layers after the motion manager
(they run inside `updateNaturalMovements`, so they never accumulate and physics
reacts to head movement):

- emotion: expression file from the model profile, or additive parameter
  offsets (eye smile, brows, mouth form, cheeks) for presets without a file;
  emotions peak, then settle to 60 %;
- gaze: pointer pursuit or idle saccades through the focus controller;
- head: state posture (thinking / listening), emphasis while speaking, sway;
- blink: Yui's own blinker, because the library blinks only when no motion plays;
- mouth: `ParamMouthOpenY` + `ParamMouthForm` from the vowel mix, or
  `ParamMouthA…O` directly for models that have them.

Profiles (`LIVE2D_PROFILES`) map emotion presets to expression names and
reaction labels to `TapBody` motions. Imported models use the parameter layer.

## Verification

Settings → Voice → «Проба голоса» plays a local Silero xenia preview through
the same lip-sync path as streamed speech. It does not require an LLM connection.

Run `npm run build` and `npm test`. For the WebGL integration checks, start
`npm run dev` and open:

- `http://127.0.0.1:5273/tests/live2d.html` — every bundled model under the
  Tauri content policy: lip sync, mouth form, emotion layer, expression reset,
  resizing, disposal and recovery after an unavailable model (the deliberate
  missing-model check emits a console error);
- `http://127.0.0.1:5273/tests/vrm.html` — VRM clips, visemes, emotion easing,
  pointer gaze, bounded procedural bone offsets, disposal, cancelled loads;
- `http://127.0.0.1:5273/tests/lipsync.html` — plays the voice sample through
  the real wLipSync worklet and compares it with the old amplitude mapping
  (`?engine=spectral` checks the fallback).

The harnesses are excluded from the build.
Cubism Core needs the `wasm-unsafe-eval` permission for its WebAssembly
runtime. The Pixi shader adapter used by the stage also needs `unsafe-eval`.

## Third-party terms

These assets are **not Apache-2.0**:

- [Haru, Hiyori, Natori, Mao / Cubism sample license](https://github.com/Live2D/CubismWebSamples/blob/4-r.7/LICENSE.md)
  (also saved as `LICENSE.samples.md` alongside the model).
- [Live2D Free Material License](https://www.live2d.com/eula/live2d-free-material-license-agreement_en.html)
  and [individual sample conditions](https://docs.live2d.com/cubism-editor-manual/sample-model/).
- [Cubism Core proprietary license](https://www.live2d.com/eula/live2d-proprietary-software-license-agreement_en.html)
  and [SDK release licensing](https://www.live2d.com/en/download/cubism-sdk/release-license/).
- PixiJS, its CSP adapter and pixi-live2d-display: MIT; the Cubism framework
  embedded by the renderer retains Live2D's Open Software License terms.

Review those terms before redistributing or publishing a commercial application.
