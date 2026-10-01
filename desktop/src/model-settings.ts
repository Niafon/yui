import { has, onLang, t, type Key } from "./i18n";
import {
  type AddModelInput,
  type CatalogModel,
  CoreClient,
  type InferenceStatus,
  type ProviderStatus,
} from "./api";

type SourceFilter = "all" | "local" | "provider";
type SettingsOptions = {
  getClient: () => CoreClient | undefined;
  getInference: () => InferenceStatus | undefined;
  onInference: (status: InferenceStatus) => void;
  onProviders: (providers: ProviderStatus[]) => void;
};

const names: Record<string, string> = {
  "ollama": "Ollama",
  "lm-studio": "LM Studio",
  "llama.cpp": "llama.cpp",
  "openrouter": "OpenRouter",
  "openai": "OpenAI",
  "elevenlabs": "ElevenLabs",
  "azure": "Azure Speech",
  "kokoro": "Kokoro (OpenAI-compatible)",
  "yui-worker": "Yui Worker",
};
const translatedNames: Record<string, Key> = { local: "service.local", mock: "service.mock", custom: "service.custom" };

type Preset = readonly [id: string, label: string | Key, endpoint: string, keyEnv?: string];
/** Services offered in the add dialog, by source and purpose. */
function servicesFor(source: "local" | "provider", kind: string): Preset[] {
  if (kind === "tts") {
    return source === "local"
      ? [["kokoro", "Kokoro-FastAPI", "http://127.0.0.1:8880/v1"], ["custom", "service.customLocal", ""]]
      : [
        ["openai", "OpenAI", "https://api.openai.com/v1", "OPENAI_API_KEY"],
        ["elevenlabs", "ElevenLabs", "https://api.elevenlabs.io/v1", "ELEVENLABS_API_KEY"],
        ["azure", "Azure Speech", "https://westeurope.tts.speech.microsoft.com/cognitiveservices/v1", "AZURE_SPEECH_KEY"],
        ["custom", "service.customProvider", ""],
      ];
  }
  return source === "local"
    ? [
      ["ollama", "Ollama", "http://127.0.0.1:11434/v1"],
      ["lm-studio", "LM Studio", "http://127.0.0.1:1234/v1"],
      ["llama.cpp", "llama.cpp", "http://127.0.0.1:8080/v1"],
      ["custom", "service.customLocal", ""],
    ]
    : [
      ["openrouter", "OpenRouter", "https://openrouter.ai/api/v1", "OPENROUTER_API_KEY"],
      ["openai", "OpenAI", "https://api.openai.com/v1", "OPENAI_API_KEY"],
      ["custom", "service.customProvider", ""],
    ];
}
const presetLabel = (label: string): string => (has(label) ? t(label) : label);
const kindName = (kind: string): string => (has(`kind.${kind}`) ? t(`kind.${kind}` as Key) : kind);

function byId<T extends HTMLElement>(id: string): T {
  const element = document.getElementById(id);
  if (!element) throw new Error(`missing model settings element: ${id}`);
  return element as T;
}

