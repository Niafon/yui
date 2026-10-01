import * as THREE from "three";
import { GLTFLoader } from "three/examples/jsm/loaders/GLTFLoader.js";
import type { VRM } from "@pixiv/three-vrm";
import { VRMAnimationLoaderPlugin, createVRMAnimationClip, type VRMAnimation } from "@pixiv/three-vrm-animation";

export type AvatarState = "idle" | "listening" | "thinking" | "speaking" | "error";

const CLIPS = ["idle", "idle-talking", "neutral", "happy", "sad", "angry", "relaxed", "nod", "think"] as const;
const GESTURES: Record<string, string> = {
  joy: "happy", warm: "nod", concern: "sad", sad: "sad", angry: "angry",
  alert: "neutral", surprise: "neutral", think: "think", calm: "relaxed", neutral: "neutral",
};

/** Owns the retargeted clips and their transitions for one VRM instance. */
export class VRMAvatarMotion {
  private readonly mixer: THREE.AnimationMixer;
  private readonly actions = new Map<string, THREE.AnimationAction>();
  private active?: THREE.AnimationAction;
  private activeName = "";
  private state: AvatarState = "idle";
  private gestureEnds = 0;
  private elapsed = 0;
  private disposed = false;
  private readonly reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  constructor(private readonly vrm: VRM) {
    this.mixer = new THREE.AnimationMixer(vrm.scene);
  }

  async load(): Promise<void> {
    const loader = new GLTFLoader();
    loader.register((parser) => new VRMAnimationLoaderPlugin(parser));
    await Promise.all(CLIPS.map(async name => {
      try {
        const gltf = await loader.loadAsync(`/assets/animations/${name}.vrma`);
        if (this.disposed) return;
        const animation = (gltf.userData.vrmAnimations as VRMAnimation[] | undefined)?.[0];
        if (!animation) throw new Error("VRMA contains no animation");
        animation.lookAtTrack = null;
        animation.expressionTracks.preset.clear();
        animation.expressionTracks.custom.clear();
        const clip = createVRMAnimationClip(animation, this.vrm);
        // Mouth, eyes and expressions belong to the avatar state and speech.
        const action = this.mixer.clipAction(clip);
        action.setLoop(name.startsWith("idle") ? THREE.LoopRepeat : THREE.LoopOnce, Infinity);
        action.clampWhenFinished = !name.startsWith("idle");
        this.actions.set(name, action);
      } catch (error) {
        console.warn(`VRM animation ${name} unavailable`, error);
      }
    }));
    if (!this.disposed) this.playBase();
  }

  setState(state: AvatarState): void {
    this.state = state;
    if (this.reduced) return;
    if (!this.gestureEnds) this.playBase();
  }

  react(emotion: string): void {
    const name = GESTURES[emotion] ?? "neutral";
    const action = this.actions.get(name);
    if (!action || this.disposed || this.reduced) return;
    this.play(name, true);
    this.gestureEnds = this.elapsed + Math.min(6, Math.max(1, action.getClip().duration - 0.35));
  }

  update(delta: number): void {
    if (this.disposed || this.reduced) return;
    const step = Math.max(0, Math.min(delta, 0.05));
    this.elapsed += step;
    this.mixer.update(step);
    if (this.gestureEnds && this.elapsed >= this.gestureEnds) {
      this.gestureEnds = 0;
      this.playBase();
    }
  }

  private playBase(): void {
    if (this.reduced) return;
    this.play(this.state === "speaking" ? "idle-talking" : "idle", false);
  }

  private play(name: string, restart: boolean): void {
    const next = this.actions.get(name) ?? this.actions.get("idle");
    if (!next || (!restart && this.active === next)) return;
    const previous = this.active;
    next.reset().setEffectiveWeight(1).play();
    if (previous && previous !== next) next.crossFadeFrom(previous, 0.35, false);
    this.active = next;
    this.activeName = name;
  }

  get currentMotion(): string { return this.activeName; }

  dispose(): void {
    this.disposed = true;
    this.mixer.stopAllAction();
    this.mixer.uncacheRoot(this.vrm.scene);
    this.actions.clear();
  }
}
