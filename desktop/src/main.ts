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
import { renderMarkdown } from "./markdown";
import { Microphone } from "./mic";
import { initModelSettings } from "./model-settings";
import { applyAppearance, onPrefs, prefs } from "./prefs";
import { initSettings, iconButton } from "./settings";
import { Stage } from "./stage";
import { WindowHub, closeDetached, isTauri, openDetached, type WindowRole } from "./windows";
import QRCode from "qrcode";

const el = <T extends HTMLElement>(id: string): T => {
  const node = document.getElementById(id);
  if (!node) throw new Error(`missing element: ${id}`);
  return node as T;
};

const STATE_LABELS: Record<string, string> = {
  idle: "готова",
  listening: "слушает",
  thinking: "думает",
  speaking: "говорит",
  acting: "выполняет действие",
  error: "ошибка",
  closed: "сессия закрыта",
};

const EMOTION_LABELS: Record<string, string> = {
  joy: "радость", warm: "тепло", concern: "сочувствие", sad: "грусть", alert: "внимание",
  surprise: "удивление", think: "раздумье", calm: "спокойствие", angry: "недовольство",
};

const query = new URLSearchParams(location.search);
const view: WindowRole = query.get("view") === "chat" ? "chat" : "main";
document.body.classList.toggle("view-main", view === "main");
document.body.classList.toggle("view-chat", view === "chat");
applyAppearance();

const stage = new Stage({
  frame: el("stage-frame"),
  canvas: el<HTMLCanvasElement>("live2d"),
  fallback: el("figure-fallback"),
  ribbon: el<HTMLCanvasElement>("presence"),
  audioButton: el<HTMLButtonElement>("stage-audio"),
});
el("presence").hidden = !prefs().showRibbon;
const hub = new WindowHub(view, () => syncWindows());
hub.onUserText = (session, text) => { if (session === sessionId) addTurn("Вы", text); };
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
let reconnectTimer: number | undefined;
let reconnectDelay = 2000;

