/**
 * Microphone capture with two modes:
 *  - push: record while the button is active, send on stop;
 *  - handsfree: the mic stays open and an energy VAD with an adaptive noise
 *    floor cuts utterances automatically (pre-roll keeps the first syllable).
 * PCM goes to the core's data socket, which bounds one utterance to ~60 s of
 * 16 kHz audio; the limit below follows the real context sample rate.
 */

import { t } from "./i18n";

export interface MicSink {
  sendAudio(pcm: ArrayBuffer): void;
  send(message: Record<string, unknown>): void;
}

export interface MicCallbacks {
  /** Human readable state for the status line. */
  status(text: string, active: boolean): void;
  /** Input level 0..1 for the presence ribbon. */
  level(value: number): void;
  error(message: string): void;
  /** Hands-free detected speech start; true if Yui is currently talking. */
  speechStart(): void;
}

export interface VadOptions {
  sensitivity: number;
  isAssistantSpeaking: () => boolean;
  bargeIn: boolean;
  /** "silero" runs the neural detector; it falls back to "energy" on failure. */
  engine: "silero" | "energy";
}

/** Minimal surface of @ricky0123/vad-web's MicVAD. */
interface NeuralVad { start(): Promise<void>; destroy(): Promise<void> }
/** The core keeps at most ~60 s of 16 kHz audio per utterance. */
const MAX_UTTERANCE_SAMPLES = 16000 * 55;

const PREROLL_MS = 320;
const START_MS = 140;
const HANGOVER_MS = 850;

export function microphoneError(error: unknown): string {
  if (error instanceof DOMException && (error.name === "NotAllowedError" || error.name === "SecurityError")) {
    return t("mic.denied");
  }
  if (error instanceof DOMException && error.name === "NotFoundError") return t("mic.notFound");
  return String(error);
}

export class Microphone {
  private stream?: MediaStream;
  private context?: AudioContext;
  private node?: AudioWorkletNode;
  private timer?: number;
  private generation = 0;
  private mode: "push" | "handsfree" = "push";
  // VAD state
  private floor = 0.004;
  private speaking = false;
  private above = 0;
  private below = 0;
  private utteranceMs = 0;
  private preroll: ArrayBuffer[] = [];
  private prerollMs = 0;
  private neural?: NeuralVad;

  constructor(
    private readonly sink: () => MicSink | undefined,
    private readonly callbacks: MicCallbacks,
    private readonly vad: () => VadOptions,
  ) {}

  get active(): boolean { return Boolean(this.stream); }
  get handsFree(): boolean { return this.active && this.mode === "handsfree"; }

