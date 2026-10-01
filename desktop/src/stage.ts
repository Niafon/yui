/**
 * The avatar stage: mounts the selected model, routes speech into lip sync,
 * follows the pointer and applies local presentation preferences. Used by
 * the main window (docked) and by the detached avatar window.
 */

import { findAvatar, mountAvatar, type MountedAvatar } from "./avatar/catalog";
import { BUILTIN_AVATARS } from "./avatar/builtin";
import type { AvatarState, Visemes } from "./avatar/types";
import { t } from "./i18n";
import { loadWLipSync } from "./lipsync-loader";
import { PresenceRibbon, type PresenceState } from "./presence";
import { onPrefs, prefs, type Prefs } from "./prefs";
import { SpeechOutput } from "./speech";

export interface StageElements {
  frame: HTMLElement;
  canvas: HTMLCanvasElement;
  fallback: HTMLElement;
  ribbon?: HTMLCanvasElement;
  audioButton?: HTMLButtonElement;
}

export class Stage {
  readonly speech: SpeechOutput;
  private mounted?: MountedAvatar;
  private generation = 0;
  private state: AvatarState = "idle";
  private lastReaction: [string, string] = ["neutral", "exp_neutral"];
  private readonly ribbon?: PresenceRibbon;
  private canvas: HTMLCanvasElement;
  private active = true;
  private micLevel = 0;
  private speechLevel = 0;
  private readonly unsubscribe: () => void;

  constructor(private readonly elements: StageElements) {
    this.canvas = elements.canvas;
    this.ribbon = elements.ribbon ? new PresenceRibbon(elements.ribbon) : undefined;
    this.speech = new SpeechOutput((visemes, level) => this.onSpeechFrame(visemes, level), { loadLipSync: loadWLipSync });
    this.applySpeechPrefs(prefs());
    this.unsubscribe = onPrefs((next, changed) => this.onPrefs(next, changed));
    elements.frame.addEventListener("pointermove", event => {
      const rect = elements.frame.getBoundingClientRect();
      const x = ((event.clientX - rect.left) / rect.width) * 2 - 1;
      const y = -(((event.clientY - rect.top) / rect.height) * 2 - 1);
      this.mounted?.avatar.pointer(Math.max(-1, Math.min(1, x)), Math.max(-1, Math.min(1, y)));
    });
    elements.frame.addEventListener("pointerleave", () => this.mounted?.avatar.pointerLeave());
    elements.audioButton?.addEventListener("click", () => void this.unlockAudio());
    // Any interaction counts as the gesture browsers require for audio.
    const unlock = () => void this.speech.resume().then(() => this.updateAudioHint());
    window.addEventListener("pointerdown", unlock, { capture: true });
    window.addEventListener("keydown", unlock, { capture: true });
  }

  /** Mounts the avatar from preferences; false shows the fallback text. */
  async load(id = prefs().avatar): Promise<boolean> {
    const generation = ++this.generation;
    this.mounted?.dispose();
    this.mounted = undefined;
    const fallback = this.elements.fallback;
    fallback.hidden = false;
    fallback.textContent = t("avatar.loading");
    if (!this.active) return false;
    // WebGL contexts cannot switch renderer type; use a fresh canvas.
    const canvas = this.canvas.cloneNode(false) as HTMLCanvasElement;
    this.canvas.replaceWith(canvas);
    this.canvas = canvas;
    const entry = (await findAvatar(id)) ?? BUILTIN_AVATARS[0]!;
    if (generation !== this.generation) return false;
    const mounted = await mountAvatar(entry, canvas);
    if (generation !== this.generation) { mounted.dispose(); return false; }
    this.mounted = mounted;
    const p = prefs();
    mounted.avatar.setOptions({ scale: p.avatarScale, offsetY: p.avatarOffsetY, followPointer: p.followPointer, motion: p.motion });
    mounted.avatar.setState(this.state);
    if (this.lastReaction[1] !== "exp_neutral") mounted.avatar.react(...this.lastReaction);
    fallback.hidden = mounted.loaded;
    if (!mounted.loaded) {
      fallback.textContent = entry.kind === "vrm"
        ? t("avatar.vrmFailed")
        : t("avatar.live2dFailed");
    }
    return mounted.loaded;
  }

  /** Detaching the avatar to another window frees this stage entirely. */
  setActive(active: boolean): void {
    if (active === this.active) return;
    this.active = active;
    if (active) void this.load();
    else { this.generation++; this.mounted?.dispose(); this.mounted = undefined; }
  }

  setState(state: string): void {
    const known: PresenceState[] = ["idle", "listening", "thinking", "speaking", "error"];
    this.state = known.includes(state as PresenceState) ? (state as AvatarState) : "idle";
    this.ribbon?.setState(this.state);
    this.mounted?.avatar.setState(this.state);
  }

  react(emotion: string, expression: string): void {
    this.lastReaction = [emotion, expression];
    this.mounted?.avatar.react(emotion, expression);
  }

  playChunk(base64: string, sampleRate: number): void {
    this.speech.playPcm16(base64, sampleRate);
    this.updateAudioHint();
  }

  stopSpeech(): void { this.speech.stop(); }

  setMicLevel(level: number): void {
    this.micLevel = level;
    this.ribbon?.setLevel(Math.max(this.micLevel, this.speechLevel));
  }

  async preview(url = "/assets/voice/xenia-demo.wav"): Promise<void> {
    this.speech.stop();
    await this.speech.resume();
    const context = this.speech.ensureContext();
    const response = await fetch(url);
    if (!response.ok) throw new Error(t("voice.previewFailed"));
    const buffer = await context.decodeAudioData(await response.arrayBuffer());
    const wasEnabled = this.speech.enabled;
    this.speech.enabled = true; // a preview is always local to this window
    this.speech.playBuffer(buffer);
    this.speech.enabled = wasEnabled;
  }

  get engineLabel(): string {
    return this.speech.engine === "mfcc" ? t("lip.mfcc") : t("lip.spectralActive");
  }

  dispose(): void {
    this.unsubscribe();
    this.generation++;
    this.mounted?.dispose();
    this.speech.dispose();
    this.ribbon?.dispose();
  }

  private async unlockAudio(): Promise<void> {
    await this.speech.resume();
    this.updateAudioHint();
  }

  private updateAudioHint(): void {
    const button = this.elements.audioButton;
    if (button) button.hidden = !(this.speech.suspended && this.speech.speaking);
  }

  private onSpeechFrame(visemes: Visemes, level: number): void {
    this.mounted?.avatar.setVisemes(visemes);
    this.speechLevel = level;
    this.ribbon?.setLevel(Math.max(this.micLevel, level));
  }

  private applySpeechPrefs(p: Prefs): void {
    this.speech.setSettings({ volume: p.volume, sensitivity: p.lipSensitivity, engine: p.lipEngine });
  }

  private onPrefs(next: Prefs, changed: Array<keyof Prefs>): void {
    this.applySpeechPrefs(next);
    if (changed.includes("avatar")) { void this.load(next.avatar); return; }
    this.mounted?.avatar.setOptions({
      scale: next.avatarScale, offsetY: next.avatarOffsetY, followPointer: next.followPointer, motion: next.motion,
    });
    if (changed.includes("showRibbon") && this.elements.ribbon) this.elements.ribbon.hidden = !next.showRibbon;
  }
}
