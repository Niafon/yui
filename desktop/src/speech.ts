/**
 * Speech playback and lip sync.
 *
 * Streamed TTS chunks are queued back to back on one long-lived AudioContext.
 * The same signal feeds a vowel classifier (wLipSync MFCC worklet when it is
 * available, a spectral heuristic otherwise). Raw classifier output is noisy,
 * so each viseme is smoothed with separate attack and release rates: the
 * mouth opens quickly on a syllable and relaxes a little slower, the way a
 * real jaw does, instead of snapping between closed and fully open.
 */

import { VISEME_KEYS, silentVisemes, type Visemes } from "./avatar/types";

/** Minimal surface of a WLipSyncAudioNode. */
export interface LipSyncNode extends AudioNode {
  readonly weights: Record<string, number>;
  readonly volume: number;
  minVolume: number;
  maxVolume: number;
  smoothness: number;
}

export interface LipSyncSettings {
  /** 0.5..2, scales how loud speech must be to open the mouth fully. */
  sensitivity: number;
  /** "auto" prefers the MFCC classifier; "spectral" forces the fallback. */
  engine: "auto" | "spectral";
  /** Playback volume 0..1. Analysis runs before the volume stage. */
  volume: number;
}

export const DEFAULT_LIPSYNC: LipSyncSettings = { sensitivity: 1, engine: "auto", volume: 1 };

type FrameCallback = (visemes: Visemes, level: number) => void;

interface Scheduler {
  request(callback: () => void): number;
  cancel(id: number): void;
  now(): number;
}

export interface SpeechOutputOptions {
  createContext?: () => AudioContext;
  loadLipSync?: (context: AudioContext) => Promise<LipSyncNode>;
  scheduler?: Scheduler;
}

const ATTACK = 30; // 1/s: ~33 ms to cover most of a step up
const RELEASE = 13; // 1/s: ~75 ms to relax
const LEVEL_RATE = 18;

const clamp01 = (value: number) => (Number.isFinite(value) ? Math.max(0, Math.min(1, value)) : 0);
const tri = (x: number, centre: number, width: number) => Math.max(0, 1 - Math.abs(x - centre) / width);

export class SpeechOutput {
  /** False when another window owns audio; chunks are then dropped. */
  enabled = true;

  private context?: AudioContext;
  private input?: GainNode;
  private output?: GainNode;
  private analyser?: AnalyserNode;
  private lipNode?: LipSyncNode;
  private lipFailed = false;
  private readonly sources = new Set<AudioBufferSourceNode>();
  private playhead = 0;
  private frame = 0;
  private lastTick = 0;
  private level = 0;
  private readonly current = silentVisemes();
  private readonly target = silentVisemes();
  private settings: LipSyncSettings = { ...DEFAULT_LIPSYNC };
  private wave?: Float32Array<ArrayBuffer>;
  private spectrum?: Float32Array<ArrayBuffer>;
  private readonly scheduler: Scheduler;

  constructor(private readonly onFrame: FrameCallback, private readonly options: SpeechOutputOptions = {}) {
    this.scheduler = options.scheduler ?? {
      request: callback => requestAnimationFrame(callback),
      cancel: id => cancelAnimationFrame(id),
      now: () => performance.now(),
    };
  }

  get speaking(): boolean { return this.sources.size > 0; }
  get audioContext(): AudioContext | undefined { return this.context; }
  get suspended(): boolean { return this.context?.state === "suspended"; }
  get engine(): "mfcc" | "spectral" { return this.lipNode && this.settings.engine === "auto" ? "mfcc" : "spectral"; }

  setSettings(settings: Partial<LipSyncSettings>): void {
    this.settings = { ...this.settings, ...settings };
    if (this.output) this.output.gain.value = clamp01(this.settings.volume);
    this.tuneLipNode();
  }

