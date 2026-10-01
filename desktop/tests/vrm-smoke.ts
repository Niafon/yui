import { VRMAvatar } from "../src/live2d";
const results = document.querySelector("#results")!;
function check(value: unknown, label: string) {
  if (!value) throw new Error(label);
  results.textContent += `\nPASS ${label}`;
}
try {
  for (const name of ["shino", "victoria"]) {
    const canvas = document.createElement("canvas");
    document.querySelector("#avatar")!.replaceWith(canvas); canvas.id = "avatar";
    const avatar = new VRMAvatar(canvas);
    check(await avatar.load(`/assets/vrm/${name}.vrm`), `${name} loads`);
    const state = avatar as any;
    check(state.motion.currentMotion === "idle", `${name} idle animation starts`);
    check(state.motion.actions.size === 9, `${name} motion pack loaded`);
    avatar.setState("speaking");
    check(state.motion.currentMotion === "idle-talking", `${name} talking idle starts`);
    avatar.react("joy", "exp_smile");
    check(state.motion.currentMotion === "happy", `${name} joy gesture starts`);
    for (let i = 0; i < 120; i++) state.motion.update(0.05);
    check(state.motion.currentMotion !== "happy", `${name} gesture returns to base`);
    check(state.model.expressionManager.getExpression("aa"), `${name} mouth binding exists`);
    avatar.setMouth(0.8); state.render();
    check(state.model.expressionManager.getValue("aa") === 0.8, `${name} mouth updates`);
    let activeMorph = false;
    state.model.scene.traverse((node: any) => { if (node.morphTargetInfluences?.some((x: number) => x > 0.5)) activeMorph = true; });
    check(activeMorph, `${name} visible morph applied`);
    avatar.setMouth(0); state.render();
    check(state.model.expressionManager.getValue("aa") === 0, `${name} mouth closes`);
    avatar.destroy(); check(!state.renderer && !state.model, `${name} disposal`);
    const pending = avatar.load(`/assets/vrm/${name}.vrm`); avatar.destroy();
    check(!(await pending) && !state.renderer && !state.model, `${name} cancelled load stays disposed`);
  }
  results.textContent += "\nALL PASSED";
} catch (error) { results.textContent += `\nFAIL ${error}`; throw error; }