function element<K extends keyof HTMLElementTagNameMap>(
  tag: K, className: string, text?: string,
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function displayName(model: CatalogModel): string {
  return model.model || model.model_family || model.id;
}

function serviceName(service: string): string {
  const key = translatedNames[service];
  return key ? t(key) : names[service] || service;
}

function operationName(model: CatalogModel): string {
  if (model.driver === "mock") return t("op.mock");
  if (model.driver === "worker") return t("op.worker");
  if (!model.local) return t("op.cloud");
  if (model.backend === "gpu") return t("op.gpu");
  if (model.backend === "cpu") return t("op.cpu");
  return t("op.localApi");
}

export function initModelSettings(options: SettingsOptions) {
  const list = byId<HTMLDivElement>("models-list");
  const detail = byId<HTMLElement>("models-detail");
  const message = byId<HTMLParagraphElement>("models-status");
  const kindFilter = byId<HTMLSelectElement>("model-kind-filter");
  const providerFilter = byId<HTMLSelectElement>("model-provider-filter");
  const search = byId<HTMLInputElement>("model-search");
  const addButton = byId<HTMLButtonElement>("model-add-open");
  const dialog = byId<HTMLDialogElement>("model-add-dialog");
  const form = byId<HTMLFormElement>("model-add-form");
  const sourceInput = byId<HTMLSelectElement>("model-add-source");
  const serviceInput = byId<HTMLSelectElement>("model-add-service");
  const endpointInput = byId<HTMLInputElement>("model-add-endpoint");
  const keyEnvInput = byId<HTMLInputElement>("model-add-key-env");
  const hint = byId<HTMLParagraphElement>("model-add-hint");
  const errorText = byId<HTMLParagraphElement>("model-add-error");
  const submit = byId<HTMLButtonElement>("model-add-submit");

  let models: CatalogModel[] = [];
  let source: SourceFilter = "all";
  let selectedId = "";
  let connected = false;
  let preferenceSignature = "";

  function isSelected(model: CatalogModel): boolean {
    if (model.kind !== "llm") return model.is_default;
    return options.getInference()?.preferences.preferred_provider === model.id;
  }

  function filtered(): CatalogModel[] {
    const term = search.value.trim().toLocaleLowerCase();
    return models.filter(model =>
      model.kind === kindFilter.value
      && (source === "all" || model.local === (source === "local"))
      && (!providerFilter.value || model.service === providerFilter.value)
      && (!term || [displayName(model), model.id, serviceName(model.service)]
        .some(value => value.toLocaleLowerCase().includes(term))));
  }

  function updateProviderFilter(): void {
    const current = providerFilter.value;
    const available = Array.from(new Set(models
      .filter(model => model.kind === kindFilter.value && (source === "all" || model.local === (source === "local")))
      .map(model => model.service))).sort();
    providerFilter.replaceChildren(new Option(t("models.allProviders"), ""));
    for (const service of available) providerFilter.add(new Option(serviceName(service), service));
    providerFilter.value = available.includes(current) ? current : "";
  }

  function field(label: string, value: string): HTMLDivElement {
    const row = element("div", "model-fact");
    row.append(element("dt", "", label), element("dd", "", value));
    return row;
  }

  function renderDetail(model?: CatalogModel): void {
    detail.replaceChildren();
    if (!model) {
      detail.append(element("p", "model-detail-empty", t("models.pick")));
      return;
    }
    const label = element("p", "model-detail-source", `${serviceName(model.service)} · ${model.local ? t("models.localLabel") : t("models.external")}`);
    const title = element("h3", "model-detail-title", displayName(model));
    const meta = element("p", "model-detail-id", model.id);
    const facts = element("dl", "model-facts");
    facts.append(
      field(t("fact.purpose"), kindName(model.kind)),
      field(t("fact.family"), model.model_family || t("fact.notSet")),
      field(t("fact.operation"), operationName(model)),
      field(t("fact.selection"), model.auto_select ? t("fact.auto") : t("fact.manual")),
      field(t("fact.quality"), model.quality ? t("fact.qualityValue", { quality: model.quality }) : t("fact.notSet")),
      field("RAM", model.estimated_ram_mb ? `≈ ${model.estimated_ram_mb} ${t("avatar.mb")}` : t("fact.notSet")),
      field("VRAM", model.estimated_vram_mb ? `≈ ${model.estimated_vram_mb} ${t("avatar.mb")}` : t("fact.notSet")),
      field("API", model.endpoint || (model.driver === "mock" ? t("fact.notNeeded") : t("fact.builtInWorker"))),
      field(t("fact.key"), model.api_key_env
        ? t(model.credential_ready ? "fact.keyReady" : "fact.keyMissing", { env: model.api_key_env })
        : t("fact.notNeeded")),
      field(t("fact.profile"), model.user_added ? t("fact.userAdded") : t("fact.fromConfig")),
    );
    if (model.voice) facts.append(field(t("fact.voice"), model.voice));
    if (model.tags?.length) facts.append(field(t("fact.tags"), model.tags.join(", ")));
    const note = element("p", "model-detail-note",
      model.driver === "mock"
        ? t("models.noteMock")
        : model.local
          ? t("models.noteLocal")
          : t("models.noteRemote"));
    const action = element("button", "button button--primary model-detail-action",
      isSelected(model) ? t("models.selected") : t("models.use"));
    action.type = "button";
    action.disabled = !connected || isSelected(model) || !model.credential_ready;
    action.addEventListener("click", () => void useModel(model));
    detail.append(label, title, meta, facts, note, action);
    if (model.api_key_env && !model.credential_ready) {
      detail.append(element("p", "model-detail-warning",
        t("models.setKey", { env: model.api_key_env })));
    }
  }

  function render(): void {
    const visible = filtered();
    list.replaceChildren();
    if (!visible.length) {
      list.append(element("p", "models-empty",
        models.length ? t("models.noMatch") : t("models.none")));
      renderDetail();
      message.textContent = connected ? t("models.canAdd") : t("models.connect");
      return;
    }
    if (!visible.some(model => model.id === selectedId)) {
      selectedId = visible.find(isSelected)?.id || visible[0]!.id;
    }
    for (const model of visible) {
      const card = element("button", `model-card${model.id === selectedId ? " model-card--on" : ""}`);
      card.type = "button";
      card.setAttribute("aria-pressed", String(model.id === selectedId));
      const top = element("span", "model-card__top");
      top.append(element("strong", "", displayName(model)));
      if (isSelected(model)) top.append(element("span", "model-card__active", t("models.selected")));
      else if (model.is_default) top.append(element("span", "model-card__default", t("models.default")));
      card.append(top, element("span", "model-card__meta",
        `${serviceName(model.service)} · ${model.local ? t("models.localShort") : t("models.externalShort")}`));
      if (!model.credential_ready) card.append(element("span", "model-card__warning", t("models.needKey")));
      card.addEventListener("click", () => {
        selectedId = model.id;
        render();
      });
      list.append(card);
    }
    message.textContent = t("models.count", { visible: visible.length, total: models.filter(model => model.kind === kindFilter.value).length });
    renderDetail(visible.find(model => model.id === selectedId));
  }

  async function useModel(model: CatalogModel): Promise<void> {
    const current = options.getClient();
    if (!current) return;
    message.textContent = t("models.saving");
    try {
      if (model.kind === "llm") {
        const prefs = options.getInference()?.preferences;
        if (!prefs) throw new Error(t("models.loadRuntime"));
        const status = await current.updateInferencePreferences({
          ...prefs,
          mode: "manual",
          preferred_provider: model.id,
          locked_model: model.model_family || model.model || "",
        });
        if (options.getClient() !== current) return;
        options.onInference(status);
      } else {
        await current.setDefaultProvider(model.kind, model.id);
        if (options.getClient() !== current) return;
        const status = await current.status();
        options.onProviders(status.providers);
        await refresh();
      }
      render();
      message.textContent = t("models.saved");
    } catch (error) {
      message.textContent = t("models.saveFailed", { error: String(error) });
    }
  }

  async function refresh(): Promise<void> {
    const current = options.getClient();
    if (!current) {
      setConnected(false);
      return;
    }
    message.textContent = t("models.loading");
    try {
      const catalog = await current.models();
      if (options.getClient() !== current) return;
      models = catalog;
      connected = true;
      updateProviderFilter();
      render();
    } catch (error) {
      if (options.getClient() !== current) return;
      message.textContent = t("models.loadFailed", { error: String(error) });
    }
  }

  function setConnected(value: boolean): void {
    connected = value;
    addButton.disabled = !value || !["localhost", "127.0.0.1", "[::1]"].includes(location.hostname);
    if (!value) {
      models = [];
      selectedId = "";
      updateProviderFilter();
      render();
    }
  }

  function renderStatus(status: InferenceStatus): void {
    const p = status.preferences;
    const signature = `${p.mode}|${p.preferred_provider || ""}|${p.locked_model || ""}`;
    if (signature !== preferenceSignature) {
      preferenceSignature = signature;
      if (models.length) render();
    }
  }

  const kindInput = byId<HTMLSelectElement>("model-add-kind");
  const voiceField = byId<HTMLElement>("model-add-voice-field");
  const voiceInput = byId<HTMLInputElement>("model-add-voice");
  const modelIdInput = byId<HTMLInputElement>("model-add-id");

  function presets(): Preset[] {
    return servicesFor(sourceInput.value as "local" | "provider", kindInput.value);
  }

  function updateServices(resetEndpoint: boolean): void {
    const previous = serviceInput.value;
    serviceInput.replaceChildren();
    for (const [value, label] of presets()) serviceInput.add(new Option(presetLabel(label), value));
    if (!resetEndpoint && Array.from(serviceInput.options).some(option => option.value === previous)) {
      serviceInput.value = previous;
    }
    applyPreset(resetEndpoint);
  }

  function applyPreset(replace: boolean): void {
    const source = sourceInput.value as "local" | "provider";
    const speech = kindInput.value === "tts";
    const preset = presets().find(([id]) => id === serviceInput.value);
    if (replace) endpointInput.value = preset?.[2] || "";
    voiceField.hidden = !speech;
    // Speech services have sensible default models; the field becomes optional.
    modelIdInput.required = !speech;
    const speechDefaults: Record<string, string> = { openai: "gpt-4o-mini-tts", elevenlabs: "eleven_multilingual_v2", azure: "azure-neural", kokoro: "kokoro" };
    modelIdInput.placeholder = speech ? speechDefaults[serviceInput.value] ?? "tts-1" : t("addModel.idPlaceholder");
    if (source === "provider") {
      if (replace) keyEnvInput.value = preset?.[3] ?? "";
      keyEnvInput.required = true;
      hint.textContent = speech ? `${t("addModel.hintRemote")} ${t("addModel.hintSpeech")}` : t("addModel.hintRemote");
    } else {
      if (replace) keyEnvInput.value = "";
      keyEnvInput.required = false;
      hint.textContent = t("addModel.hintLocal");
    }
  }

  for (const button of document.querySelectorAll<HTMLButtonElement>("[data-model-source]")) {
    button.addEventListener("click", () => {
      source = button.dataset.modelSource as SourceFilter;
      for (const other of document.querySelectorAll<HTMLButtonElement>("[data-model-source]")) {
        const active = other === button;
        other.classList.toggle("models-source__button--on", active);
        other.setAttribute("aria-pressed", String(active));
      }
      updateProviderFilter();
      render();
    });
  }
  kindFilter.addEventListener("change", () => { updateProviderFilter(); render(); });
  providerFilter.addEventListener("change", render);
  search.addEventListener("input", render);

  addButton.addEventListener("click", () => {
    errorText.textContent = "";
    form.reset();
    updateServices(true);
    dialog.showModal();
  });
  byId<HTMLButtonElement>("model-add-close").addEventListener("click", () => dialog.close());
  byId<HTMLButtonElement>("model-add-cancel").addEventListener("click", () => dialog.close());
  sourceInput.addEventListener("change", () => updateServices(true));
  kindInput.addEventListener("change", () => updateServices(true));
  serviceInput.addEventListener("change", () => applyPreset(true));
  form.addEventListener("submit", (event) => {
    event.preventDefault();
    const current = options.getClient();
    if (!current) return;
    const input: AddModelInput = {
      kind: kindInput.value as AddModelInput["kind"],
      source: sourceInput.value as AddModelInput["source"],
      service: serviceInput.value,
      endpoint: endpointInput.value.trim(),
      model: modelIdInput.value.trim(),
      api_key_env: keyEnvInput.value.trim(),
      voice: kindInput.value === "tts" ? voiceInput.value.trim() : "",
    };
    submit.disabled = true;
    errorText.textContent = "";
    void current.addModel(input).then(result => {
      if (options.getClient() !== current) return;
      models = result.models;
      kindFilter.value = input.kind;
      source = input.source;
      for (const button of document.querySelectorAll<HTMLButtonElement>("[data-model-source]")) {
        const active = button.dataset.modelSource === source;
        button.classList.toggle("models-source__button--on", active);
        button.setAttribute("aria-pressed", String(active));
      }
      selectedId = result.id;
      search.value = "";
      updateProviderFilter();
      providerFilter.value = input.service;
      dialog.close();
      render();
      message.textContent = t("models.added");
    }).catch(error => {
      errorText.textContent = String(error);
    }).finally(() => { submit.disabled = false; });
  });

  updateServices(true);
  setConnected(false);
  onLang(() => {
    updateProviderFilter();
    updateServices(false);
    render();
  });
  return { refresh, renderStatus, setConnected };
}
