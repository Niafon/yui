import type { Application } from "pixi.js";
import type { Live2DModel, Cubism4InternalModel } from "pixi-live2d-display/cubism4";
import { Blinker, Saccades, noise } from "./life";
import {
  DEFAULT_AVATAR_OPTIONS, EMOTION_PRESETS, damp, emotionPreset, mouthForm, mouthOpen, prefersReducedMotion, silentVisemes,
  type Avatar, type AvatarOptions, type AvatarState, type EmotionPreset, type Visemes,
} from "./types";

/** Per-model knowledge the package format does not carry. */
export interface Live2DProfile {
  /** Expression file names per emotion preset. */
  expressions: Partial<Record<EmotionPreset, string>>;
  /** Reaction label → TapBody motion index. */
  gestures: Record<string, number>;
  /** Strength of the parameter emotion layer for presets with an expression
   * file (presets without one always use the layer at full strength). */
  layerWithFile?: Partial<Record<EmotionPreset, number>>;
  /** Portrait zoom at scale 1 when the package's canvas has wide margins. */
  zoom?: number;
}

export const LIVE2D_PROFILES: Record<string, Live2DProfile> = {
  haru: {
    expressions: { happy: "F01", sad: "F04", surprised: "F06", angry: "F03", think: "F08" },
    gestures: { joy: 0, warm: 2, alert: 1, surprise: 0, think: 1, angry: 3 },
    layerWithFile: { happy: 0.6 },
  },
  hiyori: { expressions: {}, gestures: { joy: 0, surprise: 0 }, zoom: 1.9 },
  natori: {
    expressions: { happy: "Smile", sad: "Sad", angry: "Angry", surprised: "Surprised" },
    gestures: { joy: 0, warm: 1 },
  },
  mao: {
    expressions: { sad: "exp_05", surprised: "exp_07", angry: "exp_08" },
    gestures: { joy: 0, warm: 1, surprise: 2 },
  },
};

/** Additive parameter offsets per preset at full weight. Missing ids are
 * skipped, so one table serves every Cubism model with standard names. */
const LAYER: Record<EmotionPreset, Record<string, number>> = {
  happy: { ParamEyeLSmile: 0.8, ParamEyeRSmile: 0.8, ParamMouthForm: 0.8, ParamCheek: 0.4, ParamBrowLY: 0.25, ParamBrowRY: 0.25, ParamMouthUp: 0.6 },
  sad: { ParamBrowLForm: -0.8, ParamBrowRForm: -0.8, ParamBrowLY: -0.4, ParamBrowRY: -0.4, ParamBrowLAngle: 0.35, ParamBrowRAngle: 0.35, ParamMouthForm: -0.8, ParamEyeLOpen: -0.15, ParamEyeROpen: -0.15, ParamMouthDown: 0.6 },
  angry: { ParamBrowLAngle: -0.8, ParamBrowRAngle: -0.8, ParamBrowLForm: -0.6, ParamBrowRForm: -0.6, ParamMouthForm: -0.7, ParamEyeLOpen: -0.1, ParamEyeROpen: -0.1, ParamMouthAngry: 0.6 },
  surprised: { ParamEyeLOpen: 0.3, ParamEyeROpen: 0.3, ParamBrowLY: 0.8, ParamBrowRY: 0.8 },
  relaxed: { ParamEyeLOpen: -0.2, ParamEyeROpen: -0.2, ParamMouthForm: 0.3 },
  think: { ParamBrowLY: 0.3, ParamBrowRAngle: -0.3, ParamMouthForm: -0.3 },
};
const VOWELS = ["ParamMouthA", "ParamMouthI", "ParamMouthU", "ParamMouthE", "ParamMouthO"] as const;

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

type CoreModel = Cubism4InternalModel["coreModel"];
/** A source may be a URL or an already parsed model3.json with a base url. */
export type Live2DSource = string | (Record<string, unknown> & { url: string });

export class Live2DAvatar implements Avatar {
  private app?: Application;
  private model?: Live2DModel;
  private resizeObserver?: ResizeObserver;
  private generation = 0;
  private profile: Live2DProfile = { expressions: {}, gestures: {} };
  private ids = new Set<string>();
  private options: AvatarOptions = { ...DEFAULT_AVATAR_OPTIONS };
  private state: AvatarState = "idle";
  private visemes = silentVisemes();
  private readonly blinker = new Blinker();
  private readonly saccades = new Saccades();
  private readonly reduced = prefersReducedMotion();
  private readonly weights: Partial<Record<EmotionPreset, number>> = {};
  private readonly emotion: { preset?: EmotionPreset; peak: number; since: number } = { peak: 0, since: 0 };
  private pointerTarget: { x: number; y: number } | null = null;
  private readonly head = { x: 0, y: 0, z: 0 };
  private speech = 0;
  private time = 0;

  constructor(private readonly canvas: HTMLCanvasElement, private readonly profileId = "") {}

