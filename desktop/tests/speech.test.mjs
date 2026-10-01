import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';
import { load } from './bundle.mjs';

const { SpeechOutput } = await load('speech.ts');
const { mouthOpen } = await load('avatar/types.ts');

function setup({ lip, amplitude = 0 } = {}) {
  const frames = new Map(); const sources = []; const out = [];
  let id = 0, now = 0, closed = 0;
  const node = () => ({ connect() {}, disconnect() {}, gain: { value: 1 } });
  class Context {
    currentTime = 0; state = 'running'; destination = {}; sampleRate = 48000;
    createGain() { return node(); }
    createAnalyser() {
      return { ...node(), fftSize: 1024, frequencyBinCount: 512, getFloatTimeDomainData(a) { a.fill(amplitude); } };
    }
    createBuffer(_, size, rate) { return { duration: size / rate, getChannelData: () => new Float32Array(size) }; }
    createBufferSource() {
      const source = { ...node(), start(time) { this.time = time; }, stop() { this.stopped = true; } };
      sources.push(source); return source;
    }
    resume() { return Promise.resolve(); }
    close() { closed++; this.state = 'closed'; return Promise.resolve(); }
  }
  const scheduler = {
    request: cb => { frames.set(++id, cb); return id; },
    cancel: i => frames.delete(i),
    now: () => now,
  };
  const output = new SpeechOutput((v, level) => out.push({ v, level }), {
    createContext: () => new Context(), scheduler,
    loadLipSync: lip ? async () => lip : undefined,
  });
  const tick = (ms = 16) => { now += ms; const callbacks = [...frames.values()]; frames.clear(); callbacks.forEach(f => f()); };
  return { output, sources, out, tick, frames, closed: () => closed };
}
const chunk = Buffer.alloc(6400, 80).toString('base64'); // 0.2 s at 16 kHz

test('chunks queue back to back and nothing animates before playback ticks', () => {
  const s = setup();
  s.output.playPcm16(chunk, 16000);
  s.output.playPcm16(chunk, 16000);
  assert.deepEqual(s.sources.map(x => x.time), [0, 0.2]);
  assert.equal(s.out.length, 0);
});

test('vowel weights open the mouth smoothly instead of snapping', async () => {
  const lip = { connect() {}, weights: { A: 1, I: 0, U: 0, E: 0, O: 0, S: 0 }, volume: 1, minVolume: 0, maxVolume: 0, smoothness: 0 };
  const s = setup({ lip });
  s.output.ensureContext();
  await new Promise(r => setImmediate(r));
  assert.equal(s.output.engine, 'mfcc');
  s.output.playPcm16(chunk, 16000);
  s.tick();
  const first = mouthOpen(s.out.at(-1).v);
  assert.ok(first > 0.2 && first < 0.6, `first frame is a partial opening (${first})`);
  for (let i = 0; i < 10; i++) s.tick();
  assert.ok(s.out.at(-1).v.aa > 0.95, 'reaches the target within ~170 ms');
  lip.weights = { A: 0, I: 0, U: 1, E: 0, O: 0, S: 0 };
  s.tick();
  const blend = s.out.at(-1).v;
  assert.ok(blend.aa > 0.3 && blend.ou > 0.2, 'shapes cross-fade between vowels');
  assert.ok(Object.values(blend).reduce((a, b) => a + b, 0) <= 1.0001, 'combined shapes never exceed one opening');
  s.sources.forEach(x => x.onended());
  for (let i = 0; i < 40; i++) s.tick();
  assert.equal(mouthOpen(s.out.at(-1).v), 0, 'mouth closes after speech');
  assert.equal(s.frames.size, 0, 'animation loop stops when settled');
});

test('stop cancels every queued source but keeps the audio context for later speech', () => {
  const s = setup();
  s.output.playPcm16(chunk, 16000);
  s.output.playPcm16(chunk, 16000);
  s.output.stop();
  assert.ok(s.sources.every(x => x.stopped));
  assert.equal(s.closed(), 0, 'closing the context would need a new user gesture to play again');
  s.output.playPcm16(chunk, 16000);
  assert.equal(s.sources.at(-1).time, 0, 'playhead restarts after stop');
});

test('a window that does not own audio drops chunks', () => {
  const s = setup();
  s.output.enabled = false;
  s.output.playPcm16(chunk, 16000);
  assert.equal(s.sources.length, 0);
});

test('spectral fallback follows loudness when no MFCC worklet is available', () => {
  const quiet = setup({ amplitude: 0 });
  quiet.output.playPcm16(chunk, 16000);
  for (let i = 0; i < 5; i++) quiet.tick();
  assert.equal(mouthOpen(quiet.out.at(-1).v), 0);
  const loud = setup({ amplitude: 0.1 });
  assert.equal(loud.output.engine, 'spectral');
  loud.output.playPcm16(chunk, 16000);
  for (let i = 0; i < 8; i++) loud.tick();
  assert.ok(mouthOpen(loud.out.at(-1).v) > 0.3);
  assert.ok(loud.out.at(-1).level > 0.5);
});

test('turn completion leaves playback-driven mouth state alone', () => {
  const main = readFileSync(new URL('../src/main.ts', import.meta.url), 'utf8');
  const done = main.slice(main.indexOf('case "turn.done"'), main.indexOf('case "transcript.final"'));
  assert.ok(!/stopSpeech|setVisemes/.test(done));
});
