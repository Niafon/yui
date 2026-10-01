// Open /tests/lipsync.html with `npm run dev`. Plays the bundled voice sample
// through SpeechOutput (real wLipSync worklet) and compares the mouth signal
// with the previous amplitude-only mapping: min(1, max(0, rms - 0.008) * 7).
import { SpeechOutput } from "../src/speech";
import { loadWLipSync } from "../src/lipsync-loader";
import { mouthOpen, type Visemes } from "../src/avatar/types";

const results = document.querySelector("#results")!;
const frames: Array<{ t: number; open: number; v: Visemes; legacy: number }> = [];
const context = new AudioContext();
const legacyAnalyser = context.createAnalyser();
legacyAnalyser.fftSize = 512;
const wave = new Float32Array(512);
const featureAnalyser = context.createAnalyser();
featureAnalyser.fftSize = 1024; featureAnalyser.smoothingTimeConstant = 0.35;
const spectrum = new Float32Array(featureAnalyser.frequencyBinCount);
const features: Array<{ openness: number; front: number }> = [];
const output = new SpeechOutput((v) => {
  legacyAnalyser.getFloatTimeDomainData(wave);
  const rms = Math.sqrt(wave.reduce((sum, x) => sum + x * x, 0) / wave.length);
  featureAnalyser.getFloatFrequencyData(spectrum);
  let low = 0, mid = 0, high = 0;
  const hz = context.sampleRate / featureAnalyser.fftSize;
  for (let i = 1; i < spectrum.length; i++) {
    const f = i * hz; if (f < 250 || f > 3400) continue;
    const e = Math.pow(10, (spectrum[i] ?? -140) / 10);
    if (f < 900) low += e; else if (f < 1700) mid += e; else high += e;
  }
  const total = low + mid + high;
  if (total > 1e-7) features.push({ openness: (low + mid * 0.5) / total, front: high / total });
  frames.push({ t: context.currentTime, open: mouthOpen(v), v, legacy: Math.min(1, Math.max(0, rms - 0.008) * 7) });
}, { createContext: () => context, loadLipSync: loadWLipSync });
await context.resume();
if (new URLSearchParams(location.search).get("engine") === "spectral") output.setSettings({ engine: "spectral" });
output.ensureContext();
// Give the worklet time to register before speech starts.
await new Promise(resolve => setTimeout(resolve, 800));
const buffer = await context.decodeAudioData(await (await fetch("/assets/voice/xenia-demo.wav")).arrayBuffer());
// Tap the same signal for the legacy comparison.
const tap = context.createBufferSource(); tap.buffer = buffer; tap.connect(legacyAnalyser); tap.connect(featureAnalyser);
output.playBuffer(buffer);
tap.start(context.currentTime);
await new Promise(resolve => setTimeout(resolve, buffer.duration * 1000 + 600));

function stats(values: number[]) {
  const deltas = values.slice(1).map((x, i) => Math.abs(x - values[i]!));
  return {
    mean: +(values.reduce((a, b) => a + b, 0) / values.length).toFixed(3),
    saturated: +(values.filter(x => x >= 0.98).length / values.length).toFixed(3),
    jumps: deltas.filter(d => d > 0.25).length,
    meanDelta: +(deltas.reduce((a, b) => a + b, 0) / deltas.length).toFixed(4),
  };
}
const speaking = frames.filter(f => f.legacy > 0.02 || f.open > 0.02);
const dominant: Record<string, number> = {};
for (const f of speaking) {
  const [key] = Object.entries(f.v).sort((a, b) => b[1] - a[1])[0]!;
  dominant[key] = (dominant[key] ?? 0) + 1;
}
const report = {
  engine: output.engine, frames: frames.length, duration: +buffer.duration.toFixed(2),
  new: stats(speaking.map(f => f.open)), legacy: stats(speaking.map(f => f.legacy)),
  dominantViseme: dominant,
  features: ["openness", "front"].map(key => {
    const values = features.map(f => f[key as "openness"]).sort((a, b) => a - b);
    const q = (p: number) => +(values[Math.floor(p * (values.length - 1))] ?? 0).toFixed(3);
    return { key, p10: q(0.1), p25: q(0.25), p50: q(0.5), p75: q(0.75), p90: q(0.9) };
  }), closesAtEnd: (frames.at(-1)?.open ?? 1) < 0.01,
};
results.textContent = JSON.stringify(report, null, 2);
(window as unknown as { lipsyncReport: unknown }).lipsyncReport = report;
