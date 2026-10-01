import { defineConfig } from "vite";

// The stage is served locally and talks to the core over loopback only.
export default defineConfig({
  clearScreen: false,
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
