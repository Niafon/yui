/** Shared avatar contract. The core and UI never care which format renders. */

export type AvatarState = "idle" | "listening" | "thinking" | "speaking" | "error";

/** Mouth shapes as weights 0..1. Names follow VRM 1.0 presets. */
export interface Visemes {
  aa: number;
  ih: number;
  ou: number;
  ee: number;
  oh: number;
}

export const VISEME_KEYS = ["aa", "ih", "ou", "ee", "oh"] as const;

export function silentVisemes(): Visemes {
  return { aa: 0, ih: 0, ou: 0, ee: 0, oh: 0 };
}

/** Overall jaw opening implied by a viseme mix, 0..1. */
export function mouthOpen(v: Visemes): number {
  return Math.min(1, v.aa + v.oh * 0.8 + v.ee * 0.55 + v.ou * 0.45 + v.ih * 0.4);
}

/** Lip spread for Live2D ParamMouthForm: +1 smile/wide, -1 rounded. */
export function mouthForm(v: Visemes): number {
  const total = v.aa + v.ih + v.ou + v.ee + v.oh;
  if (total < 1e-3) return 0;
  return Math.max(-1, Math.min(1, (v.ih * 0.9 + v.ee * 0.6 - v.ou * 0.9 - v.oh * 0.6) / total));
}

export interface AvatarOptions {
  /** Zoom multiplier, 1 = default framing. */
  scale: number;
  /** Vertical framing offset, -1..1 of the stage height. */
  offsetY: number;
  /** Eyes and head follow the pointer over the stage. */
  followPointer: boolean;
  /** 0..1 strength of procedural idle motion (breathing, sway, saccades). */
  motion: number;
}

export const DEFAULT_AVATAR_OPTIONS: AvatarOptions = { scale: 1, offsetY: 0, followPointer: true, motion: 1 };

export interface Avatar {
  load(source: string): Promise<boolean>;
  setVisemes(visemes: Visemes): void;
  setState(state: AvatarState): void;
  /** emotion: core reaction label; expression: identity expression id. */
  react(emotion: string, expression: string): void;
  setOptions(options: Partial<AvatarOptions>): void;
  /** Pointer position over the stage, -1..1 each axis; null when it leaves. */
  pointer(x: number, y: number): void;
  pointerLeave(): void;
  destroy(): void;
}

/** Emotion presets understood by both renderers. */
export type EmotionPreset = "happy" | "sad" | "angry" | "surprised" | "relaxed" | "think";
export const EMOTION_PRESETS: EmotionPreset[] = ["happy", "sad", "angry", "surprised", "relaxed", "think"];

const BY_EXPRESSION: Record<string, [EmotionPreset, number]> = {
  exp_smile: ["happy", 0.7], exp_worry: ["sad", 0.55], exp_surprise: ["surprised", 0.65],
  exp_think: ["think", 0.6], exp_angry: ["angry", 0.55],
};
const BY_REACTION: Record<string, [EmotionPreset, number]> = {
  joy: ["happy", 0.85], warm: ["happy", 0.55], concern: ["sad", 0.55], sad: ["sad", 0.7],
  alert: ["surprised", 0.6], surprise: ["surprised", 0.8], angry: ["angry", 0.65],
  calm: ["relaxed", 0.5], think: ["think", 0.7],
};

/** Resolves a core reaction label or identity expression id to a preset. */
export function emotionPreset(label: string, expression: string): { preset?: EmotionPreset; weight: number } {
  const hit = BY_REACTION[label] ?? BY_EXPRESSION[expression];
  return hit ? { preset: hit[0], weight: hit[1] } : { weight: 0 };
}

export const prefersReducedMotion = (): boolean =>
  typeof window !== "undefined" && window.matchMedia?.("(prefers-reduced-motion: reduce)").matches === true;

/** Frame-rate independent exponential approach. */
export function damp(current: number, target: number, rate: number, dt: number): number {
  return target + (current - target) * Math.exp(-rate * dt);
}