  async load(source: string): Promise<boolean> { return this.loadSource(source); }

  async loadSource(source: Live2DSource): Promise<boolean> {
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
      config.motionFadingDuration = 600;
      config.expressionFadingDuration = 450;
      pendingModel = await Live2DModel.from(source as string, { autoUpdate: false, autoInteract: false });
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
        if (this.canvas.parentElement) this.resizeObserver.observe(this.canvas.parentElement);
      }
      this.app.stage.addChild(this.model);
      const model = this.model;
      model.anchor.set(0.5, 0.5);
      const internal = model.internalModel as Cubism4InternalModel;
      this.profile = LIVE2D_PROFILES[this.profileId] ?? { expressions: {}, gestures: {} };
      this.ids = new Set((internal.coreModel.getModel() as unknown as { parameters: { ids: string[] } }).parameters.ids);
      // pixi-live2d-display starts idle motions from an un-awaited promise; if
      // the model is destroyed while a motion file loads it throws uncaught.
      const manager = internal.motionManager as unknown as { destroyed: boolean; _startMotion: (...args: unknown[]) => unknown };
      const startMotion = manager._startMotion.bind(manager);
      manager._startMotion = (...args: unknown[]) => (manager.destroyed ? undefined : startMotion(...args));
      // Our blinker runs even while idle motions play (the built-in one stops).
      (internal as unknown as { eyeBlink?: unknown }).eyeBlink = undefined;
      // Runs after parameters are saved and before physics, so offsets are
      // per-frame (never accumulate) and hair/physics react to head motion.
      const natural = internal.updateNaturalMovements.bind(internal);
      internal.updateNaturalMovements = (dt: number, now: number) => {
        natural(dt, now);
        if (this.model === model) this.applyLayers(internal.coreModel, dt / 1000);
      };
      this.resize();
      this.applyExpression();
      return true;
    } catch (error) {
      if (generation === this.generation) {
        if (pendingModel && pendingModel !== this.model) {
          try { pendingModel.destroy(); } catch { /* partially initialised */ }
        }
        this.release();
        console.error("Live2D model loading failed", error);
      }
      return false;
    }
  }

  setVisemes(visemes: Visemes): void { this.visemes = visemes; }

  setState(state: AvatarState): void {
    if (state !== this.state) this.blinker.nudge();
    this.state = state;
  }

  react(emotion: string, expression: string): void {
    const { preset, weight } = emotionPreset(emotion, expression);
    const changed = preset !== this.emotion.preset;
    this.emotion.preset = preset;
    this.emotion.peak = weight;
    this.emotion.since = this.time;
    if (changed) this.applyExpression();
    const model = this.model;
    if (!model || this.reduced) return;
    const index = this.profile.gestures[emotion];
    if (index !== undefined && model.internalModel.motionManager.definitions.TapBody?.[index]) {
      void model.motion("TapBody", index, 2).catch(error => console.warn("Live2D motion failed", error));
    }
  }

  setOptions(options: Partial<AvatarOptions>): void {
    this.options = { ...this.options, ...options };
    this.resize();
  }

  pointer(x: number, y: number): void { if (this.options.followPointer) this.pointerTarget = { x, y }; }
  pointerLeave(): void { this.pointerTarget = null; }

  private applyExpression(): void {
    const model = this.model;
    const manager = model?.internalModel.motionManager.expressionManager;
    if (!model || !manager) return;
    // Cancel an older async request, including when the new state is neutral.
    manager.reserveExpressionIndex = -1;
    const name = this.emotion.preset ? this.profile.expressions[this.emotion.preset] : undefined;
    if (name) void model.expression(name).catch(error => console.warn("Live2D expression failed", error));
    else {
      manager.resetExpression();
      // The library keeps currentExpression for temporary overrides; Yui's
      // neutral is permanent and must allow the same emotion again later.
      manager.currentExpression = manager.defaultExpression;
    }
  }

  private add(core: CoreModel, id: string, value: number): void {
    if (value !== 0 && this.ids.has(id)) core.addParameterValueById(id, value);
  }

  private set(core: CoreModel, id: string, value: number): void {
    if (this.ids.has(id)) core.setParameterValueById(id, value);
  }

  private applyLayers(core: CoreModel, dtRaw: number): void {
    const dt = Math.min(0.05, Math.max(0, dtRaw));
    this.time += dt;
    const motion = this.reduced ? 0 : Math.max(0, Math.min(1.5, this.options.motion));
    const open = mouthOpen(this.visemes);
    this.speech = damp(this.speech, open, 10, dt);

    // Emotion: peak, then settle to a softer resting face.
    const settled = this.time - this.emotion.since > 5 ? this.emotion.peak * 0.6 : this.emotion.peak;
    for (const preset of EMOTION_PRESETS) {
      const target = preset === this.emotion.preset ? settled : 0;
      const current = this.weights[preset] ?? 0;
      const weight = damp(current, target, target > current ? 6 : 3.5, dt);
      this.weights[preset] = weight;
      if (weight < 0.003) continue;
      const scale = this.profile.expressions[preset] ? (this.profile.layerWithFile?.[preset] ?? 0) : 1;
      if (!scale) continue;
      for (const [id, delta] of Object.entries(LAYER[preset])) this.add(core, id, delta * weight * scale);
    }

    // Gaze: pointer pursuit or idle saccades through the focus controller.
    const idle = this.saccades.update(dt, this.state);
    const focus = this.model?.internalModel.focusController;
    if (focus) {
      if (this.pointerTarget) focus.focus(this.pointerTarget.x * 0.85, this.pointerTarget.y * 0.85);
      else focus.focus(idle.x * 0.3 * motion, idle.y * 0.3 * motion);
    }
    if (!this.pointerTarget) {
      this.add(core, "ParamEyeBallX", idle.x * 0.55 * motion);
      this.add(core, "ParamEyeBallY", idle.y * 0.55 * motion);
    }

    // Head: state posture, speech emphasis, gentle sway. Degrees.
    let x = noise(this.time * 0.25, 2.4) * 2.5 * motion;
    let y = noise(this.time * 0.3, 6.1) * 1.8 * motion;
    let z = noise(this.time * 0.35, 8.8) * 2.2 * motion;
    if (this.state === "thinking") { z += 7; y += 4; }
    if (this.state === "listening") { z += 4; y -= 2; }
    if (this.state === "speaking") {
      y -= this.speech * 6 * motion;
      x += noise(this.time * 0.9, 1.1) * 4 * this.speech * motion;
    }
    if (this.emotion.preset === "sad") y -= 5;
    this.head.x = damp(this.head.x, x, 6, dt);
    this.head.y = damp(this.head.y, y, 6, dt);
    this.head.z = damp(this.head.z, z, 4, dt);
    this.add(core, "ParamAngleX", this.head.x);
    this.add(core, "ParamAngleY", this.head.y);
    this.add(core, "ParamAngleZ", this.head.z);
    this.add(core, "ParamBodyAngleX", this.head.x * 0.3);
    this.add(core, "ParamBodyAngleZ", this.head.z * 0.25);

    // Blink scales whatever openness motions and expressions produced.
    const blink = this.blinker.update(dt, this.state);
    if (blink > 0) {
      for (const id of ["ParamEyeLOpen", "ParamEyeROpen"]) {
        if (this.ids.has(id)) core.setParameterValueById(id, core.getParameterValueById(id) * (1 - blink));
      }
    }

    // Mouth last: speech owns the mouth while it is moving.
    if (this.ids.has("ParamMouthA")) {
      const v = this.visemes;
      const values = [v.aa, v.ih, v.ou, v.ee, v.oh];
      VOWELS.forEach((id, index) => this.set(core, id, Math.min(1, (values[index] ?? 0) * 1.15)));
    } else {
      const surprised = (this.weights.surprised ?? 0) * 0.25;
      this.set(core, "ParamMouthOpenY", Math.min(1, Math.max(open * 1.1, surprised)));
      if (this.ids.has("ParamMouthForm")) {
        const base = core.getParameterValueById("ParamMouthForm");
        const talking = Math.min(1, open * 2.5);
        const target = base * 0.4 + mouthForm(this.visemes) * 0.8;
        core.setParameterValueById("ParamMouthForm", base + (target - base) * talking);
      }
    }
  }

  private resize(): void {
    if (!this.app || !this.model || !this.canvas.parentElement) return;
    const frame = this.canvas.parentElement;
    const width = Math.max(1, frame.clientWidth);
    const height = Math.max(1, frame.clientHeight);
    this.app.renderer.resize(width, height);
    const { width: modelWidth, height: modelHeight } = this.model.internalModel;
    const fit = Math.min(width * 0.94 / modelWidth, height * 0.96 / modelHeight);
    // Full-body packages read better as a portrait, like the VRM framing.
    const portrait = this.profile.zoom ?? (modelHeight / modelWidth > 1.5 ? 1.55 : 1.15);
    const scale = Math.max(0.5, Math.min(3, this.options.scale)) * portrait;
    this.model.scale.set(fit * scale);
    // Zooming keeps the face in view: the anchor drifts towards the head.
    const zoomShift = (scale - 1) * modelHeight * fit * 0.36;
    this.model.position.set(width / 2, height / 2 + zoomShift - this.options.offsetY * height * 0.5);
  }

  private release(): void {
    this.app?.stage.removeChildren();
    const model = this.model;
    this.model = undefined;
    // A model that failed half-way can throw from its own teardown; the
    // stage must stay usable for the next load either way.
    try { model?.destroy({ children: true, texture: true, baseTexture: true }); }
    catch (error) { console.warn("Live2D model teardown failed", error); }
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
