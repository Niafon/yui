// Open /tests/vrm.html with `npm run dev`. Real WebGL: clips, gaze, blink,
// visemes and disposal for both bundled anime avatars.
import { VRMAvatar } from "../src/avatar/vrm";

const results = document.querySelector("#results")!;
function check(value: unknown, label: string) {
  if (!value) throw new Error(label);
  results.textContent += `\nPASS ${label}`;
}
const visemes = (aa: number, ih = 0, ou = 0, ee = 0, oh = 0) => ({ aa, ih, ou, ee, oh });
try {
  for (const name of ["shino", "victoria"]) {
    const canvas = document.createElement("canvas");
    document.querySelector("#avatar")!.replaceWith(canvas); canvas.id = "avatar";
    const avatar = new VRMAvatar(canvas);
    check(await avatar.load(`/assets/vrm/${name}.vrm`), `${name} loads`);
    const state = avatar as any;
    const clips = state.clips;
    check(clips.currentMotion === "idle", `${name} idle animation starts`);
    check(clips.actions.size === 9, `${name} motion pack loaded`);
    avatar.setState("speaking");
    check(clips.currentMotion === "idle-talking", `${name} talking idle starts`);
    avatar.react("joy", "exp_smile");
    check(clips.currentMotion === "happy", `${name} joy gesture starts`);
    avatar.react("joy", "exp_smile");
    check(clips.currentMotion === "happy", `${name} repeated gesture does not restart`);
    for (let i = 0; i < 160; i++) clips.update(0.05);
    check(clips.currentMotion !== "happy", `${name} gesture returns to base`);

    const manager = state.model.expressionManager;
    for (const key of ["aa", "ih", "ou", "ee", "oh", "blink", "happy"]) check(manager.getExpression(key), `${name} has ${key}`);
    avatar.setVisemes(visemes(0.7, 0, 0.2)); state.render();
    check(Math.abs(manager.getValue("aa") - 0.735) < 0.01 && manager.getValue("ou") > 0.2, `${name} vowel visemes applied`);
    let activeMorph = false;
    state.model.scene.traverse((node: any) => { if (node.morphTargetInfluences?.some((x: number) => x > 0.4)) activeMorph = true; });
    check(activeMorph, `${name} visible morph applied`);
    avatar.setVisemes(visemes(0)); state.render();
    check(manager.getValue("aa") === 0, `${name} mouth closes`);

    // Emotion fades in rather than snapping.
    state.lastFrame = performance.now() - 16; state.render();
    const first = manager.getValue("happy");
    for (let i = 0; i < 40; i++) { state.lastFrame = performance.now() - 16; state.render(); }
    const later = manager.getValue("happy");
    check(first < 0.6 && later > first, `${name} emotion eases in (${first.toFixed(2)} → ${later.toFixed(2)})`);

    // Gaze follows the pointer through the lookAt target.
    avatar.pointer(1, 0);
    for (let i = 0; i < 60; i++) { state.lastFrame = performance.now() - 16; state.render(); }
    check(state.lookTarget.position.x > state.camera.position.x + 0.3, `${name} gaze follows pointer`);
    avatar.pointerLeave();

    // Procedural offsets must not accumulate on bones between frames.
    const head = state.model.humanoid.getNormalizedBoneNode("head");
    for (let i = 0; i < 200; i++) { state.lastFrame = performance.now() - 16; state.render(); }
    const angle = 2 * Math.acos(Math.min(1, Math.abs(head.quaternion.w)));
    check(angle < 0.6, `${name} head rotation bounded (${angle.toFixed(3)} rad)`);

    avatar.destroy(); check(!state.renderer && !state.model, `${name} disposal`);
    const pending = avatar.load(`/assets/vrm/${name}.vrm`); avatar.destroy();
    check(!(await pending) && !state.renderer && !state.model, `${name} cancelled load stays disposed`);
  }
  results.textContent += "\nALL PASSED";
} catch (error) { results.textContent += `\nFAIL ${error}`; throw error; }