  /** Creates the graph lazily; call from a user gesture where possible. */
  ensureContext(): AudioContext {
    if (this.context && this.context.state !== "closed") return this.context;
    const context = this.options.createContext?.() ?? new AudioContext();
    this.context = context;
    this.input = context.createGain();
    this.output = context.createGain();
    this.output.gain.value = clamp01(this.settings.volume);
    this.analyser = context.createAnalyser();
    this.analyser.fftSize = 1024;
    this.analyser.smoothingTimeConstant = 0.35;
    this.input.connect(this.output);
    this.output.connect(context.destination);
    this.input.connect(this.analyser);
    this.lipNode = undefined;
    if (this.options.loadLipSync && !this.lipFailed) {
      const owner = context;
      void this.options.loadLipSync(context).then(node => {
        if (this.context !== owner) return;
        this.lipNode = node;
        this.tuneLipNode();
        this.input?.connect(node);
        // Worklets are only pulled when they reach the destination.
        const sink = owner.createGain();
        sink.gain.value = 0;
        node.connect(sink);
        sink.connect(owner.destination);
      }).catch(error => {
        this.lipFailed = true;
        console.warn("MFCC lip sync unavailable; using the spectral fallback", error);
      });
    }
    return context;
  }

  async resume(): Promise<void> {
    const context = this.ensureContext();
    if (context.state === "suspended") await context.resume().catch(() => undefined);
  }

  playPcm16(base64: string, sampleRate: number): void {
    if (!this.enabled || !base64) return;
    const bytes = Uint8Array.from(atob(base64), c => c.charCodeAt(0));
    const samples = new Int16Array(bytes.buffer, bytes.byteOffset, Math.floor(bytes.byteLength / 2));
    if (!samples.length) return;
    const context = this.ensureContext();
    const buffer = context.createBuffer(1, samples.length, sampleRate || 16000);
    const channel = buffer.getChannelData(0);
    for (let i = 0; i < samples.length; i++) channel[i] = (samples[i] ?? 0) / 32768;
    this.playBuffer(buffer);
  }

  playBuffer(buffer: AudioBuffer): void {
    if (!this.enabled) return;
    const context = this.ensureContext();
    const source = context.createBufferSource();
    source.buffer = buffer;
    source.connect(this.input!);
    this.sources.add(source);
    source.onended = () => { this.sources.delete(source); source.disconnect(); };
    const start = Math.max(context.currentTime, this.playhead);
    this.playhead = start + buffer.duration;
    source.start(start);
    void context.resume().catch(() => undefined);
    this.kick();
  }

  /** Stops every queued chunk. The context stays alive so later speech can
   * still play without a new user gesture (autoplay policy). */
  stop(): void {
    for (const source of this.sources) {
      source.onended = null;
      try { source.stop(); } catch { /* not started yet */ }
      source.disconnect();
    }
    this.sources.clear();
    this.playhead = 0;
    for (const key of VISEME_KEYS) this.target[key] = 0;
    this.kick();
  }

  dispose(): void {
    this.stop();
    this.scheduler.cancel(this.frame);
    this.frame = 0;
    const context = this.context;
    this.context = undefined;
    this.lipNode = undefined;
    if (context && context.state !== "closed") void context.close().catch(() => undefined);
  }

  private tuneLipNode(): void {
    const node = this.lipNode;
    if (!node) return;
    const shift = 0.45 * Math.log2(Math.max(0.25, Math.min(4, this.settings.sensitivity)));
    node.minVolume = -2.55 - shift;
    node.maxVolume = -1.45 - shift;
    node.smoothness = 0.03;
  }

  private kick(): void {
    if (this.frame) return;
    this.lastTick = this.scheduler.now();
    this.frame = this.scheduler.request(this.tick);
  }

  private readonly tick = (): void => {
    this.frame = 0;
    const now = this.scheduler.now();
    const dt = Math.min(0.1, Math.max(0.001, (now - this.lastTick) / 1000));
    this.lastTick = now;
    const active = this.sources.size > 0;
    let level = 0;
    if (active) level = this.measure();
    else for (const key of VISEME_KEYS) this.target[key] = 0;

    let open = 0;
    for (const key of VISEME_KEYS) {
      const target = this.target[key];
      const value = this.current[key];
      const rate = target > value ? ATTACK : RELEASE;
      this.current[key] = target + (value - target) * Math.exp(-rate * dt);
      open += this.current[key];
    }
    // Keep combined shapes within one full opening to avoid stretched faces.
    if (open > 1) for (const key of VISEME_KEYS) this.current[key] /= open;
    this.level = level + (this.level - level) * Math.exp(-LEVEL_RATE * dt);
    this.onFrame({ ...this.current }, this.level);

    const settled = VISEME_KEYS.every(key => this.current[key] < 0.002) && this.level < 0.002;
    if (active || !settled) this.frame = this.scheduler.request(this.tick);
    else {
      for (const key of VISEME_KEYS) this.current[key] = 0;
      this.level = 0;
      this.onFrame(silentVisemes(), 0);
    }
  };

