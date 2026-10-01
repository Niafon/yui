import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { defineConfig, type Plugin } from "vite";

const modules = fileURLToPath(new URL("./node_modules/", import.meta.url));
const vadDir = path.join(modules, "@ricky0123/vad-web");
const ortDir = path.join(modules, "onnxruntime-web");

/** Silero VAD runtime files, served same-origin under /vad/ (CSP 'self'). */
const VAD_ASSETS: Record<string, string> = {
  "vad.worklet.bundle.min.js": path.join(vadDir, "dist/vad.worklet.bundle.min.js"),
  "silero_vad_v5.onnx": path.join(vadDir, "dist/silero_vad_v5.onnx"),
  "ort-wasm-simd-threaded.wasm": path.join(ortDir, "dist/ort-wasm-simd-threaded.wasm"),
  "ort-wasm-simd-threaded.mjs": path.join(ortDir, "dist/ort-wasm-simd-threaded.mjs"),
};
const MIME: Record<string, string> = { ".js": "text/javascript", ".mjs": "text/javascript", ".wasm": "application/wasm", ".onnx": "application/octet-stream" };

function vadAssets(): Plugin {
  return {
    name: "yui-vad-assets",
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        const name = req.url?.split("?")[0]?.replace(/^\/vad\//, "");
        const file = name && req.url?.startsWith("/vad/") ? VAD_ASSETS[name] : undefined;
        if (!file) return next();
        res.setHeader("Content-Type", MIME[path.extname(file)] ?? "application/octet-stream");
        res.end(readFileSync(file));
      });
    },
    generateBundle() {
      for (const [name, file] of Object.entries(VAD_ASSETS)) {
        this.emitFile({ type: "asset", fileName: `vad/${name}`, source: readFileSync(file) });
      }
    },
  };
}

// The stage is served locally and talks to the core over loopback only.
export default defineConfig({
  clearScreen: false,
  plugins: [vadAssets()],
  server: { port: 5273, strictPort: true },
  build: {
    target: "es2022",
    outDir: "dist",
    // three.js + three-vrm form one lazily loaded renderer chunk (~800 kB).
    chunkSizeWarningLimit: 900,
    // Worklet scripts must stay files: the Tauri CSP rejects data: scripts.
    assetsInlineLimit: (file: string) => (file.endsWith(".js") ? false : undefined),
    rollupOptions: {
      input: { main: "index.html", avatar: "avatar.html" },
    },
  },
});
