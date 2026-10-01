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

const HEIGHT = 40;

export class PresenceRibbon {
  private state: PresenceState = "idle";
  private level = 0;
  private shown = 0;
  private phase = 0;
  private raf = 0;
  private last = performance.now();
  private colour = "#e9b66d";
  private readonly ctx: CanvasRenderingContext2D;
  private readonly reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  private readonly observer: ResizeObserver;

  constructor(private readonly canvas: HTMLCanvasElement) {
    const ctx = canvas.getContext("2d");
    if (!ctx) throw new Error("presence ribbon needs a 2d canvas context");
    this.ctx = ctx;
    this.observer = new ResizeObserver(() => this.resize());
    this.observer.observe(canvas);
    this.resize();
    this.raf = requestAnimationFrame(this.loop);
  }

  setState(state: PresenceState): void {
    this.state = state;
    const styles = getComputedStyle(document.documentElement);
    const name = state === "error" ? "--coral" : state === "listening" ? "--jade" : "--accent";
    this.colour = styles.getPropertyValue(name).trim() || this.colour;
  }

  /** Feeds real amplitude from microphone input or synthesised audio. */
  setLevel(level: number): void {
    this.level = Math.max(0, Math.min(1, level));
  }

  private resize(): void {
    const ratio = window.devicePixelRatio || 1;
    const width = this.canvas.clientWidth || 600;
    this.canvas.width = Math.round(width * ratio);
    this.canvas.height = Math.round(HEIGHT * ratio);
    this.ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
  }

  private amplitudeAt(x: number, width: number): number {
    switch (this.state) {
      case "listening":
        return (0.12 + this.shown * 0.88) * Math.sin(x / 18 + this.phase * 4);
      case "thinking": {
        // A pulse that travels left to right: work is moving somewhere.
        const centre = (this.phase * 0.5 * width) % (width * 1.4) - width * 0.2;
        const envelope = Math.exp(-((x - centre) ** 2) / (2 * 60 ** 2));
        return envelope * Math.sin(x / 8);
      }
      case "speaking":
        return (0.2 + this.shown * 0.8) * Math.sin(x / 9 + this.phase * 9) * Math.sin(x / 47 + this.phase);
      case "error":
        return 0.4 * Math.sign(Math.sin(x / 30));
      default:
        return 0.14 * Math.sin(x / 90 + this.phase * 0.7);
    }
  }

  private loop = (now: number): void => {
    this.raf = requestAnimationFrame(this.loop);
    const dt = Math.min(0.1, (now - this.last) / 1000);
    this.last = now;
    if (this.canvas.hidden || !this.canvas.isConnected) return;
    this.phase += dt * (this.reduced ? 0.12 : 0.75);
    this.shown += (this.level - this.shown) * Math.min(1, dt * 14);
    const width = this.canvas.clientWidth || 600;
    const mid = HEIGHT / 2;
    this.ctx.clearRect(0, 0, width, HEIGHT);
    const gradient = this.ctx.createLinearGradient(0, 0, width, 0);
    gradient.addColorStop(0, "transparent");
    gradient.addColorStop(0.2, this.colour);
    gradient.addColorStop(0.8, this.colour);
    gradient.addColorStop(1, "transparent");
    this.ctx.strokeStyle = gradient;
    this.ctx.lineWidth = 1.6;
    this.ctx.globalAlpha = this.state === "idle" ? 0.55 : 0.95;
    this.ctx.beginPath();
    for (let x = 0; x <= width; x += 3) {
      const y = mid - this.amplitudeAt(x, width) * (mid - 4);
      if (x === 0) this.ctx.moveTo(x, y);
      else this.ctx.lineTo(x, y);
    }
    this.ctx.stroke();
    this.ctx.globalAlpha = 1;
  };

  dispose(): void {
    cancelAnimationFrame(this.raf);
    this.observer.disconnect();
  }
}
