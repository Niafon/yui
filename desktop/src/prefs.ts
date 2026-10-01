/**
 * Local interface preferences. They describe this screen, not the companion,
 * so they live in the browser profile and sync between Yui windows through
 * the storage event. Character, memory and models stay in the core.
 */

import { normalizeAvatarId } from "./avatar/builtin";

export type Theme = "system" | "dark" | "light";
export type Backdrop = "aurora" | "plain" | "transparent";
export type MicMode = "push" | "handsfree";
export type UiLang = "auto" | "ru" | "en";

export interface Prefs {
  avatar: string;
  avatarScale: number;
  avatarOffsetY: number;
  followPointer: boolean;
  motion: number;
  backdrop: Backdrop;
  theme: Theme;
  accent: string;
  volume: number;
  lipSensitivity: number;
  lipEngine: "auto" | "spectral";
  micMode: MicMode;
  vadEngine: "silero" | "energy";
  lang: UiLang;
  vadSensitivity: number;
  bargeIn: boolean;
  sendOnEnter: boolean;
  showRibbon: boolean;
  avatarOnTop: boolean;
}

export const DEFAULT_PREFS: Prefs = {
  avatar: "shino",
  avatarScale: 1,
  avatarOffsetY: 0,
  followPointer: true,
  motion: 1,
  backdrop: "aurora",
  theme: "system",
  accent: "#e9b66d",
  volume: 1,
  lipSensitivity: 1,
  lipEngine: "auto",
  micMode: "push",
  vadEngine: "silero",
  lang: "auto",
  vadSensitivity: 1,
  bargeIn: false,
  sendOnEnter: true,
  showRibbon: true,
  avatarOnTop: true,
};

export const ACCENTS = ["#e9b66d", "#f08b9c", "#9b8cf0", "#6fc3df", "#84cfad", "#f2a65a"];

const KEY = "yui.prefs";
type Listener = (prefs: Prefs, changed: Array<keyof Prefs>) => void;
const listeners = new Set<Listener>();

function read(): Prefs {
  let stored: Partial<Prefs> = {};
  try { stored = JSON.parse(localStorage.getItem(KEY) ?? "{}") as Partial<Prefs>; } catch { /* corrupt or blocked */ }
  const prefs = { ...DEFAULT_PREFS, ...stored };
  // Older builds kept only the avatar choice under its own key.
  try {
    if (!stored.avatar) prefs.avatar = normalizeAvatarId(localStorage.getItem("yui.avatar"));
  } catch { /* storage blocked */ }
  prefs.avatar = normalizeAvatarId(prefs.avatar);
  return prefs;
}

let current = read();

export function prefs(): Prefs { return current; }

export function updatePrefs(patch: Partial<Prefs>): void {
  const changed = (Object.keys(patch) as Array<keyof Prefs>).filter(key => patch[key] !== current[key]);
  if (!changed.length) return;
  current = { ...current, ...patch };
  try { localStorage.setItem(KEY, JSON.stringify(current)); } catch { /* private mode: keep in memory */ }
  for (const listener of listeners) listener(current, changed);
}

export function onPrefs(listener: Listener): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

if (typeof window !== "undefined") {
  window.addEventListener("storage", event => {
    if (event.key !== KEY) return;
    const next = read();
    const changed = (Object.keys(next) as Array<keyof Prefs>).filter(key => next[key] !== current[key]);
    current = next;
    if (changed.length) for (const listener of listeners) listener(current, changed);
  });
}

/** Applies theme and accent to the document. */
export function applyAppearance(p: Prefs = current): void {
  const root = document.documentElement;
  const dark = p.theme === "dark" || (p.theme === "system" && !window.matchMedia("(prefers-color-scheme: light)").matches);
  root.dataset.theme = dark ? "dark" : "light";
  root.style.setProperty("--accent", p.accent);
  root.dataset.backdrop = p.backdrop;
}
