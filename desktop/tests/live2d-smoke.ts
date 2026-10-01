// Open /tests/live2d.html with `npm run dev`. Tests real WebGL under Tauri's CSP
// for every bundled Live2D model.
import { Live2DAvatar } from "../src/avatar/live2d";
import { BUILTIN_AVATARS } from "../src/avatar/builtin";

let canvas = document.querySelector<HTMLCanvasElement>("#avatar")!;
/** The stage gives every model a fresh canvas (WebGL contexts are not reused). */
function freshCanvas(): HTMLCanvasElement {
  const next = canvas.cloneNode(false) as HTMLCanvasElement;
  canvas.replaceWith(next);
  canvas = next;
  return next;
}
const results = document.querySelector("#results")!;
const passed: string[] = [];
function check(value: unknown, label: string) {
  if (!value) throw new Error(label);
  passed.push(`PASS ${label}`);
  results.textContent = passed.join("\n");
}
const visemes = (aa: number, ih = 0, ou = 0, ee = 0, oh = 0) => ({ aa, ih, ou, ee, oh });
try {
  for (const entry of BUILTIN_AVATARS.filter(item => item.kind === "live2d")) {
    const avatar = new Live2DAvatar(freshCanvas(), entry.id);
    check(await avatar.load(entry.url), `${entry.id} loads under CSP`);
    const state = avatar as any;
    const model = state.model;
    const core = model.internalModel.coreModel;
    check(model.width > 0 && model.height > 0, `${entry.id} nonzero bounds`);
    check(model.internalModel.motionManager.definitions.Idle.length > 0, `${entry.id} idle motions present`);
    const vowels = state.ids.has("ParamMouthA");
    const read = () => core.getParameterValueById(vowels ? "ParamMouthA" : "ParamMouthOpenY");
    let mouth = -1;
    let form = 0;
    // Layer values are per-frame: read them before the core restores state.
    model.internalModel.on("beforeModelUpdate", () => { mouth = read(); form = core.getParameterValueById("ParamMouthForm"); });
    avatar.setVisemes(visemes(0.7));
    model.internalModel.update(16, 16);
    check(mouth > 0.6, `${entry.id} lip sync survives animation update (${vowels ? "vowels" : "open/form"} ${mouth.toFixed(2)})`);
    if (!vowels && state.ids.has("ParamMouthForm")) {
      avatar.setVisemes(visemes(0, 0.6));
      model.internalModel.update(16, 32);
      const spread = form;
      avatar.setVisemes(visemes(0, 0, 0.6));
      model.internalModel.update(16, 48);
      const round = form;
      check(spread > round, `${entry.id} vowel changes mouth form (${spread.toFixed(2)} > ${round.toFixed(2)})`);
    }
    avatar.setVisemes(visemes(0));
    model.internalModel.update(16, 64);
    check(mouth < 0.05, `${entry.id} silence closes mouth`);
    avatar.react("joy", "exp_smile");
    check(state.emotion.preset === "happy", `${entry.id} reaction selects happy`);
    for (let i = 0; i < 30; i++) model.internalModel.update(16, 80 + i * 16);
    check((state.weights.happy ?? 0) > 0.2, `${entry.id} emotion layer eases in`);
    avatar.react("neutral", "exp_neutral");
    const manager = model.internalModel.motionManager.expressionManager;
    if (manager) check(manager.currentExpression === manager.defaultExpression, `${entry.id} neutral resets expression file`);
    avatar.destroy();
    check(!state.model && !state.app, `${entry.id} destroy releases renderer and model`);
  }
  const avatar = new Live2DAvatar(freshCanvas(), "haru");
  check(!(await avatar.load("/missing-live2d.model3.json")), "missing model returns failure");
  check(await avatar.load("/assets/live2d/haru/Haru.model3.json"), "model reloads after failure");
  const frame = canvas.parentElement!;
  frame.style.width = "320px";
  frame.style.height = "400px";
  await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
  check((avatar as any).app.screen.width === 320 && (avatar as any).app.screen.height === 400, "resize follows frame");
  avatar.destroy();
  results.textContent += "\nALL PASSED";
} catch (error) {
  results.textContent += `\nFAIL ${String(error)}`;
  throw error;
}
