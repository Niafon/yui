/**
 * Detached avatar window ("desktop pet"). It joins the main window's core
 * session, plays the reply audio itself so lip sync stays frame-accurate, and
 * shows only the character on a transparent, draggable surface.
 */

import { CoreClient, type Frame } from "./api";
import { applyAppearance, onPrefs, prefs, updatePrefs } from "./prefs";
import { applyI18n, detectLang, setLang, t } from "./i18n";
import { Stage } from "./stage";
import { WindowHub, currentWindow, isTauri, tauriInvoke, tauriListen } from "./windows";

const el = <T extends HTMLElement>(id: string): T => document.getElementById(id) as T;
applyAppearance();
document.documentElement.dataset.backdrop = "transparent";
const resolveLang = () => (prefs().lang === "auto" ? detectLang() : prefs().lang as "ru" | "en");
setLang(resolveLang());
applyI18n();

const stage = new Stage({
  frame: el("stage-frame"),
  canvas: el<HTMLCanvasElement>("live2d"),
  fallback: el("figure-fallback"),
  audioButton: el<HTMLButtonElement>("stage-audio"),
});
void stage.load();

const status = el("pet-status");
let client: CoreClient | undefined;
let session = "";

const hub = new WindowHub("avatar", () => {
  stage.speech.enabled = hub.ownsAudio;
  const shared = hub.knownSession();
  if (shared && shared !== session) void join(shared);
});
stage.speech.enabled = hub.ownsAudio;

function handleFrame(frame: Frame): void {
  const payload = (frame.payload ?? {}) as Record<string, unknown>;
  switch (frame.type) {
    case "session.state": stage.setState(String(payload.state ?? "idle")); break;
    case "tts.chunk": stage.playChunk(String(payload.audio_b64 ?? ""), Number(payload.sample_rate ?? 16000)); break;
    case "avatar.expression": stage.react(String(payload.label ?? "neutral"), String(payload.expression ?? "exp_neutral")); break;
    case "barge_in": stage.stopSpeech(); break;
    case "error": stage.setState("error"); break;
  }
}

async function join(target: string): Promise<void> {
  let token = "";
  let base = "";
  try {
    token = localStorage.getItem("yui.token") ?? "";
    base = localStorage.getItem("yui.core") ?? location.origin;
  } catch { /* storage blocked */ }
  if (!token && isTauri()) {
    try {
      const { invoke } = await import("@tauri-apps/api/core");
      ({ base, token } = await invoke<{ base: string; token: string }>("core_endpoint"));
    } catch { /* not available */ }
  }
  if (!token || !target) { status.textContent = t("conn.connectInMain"); return; }
  session = target;
  client?.close();
  const current = new CoreClient(base, token);
  client = current;
  try {
    const info = await current.session(target);
    if (client !== current) return;
    stage.setState(info.state);
    current.connect(target, handleFrame, () => {
      if (client !== current) return;
      stage.stopSpeech();
      stage.setState("error");
      status.textContent = t("conn.lost");
      session = "";
    });
    status.textContent = "";
  } catch (error) {
    if (client === current) {
      status.textContent = t("conn.retrying");
      console.warn("avatar window join failed", error);
      session = "";
    }
  }
}

const initial = new URLSearchParams(location.search).get("session") ?? "";
void (async () => {
  const target = initial || await hub.discover();
  if (target) await join(target);
  else status.textContent = t("conn.connectInMain");
})();

// After the core restarts, rejoin as soon as the session is reachable again.
let joining = false;
window.setInterval(() => {
  if (session || joining) return;
  const target = hub.knownSession() || initial;
  if (!target) return;
  joining = true;
  void join(target).finally(() => { joining = false; });
}, 3000);

const zoom = (factor: number) => updatePrefs({ avatarScale: Math.max(0.6, Math.min(2.2, +(prefs().avatarScale * factor).toFixed(2))) });
el("pet-bigger").addEventListener("click", () => zoom(1.1));
el("pet-smaller").addEventListener("click", () => zoom(1 / 1.1));
el("stage-frame").addEventListener("wheel", event => { event.preventDefault(); zoom(event.deltaY < 0 ? 1.05 : 1 / 1.05); }, { passive: false });

const topButton = el<HTMLButtonElement>("pet-top");
async function applyOnTop(): Promise<void> {
  topButton.setAttribute("aria-pressed", String(prefs().avatarOnTop));
  topButton.hidden = !isTauri();
  await (await currentWindow())?.setAlwaysOnTop(prefs().avatarOnTop).catch(() => undefined);
}
topButton.addEventListener("click", () => updatePrefs({ avatarOnTop: !prefs().avatarOnTop }));
onPrefs((next, changed) => {
  if (changed.includes("avatarOnTop")) void applyOnTop();
  if (changed.includes("lang")) setLang(resolveLang());
  if (changed.some(key => key === "theme" || key === "accent")) { applyAppearance(next); document.documentElement.dataset.backdrop = "transparent"; }
});
void applyOnTop();

// Desktop pet: drag the window by the character itself, not only the handle.
el("stage-frame").addEventListener("pointerdown", event => {
  if (event.button !== 0 || (event.target as HTMLElement).closest("button:not(#pet-drag)")) return;
  void currentWindow().then(win => win?.startDragging());
});
el("pet-close").addEventListener("click", () => {
  void currentWindow().then(win => (win ? win.close() : window.close()));
});
// Click-through: the window ignores the mouse until Ctrl+Shift+Y or the tray.
const throughButton = el<HTMLButtonElement>("pet-through");
throughButton.hidden = !isTauri();
const showThrough = (on: boolean) => {
  el("pet-through-note").hidden = !on;
  document.body.classList.toggle("click-through", on);
};
throughButton.addEventListener("click", () => void tauriInvoke("set_click_through", { enabled: true }));
void tauriListen<boolean>("yui://click-through", showThrough);
void tauriInvoke<boolean>("click_through").then(on => showThrough(Boolean(on)));

window.addEventListener("beforeunload", () => { client?.close(); stage.dispose(); hub.close(); });
