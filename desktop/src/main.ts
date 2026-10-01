/**
 * Stage wiring. Reads frames from the core, renders state, and shows exactly
 * which data categories left the machine on each turn.
 *
 * The same page runs as the main window (avatar + chat + settings) or as a
 * detached chat window (?view=chat). Every window is an independent client of
 * the same core session; WindowHub decides which one plays audio.
 */

import {
  CoreClient,
  type Frame,
  type Identity,
  type ProviderStatus,
  type InferenceDecision,
  type InferenceStatus,
  type PendingTool,
} from "./api";
import { applyI18n, detectLang, has, onLang, setLang, t, type Key } from "./i18n";
import { renderMarkdown } from "./markdown";
import { Microphone } from "./mic";
import { initModelSettings } from "./model-settings";
import { applyAppearance, onPrefs, prefs } from "./prefs";
import { initSettings, iconButton } from "./settings";
import { Stage } from "./stage";
import { WindowHub, closeDetached, isTauri, openDetached, tauriInvoke, tauriListen, type WindowRole } from "./windows";
import QRCode from "qrcode";

const el = <T extends HTMLElement>(id: string): T => {
  const node = document.getElementById(id);
  if (!node) throw new Error(`missing element: ${id}`);
  return node as T;
};

const stateLabel = (state: string): string => (has(`state.${state}`) ? t(`state.${state}` as Key) : state);
const emotionLabel = (label: string): string => (has(`emotion.${label}`) ? t(`emotion.${label}` as Key) : "");

const query = new URLSearchParams(location.search);
const view: WindowRole = query.get("view") === "chat" ? "chat" : "main";
document.body.classList.toggle("view-main", view === "main");
document.body.classList.toggle("view-chat", view === "chat");
applyAppearance();
setLang(prefs().lang === "auto" ? detectLang() : prefs().lang as "ru" | "en");
applyI18n();

const stage = new Stage({
  frame: el("stage-frame"),
  canvas: el<HTMLCanvasElement>("live2d"),
  fallback: el("figure-fallback"),
  ribbon: el<HTMLCanvasElement>("presence"),
  audioButton: el<HTMLButtonElement>("stage-audio"),
});
el("presence").hidden = !prefs().showRibbon;
const hub = new WindowHub(view, () => syncWindows());
hub.onUserText = (session, text) => { if (session === sessionId) addTurn(t("chat.you"), text, "turn--user"); };
// The chat window never shows the avatar; main mounts it unless detached.
if (view === "main" && !hub.hasPeer("avatar")) void stage.load();
else stage.setActive(false);
window.addEventListener("beforeunload", () => { stage.dispose(); hub.close(); });

let client: CoreClient | undefined;
let sessionId = "";
let identityId = "";
let identity: Identity | undefined;
let streamingTurn: HTMLElement | undefined;
let inferenceTimer: number | undefined;
let lastInferenceStatus: InferenceStatus | undefined;
let latestLLMDecision: InferenceDecision | undefined;
let inferenceAvailable = false;
let providerLocal = new Map<string, boolean>();
let remoteDefaultProvider = false;
let ledgerLoaded = false;
let modelSettings: ReturnType<typeof initModelSettings> | undefined;
let assistantSpeaking = false;
let coreVersion = "";
let micActive = false;
let reconnectTimer: number | undefined;
let reconnectDelay = 2000;

/** The core restarts (updates, crashes, sleep): rejoin the same session. */
function scheduleReconnect(): void {
  if (reconnectTimer !== undefined) return;
  const session = localStorage.getItem("yui.session") ?? "";
  el("bar-core").textContent = t("conn.retry", { seconds: Math.round(reconnectDelay / 1000) });
  reconnectTimer = window.setTimeout(() => {
    reconnectTimer = undefined;
    reconnectDelay = Math.min(30000, reconnectDelay * 2);
    void connect(view === "main" ? session : hub.knownSession() || session, true);
  }, reconnectDelay);
}

/** Reacts to windows opening and closing: audio ownership and layout. */
function syncWindows(): void {
  const avatarAway = view === "main" && hub.hasPeer("avatar");
  const chatAway = view === "main" && hub.hasPeer("chat");
  document.body.classList.toggle("avatar-detached", avatarAway);
  document.body.classList.toggle("chat-detached", chatAway);
  el("stage-frame").hidden = avatarAway;
  el("stage-detached").hidden = !avatarAway;
  el("chat-detached-note").hidden = !chatAway;
  if (view === "main") stage.setActive(!avatarAway);
  const owns = hub.ownsAudio;
  if (!owns && stage.speech.enabled) stage.stopSpeech();
  stage.speech.enabled = owns;
  // A detached window follows the main window to a new conversation.
  if (view !== "main" && client && sessionId) {
    const shared = hub.knownSession();
    if (shared && shared !== sessionId) void connect(shared);
  }
}

function updatePrivacyTag(preferences?: InferenceStatus["preferences"]): void {
  const remoteLock = preferences?.preferred_provider
    ? providerLocal.get(preferences.preferred_provider) === false
    : lastInferenceStatus?.models.some(model => model.kind === "llm" && model.local === false
      && (model.model_family || model.model) === preferences?.locked_model) ?? false;
  const tag = el("bar-privacy");
  tag.textContent = remoteLock ? t("privacy.remoteLock")
    : remoteDefaultProvider ? t("privacy.remoteDefault") : t("privacy.local");
  tag.className = `tag ${remoteLock || remoteDefaultProvider ? "tag--remote" : "tag--local"}`;
}

const INFERENCE_CONTROL_IDS = [
  "inference-mode",
  "inference-model",
  "inference-quality",
  "inference-downgrade",
] as const;

function setInferenceControlsEnabled(enabled: boolean): void {
  inferenceAvailable = enabled;
  for (const id of INFERENCE_CONTROL_IDS) {
    el<HTMLInputElement | HTMLSelectElement>(id).disabled = !enabled;
  }
}

/** Settings come from the core's own launch output, not from a hardcoded key. */
function readConnection(): { base: string; token: string } {
  const params = new URLSearchParams(location.search);
  const fragment = new URLSearchParams(location.hash.slice(1));
  const sameOrigin = location.protocol.startsWith("http") && location.port !== "5273";
  return {
    base: params.get("core") ?? localStorage.getItem("yui.core") ?? (sameOrigin ? location.origin : "http://127.0.0.1:8766"),
    token: fragment.get("token") ?? params.get("token") ?? localStorage.getItem("yui.token") ?? "",
  };
}

