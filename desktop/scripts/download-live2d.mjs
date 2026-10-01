import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

// Official Cubism 4 samples; keep every model on the same release.
// Usage: node scripts/download-live2d.mjs [haru hiyori natori ...]
const root = path.resolve(fileURLToPath(new URL('../public/assets/live2d/', import.meta.url)));
const base = 'https://raw.githubusercontent.com/Live2D/CubismWebSamples/4-r.7/';
const MODELS = { haru: 'Haru', hiyori: 'Hiyori', natori: 'Natori', mao: 'Mao' };
const requested = process.argv.slice(2).filter(name => MODELS[name]);
const selected = requested.length ? requested : ['haru', 'hiyori', 'natori'];
const provenance = [];

async function download(url, relative) {
  const response = await fetch(url, { signal: AbortSignal.timeout(60000) });
  if (!response.ok) throw new Error(`${response.status}: ${url}`);
  const bytes = Buffer.from(await response.arrayBuffer());
  const target = path.resolve(root, relative);
  if (!target.startsWith(root + path.sep)) throw new Error('Invalid asset path');
  await mkdir(path.dirname(target), { recursive: true });
  await writeFile(target, bytes);
  provenance.push({ file: relative, url, bytes: bytes.length,
    sha256: createHash('sha256').update(bytes).digest('hex') });
  return bytes;
}

for (const folder of selected) {
  const name = MODELS[folder];
  const source = `${base}Samples/Resources/${name}/`;
  const model = JSON.parse(await download(`${source}${name}.model3.json`, `${folder}/${name}.model3.json`));
  const refs = model.FileReferences;
  // Motion sounds are skipped: Yui's TTS owns the voice channel.
  const files = new Set([refs.Moc, refs.Physics, refs.Pose, refs.DisplayInfo, refs.UserData,
    ...refs.Textures, ...(refs.Expressions ?? []).map(x => x.File),
    ...Object.values(refs.Motions ?? {}).flat().map(x => x.File)].filter(Boolean));
  for (const file of files) await download(source + file, `${folder}/${file}`);
}
await download(base + 'LICENSE.md', 'LICENSE.samples.md');
const coreUrl = 'https://cubism.live2d.com/sdk-web/cubismcore/live2dcubismcore.min.js';
try {
  await download(coreUrl, 'live2dcubismcore.min.js');
} catch (error) {
  // The vendor host is sometimes unreachable; an existing Core stays valid.
  const bytes = await readFile(path.join(root, 'live2dcubismcore.min.js')).catch(() => { throw error; });
  console.warn(`Cubism Core download failed (${error.cause?.message ?? error.message}); keeping the local copy.`);
  provenance.push({ file: 'live2dcubismcore.min.js', url: coreUrl, bytes: bytes.length,
    sha256: createHash('sha256').update(bytes).digest('hex') });
}
await writeFile(path.join(root, 'provenance.json'), JSON.stringify(provenance, null, 2) + '\n');
console.log(`Downloaded ${provenance.length} Live2D files (${selected.join(', ')}) to ${root}`);