  async start(mode: "push" | "handsfree"): Promise<void> {
    const generation = ++this.generation;
    this.mode = mode;
    try {
      if (!navigator.mediaDevices?.getUserMedia) {
        throw new Error(t("mic.unavailable"));
      }
      const stream = await navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true, channelCount: 1 } });
      if (generation !== this.generation) { stream.getTracks().forEach(track => track.stop()); return; }
      if (mode === "handsfree" && this.vad().engine === "silero") {
        this.stream = stream;
        try {
          await this.startNeural(stream, generation);
          return;
        } catch (error) {
          if (generation !== this.generation) return;
          console.warn("Silero VAD unavailable", error);
          this.callbacks.error(t("mic.vadFallback", { error: error instanceof Error ? error.message : String(error) }));
        }
      }
      const context = createContext();
      this.stream = stream;
      this.context = context;
      await context.resume();
      await context.audioWorklet.addModule("/pcm-capture.js?v=2");
      if (generation !== this.generation) { this.release(); return; }
      const node = new AudioWorkletNode(context, "pcm-capture");
      this.node = node;
      const chunkMs = (buffer: ArrayBuffer) => (buffer.byteLength / 2 / context.sampleRate) * 1000;
      const limitMs = Math.max(1000, Math.min(50000, Math.floor((16000 / context.sampleRate) * 60000) - 1000));
      node.port.onmessage = (event: MessageEvent<ArrayBuffer>) => {
        if (generation !== this.generation) return;
        const pcm = event.data;
        if (this.mode === "push") {
          this.forward(pcm);
          this.callbacks.level(Math.min(1, rms(pcm) * 6));
          return;
        }
        this.detect(pcm, chunkMs(pcm), limitMs);
      };
      context.createMediaStreamSource(stream).connect(node);
      node.connect(context.destination);
      if (mode === "push") {
        const seconds = Math.max(1, Math.floor(limitMs / 1000));
        this.callbacks.status(t("mic.push", { seconds }), true);
        this.timer = window.setTimeout(() => this.stop(true), limitMs);
      } else {
        this.resetVad();
        this.callbacks.status(t("mic.handsfree"), true);
      }
    } catch (error) {
      if (generation !== this.generation) return;
      this.stop(false);
      this.callbacks.error(microphoneError(error));
    }
  }

  /** Stops capture. send=true finishes a push-to-talk utterance. */
  stop(send: boolean): void {
    this.generation += 1;
    const wasSpeaking = this.mode === "handsfree" && this.speaking;
    const context = this.context;
    this.release();
    const sink = this.sink();
    try {
      if (send || wasSpeaking) sink?.send({ type: "audio.end", sample_rate: context?.sampleRate ?? 16000, channels: 1 });
      else sink?.send({ type: "audio.clear" });
    } catch { /* socket already closed */ }
    this.speaking = false;
    this.callbacks.level(0);
    this.callbacks.status(t("mic.off"), false);
  }

  /** Neural VAD: Silero v5 in onnxruntime-web; one PCM upload per utterance. */
  private async startNeural(stream: MediaStream, generation: number): Promise<void> {
    const { MicVAD } = await import("@ricky0123/vad-web");
    const options = this.vad();
    const sensitivity = Math.max(0.3, Math.min(3, options.sensitivity));
    const positive = Math.max(0.2, Math.min(0.85, 0.5 / sensitivity));
    let interrupted = false;
    const vad = await MicVAD.new({
      model: "v5",
      baseAssetPath: "/vad/",
      onnxWASMBasePath: "/vad/",
      getStream: async () => stream,
      pauseStream: async () => undefined,
      resumeStream: async current => current,
      positiveSpeechThreshold: positive,
      negativeSpeechThreshold: Math.max(0.1, positive - 0.15),
      redemptionMs: 700,
      preSpeechPadMs: 320,
      minSpeechMs: 250,
      onFrameProcessed: (_probabilities, frame) => {
        let sum = 0;
        for (const sample of frame) sum += sample * sample;
        this.callbacks.level(Math.min(1, Math.sqrt(sum / frame.length) * 6));
      },
      onSpeechRealStart: () => {
        if (generation !== this.generation) return;
        const current = this.vad();
        interrupted = current.isAssistantSpeaking();
        // Echo of Yui's own voice must not start a turn unless barge-in is on.
        if (interrupted && !current.bargeIn) return;
        if (interrupted) {
          try { this.sink()?.send({ type: "barge_in", text: "speech_aec" }); } catch { /* closed */ }
        }
        this.speaking = true;
        this.callbacks.speechStart();
        this.callbacks.status(t("mic.hearing"), true);
      },
      onVADMisfire: () => { if (generation === this.generation) this.callbacks.status(t("mic.handsfree"), true); },
      onSpeechEnd: audio => {
        if (generation !== this.generation) return;
        const wanted = this.speaking;
        this.speaking = false;
        this.callbacks.status(t("mic.handsfree"), true);
        if (!wanted) return;
        const samples = audio.length > MAX_UTTERANCE_SAMPLES ? audio.subarray(audio.length - MAX_UTTERANCE_SAMPLES) : audio;
        const pcm = new Int16Array(samples.length);
        for (let i = 0; i < samples.length; i++) pcm[i] = Math.round(Math.max(-1, Math.min(1, samples[i] ?? 0)) * 32767);
        try {
          // Chunked like the live path so one frame never exceeds socket limits.
          for (let offset = 0; offset < pcm.length; offset += 16000) {
            this.sink()?.sendAudio(pcm.slice(offset, offset + 16000).buffer);
          }
          this.sink()?.send({ type: "audio.end", sample_rate: 16000, channels: 1 });
        } catch (error) { this.stop(false); this.callbacks.error(String(error)); }
      },
    });
    if (generation !== this.generation) { await vad.destroy(); return; }
    this.neural = vad;
    await vad.start();
    this.callbacks.status(t("mic.handsfree"), true);
  }

  private release(): void {
    const neural = this.neural;
    this.neural = undefined;
    void neural?.destroy().catch(() => undefined);
    this.stream?.getTracks().forEach(track => track.stop());
    this.node?.disconnect();
    if (this.timer !== undefined) clearTimeout(this.timer);
    const context = this.context;
    if (context && context.state !== "closed") void context.close().catch(() => undefined);
    this.stream = undefined; this.context = undefined; this.node = undefined; this.timer = undefined;
  }

  private forward(pcm: ArrayBuffer): void {
    try { this.sink()?.sendAudio(pcm); }
    catch (error) { this.stop(false); this.callbacks.error(String(error)); }
  }

  private resetVad(): void {
    this.speaking = false; this.above = 0; this.below = 0; this.utteranceMs = 0;
    this.preroll = []; this.prerollMs = 0;
  }

  private detect(pcm: ArrayBuffer, ms: number, limitMs: number): void {
    const options = this.vad();
    const energy = rms(pcm);
    const assistant = options.isAssistantSpeaking();
    // Echo cancellation is imperfect; demand more energy while Yui talks.
    const gate = assistant ? (options.bargeIn ? 2.6 : Infinity) : 1;
    const sensitivity = Math.max(0.3, Math.min(3, options.sensitivity));
    const threshold = Math.max(0.012 / sensitivity, this.floor * 3.2 / sensitivity) * gate;
    this.callbacks.level(Math.min(1, energy * 6));

    if (!this.speaking) {
      // Track background noise only between utterances.
      this.floor = this.floor + (Math.min(energy, 0.05) - this.floor) * (energy < this.floor ? 0.05 : 0.004);
      this.preroll.push(pcm);
      this.prerollMs += ms;
      while (this.prerollMs > PREROLL_MS && this.preroll.length > 1) {
        const dropped = this.preroll.shift()!;
        this.prerollMs -= (dropped.byteLength / 2 / (this.context?.sampleRate ?? 16000)) * 1000;
      }
      this.above = energy > threshold ? this.above + ms : 0;
      if (this.above >= START_MS) {
        this.speaking = true;
        this.below = 0;
        this.utteranceMs = this.prerollMs;
        if (assistant) {
          try { this.sink()?.send({ type: "barge_in", text: "speech_aec" }); } catch { /* closed */ }
        }
        this.callbacks.speechStart();
        this.callbacks.status(t("mic.hearing"), true);
        for (const chunk of this.preroll) this.forward(chunk);
        this.preroll = []; this.prerollMs = 0;
      }
      return;
    }

    this.forward(pcm);
    this.utteranceMs += ms;
    this.below = energy < threshold * 0.65 ? this.below + ms : 0;
    if (this.below >= HANGOVER_MS || this.utteranceMs >= limitMs) {
      try {
        this.sink()?.send({ type: "audio.end", sample_rate: this.context?.sampleRate ?? 16000, channels: 1 });
      } catch { /* closed */ }
      this.resetVad();
      this.callbacks.status(t("mic.handsfree"), true);
    }
  }
}

function createContext(): AudioContext {
  try {
    return new AudioContext({ sampleRate: 16000 });
  } catch {
    // iOS Safari can reject a requested rate; audio.end reports the real one.
    return new AudioContext();
  }
}

export function rms(pcm: ArrayBuffer): number {
  const samples = new Int16Array(pcm);
  if (!samples.length) return 0;
  let sum = 0;
  for (const sample of samples) sum += (sample / 32768) ** 2;
  return Math.sqrt(sum / samples.length);
}
