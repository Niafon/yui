/**
 * Small procedural "alive" signals shared by both renderers: natural blinks,
 * idle eye saccades and smooth low-frequency noise for sway.
 */

import type { AvatarState } from "./types";

const rand = (min: number, max: number) => min + Math.random() * (max - min);

/** Blink timing modelled on human averages: 2–6 s apart, ~180 ms long,
 * sometimes doubled, quick close and slower open. */
export class Blinker {
  private time = 0;
  private next = rand(1, 3.5);
  private start = -1;
  private readonly duration = 0.19;

  /** Returns eyelid closure 0..1. */
  update(dt: number, state: AvatarState): number {
    this.time += dt;
    if (this.start < 0 && this.time >= this.next) {
      this.start = this.time;
      const thinking = state === "thinking";
      const double = Math.random() < 0.14;
      this.next = this.time + (double ? 0.32 : thinking ? rand(1.5, 3.5) : rand(2.2, 6));
    }
    if (this.start < 0) return 0;
    const p = (this.time - this.start) / this.duration;
    if (p >= 1) { this.start = -1; return 0; }
    const close = 0.32, hold = 0.12;
    if (p < close) { const x = p / close; return x * x; }
    if (p < close + hold) return 1;
    const x = (p - close - hold) / (1 - close - hold);
    return 1 - (1 - (1 - x) * (1 - x));
  }

  /** Trigger a blink soon, e.g. on a gaze shift or state change. */
  nudge(): void {
    if (this.start < 0) this.next = Math.min(this.next, this.time + 0.05);
  }
}

/** Idle eye movement: brief fixations with fast jumps between them. */
export class Saccades {
  private time = 0;
  private next = rand(0.6, 2);
  x = 0;
  y = 0;

  /** Returns a gaze offset in -1..1 units around the current focus. */
  update(dt: number, state: AvatarState): { x: number; y: number } {
    this.time += dt;
    if (this.time >= this.next) {
      const thinking = state === "thinking";
      const listening = state === "listening";
      const spread = listening ? 0.08 : state === "speaking" ? 0.14 : 0.22;
      if (thinking) {
        // Looking up and aside while recalling, a common thinking cue.
        this.x = rand(0.35, 0.6) * (Math.random() < 0.5 ? -1 : 1);
        this.y = rand(0.3, 0.55);
      } else {
        this.x = rand(-spread, spread);
        this.y = rand(-spread * 0.6, spread * 0.6);
      }
      this.next = this.time + (thinking ? rand(1.2, 2.6) : listening ? rand(1.2, 3) : rand(0.5, 2.2));
    }
    return { x: this.x, y: this.y };
  }
}

/** Sum of incommensurate sines: smooth, non-repeating, bounded to ±1. */
export function noise(t: number, seed: number): number {
  return (Math.sin(t * 0.61 + seed) * 0.5 + Math.sin(t * 1.37 + seed * 2.1) * 0.3 + Math.sin(t * 2.71 + seed * 3.7) * 0.2);
}
