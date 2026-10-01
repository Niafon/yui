// Open /tests/live2d.html with npm run dev. Tests real WebGL under Tauri's CSP.
import { Live2DAvatar, DEFAULT_MODEL } from "../src/live2d";

const canvas = document.querySelector<HTMLCanvasElement>("#avatar")!;
const results = document.querySelector("#results")!;
const avatar = new Live2DAvatar(canvas);
const passed: string[] = [];
function check(value: unknown, label: string) {
  if (!value) throw new Error(label);
  passed.push(`PASS ${label}`);
  results.textContent = passed.join("\n");
}
try {
  check(await avatar.load(DEFAULT_MODEL), "model loads under CSP");
  // Inspect the adapter for deterministic integration assertions without exposing
  // debug globals in the application.
  const state = avatar as any;
  const model = state.model;
  check(model.width > 0 && model.height > 0, "nonzero model bounds");
  check(model.internalModel.motionManager.definitions.Idle.length > 0, "idle motions present");
  avatar.setMouth(0.75);
  let mouth = -1;
  model.internalModel.on("beforeModelUpdate", () => {
    mouth = model.internalModel.coreModel.getParameterValueById("ParamMouthOpenY");
  });
  model.internalModel.update(16, 16);
  check(Math.abs(mouth - 0.75) < 0.01, "lip sync survives animation update");
  avatar.setMouth(Number.NaN);
  model.internalModel.update(16, 32);
  check(mouth === 0, "invalid amplitude closes mouth");
  for (const expression of ["F01", "F04", "F08", "F06"]) {
    check(await model.expression(expression), `expression ${expression} loads`);
  }
  avatar.setExpression("exp_neutral");
  const manager = model.internalModel.motionManager.expressionManager;
  check(manager.currentExpression === manager.defaultExpression, "neutral resets expression");
  avatar.react("joy", "exp_smile");
  check(state.expression === "exp_smile", "reaction selects smile");
  check(state.reactionUntil > performance.now(), "reaction movement scheduled");
  await new Promise(resolve => setTimeout(resolve, 400));
  check(model.internalModel.motionManager.state.currentPriority >= 2, "reaction motion overrides idle");
  const frame = canvas.parentElement!;
  frame.style.width = "320px";
  frame.style.height = "400px";
  await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
  check(state.app.screen.width === 320 && state.app.screen.height === 400, "resize follows frame");
  check(!(await avatar.load("/missing-live2d.model3.json")), "missing model returns failure");
  check(await avatar.load(DEFAULT_MODEL), "model reloads after failure");
  avatar.destroy();
  check(!state.model && !state.app, "destroy releases renderer and model");
  results.textContent += "\nALL PASSED";
} catch (error) {
  results.textContent += `\nFAIL ${String(error)}`;
  throw error;
}
