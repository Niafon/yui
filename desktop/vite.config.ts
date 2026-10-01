import { defineConfig } from "vite";

// The stage is served locally and talks to the core over loopback only.
export default defineConfig({
  clearScreen: false,
  server: { port: 5273, strictPort: true },
  build: { target: "es2022", outDir: "dist" },
});
