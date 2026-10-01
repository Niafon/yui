import assert from 'node:assert/strict';
import test from 'node:test';
import { load } from './bundle.mjs';

const { emotionPreset, mouthForm, mouthOpen, damp } = await load('avatar/types.ts');
const v = (aa, ih = 0, ou = 0, ee = 0, oh = 0) => ({ aa, ih, ou, ee, oh });

test('core reaction labels and expression ids map to shared presets', () => {
  assert.deepEqual(emotionPreset('joy', 'exp_neutral'), { preset: 'happy', weight: 0.85 });
  assert.equal(emotionPreset('unknown', 'exp_worry').preset, 'sad');
  assert.equal(emotionPreset('neutral', 'exp_neutral').preset, undefined);
  for (const label of ['joy', 'warm', 'concern', 'sad', 'alert', 'surprise', 'think', 'calm', 'angry']) {
    assert.ok(emotionPreset(label, '').preset, `${label} is mapped`);
  }
});

test('vowels drive Live2D jaw and lip spread consistently', () => {
  assert.equal(mouthOpen(v(0)), 0);
  assert.ok(mouthOpen(v(1)) > mouthOpen(v(0, 1)), 'A opens wider than I');
  assert.ok(mouthForm(v(0, 1)) > 0 && mouthForm(v(0, 0, 1)) < 0, 'I spreads, U rounds');
  assert.equal(mouthForm(v(0)), 0);
});

test('damping is frame-rate independent', () => {
  let a = 0; for (let i = 0; i < 2; i++) a = damp(a, 1, 10, 0.016);
  const b = damp(0, 1, 10, 0.032);
  assert.ok(Math.abs(a - b) < 1e-9);
});
