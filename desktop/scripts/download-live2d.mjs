import { mkdir, writeFile } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

// Official Cubism 4 sample; keep model files on the same release.
const root = path.resolve(fileURLToPath(new URL('../public/assets/live2d/', import.meta.url)));
const base = 'https://raw.githubusercontent.com/Live2D/CubismWebSamples/4-r.7/';
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
const model = JSON.parse(await download(base + 'Samples/Resources/Haru/Haru.model3.json', 'haru/Haru.model3.json'));
const refs = model.FileReferences;
const files = new Set([refs.Moc, refs.Physics, refs.Pose, refs.DisplayInfo,
  ...refs.Textures, ...(refs.Expressions ?? []).map(x => x.File),
  ...Object.values(refs.Motions ?? {}).flat().flatMap(x => [x.File, x.Sound])].filter(Boolean));
for (const file of files) await download(base + 'Samples/Resources/Haru/' + file, 'haru/' + file);
await download(base + 'LICENSE.md', 'LICENSE.samples.md');
await download('https://cubism.live2d.com/sdk-web/cubismcore/live2dcubismcore.min.js', 'live2dcubismcore.min.js');
await writeFile(path.join(root, 'provenance.json'), JSON.stringify(provenance, null, 2) + '\n');
console.log(`Downloaded ${provenance.length} Live2D files to ${root}`);