  /** Fills this.target from the classifier; returns the loudness 0..1. */
  private measure(): number {
    const node = this.settings.engine === "auto" ? this.lipNode : undefined;
    if (node) {
      const volume = clamp01(node.volume);
      const shaped = Math.pow(volume, 0.85);
      const w = node.weights;
      // "S" (sibilants) keeps the mouth nearly closed but slightly spread.
      this.target.aa = clamp01((w.A ?? 0) * shaped);
      this.target.ih = clamp01(((w.I ?? 0) + (w.S ?? 0) * 0.3) * shaped);
      this.target.ou = clamp01((w.U ?? 0) * shaped);
      this.target.ee = clamp01((w.E ?? 0) * shaped);
      this.target.oh = clamp01((w.O ?? 0) * shaped);
      return volume;
    }
    return this.measureSpectrum();
  }

  /** Formant-band heuristic: F1 energy opens the jaw, F2 energy spreads lips. */
  private measureSpectrum(): number {
    const analyser = this.analyser;
    if (!analyser) return 0;
    this.wave ??= new Float32Array(analyser.fftSize);
    this.spectrum ??= new Float32Array(analyser.frequencyBinCount);
    analyser.getFloatTimeDomainData(this.wave);
    let sum = 0;
    for (const sample of this.wave) sum += sample * sample;
    const rms = Math.sqrt(sum / this.wave.length);
    const shift = 0.45 * Math.log2(Math.max(0.25, Math.min(4, this.settings.sensitivity)));
    const volume = clamp01((Math.log10(Math.max(rms, 1e-6)) - (-2.55 - shift)) / 1.1);
    if (volume <= 0) {
      for (const key of VISEME_KEYS) this.target[key] = 0;
      return 0;
    }
    const shaped = Math.pow(volume, 0.85);
    let low = 0, mid = 0, high = 0;
    if (typeof analyser.getFloatFrequencyData === "function") {
      analyser.getFloatFrequencyData(this.spectrum);
      const hz = (this.context?.sampleRate ?? 48000) / analyser.fftSize;
      for (let i = 1; i < this.spectrum.length; i++) {
        const f = i * hz;
        if (f < 250 || f > 3400) continue;
        const energy = Math.pow(10, (this.spectrum[i] ?? -140) / 10);
        if (f < 900) low += energy; else if (f < 1700) mid += energy; else high += energy;
      }
    }
    const total = low + mid + high;
    if (total <= 0) {
      // No spectrum (very old engines): an open vowel is the safest shape.
      this.target.aa = shaped * 0.8; this.target.oh = shaped * 0.2;
      this.target.ih = this.target.ou = this.target.ee = 0;
      return volume;
    }
    // Calibrated on Silero speech: F1-band dominance → open jaw, energy above
    // 1.7 kHz (second formant of front vowels) → spread lips.
    const open = clamp01(((low + mid * 0.5) / total - 0.7) / 0.29);
    const front = clamp01((Math.log10(high / total + 1e-4) + 2.5) / 1.8);
    const weights = {
      aa: tri(open, 0.85, 0.4) * tri(front, 0.3, 0.45),
      oh: tri(open, 0.6, 0.3) * tri(front, 0.1, 0.3),
      ou: tri(open, 0.25, 0.3) * tri(front, 0.1, 0.35),
      ee: tri(front, 0.65, 0.25),
      ih: tri(front, 0.9, 0.2),
    };
    const sumWeights = weights.aa + weights.oh + weights.ou + weights.ee + weights.ih || 1;
    // Heuristic weights are less certain than MFCC; keep the jaw a bit lower.
    for (const key of VISEME_KEYS) this.target[key] = clamp01((weights[key] / sumWeights) * shaped * 0.8);
    return volume;
  }
}
