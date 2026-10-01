/**
 * Stage wiring. Reads frames from the core, renders state, and shows exactly
 * which data categories left the machine on each turn.
 */

import {
  CoreClient,
  type Frame,
  type ProviderStatus,
  type InferenceDecision,
  type InferenceStatus,
  type PendingTool,
} from "./api";
import { PresenceRibbon, type PresenceState } from "./presence";
import { createAvatar, createVRMAvatar, DEFAULT_MODEL, DEFAULT_VRM_MODEL, type Avatar } from "./live2d";
import { initModelSettings } from "./model-settings";
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

const ribbon = new PresenceRibbon(el<HTMLCanvasElement>("presence"));
let avatar: Avatar = createAvatar(el<HTMLCanvasElement>("live2d"));
let avatarGeneration = 0;
let currentAvatarState: PresenceState = "idle";
const vrmModels: Record<string, string> = { vrm: DEFAULT_VRM_MODEL, shino: "/assets/vrm/shino.vrm", victoria: "/assets/vrm/victoria.vrm" };
async function loadAvatar(format: string): Promise<void> {
  const generation = ++avatarGeneration;
  avatar.destroy();
  const oldCanvas = el<HTMLCanvasElement>("live2d");
  const canvas = oldCanvas.cloneNode(false) as HTMLCanvasElement;
  oldCanvas.replaceWith(canvas);
  const path = vrmModels[format];
  avatar = path ? createVRMAvatar(canvas) : createAvatar(canvas);
  const loaded = await avatar.load(path ?? DEFAULT_MODEL);
  if (generation !== avatarGeneration) return;
  avatar.setState(currentAvatarState);
  const fallback = el("figure-fallback");
  fallback.hidden = loaded;
  if (!loaded) fallback.textContent = path ? "Не удалось загрузить VRM. Попробуйте другую модель или проверьте WebGL." : "Не удалось загрузить Live2D. Проверьте файлы модели и поддержку WebGL.";
}
const avatarFormat = localStorage.getItem("yui.avatar") ?? "shino";
el<HTMLSelectElement>("avatar-format").value = avatarFormat;
void loadAvatar(avatarFormat);
el<HTMLSelectElement>("avatar-format").addEventListener("change", (event) => {
  const format = (event.target as HTMLSelectElement).value;
  localStorage.setItem("yui.avatar", format); void loadAvatar(format);
});
window.addEventListener("beforeunload", () => avatar.destroy());

let client: CoreClient | undefined;
let sessionId = "";
let identityId = "";
let streamingTurn: HTMLParagraphElement | undefined;
let inferenceTimer: number | undefined;
let lastInferenceStatus: InferenceStatus | undefined;
let latestLLMDecision: InferenceDecision | undefined;
let inferenceAvailable = false;
let providerLocal = new Map<string, boolean>();
let remoteDefaultProvider = false;
let ledgerLoaded = false;
let modelSettings: ReturnType<typeof initModelSettings> | undefined;

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
  return {
    base: params.get("core") ?? localStorage.getItem("yui.core") ?? (location.port === "5273" ? "http://127.0.0.1:8766" : location.origin),
    token: fragment.get("token") ?? params.get("token") ?? localStorage.getItem("yui.token") ?? "",
  };
}

function setState(state: string): void {
  el("state-label").textContent = STATE_LABELS[state] ?? state;
  el("state-dot").dataset.state = state;
  const known: PresenceState[] = ["idle", "listening", "thinking", "speaking", "error"];
  currentAvatarState = known.includes(state as PresenceState) ? (state as PresenceState) : "idle";
  ribbon.setState(currentAvatarState);
  avatar.setState(currentAvatarState);
}

function addTurn(who: string, text: string, kind = ""): HTMLParagraphElement {
  const list = el<HTMLOListElement>("transcript");
  document.getElementById("transcript-empty")?.remove();
  const item = document.createElement("li");
  item.className = `turn ${kind || (who === "Вы" ? "turn--user" : "turn--notice")}`;
  const label = document.createElement("span");
  label.className = "turn__who";
  label.textContent = who;
  const body = document.createElement("p");
  body.className = "turn__text";
  body.textContent = text;
  item.append(label, body);
  list.append(item);
  list.scrollTop = list.scrollHeight;
  return body;
}

