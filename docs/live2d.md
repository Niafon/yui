# Live2D

The desktop stage renders Cubism 3/4 `.model3.json` packages with PixiJS 6 and
pixi-live2d-display. The default is the official **Haru** sample from
Live2D/CubismWebSamples, release `4-r.7` (© Live2D Inc.).

Assets are local in `desktop/public/assets/live2d/` and copied into `desktop/dist`
by Vite. No CDN or network download is needed when opening the application.
The model loads independently of the core connection. Idle animations, physics,
blinking, expression events and speech amplitude use the existing avatar adapter.
Sample voice recordings are disabled so they cannot overlap Yui's TTS.

To restore the downloaded assets, run `npm run live2d:download` from `desktop`.
`provenance.json` records source URLs, file sizes and SHA-256 checksums. Model
resources are pinned to a sample release; the official Core download URL is
vendor-maintained, so its checksum can change on a subsequent download.

To replace Haru, copy a complete Cubism 3/4 model package under `desktop/public`,
change `DEFAULT_MODEL` in `desktop/src/live2d.ts`, and adapt `EXPRESSIONS` to its
expression names. Keep the package's relative texture/motion paths intact.
The supplied mapping is smile F01, concern F04, thinking F08, surprise F06;
neutral/unknown resets the expression. Lip sync uses `ParamMouthOpenY`.

## Verification

The avatar selector also includes Shino and Victoria (VRM anime samples).
`Проба голоса` plays a local Silero xenia preview through the same analyser-driven
mouth animation as streamed speech. It does not require an LLM connection.
Run `/tests/vrm.html` on the development server to verify both avatars' mouth
morphs, disposal, and cancelled loads. Source/license notes are alongside assets.

Run `npm run build` and `npm test`. For the WebGL integration check, start
`npm run dev` and open `http://127.0.0.1:5273/tests/live2d.html`.
It checks loading under the Tauri content policy, mouth updates, expressions,
resizing, disposal and recovery after an unavailable model. The deliberate
missing-model check emits a console error. The harness is excluded from the build.
Cubism Core needs the `wasm-unsafe-eval` permission for its WebAssembly
runtime. The Pixi shader adapter used by the stage also needs `unsafe-eval`.

## Third-party terms

These assets are **not Apache-2.0**:

- [Haru / Cubism sample license](https://github.com/Live2D/CubismWebSamples/blob/4-r.7/LICENSE.md)
  (also saved as `LICENSE.samples.md` alongside the model).
- [Live2D Free Material License](https://www.live2d.com/eula/live2d-free-material-license-agreement_en.html)
  and [individual sample conditions](https://docs.live2d.com/cubism-editor-manual/sample-model/).
- [Cubism Core proprietary license](https://www.live2d.com/eula/live2d-proprietary-software-license-agreement_en.html)
  and [SDK release licensing](https://www.live2d.com/en/download/cubism-sdk/release-license/).
- PixiJS, its CSP adapter and pixi-live2d-display: MIT; the Cubism framework
  embedded by the renderer retains Live2D's Open Software License terms.

Review those terms before redistributing or publishing a commercial application.
