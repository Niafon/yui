import type { Application } from "pixi.js";
import type { Live2DModel, Cubism4InternalModel } from "pixi-live2d-display/cubism4";
import * as THREE from "three";
import { GLTFLoader } from "three/examples/jsm/loaders/GLTFLoader.js";
import { VRM, VRMLoaderPlugin, VRMUtils } from "@pixiv/three-vrm";
import { VRMAvatarMotion, type AvatarState } from "./avatar-motion";

export const DEFAULT_MODEL = "/assets/live2d/haru/Haru.model3.json";
export const DEFAULT_VRM_MODEL = "/assets/vrm/default.vrm";

export interface Avatar {
  load(packagePath: string): Promise<boolean>;
  setExpression(expression: string): void;
  /** 0..1 mouth opening, driven by synthesised audio amplitude. */
  setMouth(open: number): void;
  setState(state: AvatarState): void;
  react(emotion: string, expression: string): void;
  destroy(): void;
}

/** A small VRM stage with the same lifecycle as Live2D. The renderer is
 * deliberately behind the Avatar interface so the core and UI do not care
 * which character format is selected. */
export class VRMAvatar implements Avatar {
  private renderer?: THREE.WebGLRenderer;
  private scene?: THREE.Scene;
  private camera?: THREE.PerspectiveCamera;
  private model?: VRM;
  private motion?: VRMAvatarMotion;
  private lastFrame = performance.now();
  private emotion = "exp_neutral";
  private expressionLevel = 0;
  private fallbackTime = 0;
  private state: AvatarState = "idle";
  private frame = 0;
  private generation = 0;
  private mouth = 0;
  private resizeObserver?: ResizeObserver;

  constructor(private readonly canvas: HTMLCanvasElement) {}

  async load(packagePath: string): Promise<boolean> {
    this.destroy();
    const generation = this.generation;
    try {
      const renderer = new THREE.WebGLRenderer({ canvas: this.canvas, alpha: true, antialias: true });
      this.renderer = renderer;
      renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2));
      renderer.setAnimationLoop(() => this.render());
      const scene = new THREE.Scene();
      scene.add(new THREE.HemisphereLight(0xffffff, 0x332244, 2));
      const camera = new THREE.PerspectiveCamera(30, 1, 0.1, 100);
      camera.position.set(0, 1.35, 2.2);
      const loader = new GLTFLoader();
      loader.register((parser) => new VRMLoaderPlugin(parser));
      const gltf = await loader.loadAsync(packagePath);
      if (generation !== this.generation) { VRMUtils.deepDispose(gltf.scene); return false; }
      const model = gltf.userData.vrm as VRM | undefined;
      if (!model) throw new Error("Файл не содержит VRM-модель");
      VRMUtils.rotateVRM0(model);
      model.humanoid.getNormalizedBoneNode("leftUpperArm")?.rotation.set(0, 0, 1.15);
      model.humanoid.getNormalizedBoneNode("rightUpperArm")?.rotation.set(0, 0, -1.15);
      camera.lookAt(0, 1.2, 0);
      VRMUtils.removeUnnecessaryVertices(gltf.scene);
      scene.add(gltf.scene);
      this.renderer = renderer; this.scene = scene; this.camera = camera; this.model = model;
      this.motion = new VRMAvatarMotion(model);
      await this.motion.load();
      if (generation !== this.generation) return false;
      this.motion.setState(this.state);
      this.resizeObserver = new ResizeObserver(() => this.resize());
      if (this.canvas.parentElement) this.resizeObserver.observe(this.canvas.parentElement);
      this.resize();
      return true;
    } catch (error) { if (generation === this.generation) { console.error("VRM model loading failed", error); this.destroy(); } return false; }
  }

  setExpression(expression: string): void {
    this.emotion = expression;
    this.expressionLevel = 0;
  }

  setState(state: AvatarState): void { this.state = state; this.motion?.setState(state); }
  react(emotion: string, expression: string): void {
    this.setExpression(expression);
    this.motion?.react(emotion);
  }

  setMouth(open: number): void { this.mouth = Number.isFinite(open) ? Math.max(0, Math.min(1, open)) : 0; }

  private resize(): void {
    if (!this.renderer || !this.camera || !this.canvas.parentElement) return;
    const frame = this.canvas.parentElement;
    const width = Math.max(1, frame.clientWidth), height = Math.max(1, frame.clientHeight);
    this.renderer.setSize(width, height, false); this.camera.aspect = width / height; this.camera.updateProjectionMatrix();
  }

  private render(): void {
    if (!this.renderer || !this.scene || !this.camera) return;
    const now = performance.now();
    const dt = Math.min(Math.max(0, (now - this.lastFrame) / 1000), 0.05);
    this.lastFrame = now;
    this.motion?.update(dt);
    this.fallbackTime += dt;
    if (!this.motion?.currentMotion) {
      const head = this.model?.humanoid?.getNormalizedBoneNode("head");
      if (head) head.rotation.z = 0.035 * Math.sin(this.fallbackTime * 0.8);
    }
    this.expressionLevel += (1 - this.expressionLevel) * Math.min(1, dt * 8);
    const expressions = this.model?.expressionManager;
    if (expressions) {
      expressions.setValue("happy", this.emotion === "exp_smile" ? this.expressionLevel : 0);
      expressions.setValue("sad", this.emotion === "exp_worry" ? this.expressionLevel : 0);
      expressions.setValue("surprised", this.emotion === "exp_surprise" ? this.expressionLevel : 0);
      expressions.setValue("aa", this.mouth);
    }
    this.model?.update(dt);
    this.renderer.render(this.scene, this.camera); this.frame++;
  }

  destroy(): void {
    this.generation++;
    this.motion?.dispose(); this.motion = undefined;
    this.resizeObserver?.disconnect(); this.resizeObserver = undefined;
    this.renderer?.setAnimationLoop(null); this.renderer?.dispose();
    this.renderer = undefined;
    if (this.model) VRMUtils.deepDispose(this.model.scene);
    if (this.scene) this.scene.clear(); this.scene = undefined; this.camera = undefined; this.model = undefined;
  }
}

