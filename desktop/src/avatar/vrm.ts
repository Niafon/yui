import * as THREE from "three";
import { GLTFLoader } from "three/examples/jsm/loaders/GLTFLoader.js";
import { VRMLoaderPlugin, VRMUtils, type VRM, type VRMHumanBoneName } from "@pixiv/three-vrm";
import { VRMClipPlayer } from "./vrm-motion";
import { Blinker, Saccades, noise } from "./life";
import {
  DEFAULT_AVATAR_OPTIONS, VISEME_KEYS, damp, emotionPreset, mouthOpen, prefersReducedMotion, silentVisemes,
  type Avatar, type AvatarOptions, type AvatarState, type EmotionPreset, type Visemes,
} from "./types";

/** Shared presets → VRM expression names ("think" has no VRM preset). */
const VRM_PRESET: Record<EmotionPreset, [string, number]> = {
  happy: ["happy", 1], sad: ["sad", 1], angry: ["angry", 1], surprised: ["surprised", 1],
  relaxed: ["relaxed", 1], think: ["relaxed", 0.35],
};
const EMOTION_NAMES = ["happy", "sad", "angry", "surprised", "relaxed"] as const;
const PROCEDURAL_BONES: VRMHumanBoneName[] = ["spine", "chest", "upperChest", "neck", "head"];

const euler = new THREE.Euler();
const offset = new THREE.Quaternion();

/** A small VRM stage with layered, frame-rate independent animation:
 * body clips → breathing/sway → gaze and head follow → emotion → visemes. */
export class VRMAvatar implements Avatar {
  private renderer?: THREE.WebGLRenderer;
  private scene?: THREE.Scene;
  private camera?: THREE.PerspectiveCamera;
  private model?: VRM;
  private clips?: VRMClipPlayer;
  private readonly lookTarget = new THREE.Object3D();
  private readonly rest = new Map<THREE.Object3D, THREE.Quaternion>();
  private readonly blinker = new Blinker();
  private readonly saccades = new Saccades();
  private readonly reduced = prefersReducedMotion();
  private options: AvatarOptions = { ...DEFAULT_AVATAR_OPTIONS };
  private state: AvatarState = "idle";
  private visemes = silentVisemes();
  private readonly emotion = { name: "", peak: 0, since: 0 };
  private readonly weights: Record<string, number> = {};
  private pointerTarget: { x: number; y: number } | null = null;
  private readonly gaze = { x: 0, y: 0 };
  private readonly head = { yaw: 0, pitch: 0, roll: 0 };
  private speech = 0;
  private time = 0;
  private lastFrame = performance.now();
  private headHeight = 1.4;
  private generation = 0;
  private resizeObserver?: ResizeObserver;

  constructor(private readonly canvas: HTMLCanvasElement) {}

  async load(packagePath: string): Promise<boolean> {
    this.destroy();
    const generation = this.generation;
    try {
      const renderer = new THREE.WebGLRenderer({ canvas: this.canvas, alpha: true, antialias: true, powerPreference: "high-performance" });
      renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2));
      renderer.setClearColor(0x000000, 0);
      const scene = new THREE.Scene();
      const key = new THREE.DirectionalLight(0xffffff, Math.PI * 0.75);
      key.position.set(0.6, 1.6, 2.2);
      scene.add(key, new THREE.HemisphereLight(0xfff4ee, 0x3a3046, 1.3));
      const camera = new THREE.PerspectiveCamera(26, 1, 0.1, 50);
      scene.add(this.lookTarget);

      const loader = new GLTFLoader();
      loader.register(parser => new VRMLoaderPlugin(parser));
      const gltf = await loader.loadAsync(packagePath);
      if (generation !== this.generation) { VRMUtils.deepDispose(gltf.scene); renderer.dispose(); return false; }
      const model = gltf.userData.vrm as VRM | undefined;
      if (!model) throw new Error("Файл не содержит VRM-модель");
      VRMUtils.removeUnnecessaryVertices(gltf.scene);
      VRMUtils.combineSkeletons(gltf.scene);
      VRMUtils.combineMorphs(model);
      VRMUtils.rotateVRM0(model);
      model.scene.traverse(object => { object.frustumCulled = false; });
      // A relaxed A-pose until the first clip arrives.
      model.humanoid.getNormalizedBoneNode("leftUpperArm")?.rotation.set(0, 0, 1.15);
      model.humanoid.getNormalizedBoneNode("rightUpperArm")?.rotation.set(0, 0, -1.15);
      scene.add(model.scene);
      model.scene.updateMatrixWorld(true);
      // Raw bones carry the real skeleton position (the normalized rig only
      // syncs on update), so use them to frame the face.
      const headNode = model.humanoid.getRawBoneNode("head") ?? model.humanoid.getNormalizedBoneNode("head");
      this.headHeight = headNode ? headNode.getWorldPosition(new THREE.Vector3()).y : 1.4;
      for (const name of PROCEDURAL_BONES) {
        const bone = model.humanoid.getNormalizedBoneNode(name);
        if (bone) this.rest.set(bone, bone.quaternion.clone());
      }
      if (model.lookAt) { model.lookAt.target = this.lookTarget; model.lookAt.autoUpdate = true; }