/** The core restarts (updates, crashes, sleep): rejoin the same session. */
function scheduleReconnect(): void {
  if (reconnectTimer !== undefined) return;
  const session = localStorage.getItem("yui.session") ?? "";
  el("bar-core").textContent = `переподключение через ${Math.round(reconnectDelay / 1000)} с`;
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
  tag.textContent = remoteLock ? "выбрана внешняя модель"
    : remoteDefaultProvider ? "разрешён внешний провайдер" : "только локально";
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

function setState(state: string): void {
  el("state-label").textContent = STATE_LABELS[state] ?? state;
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
  item.className = `turn ${kind || (who === "Вы" ? "turn--user" : "turn--notice")}`;
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
  const body = addTurn(identity?.name ?? "Юи", "", "turn--assistant", at);
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
    + "<strong>Новый разговор</strong><span>Напишите Юи или включите микрофон.</span>";
  list.append(empty);
  streamingTurn = undefined;
}

async function loadHistory(currentClient: CoreClient, session: string): Promise<void> {
  try {
    const turns = await currentClient.turns(session, 60);
    if (client !== currentClient || sessionId !== session) return;
    clearTranscript();
    for (const turn of turns.slice().reverse()) {
      if (turn.role === "user") addTurn("Вы", turn.text, "", turn.started_at);
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
  meta.textContent = `${payload.category} · ${payload.status}${payload.pinned ? " · закреплено" : ""}`;
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
      el("memory-empty").textContent = `Не удалось изменить память: ${String(error)}`;
      el("memory-empty").classList.remove("pane--hidden");
    });
  };
  if (payload.status === "needs_confirmation") {
    actions.append(iconButton("check", "Подтвердить", act(current => current.confirmMemory(id, true))));
  }
  const pinned = payload.pinned === true;
  actions.append(
    iconButton("pin", pinned ? "Открепить" : "Закрепить", act(current => current.pinMemory(id, !pinned))),
    iconButton("trash", "Удалить", act(current => current.deleteMemory(id))),
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
  meta.textContent = `${provider} · ${remote === undefined ? "тип провайдера неизвестен" : remote ? "внешний" : "локальный"}${result ? ` · ${result}` : ""}`;
  const sent = document.createElement("div");
  sent.textContent = `${result === "error" ? "Категории попытки вызова" : remote === undefined ? "Категории вызова" : remote ? "Передано вовне" : "Передано локально"}: ${included.join(", ") || "без категорий"}`;
  item.append(meta, sent);
  if (excluded.length > 0) {
    const held = document.createElement("div");
    held.className = "memory__meta";
    held.textContent = `исключено: ${excluded.join(", ")}`;
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
    empty.textContent = "Сохранённых воспоминаний пока нет.";
    empty.classList.toggle("pane--hidden", list.children.length > 0);
  } catch (error) {
    if (client !== currentClient) return;
    const empty = el("memory-empty");
    empty.textContent = `Не удалось загрузить память: ${String(error)}`;
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
      addLedgerEntry(record.provider ?? "модель", record.categories ?? [], [], remote, record.id, record.result);
    }
    ledgerLoaded = true;
    if (el("ledger-list").children.length === 0) {
      el("ledger-empty").textContent = "Вызовов модели пока нет.";
    }
  } catch (error) {
    if (client !== currentClient) return;
    el("ledger-empty").textContent = `Не удалось загрузить журнал: ${String(error)}`;
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
  el("consent-text").textContent =
    `Провайдер «${pending.provider ?? "неизвестный"}» запрашивает категорию «${pending.category}». ` +
    `Одноразовое разрешение действует для следующего запроса в течение пяти минут.`;
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
      el("consent-text").textContent = `Не удалось сохранить решение: ${String(error)}. Повторите попытку.`;
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
  const body = addTurn("Действие", "");
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
  const text = addTurn("Подтверждение действия", pending.human_readable_action || pending.tool);
  const actions = document.createElement("div");
  actions.className = "turn__actions";
  const approve = document.createElement("button");
  const reject = document.createElement("button");
  approve.className = "button button--warn button--small";
  reject.className = "button button--small";
  approve.textContent = "Подтвердить";
  reject.textContent = "Отклонить";
  const strongFactor = pending.required_method === "pin" || pending.required_method === "biometric";
  approve.disabled = strongFactor;
  if (strongFactor) text.textContent += "\nНужно подтверждение PIN или биометрией на устройстве с поддержкой этой проверки.";
  actions.append(approve, reject);
  text.after(actions);
  let finished = false;
  const finish = () => { finished = true; approve.disabled = true; reject.disabled = true; clearTimeout(timer); };
  const expiry = Date.parse(pending.expires_at);
  const expire = () => {
    finish();
    text.textContent += "\nСрок подтверждения истёк. Повторите запрос.";
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
      addTurn("Ошибка подтверждения", String(error), "turn--error");
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
  el("runtime-reason").textContent = decision.reason || "Выбрано без дополнительного пояснения.";
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
    auto.textContent = "Автовыбор";
    modelSelect.append(auto);
    for (const family of families) {
      const option = document.createElement("option");
      option.value = family;
      const variants = status.models.filter((m) => m.kind === "llm" && (m.model_family || m.model) === family);
      const backends = Array.from(new Set(variants.map((m) => m.backend || "auto"))).join("/");
      option.textContent = `${family} · ${variants.some((m) => m.local === false) ? "внешняя модель ↗" : backends}`;
      modelSelect.append(option);
    }
    modelSelect.dataset.signature = signature;
  }
  modelSelect.value = prefs.locked_model || "";

  const t = status.telemetry;
  el("runtime-cpu").textContent = formatPercent(t.cpu_percent);
  el("runtime-gpu").textContent = formatPercent(t.gpu_percent);
  el("runtime-ram").textContent = t.ram_free_mb > 0 ? `${t.ram_free_mb} MB free` : "—";
  el("runtime-vram").textContent = t.vram_free_mb > 0 ? `${t.vram_free_mb} MB free` : "—";
  el("runtime-game").textContent = t.game_active ? (t.foreground_process || "active") : "нет";
  el("runtime-fps").textContent = t.fps && t.fps > 0 ? t.fps.toFixed(0) : "—";
  const lastLLM = status.last_llm_decision ?? (status.last_decision?.kind === "llm" ? status.last_decision : undefined) ?? latestLLMDecision;
  if (lastLLM) {
    renderInferenceDecision(lastLLM);
    const active = latestLLMDecision ?? lastLLM;
    if (prefs.locked_model && prefs.locked_model !== (active.model_family || active.model)) {
      el("runtime-reason").textContent = `Выбрана ${prefs.locked_model}; последний вызов был на ${active.model_family || active.model || active.provider_id}. Новая модель применится при следующем запросе.`;
    }
  } else {
    el("runtime-model").textContent = "ещё не вызывалась";
    el("runtime-backend").textContent = "—";
    el("runtime-reason").textContent = prefs.locked_model
      ? `Выбрана ${prefs.locked_model}. Рабочая модель появится после первого запроса.`
      : "Рабочая модель появится после первого запроса.";
    el("bar-runtime").textContent = "ожидает вызова";
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
    el("bar-runtime").textContent = "выключен";
    el("runtime-reason").textContent = "Не удалось получить состояние AI Runtime.";
  }
}

async function saveInferencePreferences(): Promise<void> {
  if (!client || !inferenceAvailable) {
    el("runtime-reason").textContent = "Сначала подключите ядро, затем выбирайте модель.";
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
      addTurn("Вы", String(payload.text ?? ""));
      break;

    case "tts.chunk":
      stage.playChunk(String(payload.audio_b64 ?? ""), Number(payload.sample_rate ?? 16000));
      break;

    case "avatar.expression": {
      const label = String(payload.label ?? "neutral");
      const chip = el("emotion-label");
      chip.textContent = EMOTION_LABELS[label] ?? "";
      chip.hidden = !chip.textContent;
      stage.react(label, String(payload.expression ?? "exp_neutral"));
      break;
    }

    case "memory.indicator":
      addMemory(payload);
      break;

    case "data.manifest":
      addLedgerEntry(String(payload.provider ?? "модель"),
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
      addTurn("Напоминание", String(payload.text ?? "Сработал таймер"));
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
      addTurn("Ошибка", `${payload.stage}: ${payload.error}`, "turn--error");
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
  el("connect").textContent = "Подключение…";

  client = new CoreClient(base, token);
  const currentClient = client;
  try {
    const status = await currentClient.status();
    if (client !== currentClient) return;
    renderProviders(status.providers);
    el("bar-core").textContent = `подключено · ${status.version}`;

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
      el("bar-core").textContent = "соединение потеряно";
      el("connect").textContent = "Подключиться";
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
      if (client === currentClient && sessionId) addTurn("Ошибка подтверждений", String(error), "turn--error");
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
    el("connect").textContent = "Подключиться";
    el("connect").hidden = false;
    // Silent retries while the core is down; one visible error otherwise.
    if (!automatic) addTurn("Ошибка", String(error), "turn--error");
    if (isUnauthorized(error)) {
      // A paired device token can be revoked or replaced by a fresh core
      // launch. Do not keep retrying it on every click.
      localStorage.removeItem("yui.token");
      el("bar-core").textContent = "токен устарел";
      el("runtime-reason").textContent = "Токен отклонён ядром. Откройте новую ссылку подключения Yui.";
    } else {
      el("bar-core").textContent = "не подключено";
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
    addTurn("Окно", String(error instanceof Error ? error.message : error), "turn--error");
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
    el("memory-empty").textContent = `Не удалось сохранить: ${String(error)}`;
    el("memory-empty").classList.remove("pane--hidden");
  }).finally(() => { input.disabled = !sessionId; });
});
el("open-settings").addEventListener("click", () => settings.open());
el("detach-avatar").addEventListener("click", () => void detach("avatar"));
el("detach-chat").addEventListener("click", () => void detach("chat"));
el("attach-avatar").addEventListener("click", () => void closeDetached(hub, "avatar"));
el("attach-chat").addEventListener("click", () => void closeDetached(hub, "chat"));
el<HTMLButtonElement>("new-chat").addEventListener("click", () => {
  if (confirm("Начать новый разговор? Текущий будет завершён, память сохранится.")) void newConversation();
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
    addTurn("Вы", text);
    hub.shareUserText(text);
    composerInput.value = "";
    autosize();
  } catch (error) { addTurn("Ошибка", String(error), "turn--error"); }
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
    help.textContent = "Введите HTTPS-адрес компьютера из окна запуска, чтобы показать QR-код.";
    return;
  }
  try {
    const url = new URL(raw);
    if (url.protocol !== "https:" || !url.hostname ||
        ["0.0.0.0", "::", "localhost", "127.0.0.1"].includes(url.hostname) ||
        url.username || url.password || url.pathname !== "/" || url.search || url.hash) {
      throw new Error("Укажите доступный HTTPS-адрес компьютера без пути.");
    }
    await QRCode.toCanvas(canvas, JSON.stringify({v: 1, host: url.origin, code: pairingCode, name: "Yui Core"}),
      {width: 220, margin: 2});
    canvas.hidden = false;
    help.textContent = "Отсканируйте QR-код в приложении Yui на телефоне.";
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
      if (!token) throw new Error("Откройте ссылку с токеном из окна запуска Yui на компьютере.");
      const result = await new CoreClient(base, token).request<{code: string; expires_at: string}>("/v1/pair/start", {method:"POST", body:"{}"});
      pairingCode = result.code;
      el("pair-code").textContent = result.code;
      el("pair-help").textContent = `На телефоне откройте HTTPS-адрес компьютера из окна запуска. Код действует до ${new Date(result.expires_at).toLocaleString()}.`;
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
      body: JSON.stringify({code: el<HTMLInputElement>("pair-input").value.trim(), name: "Телефон · браузер", kind:"web", capabilities:["text","audio","camera"]}),
    });
    if (!response.ok) {
      if (response.status === 403) {
        throw new Error("Одноразовый код недействителен, уже использован или истёк. Получите новый код на компьютере.");
      }
      const detail = await response.text();
      throw new Error(`Сопряжение: ${detail || `ошибка ${response.status}`}`);
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
      micButton.title = active ? "Выключить микрофон" : "Говорить";
      document.body.classList.toggle("mic-on", active);
    },
    level: value => stage.setMicLevel(value),
    error: message => addTurn("Ошибка микрофона", message, "turn--error"),
    speechStart: () => { void stage.speech.resume(); },
  },
  () => ({ sensitivity: prefs().vadSensitivity, bargeIn: prefs().bargeIn, isAssistantSpeaking: () => assistantSpeaking || stage.speech.speaking }),
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
  if (changed.includes("micMode") && microphone.active) {
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
  const progress = addTurn("Фото", "Подготовка фотографии…");
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
        image!.onerror = () => reject(new Error("Не удалось прочитать фотографию."));
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
    if (!context) throw new Error("Браузер не поддерживает обработку фотографий.");
    context.drawImage(source, 0, 0, canvas.width, canvas.height);
    if (client !== currentClient || sessionId !== currentSession) { progress.textContent = "Отправка отменена: соединение изменилось."; return; }
    progress.textContent = "Фото отправляется…";
    const result = await currentClient.request<{description:string}>(`/v1/sessions/${currentSession}/vision`, {
      method:"POST", body:JSON.stringify({image_b64:canvas.toDataURL("image/jpeg",0.8).split(",")[1], mime:"image/jpeg", question:"Опиши, что видишь на фотографии. Ответь по-русски."}),
    });
    progress.textContent = client === currentClient && sessionId === currentSession
      ? result.description || "Модель не вернула описание. Попробуйте другое фото."
      : "Соединение изменилось. Отправьте фотографию повторно.";
  } catch (error) { progress.textContent = `Не удалось отправить фото: ${String(error)}`; }
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
syncWindows();
void (async () => {
  if (readConnection().token || (await tauriConnection())) void connect();
})();