let coreReady: Promise<void> | undefined;
function loadCore(): Promise<void> {
  return coreReady ??= new Promise((resolve, reject) => {
    const script = document.createElement("script");
    script.src = "/assets/live2d/live2dcubismcore.min.js";
    script.onload = () => resolve();
    script.onerror = () => {
      coreReady = undefined;
      script.remove();
      reject(new Error("Не удалось загрузить Cubism Core"));
    };
    document.head.append(script);
  });
}

// Haru's supplied expression names. Unknown emotions return to neutral.
const EXPRESSIONS: Record<string, string> = {
  exp_smile: "F01", exp_worry: "F04", exp_think: "F08", exp_surprise: "F06",
};

export class Live2DAvatar implements Avatar {
  private app?: Application;
  private model?: Live2DModel;
  private resizeObserver?: ResizeObserver;
  private expression = "exp_neutral";
  private mouth = 0;
  private generation = 0;
  private reactionUntil = 0;
  private reactionLabel = "neutral";

  constructor(private readonly canvas: HTMLCanvasElement) {}

  async load(packagePath: string): Promise<boolean> {
    this.release();
    const generation = ++this.generation;
    let pendingModel: Live2DModel | undefined;
    try {
      await loadCore();
      const [{ Application, ShaderSystem }, { Live2DModel, config }] = await Promise.all([
        import("pixi.js"), import("pixi-live2d-display/cubism4"),
      ]);
      const { install } = await import("@pixi/unsafe-eval");
      install({ ShaderSystem });
      // Sample motion audio must never overlap Yui's synthesised speech.
      config.sound = false;
      pendingModel = await Live2DModel.from(packagePath, { autoUpdate: false, autoInteract: false });
      if (generation !== this.generation) {
        pendingModel.destroy({ children: true, texture: true, baseTexture: true });
        return false;
      }
      this.model = pendingModel;
      if (!this.app) {
        this.app = new Application({
        view: this.canvas, width: 720, height: 900,
        backgroundAlpha: 0, antialias: true,
        resolution: Math.min(window.devicePixelRatio || 1, 2), autoDensity: true,
        });
        this.app.ticker.add(() => {
          if (!document.hidden) this.model?.update(this.app!.ticker.deltaMS);
        });
        this.resizeObserver = new ResizeObserver(() => this.resize());
        this.resizeObserver.observe(this.canvas.parentElement!);
      }
      this.app.stage.addChild(this.model);
      const model = this.model;
      model.anchor.set(0.5, 0.5);
      // Apply after motions/physics so idle animations cannot overwrite lip sync.
      model.internalModel.on("beforeModelUpdate", () => {
        (model.internalModel as Cubism4InternalModel).coreModel.setParameterValueById("ParamMouthOpenY", this.mouth);
        if (performance.now() < this.reactionUntil) {
          const core = (model.internalModel as Cubism4InternalModel).coreModel;
          const sway = Math.sin(performance.now() / 170) * 8;
          core.addParameterValueById("ParamAngleZ", this.reactionLabel === "sad" || this.reactionLabel === "concern" ? -Math.abs(sway) : sway, 0.5);
        }
      });
      this.resize();
      this.setExpression(this.expression);
      return true;
    } catch (error) {
      if (generation === this.generation) {
        if (pendingModel && pendingModel !== this.model) pendingModel.destroy();
        this.release();
        console.error("Live2D model loading failed", error);
      }
      return false;
    }
  }

