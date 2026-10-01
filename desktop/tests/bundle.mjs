// Bundles a TypeScript module with its imports (esbuild ships with Vite) so
// tests exercise the real source instead of copied snippets.
import { build } from 'esbuild';

export async function load(entry) {
  const result = await build({
    entryPoints: [new URL(`../src/${entry}`, import.meta.url).pathname],
    bundle: true, write: false, format: 'esm', platform: 'neutral', target: 'es2022', logLevel: 'silent',
  });
  const code = result.outputFiles[0].text;
  return import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);
}
