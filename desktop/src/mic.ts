/**
 * Microphone capture with two modes:
 *  - push: record while the button is active, send on stop;
 *  - handsfree: the mic stays open and an energy VAD with an adaptive noise
 *    floor cuts utterances automatically (pre-roll keeps the first syllable).
 * PCM goes to the core's data socket, which bounds one utterance to ~60 s of
 * 16 kHz audio; the limit below follows the real context sample rate.
 */

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

export interface VadOptions { sensitivity: number; isAssistantSpeaking: () => boolean; bargeIn: boolean }

const PREROLL_MS = 320;
const START_MS = 140;
const HANGOVER_MS = 850;

export function microphoneError(error: unknown): string {
  if (error instanceof DOMException && (error.name === "NotAllowedError" || error.name === "SecurityError")) {
    return "Нет доступа к микрофону. Разрешите микрофон для сайта и откройте его по HTTPS или с компьютера.";
  }
  if (error instanceof DOMException && error.name === "NotFoundError") return "Микрофон не найден.";
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
        throw new Error("Микрофон недоступен. На телефоне откройте HTTPS-адрес Юи и разрешите доступ к микрофону.");
      }
      const stream = await navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true, channelCount: 1 } });
      if (generation !== this.generation) { stream.getTracks().forEach(track => track.stop()); return; }
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
        this.callbacks.status(`Слушаю · до ${seconds} с`, true);
        this.timer = window.setTimeout(() => this.stop(true), limitMs);
      } else {
        this.resetVad();
        this.callbacks.status("Свободный разговор · говорите, когда захотите", true);
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
    this.callbacks.status("Микрофон выключен", false);
  }

  private release(): void {
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
        this.callbacks.status("● Слушаю…", true);
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
      this.callbacks.status("Свободный разговор · говорите, когда захотите", true);
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
