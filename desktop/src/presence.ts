/**
 * The presence ribbon.
 *
 * One canvas renders every state the companion can be in, so the operator can
 * tell from motion alone what is happening: slow breathing when idle, input
 * level while listening, a travelling pulse while the model thinks, and real
 * output amplitude while speaking. It replaces four separate spinners with one
 * continuous signal.
 */

export type PresenceState = "idle" | "listening" | "thinking" | "speaking" | "error";

const COLOURS: Record<PresenceState, string> = {
  idle: "#6b6579",
  listening: "#6fbf9b",
  thinking: "#e3a951",
  speaking: "#e3a951",
  error: "#d9635b",
};

export class PresenceRibbon {
  private state: PresenceState = "idle";
  private level = 0;
  private phase = 0;
  private raf = 0;
  private readonly ctx: CanvasRenderingContext2D;
  private readonly reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  constructor(private readonly canvas: HTMLCanvasElement) {
    const ctx = canvas.getContext("2d");
    if (!ctx) throw new Error("presence ribbon needs a 2d canvas context");
    this.ctx = ctx;
    this.resize();
    window.addEventListener("resize", () => this.resize());
    this.loop();
  }

  setState(state: PresenceState): void {
    this.state = state;
  }

  /** Feeds real amplitude from microphone input or synthesised audio. */
  setLevel(level: number): void {
    this.level = Math.max(0, Math.min(1, level));
  }

  private resize(): void {
    const ratio = window.devicePixelRatio || 1;
    const width = this.canvas.clientWidth || 1200;
    const height = 72;
    this.canvas.width = width * ratio;
    this.canvas.height = height * ratio;
    this.ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
  }

  private amplitudeAt(x: number, width: number): number {
    switch (this.state) {
      case "listening":
        return this.level * Math.sin(x / 18 + this.phase * 4);
      case "thinking": {
        // A pulse that travels left to right: work is moving somewhere.
        const centre = (this.phase * 1.6 * width) % (width * 1.4) - width * 0.2;
        const envelope = Math.exp(-((x - centre) ** 2) / (2 * 60 ** 2));
        return envelope * Math.sin(x / 8);
      }
      case "speaking":
        return (0.35 + this.level * 0.65) * Math.sin(x / 9 + this.phase * 9) * Math.sin(x / 47);
      case "error":
        return 0.5 * Math.sign(Math.sin(x / 30));
      default:
        // Breathing: a slow, shallow rise and fall.
        return 0.16 * Math.sin(x / 90 + this.phase * 0.7);
    }
  }

  private loop = (): void => {
    const width = this.canvas.clientWidth || 1200;
    const height = 72;
    const mid = height / 2;
    this.phase += this.reduced ? 0.002 : 0.012;

    this.ctx.clearRect(0, 0, width, height);

    // Baseline: always visible, so the ribbon reads as an instrument even at
    // rest rather than as an empty area.
    this.ctx.strokeStyle = "rgba(232, 228, 218, 0.08)";
    this.ctx.lineWidth = 1;
    this.ctx.beginPath();
    this.ctx.moveTo(0, mid);
    this.ctx.lineTo(width, mid);
    this.ctx.stroke();

    this.ctx.strokeStyle = COLOURS[this.state];
    this.ctx.lineWidth = 1.5;
    this.ctx.beginPath();
    for (let x = 0; x <= width; x += 2) {
      const y = mid - this.amplitudeAt(x, width) * (mid - 8);
      if (x === 0) this.ctx.moveTo(x, y);
      else this.ctx.lineTo(x, y);
    }
    this.ctx.stroke();

    this.raf = requestAnimationFrame(this.loop);
  };

  dispose(): void {
    cancelAnimationFrame(this.raf);
  }
}