/** Inside the desktop shell the launcher hands the endpoint to Tauri. */
async function tauriConnection(): Promise<{ base: string; token: string } | undefined> {
  if (!isTauri()) return undefined;
  try {
    const { invoke } = await import("@tauri-apps/api/core");
    const endpoint = await invoke<{ base: string; token: string }>("core_endpoint");
    return endpoint.token ? endpoint : undefined;
  } catch { return undefined; }
}

let currentState = "offline";
function setState(state: string): void {
  currentState = state;
  el("state-label").textContent = stateLabel(state);
  el("state-dot").dataset.state = state;
  assistantSpeaking = state === "speaking";
  stage.setState(state);
}

function formatTime(at?: string): string {
  const time = at ? new Date(at) : new Date();
  return Number.isNaN(time.getTime()) ? "" : time.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

function addTurn(who: string, text: string, kind = "", at?: string): HTMLParagraphElement {
  const list = el<HTMLOListElement>("transcript");
  document.getElementById("transcript-empty")?.remove();
  const stick = list.scrollHeight - list.scrollTop - list.clientHeight < 80;
  const item = document.createElement("li");
  item.className = `turn ${kind || (who === t("chat.you") ? "turn--user" : "turn--notice")}`;
  const label = document.createElement("span");
  label.className = "turn__who";
  label.textContent = who;
  const time = document.createElement("time");
  time.className = "turn__time";
  time.textContent = formatTime(at);
  label.append(time);
  const body = document.createElement("p");
  body.className = "turn__text";
  body.textContent = text;
  item.append(label, body);
  list.append(item);
  // Follow new messages unless the reader scrolled up to read history.
  if (stick) list.scrollTop = list.scrollHeight;
  return body;
}

/** Assistant replies are Markdown; rendering is batched per frame while streaming. */
function renderAssistant(body: HTMLElement, text: string): void {
  body.dataset.raw = text;
  if (body.dataset.pending) return;
  body.dataset.pending = "1";
  requestAnimationFrame(() => {
    delete body.dataset.pending;
    const list = el("transcript");
    const stick = list.scrollHeight - list.scrollTop - list.clientHeight < 80;
    body.classList.add("turn__text--md");
    body.innerHTML = renderMarkdown(body.dataset.raw ?? "");
    if (stick) list.scrollTop = list.scrollHeight;
  });
}

function addAssistantTurn(text: string, at?: string): HTMLElement {
  const body = addTurn(identity?.name ?? t("chat.yui"), "", "turn--assistant", at);
  if (text) renderAssistant(body, text);
  return body;
}

function clearTranscript(): void {
  const list = el("transcript");
  list.replaceChildren();
  const empty = document.createElement("li");
  empty.id = "transcript-empty";
  empty.className = "transcript__empty";
  empty.innerHTML = '<span class="transcript__glyph" aria-hidden="true"><svg><use href="#i-sparkle"/></svg></span>'
    + `<strong data-i18n="chat.newTitle">${t("chat.newTitle")}</strong><span data-i18n="chat.newText">${t("chat.newText")}</span>`;
  list.append(empty);
  streamingTurn = undefined;
}

async function loadHistory(currentClient: CoreClient, session: string): Promise<void> {
  try {
    const turns = await currentClient.turns(session, 60);
    if (client !== currentClient || sessionId !== session) return;
    clearTranscript();
    for (const turn of turns.slice().reverse()) {
      if (turn.role === "user") addTurn(t("chat.you"), turn.text, "turn--user", turn.started_at);
      else if (turn.role === "assistant" && turn.text) addAssistantTurn(turn.text, turn.completed_at || turn.started_at);
    }
  } catch { /* history is a convenience; the live session still works */ }
}

function addMemory(payload: Record<string, unknown>): void {
  const list = el<HTMLUListElement>("memory-list");
  const id = String(payload.id ?? "");
  const item = Array.from(list.children).find(child => (child as HTMLLIElement).dataset.id === id) as HTMLLIElement | undefined
    ?? document.createElement("li");
  if (id) item.dataset.id = id;
  item.dataset.status = String(payload.status ?? "");
  item.dataset.pinned = String(payload.pinned === true);
  item.replaceChildren();
  const meta = document.createElement("div");
  meta.className = "memory__meta";
  meta.textContent = `${payload.category} · ${payload.status}${payload.pinned ? ` · ${t("memory.pinned")}` : ""}`;
  const text = document.createElement("div");
  text.textContent = String(payload.content ?? "");
  item.append(meta, text);
  if (id) item.append(memoryActions(id, payload));
  list.prepend(item);
  el("memory-empty").classList.toggle("pane--hidden", list.children.length > 0);
}

function memoryActions(id: string, payload: Record<string, unknown>): HTMLElement {
  const actions = document.createElement("div");
  actions.className = "memory__actions";
  const act = (task: (current: CoreClient) => Promise<unknown>) => () => {
    const current = client;
    if (!current) return;
    void task(current).then(() => refreshMemory()).catch(error => {
      el("memory-empty").textContent = t("memory.changeFailed", { error: String(error) });
      el("memory-empty").classList.remove("pane--hidden");
    });
  };
  if (payload.status === "needs_confirmation") {
    actions.append(iconButton("check", t("memory.confirm"), act(current => current.confirmMemory(id, true))));
  }
  const pinned = payload.pinned === true;
  actions.append(
    iconButton("pin", pinned ? t("memory.unpin") : t("memory.pin"), act(current => current.pinMemory(id, !pinned))),
    iconButton("trash", t("memory.delete"), act(current => current.deleteMemory(id))),
  );
  return actions;
}

function addLedgerEntry(provider: string, included: string[], excluded: string[], remote?: boolean, id = "", result = ""): void {
  const list = el<HTMLUListElement>("ledger-list");
  if (id && Array.from(list.children).some(child => (child as HTMLLIElement).dataset.id === id)) return;
  const item = document.createElement("li");
  if (id) item.dataset.id = id;
  item.dataset.remote = String(remote);
  const meta = document.createElement("div");
  meta.className = "ledger__meta";
  meta.textContent = `${provider} · ${remote === undefined ? t("ledger.unknownType") : remote ? t("ledger.remote") : t("ledger.local")}${result ? ` · ${result}` : ""}`;
  const sent = document.createElement("div");
  sent.textContent = `${result === "error" ? t("ledger.attempt") : remote === undefined ? t("ledger.categories") : remote ? t("ledger.sentOut") : t("ledger.sentLocal")}: ${included.join(", ") || t("ledger.none")}`;
  item.append(meta, sent);
  if (excluded.length > 0) {
    const held = document.createElement("div");
    held.className = "memory__meta";
    held.textContent = t("ledger.excluded", { list: excluded.join(", ") });
    item.append(held);
  }
  list.prepend(item);
  el("ledger-empty").classList.add("pane--hidden");
}

async function refreshMemory(): Promise<void> {
  const currentClient = client;
  const currentIdentity = identityId;
  if (!currentClient || !currentIdentity) return;
  try {
    const items = await currentClient.memories(currentIdentity);
    if (client !== currentClient || identityId !== currentIdentity) return;
    const list = el<HTMLUListElement>("memory-list");
    const known = new Set(items.map(item => item.id));
    for (const child of Array.from(list.children)) {
      if (!known.has((child as HTMLLIElement).dataset.id ?? "")) child.remove();
    }
    for (const item of items.reverse()) addMemory(item as unknown as Record<string, unknown>);
    const empty = el("memory-empty");
    empty.textContent = t("memory.empty");
    empty.classList.toggle("pane--hidden", list.children.length > 0);
  } catch (error) {
    if (client !== currentClient) return;
    const empty = el("memory-empty");
    empty.textContent = t("memory.loadFailed", { error: String(error) });
    empty.classList.remove("pane--hidden");
  }
}

async function loadLedger(): Promise<void> {
  const currentClient = client;
  const currentIdentity = identityId;
  if (!currentClient || !currentIdentity) return;
  try {
    const records = await currentClient.audit(currentIdentity);
    if (client !== currentClient || identityId !== currentIdentity) return;
    for (const record of records.reverse()) {
      if (record.action !== "provider.call" || record.reason !== "dialog_turn") continue;
      const local = providerLocal.get(record.provider ?? "");
      const remote = local === undefined ? undefined : !local;
      addLedgerEntry(record.provider ?? t("ledger.model"), record.categories ?? [], [], remote, record.id, record.result);
    }
    ledgerLoaded = true;
    if (el("ledger-list").children.length === 0) {
      el("ledger-empty").textContent = t("privacy.noCalls");
    }
  } catch (error) {
    if (client !== currentClient) return;
    el("ledger-empty").textContent = t("ledger.loadFailed", { error: String(error) });
  }
}

type ConsentRequest = { id: string; category: string; provider?: string };
const consentQueue: ConsentRequest[] = [];

/** Keep every category visible until its answer has reached the core. */
function askConsent(pending: ConsentRequest): void {
  if (consentQueue.some(item => item.id === pending.id)) return;
  consentQueue.push(pending);
  if (consentQueue.length === 1) showConsent();
}

function showConsent(): void {
  const pending = consentQueue[0];
  if (!pending) { el("consent").classList.add("consent--hidden"); return; }
  const currentClient = client;
  el("consent-text").textContent = t("consent.text", { provider: pending.provider ?? t("consent.unknown"), category: pending.category });
  el("consent").classList.remove("consent--hidden");

  const buttons = ["consent-deny", "consent-allow", "consent-always"].map(id => el<HTMLButtonElement>(id));
  buttons.forEach(button => { button.disabled = false; });
  const answer = async (allow: boolean, remember: boolean) => {
    if (!currentClient || currentClient !== client) return;
    buttons.forEach(button => { button.disabled = true; });
    try {
      await currentClient.resolvePermission(pending.id, allow, remember);
      if (client !== currentClient) return;
      consentQueue.shift();
      showConsent();
    } catch (error) {
      if (client !== currentClient) return;
      el("consent-text").textContent = t("consent.failed", { error: String(error) });
      buttons.forEach(button => { button.disabled = false; });
    }
  };
  el("consent-deny").onclick = () => void answer(false, true);
  el("consent-allow").onclick = () => void answer(true, false);
  el("consent-always").onclick = () => void answer(true, true);
}

const toolCards = new Map<string, () => void>();
const completedTools = new Set<string>();

function showToolResult(id: string, text: string): void {
  toolCards.get(id)?.();
  if (id && completedTools.has(id)) return;
  if (id) completedTools.add(id);
  const body = addTurn(t("chat.action"), "");
  const urls = /https?:\/\/[^\s<>]+/g;
  let from = 0;
  for (const match of text.matchAll(urls)) {
    const index = match.index ?? 0;
    body.append(document.createTextNode(text.slice(from, index)));
    try {
      const url = new URL(match[0]);
      const link = document.createElement("a");
      link.href = url.href;
      link.target = "_blank";
      link.rel = "noopener noreferrer";
      link.textContent = match[0];
      body.append(link);
    } catch {
      body.append(document.createTextNode(match[0]));
    }
    from = index + match[0].length;
  }
  body.append(document.createTextNode(text.slice(from)));
}

function askTool(pending: PendingTool): void {
  const id = pending.invocation_id;
  if (!client || !id || toolCards.has(id) || completedTools.has(id)) return;
  const currentClient = client;
  const currentSession = sessionId;
  const text = addTurn(t("tool.title"), pending.human_readable_action || pending.tool);
  const actions = document.createElement("div");
  actions.className = "turn__actions";
  const approve = document.createElement("button");
  const reject = document.createElement("button");
  approve.className = "button button--warn button--small";
  reject.className = "button button--small";
  approve.textContent = t("tool.approve");
  reject.textContent = t("tool.reject");
  const strongFactor = pending.required_method === "pin" || pending.required_method === "biometric";
  approve.disabled = strongFactor;
  if (strongFactor) text.textContent += `\n${t("tool.strong")}`;
  actions.append(approve, reject);
  text.after(actions);
  let finished = false;
  const finish = () => { finished = true; approve.disabled = true; reject.disabled = true; clearTimeout(timer); };
  const expiry = Date.parse(pending.expires_at);
  const expire = () => {
    finish();
    text.textContent += `\n${t("tool.expired")}`;
  };
  const remaining = () => Number.isFinite(expiry) ? Math.max(0, expiry - Date.now()) : 120000;
  let timer = window.setTimeout(expire, remaining());
  toolCards.set(id, finish);
  const answer = async (approved: boolean) => {
    if (finished || client !== currentClient || sessionId !== currentSession) return;
    if (approved && strongFactor) return;
    approve.disabled = true; reject.disabled = true;
    clearTimeout(timer);
    try {
      const result = await currentClient.confirmTool(id, approved, currentSession);
      if (client !== currentClient || sessionId !== currentSession) return;
      finish();
      showToolResult(id, result.result);
    } catch (error) {
      if (client !== currentClient || sessionId !== currentSession) return;
      addTurn(t("chat.confirmError"), String(error), "turn--error");
      if (!finished) {
        approve.disabled = strongFactor; reject.disabled = false;
        timer = window.setTimeout(expire, remaining());
      }
    }
  };
  approve.onclick = () => void answer(true);
  reject.onclick = () => void answer(false);
}

function clearRequests(): void {
  consentQueue.length = 0;
  el("consent").classList.add("consent--hidden");
  for (const finish of toolCards.values()) finish();
  toolCards.clear();
  completedTools.clear();
}

function stopSpeech(): void {
  stage.stopSpeech();
}

function formatPercent(value: number): string {
  return Number.isFinite(value) && value > 0 ? `${value.toFixed(0)}%` : "—";
}

function renderInferenceDecision(decision?: InferenceDecision): void {
  if (!decision || decision.kind !== "llm") return;
  if (latestLLMDecision?.at && decision.at && Date.parse(decision.at) < Date.parse(latestLLMDecision.at)) return;
  latestLLMDecision = decision;
  el("runtime-model").textContent = decision.model_family || decision.model || decision.provider_id;
  el("runtime-backend").textContent = decision.backend || "auto";
  el("runtime-reason").textContent = decision.reason || t("runtime.noReason");
  el("bar-runtime").textContent = `${decision.model_family || decision.model || decision.provider_id} · ${decision.backend || "auto"}`;
  el("bar-llm").textContent = decision.provider_id;
}

function renderInference(status: InferenceStatus): void {
  setInferenceControlsEnabled(true);
  lastInferenceStatus = status;
  modelSettings?.renderStatus(status);
  const prefs = status.preferences;
  updatePrivacyTag(prefs);
  el<HTMLSelectElement>("inference-mode").value = prefs.mode;
  el<HTMLInputElement>("inference-quality").value = String(prefs.minimum_quality);
  el<HTMLInputElement>("inference-downgrade").checked = prefs.allow_auto_downgrade;

  const modelSelect = el<HTMLSelectElement>("inference-model");
  const families = Array.from(new Set(
    status.models
      .filter((m) => m.kind === "llm" && (m.model_family || m.model))
      .map((m) => m.model_family || m.model || ""),
  )).sort();
  const signature = families.join("|");
  if (modelSelect.dataset.signature !== signature) {
    modelSelect.replaceChildren();
    const auto = document.createElement("option");
    auto.value = "";
    auto.textContent = t("runtime.autoPick");
    modelSelect.append(auto);
    for (const family of families) {
      const option = document.createElement("option");
      option.value = family;
      const variants = status.models.filter((m) => m.kind === "llm" && (m.model_family || m.model) === family);
      const backends = Array.from(new Set(variants.map((m) => m.backend || "auto"))).join("/");
      option.textContent = `${family} · ${variants.some((m) => m.local === false) ? t("runtime.remoteOption") : backends}`;
      modelSelect.append(option);
    }
    modelSelect.dataset.signature = signature;
  }
  modelSelect.value = prefs.locked_model || "";

  const tm = status.telemetry;
  el("runtime-cpu").textContent = formatPercent(tm.cpu_percent);
  el("runtime-gpu").textContent = formatPercent(tm.gpu_percent);
  el("runtime-ram").textContent = tm.ram_free_mb > 0 ? t("runtime.free", { mb: tm.ram_free_mb }) : "—";
  el("runtime-vram").textContent = tm.vram_free_mb > 0 ? t("runtime.free", { mb: tm.vram_free_mb }) : "—";
  el("runtime-game").textContent = tm.game_active ? (tm.foreground_process || t("runtime.active")) : t("runtime.noGame");
  el("runtime-fps").textContent = tm.fps && tm.fps > 0 ? tm.fps.toFixed(0) : "—";
  const lastLLM = status.last_llm_decision ?? (status.last_decision?.kind === "llm" ? status.last_decision : undefined) ?? latestLLMDecision;
  if (lastLLM) {
    renderInferenceDecision(lastLLM);
    const active = latestLLMDecision ?? lastLLM;
    if (prefs.locked_model && prefs.locked_model !== (active.model_family || active.model)) {
      el("runtime-reason").textContent = t("runtime.pendingSwitch", { locked: prefs.locked_model, active: active.model_family || active.model || active.provider_id });
    }
  } else {
    el("runtime-model").textContent = t("runtime.notCalled");
    el("runtime-backend").textContent = "—";
    el("runtime-reason").textContent = prefs.locked_model
      ? t("runtime.lockedPending", { locked: prefs.locked_model })
      : t("runtime.firstRequest");
    el("bar-runtime").textContent = t("runtime.waiting");
  }
}

async function refreshInference(): Promise<void> {
  const currentClient = client;
  if (!currentClient) {
    setInferenceControlsEnabled(false);
    return;
  }
  try {
    const status = await currentClient.inferenceStatus();
    if (client !== currentClient || !sessionId) return;
    renderInference(status);
  } catch {
    if (client !== currentClient || !sessionId) return;
    setInferenceControlsEnabled(false);
    el("bar-runtime").textContent = t("runtime.off");
    el("runtime-reason").textContent = t("runtime.statusFailed");
  }
}

async function saveInferencePreferences(): Promise<void> {
  if (!client || !inferenceAvailable) {
    el("runtime-reason").textContent = t("runtime.connectFirst");
    return;
  }
  const mode = el<HTMLSelectElement>("inference-mode").value as InferenceStatus["preferences"]["mode"];
  const modelSelect = el<HTMLSelectElement>("inference-model");
  let lockedModel = modelSelect.value;
  if (mode === "manual" && !lockedModel) {
    lockedModel = lastInferenceStatus?.last_llm_decision?.model_family
      || (lastInferenceStatus?.last_decision?.kind === "llm" ? lastInferenceStatus.last_decision.model_family : "")
      || lastInferenceStatus?.models.find((m) => m.kind === "llm")?.model_family
      || "";
    modelSelect.value = lockedModel;
  }
  const minimumQuality = Math.max(1, Math.min(100, Number(el<HTMLInputElement>("inference-quality").value) || 55));
  const preferred = mode === "manual" ? lastInferenceStatus?.preferences?.preferred_provider : undefined;
  const selectedPreferred = lastInferenceStatus?.models.find((m) =>
    m.kind === "llm" && m.provider_id === preferred && (m.model_family || m.model) === lockedModel);
  const selectedRemote = lastInferenceStatus?.models.find((m) =>
    m.kind === "llm" && m.local === false && (m.model_family || m.model) === lockedModel);
  try {
    const status = await client.updateInferencePreferences({
      mode: selectedRemote ? "manual" : mode,
      preferred_provider: selectedPreferred?.provider_id || selectedRemote?.provider_id || "",
      locked_model: lockedModel,
      allow_auto_downgrade: el<HTMLInputElement>("inference-downgrade").checked,
      minimum_quality: minimumQuality,
    });
    renderInference(status);
  } catch (error) {
    el("runtime-reason").textContent = String(error);
  }
}

function isUnauthorized(error: unknown): boolean {
  return error instanceof Error && /\b401\b/.test(error.message);
}

function handleFrame(frame: Frame): void {
  const payload = (frame.payload ?? {}) as Record<string, unknown>;
  switch (frame.type) {
    case "session.state":
      setState(String(payload.state ?? "idle"));
      break;

    case "turn.delta": {
      streamingTurn ??= addAssistantTurn("");
      renderAssistant(streamingTurn, (streamingTurn.dataset.raw ?? "") + String(payload.text ?? ""));
      break;
    }

    case "turn.done": {
      // The final text has reaction tags removed; prefer it over the stream.
      const final = String(payload.text ?? "");
      if (!streamingTurn) { if (final) addAssistantTurn(final); }
      else if (final) renderAssistant(streamingTurn, final);
      streamingTurn = undefined;
      // Playback owns mouth state: queued speech can outlive turn.done.
      break;
    }

    case "transcript.final":
      addTurn(t("chat.you"), String(payload.text ?? ""), "turn--user");
      break;

    case "tts.chunk":
      stage.playChunk(String(payload.audio_b64 ?? ""), Number(payload.sample_rate ?? 16000));
      break;

    case "avatar.expression": {
      const label = String(payload.label ?? "neutral");
      const chip = el("emotion-label");
      chip.textContent = emotionLabel(label);
      chip.hidden = !chip.textContent;
      stage.react(label, String(payload.expression ?? "exp_neutral"));
      break;
    }

    case "memory.indicator":
      addMemory(payload);
      break;

    case "data.manifest":
      addLedgerEntry(String(payload.provider ?? t("ledger.model")),
        Array.isArray(payload.included_categories) ? payload.included_categories.map(String) : [],
        Array.isArray(payload.excluded_categories) ? payload.excluded_categories.map(String) : [],
        payload.remote === true);
      break;

    case "permission.request":
      askConsent(payload as { id: string; category: string; provider?: string });
      break;

    case "tool.confirm":
      askTool(payload as unknown as PendingTool);
      break;

    case "tool.result":
      showToolResult(String(payload.invocation_id ?? ""), String(payload.result ?? ""));
      break;

    case "proactive.reminder":
      addTurn(t("chat.reminder"), String(payload.text ?? t("chat.timer")));
      break;

    case "barge_in":
      stopSpeech();
      streamingTurn = undefined;
      break;

    case "inference.decision":
      renderInferenceDecision(payload as unknown as InferenceDecision);
      if (lastInferenceStatus) {
        lastInferenceStatus.last_decision = payload as unknown as InferenceDecision;
        if (payload.kind === "llm") lastInferenceStatus.last_llm_decision = payload as unknown as InferenceDecision;
      }
      break;

    case "error":
      addTurn(t("chat.error"), `${payload.stage}: ${payload.error}`, "turn--error");
      setState("error");
      break;
  }
}

function renderProviders(providers: ProviderStatus[]): void {
  providerLocal = new Map(providers.map(status => [status.info.id, status.info.local]));
  remoteDefaultProvider = false;
  for (const status of providers) {
    if (!status.is_default) continue;
    const target = { llm: "bar-llm", stt: "bar-stt", tts: "bar-tts" }[status.info.kind];
    if (target) {
      el(target).textContent = `${status.info.id}${status.info.local ? "" : " ↗"}`;
    }
    if (!status.info.local) remoteDefaultProvider = true;
  }
  updatePrivacyTag(lastInferenceStatus?.preferences);
}

function setIdentity(next: Identity | undefined): void {
  identity = next;
  // Only called from connect(), after settings exist.
  settings.refreshCharacter();
  el("identity-name").textContent = next?.name ?? "Yui";
  document.title = next?.name ? `${next.name} · Yui` : "Yui";
}

/** Joins an explicit session, a sibling window's session, the last one, or starts fresh. */
async function pickSession(currentClient: CoreClient, explicit: string, identityHint: string): Promise<string> {
  const candidates = [explicit, view === "main" ? "" : await hub.discover(), localStorage.getItem("yui.session") ?? ""];
  for (const candidate of candidates) {
    if (!candidate) continue;
    try {
      const existing = await currentClient.session(candidate);
      if (!existing.closed_at && existing.state !== "closed" && (!identityHint || existing.identity_id === identityHint)) {
        return existing.id;
      }
    } catch { /* gone or not ours: try the next one */ }
  }
  const session = await currentClient.startSession(identityHint || undefined);
  return session.id;
}

async function connect(explicitSession = query.get("session") ?? "", automatic = false): Promise<void> {
  if (reconnectTimer !== undefined) { window.clearTimeout(reconnectTimer); reconnectTimer = undefined; }
  let { base, token } = readConnection();
  if (!token) {
    const shell = await tauriConnection();
    if (shell) ({ base, token } = shell);
  }
  if (!token) {
    await showPairing();
    return;
  }
  try { localStorage.setItem("yui.core", base); localStorage.setItem("yui.token", token); } catch { /* private mode */ }

  if (location.hash || query.has("token")) {
    const clean = new URLSearchParams(location.search);
    clean.delete("token");
    history.replaceState(null, "", `${location.pathname}${clean.size ? `?${clean}` : ""}`);
  }
  client?.close();
  clearRequests();
  sessionId = "";
  identityId = "";
  lastInferenceStatus = undefined;
  latestLLMDecision = undefined;
  ledgerLoaded = false;
  el("memory-list").replaceChildren();
  el("ledger-list").replaceChildren();
  modelSettings?.setConnected(false);
  el("memory-empty").classList.remove("pane--hidden");
  el("ledger-empty").classList.remove("pane--hidden");
  setSensorsEnabled(false);
  microphone.stop(false);
  if (inferenceTimer !== undefined) window.clearInterval(inferenceTimer);
  el("connect").textContent = t("app.connecting");

  client = new CoreClient(base, token);
  const currentClient = client;
  try {
    const status = await currentClient.status();
    if (client !== currentClient) return;
    renderProviders(status.providers);
    coreVersion = status.version;
    el("bar-core").textContent = t("conn.connected", { version: status.version });

    const identities = await currentClient.identities();
    if (client !== currentClient) return;
    setIdentity(identities[0]);
    identityId = identities[0]?.id ?? "";

    const chosen = await pickSession(currentClient, explicitSession, identityId);
    if (client !== currentClient) return;
    sessionId = chosen;
    const session = await currentClient.session(chosen);
    if (client !== currentClient) return;
    identityId = session.identity_id;
    if (identityId !== identity?.id) setIdentity(identities.find(item => item.id === identityId) ?? identity);
    if (view === "main") { try { localStorage.setItem("yui.session", sessionId); } catch { /* ignore */ } }
    hub.setSession(sessionId);
    currentClient.connect(sessionId, handleFrame, () => {
      if (client !== currentClient) return;
      microphone.stop(false);
      stopSpeech();
      clearRequests();
      if (inferenceTimer !== undefined) window.clearInterval(inferenceTimer);
      sessionId = "";
      setInferenceControlsEnabled(false);
      setSensorsEnabled(false);
      modelSettings?.setConnected(false);
      el("bar-core").textContent = t("conn.lost");
      el("connect").textContent = t("app.connect");
      el("connect").hidden = false;
      setState("error");
      scheduleReconnect();
    });
    await loadHistory(currentClient, sessionId);
    if (client !== currentClient) return;
    setState(session.state === "closed" ? "idle" : session.state || "idle");
    setSensorsEnabled(true);
    el("connect").hidden = true;
    reconnectDelay = 2000;
    modelSettings?.setConnected(true);
    void refreshMemory();
    void loadLedger();
    void currentClient.pendingTools().then(pending => {
      if (client === currentClient && sessionId) pending.forEach(askTool);
    }).catch(error => {
      if (client === currentClient && sessionId) addTurn(t("chat.confirmationsError"), String(error), "turn--error");
    });
    await refreshInference();
    await modelSettings?.refresh();
    if (client !== currentClient || !sessionId) return;
    if (inferenceTimer !== undefined) window.clearInterval(inferenceTimer);
    // Telemetry only matters while someone can see it.
    inferenceTimer = window.setInterval(() => { if (!document.hidden) void refreshInference(); }, 3000);
  } catch (error) {
    if (client !== currentClient) return;
    currentClient.close();
    sessionId = "";
    setInferenceControlsEnabled(false);
    setSensorsEnabled(false);
    modelSettings?.setConnected(false);
    el("connect").textContent = t("app.connect");
    el("connect").hidden = false;
    // Silent retries while the core is down; one visible error otherwise.
    if (!automatic) addTurn(t("chat.error"), String(error), "turn--error");
    if (isUnauthorized(error)) {
      // A paired device token can be revoked or replaced by a fresh core
      // launch. Do not keep retrying it on every click.
      localStorage.removeItem("yui.token");
      el("bar-core").textContent = t("conn.tokenExpired");
      el("runtime-reason").textContent = t("conn.tokenRejected");
    } else {
      el("bar-core").textContent = t("conn.disconnected");
      if (automatic) scheduleReconnect();
    }
  }
}

async function newConversation(): Promise<void> {
  const current = client;
  if (!current || !sessionId) return;
  const old = sessionId;
  try { await current.closeSession(old); } catch { /* a closed session is fine */ }
  try { localStorage.removeItem("yui.session"); } catch { /* ignore */ }
  clearTranscript();
  const fresh = await current.startSession(identityId || undefined);
  if (client !== current) return;
  await connect(fresh.id);
}

modelSettings = initModelSettings({
  getClient: () => client,
  getInference: () => lastInferenceStatus,
  onInference: renderInference,
  onProviders: renderProviders,
});

const settings = initSettings({
  client: () => client,
  identity: () => identity,
  onIdentity: next => setIdentity(next),
  onTab: tab => {
    if (tab === "memory") void refreshMemory();
    if (tab === "ledger" && !ledgerLoaded) void loadLedger();
    if (tab === "models") void modelSettings?.refresh();
    if (tab === "inference") void refreshInference();
  },
  previewVoice: () => stage.preview(),
  lipEngine: () => stage.engineLabel,
  detachAvatar: () => void detach("avatar"),
  reconnect: base => {
    if (base) { try { localStorage.setItem("yui.core", base); } catch { /* ignore */ } }
    void connect(sessionId);
  },
  coreBase: () => readConnection().base,
});

async function detach(role: "avatar" | "chat"): Promise<void> {
  try {
    await openDetached(role, sessionId, prefs().avatarOnTop);
  } catch (error) {
    addTurn(t("app.window"), String(error instanceof Error ? error.message : error), "turn--error");
  }
}

el("connect").addEventListener("click", () => void connect());
el<HTMLFormElement>("memory-add").addEventListener("submit", event => {
  event.preventDefault();
  const input = el<HTMLInputElement>("memory-add-text");
  const text = input.value.trim();
  const current = client;
  if (!text || !current || !identityId) return;
  input.disabled = true;
  void current.addMemory(identityId, text).then(item => {
    input.value = "";
    addMemory(item as unknown as Record<string, unknown>);
  }).catch(error => {
    el("memory-empty").textContent = t("memory.saveFailed", { error: String(error) });
    el("memory-empty").classList.remove("pane--hidden");
  }).finally(() => { input.disabled = !sessionId; });
});
el("open-settings").addEventListener("click", () => settings.open());
el("detach-avatar").addEventListener("click", () => void detach("avatar"));
el("detach-chat").addEventListener("click", () => void detach("chat"));
el("attach-avatar").addEventListener("click", () => void closeDetached(hub, "avatar"));
el("attach-chat").addEventListener("click", () => void closeDetached(hub, "chat"));
el<HTMLButtonElement>("new-chat").addEventListener("click", () => {
  if (confirm(t("app.newChatConfirm"))) void newConversation();
});

const composerInput = el<HTMLTextAreaElement>("composer-input");
function autosize(): void {
  composerInput.style.height = "auto";
  composerInput.style.height = `${Math.min(180, composerInput.scrollHeight)}px`;
}
composerInput.addEventListener("input", autosize);
composerInput.addEventListener("keydown", event => {
  if (event.key === "Enter" && !event.shiftKey && !event.isComposing && prefs().sendOnEnter) {
    event.preventDefault();
    el<HTMLFormElement>("composer").requestSubmit();
  }
});

el<HTMLFormElement>("composer").addEventListener("submit", (event) => {
  event.preventDefault();
  const text = composerInput.value.trim();
  if (!text || !sessionId) return;
  try {
    void stage.speech.resume();
    client?.sendText(text);
    addTurn(t("chat.you"), text, "turn--user");
    hub.shareUserText(text);
    composerInput.value = "";
    autosize();
  } catch (error) { addTurn(t("chat.error"), String(error), "turn--error"); }
});

setInferenceControlsEnabled(false);
for (const id of INFERENCE_CONTROL_IDS) {
  el(id).addEventListener("change", () => void saveInferencePreferences());
}

document.addEventListener("keydown", (event) => {
  if (event.key !== "Escape" || !sessionId) return;
  if (document.querySelector("dialog[open]")) return; // Esc closes the dialog instead
  try { client?.cancelTurn(); } catch { /* socket closed */ }
  stopSpeech();
});

// Exported for the ledger view, which the core will populate once turn
// manifests are streamed on the control plane.
export { addLedgerEntry };

function setSensorsEnabled(enabled: boolean): void {
  for (const id of ["microphone", "stop-turn", "camera", "send-message", "new-chat", "memory-add-text", "memory-add-submit"]) {
    el<HTMLButtonElement>(id).disabled = !enabled;
  }
}

let pairingCode = "";
async function renderPairingQr(): Promise<void> {
  const canvas = el<HTMLCanvasElement>("pair-qr");
  const help = el("pair-qr-help");
  canvas.hidden = true;
  if (!pairingCode) return;
  const raw = el<HTMLInputElement>("pair-host").value.trim();
  if (!raw) {
    help.textContent = t("pair.needHost");
    return;
  }
  try {
    const url = new URL(raw);
    if (url.protocol !== "https:" || !url.hostname ||
        ["0.0.0.0", "::", "localhost", "127.0.0.1"].includes(url.hostname) ||
        url.username || url.password || url.pathname !== "/" || url.search || url.hash) {
      throw new Error(t("pair.badHost"));
    }
    await QRCode.toCanvas(canvas, JSON.stringify({v: 1, host: url.origin, code: pairingCode, name: "Yui Core"}),
      {width: 220, margin: 2});
    canvas.hidden = false;
    help.textContent = t("pair.scan");
    localStorage.setItem("yui.pair-host", url.origin);
  } catch (error) {
    help.textContent = String(error);
  }
}

async function showPairing(): Promise<void> {
  const dialog = el<HTMLDialogElement>("pair-dialog");
  const local = ["localhost", "127.0.0.1", "[::1]"].includes(location.hostname) || isTauri();
  el("pair-error").textContent = "";
  el("pair-code").textContent = "";
  pairingCode = "";
  el("pair-host-label").hidden = !local;
  el("pair-qr-help").hidden = !local;
  el<HTMLCanvasElement>("pair-qr").hidden = true;
  el<HTMLInputElement>("pair-host").value = localStorage.getItem("yui.pair-host") ?? "";
  el("pair-input-label").hidden = local;
  el<HTMLInputElement>("pair-input").disabled = local;
  el("pair-submit").hidden = local;
  dialog.showModal();
  if (local) {
    try {
      const { base, token } = readConnection();
      if (!token) throw new Error(t("pair.needToken"));
      const result = await new CoreClient(base, token).request<{code: string; expires_at: string}>("/v1/pair/start", {method:"POST", body:"{}"});
      pairingCode = result.code;
      el("pair-code").textContent = result.code;
      el("pair-help").textContent = t("pair.codeValid", { time: new Date(result.expires_at).toLocaleString() });
      await renderPairingQr();
    } catch (error) { el("pair-error").textContent = String(error); }
  }
}
el("pair-device").onclick = () => void showPairing();
el<HTMLInputElement>("pair-host").oninput = () => void renderPairingQr();
el("pair-close").onclick = () => el<HTMLDialogElement>("pair-dialog").close();
el<HTMLFormElement>("pair-form").onsubmit = async (event) => {
  event.preventDefault();
  const button = el<HTMLButtonElement>("pair-submit");
  button.disabled = true;
  try {
    const response = await fetch(`${readConnection().base}/v1/pair/claim`, {
      method: "POST", headers: {"Content-Type":"application/json"},
      body: JSON.stringify({code: el<HTMLInputElement>("pair-input").value.trim(), name: t("pair.deviceName"), kind:"web", capabilities:["text","audio","camera"]}),
    });
    if (!response.ok) {
      if (response.status === 403) {
        throw new Error(t("pair.badCode"));
      }
      const detail = await response.text();
      throw new Error(t("pair.failed", { detail: detail || t("pair.errorCode", { status: response.status }) }));
    }
    const result = await response.json() as {token:string};
    localStorage.setItem("yui.token", result.token);
    el<HTMLDialogElement>("pair-dialog").close();
    await connect();
  } catch (error) { el("pair-error").textContent = String(error); }
  finally { button.disabled = false; }
};

const micButton = el<HTMLButtonElement>("microphone");
const microphone = new Microphone(
  () => client,
  {
    status: (text, active) => {
      el("sensor-state").textContent = text;
      micButton.classList.toggle("icon-button--live", active);
      micButton.setAttribute("aria-pressed", String(active));
      micButton.title = active ? t("chat.micOff") : t("chat.speak");
      micButton.setAttribute("aria-label", micButton.title);
      micActive = active;
      document.body.classList.toggle("mic-on", active);
    },
    level: value => stage.setMicLevel(value),
    error: message => addTurn(t("chat.micError"), message, "turn--error"),
    speechStart: () => { void stage.speech.resume(); },
  },
  () => ({
    sensitivity: prefs().vadSensitivity, bargeIn: prefs().bargeIn, engine: prefs().vadEngine,
    isAssistantSpeaking: () => assistantSpeaking || stage.speech.speaking,
  }),
);

async function startMicrophone(): Promise<void> {
  await stage.speech.resume();
  await microphone.start(prefs().micMode);
}

micButton.onclick = () => {
  if (microphone.active) microphone.stop(prefs().micMode === "push");
  else void startMicrophone();
};
onPrefs((_, changed) => {
  if ((changed.includes("micMode") || changed.includes("vadEngine")) && microphone.active) {
    microphone.stop(false);
    void startMicrophone();
  }
  if (changed.includes("showRibbon")) el("presence").hidden = !prefs().showRibbon;
});
el("stop-turn").onclick = () => {
  if (prefs().micMode === "push") microphone.stop(false);
  try { client?.cancelTurn(); } catch { /* socket closed */ }
  stopSpeech();
};
el<HTMLInputElement>("camera").onchange = async (event) => {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  if (!file || !client || !sessionId) return;
  const currentClient = client;
  const currentSession = sessionId;
  let bitmap: ImageBitmap | undefined;
  let objectUrl: string | undefined;
  const progress = addTurn(t("photo.title"), t("photo.preparing"));
  try {
    let image: HTMLImageElement | undefined;
    if (typeof createImageBitmap === "function") {
      try { bitmap = await createImageBitmap(file); } catch { /* Use the iOS-compatible image element fallback. */ }
    }
    if (!bitmap) {
      objectUrl = URL.createObjectURL(file);
      image = new Image();
      await new Promise<void>((resolve, reject) => {
        image!.onload = () => resolve();
        image!.onerror = () => reject(new Error(t("photo.unreadable")));
        image!.src = objectUrl!;
      });
    }
    const source = bitmap ?? image!;
    const width = bitmap?.width ?? image!.naturalWidth;
    const height = bitmap?.height ?? image!.naturalHeight;
    const scale = Math.min(1, 1280 / Math.max(width, height));
    const canvas = document.createElement("canvas");
    canvas.width = Math.max(1, Math.round(width * scale)); canvas.height = Math.max(1, Math.round(height * scale));
    const context = canvas.getContext("2d");
    if (!context) throw new Error(t("photo.unsupported"));
    context.drawImage(source, 0, 0, canvas.width, canvas.height);
    if (client !== currentClient || sessionId !== currentSession) { progress.textContent = t("photo.cancelled"); return; }
    progress.textContent = t("photo.sending");
    const result = await currentClient.request<{description:string}>(`/v1/sessions/${currentSession}/vision`, {
      method:"POST", body:JSON.stringify({image_b64:canvas.toDataURL("image/jpeg",0.8).split(",")[1], mime:"image/jpeg", question:t("photo.question")}),
    });
    progress.textContent = client === currentClient && sessionId === currentSession
      ? result.description || t("photo.empty")
      : t("photo.changed");
  } catch (error) { progress.textContent = t("photo.failed", { error: String(error) }); }
  finally {
    bitmap?.close();
    if (objectUrl) URL.revokeObjectURL(objectUrl);
    input.value = "";
  }
};
document.addEventListener("visibilitychange", () => {
  // Push-to-talk never records in the background; hands-free is an explicit choice.
  if (document.hidden && microphone.active && prefs().micMode === "push") microphone.stop(false);
});
/** Text that JavaScript owns (not data-i18n) follows the language too. */
function renderDynamicText(): void {
  el("state-label").textContent = currentState === "offline" ? t("state.offline") : stateLabel(currentState);
  updatePrivacyTag(lastInferenceStatus?.preferences);
  el("connect").textContent = t("app.connect");
  el("bar-core").textContent = coreVersion && sessionId ? t("conn.connected", { version: coreVersion }) : t("conn.disconnected");
  if (!micActive) el("sensor-state").textContent = t("mic.off");
  micButton.title = micActive ? t("chat.micOff") : t("chat.speak");
  micButton.setAttribute("aria-label", micButton.title);
  if (lastInferenceStatus) renderInference(lastInferenceStatus);
  else el("runtime-reason").textContent = t("runtime.none");
  el("figure-fallback").textContent = t("avatar.loading");
  el("memory-empty").textContent = t("memory.emptyNew");
  el("ledger-empty").textContent = t("privacy.noCalls");
  el("model-add-hint").textContent = t("addModel.hintLocal");
  el("pair-help").textContent = t("pair.help");
}
renderDynamicText();
document.body.classList.toggle("tauri", isTauri());
const syncTrayLanguage = () => void tauriInvoke("set_tray_language", { lang: document.documentElement.lang }).catch(() => undefined);
syncTrayLanguage();
// Ctrl+Shift+Space anywhere in the system (and the tray item) toggles the mic.
if (view === "main") void tauriListen("yui://ptt", () => micButton.click());
onLang(() => {
  renderDynamicText();
  syncTrayLanguage();
  if (sessionId) { void refreshMemory(); }
});
onPrefs((next, changed) => {
  if (changed.includes("lang")) setLang(next.lang === "auto" ? detectLang() : next.lang as "ru" | "en");
});
syncWindows();
void (async () => {
  if (readConnection().token || (await tauriConnection())) void connect();
})();