function addMemory(payload: Record<string, unknown>): void {
  const list = el<HTMLUListElement>("memory-list");
  const id = String(payload.id ?? "");
  const item = Array.from(list.children).find(child => (child as HTMLLIElement).dataset.id === id) as HTMLLIElement | undefined
    ?? document.createElement("li");
  if (id) item.dataset.id = id;
  item.dataset.status = String(payload.status ?? "");
  item.replaceChildren();
  const meta = document.createElement("div");
  meta.className = "memory__meta";
  meta.textContent = `${payload.category} · ${payload.status}`;
  const text = document.createElement("div");
  text.textContent = String(payload.content ?? "");
  item.append(meta, text);
  list.prepend(item);
  el("memory-empty").classList.toggle("pane--hidden", list.children.length > 0);
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
  const approve = document.createElement("button");
  const reject = document.createElement("button");
  approve.className = "button button--warn";
  reject.className = "button";
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

function playAudioChunk(base64: string, sampleRate: number): void {
  const bytes = Uint8Array.from(atob(base64), (c) => c.charCodeAt(0));
  const samples = new Int16Array(bytes.buffer, bytes.byteOffset, Math.floor(bytes.byteLength / 2));
  const context = audioContext();
  const buffer = context.createBuffer(1, samples.length || 1, sampleRate || 16000);
  const channel = buffer.getChannelData(0);
  for (let i = 0; i < samples.length; i++) channel[i] = (samples[i] ?? 0) / 32768;
  queueSpeech(context, buffer);
}

function queueSpeech(context: AudioContext, buffer: AudioBuffer): void {
  const source = context.createBufferSource();
  source.buffer = buffer;
  source.connect(speechAnalyser!);
  speechSources.add(source);
  source.onended = () => { speechSources.delete(source); source.disconnect(); };
  source.start(nextPlaybackTime(context, buffer.duration));
  void context.resume();
  if (!speechFrame) speechFrame = requestAnimationFrame(animateSpeech);
}

let sharedContext: AudioContext | undefined;
let playhead = 0;
let speechAnalyser: AnalyserNode | undefined;
let speechFrame = 0;
const speechSources = new Set<AudioBufferSourceNode>();
const speechWave = new Float32Array(512);

function animateSpeech(): void {
  speechFrame = 0;
  speechAnalyser?.getFloatTimeDomainData(speechWave);
  const rms = Math.sqrt(speechWave.reduce((sum, value) => sum + value * value, 0) / speechWave.length);
  const level = speechSources.size ? Math.min(1, Math.max(0, rms - 0.008) * 7) : 0;
  avatar.setMouth(level);
  ribbon.setLevel(level);
  if (speechSources.size) speechFrame = requestAnimationFrame(animateSpeech);
}

function stopSpeech(): void {
  cancelAnimationFrame(speechFrame); speechFrame = 0;
  for (const source of speechSources) { source.stop(); source.disconnect(); }
  speechSources.clear();
  void sharedContext?.close(); sharedContext = undefined; speechAnalyser = undefined;
  playhead = 0; speechWave.fill(0); avatar.setMouth(0); ribbon.setLevel(0);
}

function audioContext(): AudioContext {
  if (!sharedContext) {
    sharedContext = new AudioContext();
    speechAnalyser = sharedContext.createAnalyser();
    speechAnalyser.fftSize = 512;
    speechAnalyser.connect(sharedContext.destination);
  }
  return sharedContext;
}

/** Queues chunks back to back so streamed speech does not overlap. */
function nextPlaybackTime(context: AudioContext, duration: number): number {
  const start = Math.max(context.currentTime, playhead);
  playhead = start + duration;
  return start;
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
      streamingTurn ??= addTurn("Юи", "", "turn--assistant");
      streamingTurn.textContent += String(payload.text ?? "");
      break;
    }

    case "turn.done": {
      if (!streamingTurn) addTurn("Юи", String(payload.text ?? ""), "turn--assistant");
      streamingTurn = undefined;
      // Playback owns mouth state: queued speech can outlive turn.done.
      break;
    }

    case "transcript.final":
      addTurn("Вы", String(payload.text ?? ""));
      break;

    case "tts.chunk":
      playAudioChunk(String(payload.audio_b64 ?? ""), Number(payload.sample_rate ?? 16000));
      break;

    case "avatar.expression":
      el("emotion-label").textContent = String(payload.label ?? "");
      avatar.react(String(payload.label ?? "neutral"), String(payload.expression ?? "exp_neutral"));
      break;

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

async function connect(): Promise<void> {
  const { base, token } = readConnection();
  if (!token) {
    await showPairing();
    return;
  }
  localStorage.setItem("yui.core", base);
  localStorage.setItem("yui.token", token);

  history.replaceState(null, "", location.pathname);
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
  stopMicrophone(false);
  if (inferenceTimer !== undefined) window.clearInterval(inferenceTimer);

  client = new CoreClient(base, token);
  const currentClient = client;
  try {
    const status = await currentClient.status();
    if (client !== currentClient) return;
    renderProviders(status.providers);
    el("bar-core").textContent = `подключено · ${status.version}`;

    const identities = await currentClient.identities();
    if (client !== currentClient) return;
    const identity = identities[0];
    if (identity) el("identity-name").textContent = identity.name;
    identityId = identity?.id ?? "";

    const session = await currentClient.startSession(identity?.id);
    if (client !== currentClient) return;
    sessionId = session.id;
    identityId = session.identity_id;
    currentClient.connect(sessionId, handleFrame, () => {
      if (client !== currentClient) return;
      stopMicrophone(false);
      stopSpeech();
      clearRequests();
      if (inferenceTimer !== undefined) window.clearInterval(inferenceTimer);
      sessionId = "";
      setInferenceControlsEnabled(false);
      setSensorsEnabled(false);
      modelSettings?.setConnected(false);
      el("bar-core").textContent = "соединение потеряно";
      setState("error");
    });
    setState("idle");
    setSensorsEnabled(true);
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
    inferenceTimer = window.setInterval(() => void refreshInference(), 3000);
  } catch (error) {
    if (client !== currentClient) return;
    currentClient.close();
    sessionId = "";
    setInferenceControlsEnabled(false);
    setSensorsEnabled(false);
    modelSettings?.setConnected(false);
    addTurn("Ошибка", String(error), "turn--error");
    if (isUnauthorized(error)) {
      // A paired device token can be revoked or replaced by a fresh core
      // launch. Do not keep retrying it on every click.
      localStorage.removeItem("yui.token");
      el("bar-core").textContent = "токен устарел";
      el("runtime-reason").textContent = "Токен отклонён ядром. Откройте новую ссылку подключения Yui.";
    } else {
      el("bar-core").textContent = "не подключено";
    }
  }
}

modelSettings = initModelSettings({
  getClient: () => client,
  getInference: () => lastInferenceStatus,
  onInference: renderInference,
  onProviders: renderProviders,
});

el("connect").addEventListener("click", () => void connect());

el<HTMLFormElement>("composer").addEventListener("submit", (event) => {
  event.preventDefault();
  const input = el<HTMLTextAreaElement>("composer-input");
  const text = input.value.trim();
  if (!text || !sessionId) return;
  try {
    void audioContext().resume();
    client?.sendText(text);
    addTurn("Вы", text);
    input.value = "";
  } catch (error) { addTurn("Ошибка", String(error), "turn--error"); }
});

for (const tab of document.querySelectorAll<HTMLButtonElement>(".tab")) {
  tab.addEventListener("click", () => {
    for (const other of document.querySelectorAll<HTMLButtonElement>(".tab")) {
      const selected = other === tab;
      other.classList.toggle("tab--on", selected);
      other.setAttribute("aria-selected", String(selected));
    }
    for (const name of ["dialogue", "memory", "ledger", "models", "inference"]) {
      el(`panel-${name}`).classList.toggle("pane--hidden", name !== tab.dataset.tab);
    }
    if (tab.dataset.tab === "memory") void refreshMemory();
    if (tab.dataset.tab === "ledger" && !ledgerLoaded) void loadLedger();
    if (tab.dataset.tab === "models") void modelSettings?.refresh();
  });
  tab.addEventListener("keydown", (event) => {
    const tabs = Array.from(document.querySelectorAll<HTMLButtonElement>(".tab"));
    const index = tabs.indexOf(tab);
    const next = event.key === "ArrowRight" ? (index + 1) % tabs.length
      : event.key === "ArrowLeft" ? (index - 1 + tabs.length) % tabs.length
      : event.key === "Home" ? 0
      : event.key === "End" ? tabs.length - 1 : -1;
    const nextTab = tabs[next];
    if (!nextTab) return;
    event.preventDefault();
    nextTab.focus();
    nextTab.click();
  });
}

setInferenceControlsEnabled(false);
for (const id of INFERENCE_CONTROL_IDS) {
  el(id).addEventListener("change", () => void saveInferencePreferences());
}

document.addEventListener("keydown", (event) => {
  if (event.key === "Escape" && sessionId) client?.cancelTurn();
});

// Exported for the ledger view, which the core will populate once turn
// manifests are streamed on the control plane.
export { addLedgerEntry };

function setSensorsEnabled(enabled: boolean): void {
  for (const id of ["microphone", "stop-turn", "camera", "send-message"]) {
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
  const local = ["localhost", "127.0.0.1", "[::1]"].includes(location.hostname);
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

let micStream: MediaStream | undefined;
let micContext: AudioContext | undefined;
let micNode: AudioWorkletNode | undefined;
let micTimer: number | undefined;
let micGeneration = 0;
function stopMicrophone(send: boolean): void {
  micGeneration += 1;
  const stream = micStream;
  const context = micContext;
  const node = micNode;
  const timer = micTimer;
  micStream = undefined;
  micContext = undefined;
  micNode = undefined;
  micTimer = undefined;
  stream?.getTracks().forEach(track => track.stop());
  node?.disconnect();
  if (timer !== undefined) clearTimeout(timer);
  if (send) {
    try { client?.send({type:"audio.end", sample_rate:context?.sampleRate ?? 16000, channels:1}); }
    catch { /* Socket already closed. */ }
  } else {
    try { client?.send({type:"audio.clear"}); } catch { /* Socket already closed. */ }
  }
  if (context && context.state !== "closed") void context.close().catch(() => undefined);
  el("microphone").textContent = "Говорить";
  el("sensor-state").textContent = "Микрофон выключен";
}
function createMicrophoneContext(): AudioContext {
  try {
    return new AudioContext({sampleRate:16000});
  } catch {
    // iOS Safari can reject a requested rate; the worklet uses this context's
    // native rate and audio.end reports it to the STT pipeline.
    return new AudioContext();
  }
}
function microphoneError(error: unknown): string {
  if (error instanceof DOMException && (error.name === "NotAllowedError" || error.name === "SecurityError")) {
    return "Нет доступа к микрофону. Разрешите микрофон для сайта и откройте его по HTTPS или с компьютера.";
  }
  return String(error);
}
el("microphone").onclick = async () => {
  const generation = ++micGeneration;
  try {
    if (micStream) { stopMicrophone(true); return; }
    if (!navigator.mediaDevices?.getUserMedia) throw new Error("Микрофон недоступен. На телефоне откройте HTTPS-адрес Юи и разрешите доступ к микрофону.");
    await audioContext().resume();
    if (generation !== micGeneration) return;
    const stream = await navigator.mediaDevices.getUserMedia({audio:{echoCancellation:true, noiseSuppression:true, channelCount:1}});
    if (generation !== micGeneration) { stream.getTracks().forEach(track => track.stop()); return; }
    const context = createMicrophoneContext();
    micStream = stream;
    micContext = context;
    await context.resume();
    if (generation !== micGeneration) { stream.getTracks().forEach(track => track.stop()); await context.close(); return; }
    await context.audioWorklet.addModule(`/pcm-capture.js?v=2`);
    if (generation !== micGeneration) { stream.getTracks().forEach(track => track.stop()); await context.close(); return; }
    const node = new AudioWorkletNode(context, "pcm-capture");
    micNode = node;
    node.port.onmessage = (event: MessageEvent<ArrayBuffer>) => {
      if (generation !== micGeneration || micStream !== stream || micNode !== node) return;
      try { client?.sendAudio(event.data); }
      catch (error) { stopMicrophone(false); addTurn("Ошибка", String(error), "turn--error"); }
    };
    context.createMediaStreamSource(stream).connect(node);
    node.connect(context.destination);
    const limitMs = Math.max(1000, Math.min(50000, Math.floor((16000 / context.sampleRate) * 60000) - 1000));
    const limitSeconds = Math.max(1, Math.floor(limitMs / 1000));
    el("microphone").textContent = "Закончить запись";
    el("sensor-state").textContent = `● Микрофон включён · до ${limitSeconds} секунд`;
    micTimer = window.setTimeout(() => stopMicrophone(true), limitMs);
  } catch (error) {
    if (generation !== micGeneration) return;
    stopMicrophone(false);
    addTurn("Ошибка микрофона", microphoneError(error), "turn--error");
  }
};
el("stop-turn").onclick = () => {
  stopMicrophone(false);
  client?.cancelTurn();
  stopSpeech();
};
el<HTMLButtonElement>("voice-preview").onclick = async () => {
  const button = el<HTMLButtonElement>("voice-preview");
  button.disabled = true;
  stopSpeech();
  const context = audioContext();
  try {
    await context.resume();
    const response = await fetch("/assets/voice/xenia-demo.wav");
    if (!response.ok) throw new Error("Не удалось загрузить пример голоса");
    const buffer = await context.decodeAudioData(await response.arrayBuffer());
    if (context === sharedContext) queueSpeech(context, buffer);
  } catch (error) {
    if (context === sharedContext) addTurn("Проба голоса", String(error), "turn--error");
  } finally { button.disabled = false; }
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
document.addEventListener("visibilitychange", () => { if (document.hidden) stopMicrophone(false); });
if (readConnection().token) void connect();
