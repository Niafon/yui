import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';
import ts from 'typescript';

// Exercise the actual stage audio wiring with a deterministic audio clock.
const main = readFileSync(new URL('../src/main.ts', import.meta.url), 'utf8');
const code = main.slice(main.indexOf('function playAudioChunk('), main.indexOf('function formatPercent('));
const compiled = ts.transpileModule(code, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText;
function setup() {
  const frames = new Map(), sources = [], levels = [];
  let id = 0, amplitude = 0;
  class Context {
    currentTime = 0; destination = {};
    createAnalyser() { return { connect() {}, getFloatTimeDomainData(a) { a.fill(amplitude); } }; }
    createBuffer(_, size, rate) { return { duration: size / rate, getChannelData: () => new Float32Array(size) }; }
    createBufferSource() {
      const source = { connect() {}, disconnect() {}, start(time) { this.time = time; }, stop() { this.stopped = true; } };
      sources.push(source); return source;
    }
    resume() { return Promise.resolve(); }
    close() { return Promise.resolve(); }
  }
  const scope = vm.createContext({ AudioContext: Context, Float32Array, Uint8Array, Int16Array, atob,
    avatar: { setMouth: value => levels.push(value) }, ribbon: { setLevel() {} },
    requestAnimationFrame: callback => { frames.set(++id, callback); return id; },
    cancelAnimationFrame: id => frames.delete(id),
  });
  vm.runInContext(compiled, scope);
  return { scope, sources, levels, amplitude: v => amplitude = v, tick() { const callbacks = [...frames.values()]; frames.clear(); callbacks.forEach(f => f()); } };
}
test('queued chunks do not animate mouths before playback; analyser drives lipsync', () => {
  const s = setup();
  s.scope.playAudioChunk(Buffer.alloc(6400, 80).toString('base64'), 16000);
  s.scope.playAudioChunk(Buffer.alloc(6400, 80).toString('base64'), 16000);
  assert.deepEqual(s.sources.map(x => x.time), [0, 0.2]);
  assert.equal(s.levels.length, 0);
  s.tick(); assert.equal(s.levels.at(-1), 0);
  s.amplitude(0.1); s.tick(); assert.ok(s.levels.at(-1) > 0.5);
  s.sources.forEach(x => x.onended()); s.tick(); assert.equal(s.levels.at(-1), 0);
});
test('stop cancels every queued source and resets mouth', () => {
  const s = setup();
  s.scope.playAudioChunk('AAAAAA==', 16000);
  s.scope.stopSpeech();
  assert.ok(s.sources.every(x => x.stopped));
  assert.equal(s.levels.at(-1), 0);
});
test('turn completion does not reset ongoing playback mouth state', () => {
  const done = main.slice(main.indexOf('case "turn.done"'), main.indexOf('case "transcript.final"'));
  assert.ok(!done.includes('setMouth'));
});
