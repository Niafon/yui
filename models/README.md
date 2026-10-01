# Local model weights

This local checkout has a prepared model set. See `installed-manifest.json`
for pinned sources and checksums, and `../docs/LOCAL-SETUP.md` for usage.
Run `python scripts/download-models.py` from the project root to verify/resume it.

Weights are intentionally not bundled with the source archive.

The example managed llama.cpp profiles expect these filenames:

- `Qwen3.5-9B-Q4_K_M.gguf`
- `Qwen3.5-4B-Q4_K_M.gguf`

Place compatible GGUF files here or change the `-m` paths in
`yui.config.json`. The scheduler does not depend on those exact filenames; it
only needs each configured provider/worker to expose the declared endpoint.

For a CPU profile the example uses `-ngl 0`. The GPU profile uses full GPU
offload. Context is deliberately capped at 8192 for the gaming profiles to
avoid spending RAM/VRAM on a context window the companion normally does not
need.