      this.renderer = renderer; this.scene = scene; this.camera = camera; this.model = model;
      this.frame();
      this.resizeObserver = new ResizeObserver(() => this.resize());
      if (this.canvas.parentElement) this.resizeObserver.observe(this.canvas.parentElement);
      this.resize();
      renderer.setAnimationLoop(() => this.render());

      // Body clips stream in after the model is already visible.
      const clips = new VRMClipPlayer(model, this.reduced);
      this.clips = clips;
      await clips.load();
      if (generation !== this.generation) return false;
      clips.setState(this.state);
      return true;
    } catch (error) {
      if (generation === this.generation) { console.error("VRM model loading failed", error); this.destroy(); }
      return false;
    }
  }

  setVisemes(visemes: Visemes): void { this.visemes = visemes; }

  setState(state: AvatarState): void {
    if (state !== this.state) this.blinker.nudge();
    this.state = state;
    this.clips?.setState(state);
  }

  react(emotion: string, expression: string): void {
    const { preset, weight } = emotionPreset(emotion, expression);
    const mapped = preset ? VRM_PRESET[preset] : undefined;
    this.emotion.name = mapped?.[0] ?? "";
    this.emotion.peak = (mapped?.[1] ?? 0) * weight;
    this.emotion.since = this.time;
    this.clips?.react(emotion);
  }

  setOptions(options: Partial<AvatarOptions>): void {
    this.options = { ...this.options, ...options };
    this.frame();
  }

  pointer(x: number, y: number): void { if (this.options.followPointer) this.pointerTarget = { x, y }; }
  pointerLeave(): void { this.pointerTarget = null; }

  private frame(): void {
    const camera = this.camera;
    if (!camera) return;
    const scale = Math.max(0.5, Math.min(2.5, this.options.scale));
    // Waist-up portrait with headroom; zoom moves towards the face.
    const focusY = this.headHeight - 0.13 + (scale - 1) * 0.08 + this.options.offsetY * 0.6;
    camera.position.set(0, focusY + 0.03, 1.85 / scale);
    camera.lookAt(0, focusY, 0);
    camera.updateProjectionMatrix();
  }

  private resize(): void {
    if (!this.renderer || !this.camera || !this.canvas.parentElement) return;
    const frame = this.canvas.parentElement;
    const width = Math.max(1, frame.clientWidth), height = Math.max(1, frame.clientHeight);
    this.renderer.setSize(width, height, false);
    this.camera.aspect = width / height;
    // Narrow portrait stages need a wider field of view to keep the shoulders.
    this.camera.fov = width / height < 0.65 ? 32 : 26;
    this.camera.updateProjectionMatrix();
  }

  private render(): void {
    const renderer = this.renderer, scene = this.scene, camera = this.camera, model = this.model;
    if (!renderer || !scene || !camera || !model) return;
    const now = performance.now();
    const dt = Math.min(Math.max(0, (now - this.lastFrame) / 1000), 0.05);
    this.lastFrame = now;
    this.time += dt;

    // 1. Body clips write absolute bone poses; restore the procedurally
    //    touched bones first so offsets never accumulate on un-keyed bones.
    for (const [bone, quaternion] of this.rest) bone.quaternion.copy(quaternion);
    this.clips?.update(dt);

    // 2. Procedural layers.
    const motion = this.reduced ? 0 : Math.max(0, Math.min(1.5, this.options.motion));
    const open = mouthOpen(this.visemes);
    this.speech = damp(this.speech, open, 10, dt);
    this.updateGaze(dt, motion);
    this.applyBody(dt, motion);

    // 3. Face: blink, emotion, mouth.
    const expressions = model.expressionManager;
    if (expressions) {
      const blink = this.blinker.update(dt, this.state);
      this.updateEmotion(dt);
      for (const name of EMOTION_NAMES) expressions.setValue(name, this.weights[name] ?? 0);
      // Strong smiles already close the eyes; avoid a double squeeze.
      expressions.setValue("blink", blink * (1 - Math.min(0.7, (this.weights.happy ?? 0) * 0.9)));
      for (const key of VISEME_KEYS) expressions.setValue(key, Math.min(1, this.visemes[key] * 1.05));
    }

    model.update(dt);
    renderer.render(scene, camera);
  }

  private updateGaze(dt: number, motion: number): void {
    const camera = this.camera!;
    const idle = this.saccades.update(dt, this.state);
    const goal = this.pointerTarget
      ? { x: this.pointerTarget.x * 0.9, y: this.pointerTarget.y * 0.7 }
      : { x: idle.x * motion, y: idle.y * motion };
    // Saccades are near-instant; following the pointer is a smooth pursuit.
    const rate = this.pointerTarget ? 9 : 30;
    this.gaze.x = damp(this.gaze.x, goal.x, rate, dt);
    this.gaze.y = damp(this.gaze.y, goal.y, rate, dt);
    this.lookTarget.position.set(
      camera.position.x + this.gaze.x * 0.55,
      camera.position.y + this.gaze.y * 0.4,
      camera.position.z,
    );
  }

  private applyBody(dt: number, motion: number): void {
    const model = this.model!;
    const t = this.time;
    const level = this.speech;
    // Head turns a fraction of the gaze, like people do with small shifts.
    const follow = this.pointerTarget ? 0.42 : 0.18;
    let yaw = this.gaze.x * follow;
    let pitch = -this.gaze.y * follow * 0.6;
    let roll = noise(t * 0.35, 1.3) * 0.025 * motion;
    yaw += noise(t * 0.22, 4.1) * 0.03 * motion;
    pitch += noise(t * 0.27, 7.7) * 0.02 * motion;
    switch (this.state) {
      case "thinking": roll += 0.07; pitch -= 0.04; break;
      case "listening": roll += 0.045; pitch += 0.03; break;
      case "speaking":
        // Small emphasis nods that track syllable energy.
        pitch += level * 0.06 * motion + Math.sin(t * 5.3) * level * 0.015 * motion;
        yaw += noise(t * 0.9, 2.2) * 0.04 * level * motion;
        break;
    }
    const emotionName = this.emotion.name;
    if (emotionName === "sad") { pitch += 0.06; roll -= 0.03; }
    if (emotionName === "happy") roll += Math.sin(t * 1.4) * 0.02 * motion;
    if (emotionName === "surprised") pitch -= 0.04;
    this.head.yaw = damp(this.head.yaw, yaw, 7, dt);
    this.head.pitch = damp(this.head.pitch, pitch, 7, dt);
    this.head.roll = damp(this.head.roll, roll, 5, dt);

    const breath = Math.sin(t * (Math.PI * 2) / 4.2);
    this.rotate(model, "neck", this.head.pitch * 0.4, this.head.yaw * 0.4, this.head.roll * 0.4);
    this.rotate(model, "head", this.head.pitch * 0.6, this.head.yaw * 0.6, this.head.roll * 0.6);
    this.rotate(model, "chest", breath * 0.014 * motion, 0, 0);
    this.rotate(model, "upperChest", breath * 0.01 * motion, 0, 0);
    this.rotate(model, "spine", noise(t * 0.18, 9.1) * 0.012 * motion, noise(t * 0.15, 5.5) * 0.015 * motion,
      noise(t * 0.2, 3.3) * 0.018 * motion);
  }

  private rotate(model: VRM, name: VRMHumanBoneName, x: number, y: number, z: number): void {
    const bone = model.humanoid.getNormalizedBoneNode(name);
    if (!bone) return;
    offset.setFromEuler(euler.set(x, y, z));
    bone.quaternion.multiply(offset);
  }

  private updateEmotion(dt: number): void {
    const { name, peak, since } = this.emotion;
    // Peak briefly, then settle to a softer resting expression.
    const settled = this.time - since > 5 ? peak * 0.55 : peak;
    // A broad smile hides mouth shapes; ease it while talking.
    const speaking = Math.min(1, this.speech * 1.6);
    for (const preset of EMOTION_NAMES) {
      let target = preset === name ? settled : 0;
      if (preset === "happy") target *= 1 - speaking * 0.45;
      this.weights[preset] = damp(this.weights[preset] ?? 0, target, target > (this.weights[preset] ?? 0) ? 6 : 3.5, dt);
    }
  }

  destroy(): void {
    this.generation++;
    this.clips?.dispose(); this.clips = undefined;
    this.resizeObserver?.disconnect(); this.resizeObserver = undefined;
    this.renderer?.setAnimationLoop(null);
    if (this.model) VRMUtils.deepDispose(this.model.scene);
    this.renderer?.dispose();
    // Browsers cap live WebGL contexts; release ours immediately.
    this.renderer?.forceContextLoss();
    this.renderer = undefined;
    this.scene?.clear();
    this.scene = undefined; this.camera = undefined; this.model = undefined;
    this.rest.clear();
  }
}
