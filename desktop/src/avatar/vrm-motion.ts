import * as THREE from "three";
import { GLTFLoader } from "three/examples/jsm/loaders/GLTFLoader.js";
import type { VRM } from "@pixiv/three-vrm";
import { VRMAnimationLoaderPlugin, createVRMAnimationClip, type VRMAnimation } from "@pixiv/three-vrm-animation";
import type { AvatarState } from "./types";

const CLIPS = ["idle", "idle-talking", "neutral", "happy", "sad", "angry", "relaxed", "nod", "think"] as const;
const GESTURES: Record<string, string> = {
  joy: "happy", warm: "nod", concern: "sad", sad: "sad", angry: "angry",
  alert: "neutral", surprise: "neutral", think: "think", calm: "relaxed", neutral: "nod",
};
/** Quiet variations that break up a long idle loop. */
const IDLE_VARIATIONS = ["relaxed", "neutral", "think"];

const BASE_FADE = 0.8;
const GESTURE_FADE = 0.45;

/**
 * Body clips for one VRM: a looping base (idle / talking idle), one-shot
 * emotional gestures and occasional idle variations, all cross-faded.
 * Clips are loaded once per process and retargeted per model.
 */
export class VRMClipPlayer {
  private readonly mixer: THREE.AnimationMixer;
  private readonly actions = new Map<string, THREE.AnimationAction>();
  private active?: THREE.AnimationAction;
  private activeName = "";
  private state: AvatarState = "idle";
  private gestureEnds = 0;
  private elapsed = 0;
  private nextVariation = 0;
  private disposed = false;

  constructor(private readonly vrm: VRM, private readonly reduced: boolean) {
    this.mixer = new THREE.AnimationMixer(vrm.scene);
    this.nextVariation = 14 + Math.random() * 12;
  }

  get ready(): boolean { return this.actions.size > 0; }
  get currentMotion(): string { return this.activeName; }

  async load(): Promise<void> {
    const animations = await loadClips();
    if (this.disposed) return;
    for (const [name, animation] of animations) {
      try {
        const clip = createVRMAnimationClip(animation, this.vrm);
        const action = this.mixer.clipAction(clip);
        const loop = name.startsWith("idle");
        action.setLoop(loop ? THREE.LoopRepeat : THREE.LoopOnce, Infinity);
        action.clampWhenFinished = !loop;
        this.actions.set(name, action);
      } catch (error) {
        console.warn(`VRM animation ${name} cannot be retargeted`, error);
      }
    }
    if (!this.disposed) this.playBase(0);
  }

  setState(state: AvatarState): void {
    const changed = state !== this.state;
    this.state = state;
    if (changed && !this.gestureEnds) this.playBase(BASE_FADE);
  }

  react(emotion: string): void {
    if (this.disposed || this.reduced) return;
    const name = GESTURES[emotion];
    if (!name) return;
    const action = this.actions.get(name);
    if (!action) return;
    const remaining = Math.max(1.2, Math.min(6, action.getClip().duration - GESTURE_FADE));
    // Re-triggering the gesture already in progress would snap it to frame
    // zero; extending it reads as one continuous reaction instead.
    if (this.activeName === name && this.gestureEnds) {
      this.gestureEnds = Math.max(this.gestureEnds, this.elapsed + 1);
      return;
    }
    this.play(name, GESTURE_FADE);
    this.gestureEnds = this.elapsed + remaining;
  }

  update(delta: number): void {
    if (this.disposed) return;
    const step = Math.max(0, Math.min(delta, 0.05));
    this.elapsed += step;
    this.mixer.update(this.reduced ? 0 : step);
    if (this.gestureEnds && this.elapsed >= this.gestureEnds) {
      this.gestureEnds = 0;
      this.playBase(BASE_FADE);
    }
    if (!this.reduced && this.state === "idle" && !this.gestureEnds && this.elapsed >= this.nextVariation) {
      this.nextVariation = this.elapsed + 18 + Math.random() * 16;
      const options = IDLE_VARIATIONS.filter(name => this.actions.has(name));
      const name = options[Math.floor(Math.random() * options.length)];
      if (name) {
        this.play(name, 1.1);
        this.gestureEnds = this.elapsed + Math.min(5, (this.actions.get(name)?.getClip().duration ?? 3) - 1.1);
      }
    }
  }

  private playBase(fade: number): void {
    this.play(this.state === "speaking" && this.actions.has("idle-talking") ? "idle-talking" : "idle", fade);
  }

  private play(name: string, fade: number): void {
    const next = this.actions.get(name);
    if (!next || this.active === next) return;
    const previous = this.active;
    next.reset();
    next.timeScale = 0.94 + Math.random() * 0.12;
    next.setEffectiveWeight(1).play();
    if (previous && fade > 0) next.crossFadeFrom(previous, fade, false);
    else previous?.stop();
    this.active = next;
    this.activeName = name;
  }

  dispose(): void {
    this.disposed = true;
    this.mixer.stopAllAction();
    this.mixer.uncacheRoot(this.vrm.scene);
    this.actions.clear();
  }
}

let shared: Promise<Map<string, VRMAnimation>> | undefined;

/** VRMA files are parsed once; each model only retargets them. */
function loadClips(): Promise<Map<string, VRMAnimation>> {
  return shared ??= (async () => {
    const loader = new GLTFLoader();
    loader.register(parser => new VRMAnimationLoaderPlugin(parser));
    const result = new Map<string, VRMAnimation>();
    await Promise.all(CLIPS.map(async name => {
      try {
        const gltf = await loader.loadAsync(`/assets/animations/${name}.vrma`);
        const animation = (gltf.userData.vrmAnimations as VRMAnimation[] | undefined)?.[0];
        if (!animation) throw new Error("VRMA contains no animation");
        // Face, eyes and mouth belong to emotion, gaze and speech layers.
        animation.lookAtTrack = null;
        animation.expressionTracks.preset.clear();
        animation.expressionTracks.custom.clear();
        result.set(name, animation);
      } catch (error) {
        console.warn(`VRM animation ${name} unavailable`, error);
      }
    }));
    if (!result.size) shared = undefined;
    return result;
  })();
}