  setExpression(expression: string): void {
    this.expression = expression;
    const model = this.model;
    if (!model) return;
    const manager = model.internalModel.motionManager.expressionManager;
    if (!manager) return;
    // Cancel an older async request, including when the new state is neutral.
    manager.reserveExpressionIndex = -1;
    const name = EXPRESSIONS[expression];
    if (name) void model.expression(name).catch(error => console.warn("Live2D expression failed", error));
    else {
      manager.resetExpression();
      // The library's reset preserves currentExpression for temporary motion
      // overrides; Yui's neutral is permanent and must allow that emotion again.
      manager.currentExpression = manager.defaultExpression;
    }
  }

  setMouth(open: number): void {
    this.mouth = Number.isFinite(open) ? Math.max(0, Math.min(1, open)) : 0;
  }

  setState(state: AvatarState): void { if (state === "error") this.reactionUntil = 0; }
  react(emotion: string, expression: string): void {
    this.setExpression(expression);
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    this.reactionLabel = emotion;
    this.reactionUntil = performance.now() + 3000;
    const model = this.model;
    if (!model) return;
    const gestures: Record<string, number> = { joy: 0, warm: 2, alert: 1, surprise: 0, think: 1, angry: 3 };
    const index = gestures[emotion];
    if (index !== undefined && model.internalModel.motionManager.definitions.TapBody?.[index]) {
      void model.motion("TapBody", index, 2).catch(error => console.warn("Live2D motion failed", error));
    }
  }

  private resize(): void {
    if (!this.app || !this.model) return;
    const frame = this.canvas.parentElement!;
    const width = Math.max(1, frame.clientWidth);
    const height = Math.max(1, frame.clientHeight);
    this.app.renderer.resize(width, height);
    const { width: modelWidth, height: modelHeight } = this.model.internalModel;
    this.model.scale.set(Math.min(width * 0.94 / modelWidth, height * 0.96 / modelHeight));
    this.model.position.set(width / 2, height / 2);
  }

  private release(): void {
    this.app?.stage.removeChildren();
    this.model?.destroy({ children: true, texture: true, baseTexture: true });
    this.model = undefined;
  }

  destroy(): void {
    ++this.generation;
    this.release();
    this.resizeObserver?.disconnect();
    this.resizeObserver = undefined;
    this.app?.destroy(false);
    this.app = undefined;
  }
}

export function createAvatar(canvas: HTMLCanvasElement): Avatar {
  return new Live2DAvatar(canvas);
}

export function createVRMAvatar(canvas: HTMLCanvasElement): Avatar { return new VRMAvatar(canvas); }
