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
  "local": "Локальный сервер",
  "yui-worker": "Yui Worker",
  "mock": "Встроенная",
  "custom": "Другой сервис",
};

const kinds: Record<string, string> = {
  llm: "Диалог",
  stt: "Распознавание речи",
  tts: "Голос",
  vision: "Зрение",
  embeddings: "Память",
};

const services = {
  local: [
    ["ollama", "Ollama", "http://127.0.0.1:11434/v1"],
    ["lm-studio", "LM Studio", "http://127.0.0.1:1234/v1"],
    ["llama.cpp", "llama.cpp", "http://127.0.0.1:8080/v1"],
    ["custom", "Другой локальный сервер", ""],
  ],
  provider: [
    ["openrouter", "OpenRouter", "https://openrouter.ai/api/v1"],
    ["openai", "OpenAI", "https://api.openai.com/v1"],
    ["custom", "Другой провайдер", ""],
  ],
} as const;

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
  return names[service] || service;
}

function operationName(model: CatalogModel): string {
  if (model.driver === "mock") return "Встроенная тестовая модель";
  if (model.driver === "worker") return "Локальный worker";
  if (!model.local) return "Облачный API";
  if (model.backend === "gpu") return "Локально · GPU";
  if (model.backend === "cpu") return "Локально · CPU";
  return "Локальный API";
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
    providerFilter.replaceChildren(new Option("Все провайдеры", ""));
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
      detail.append(element("p", "model-detail-empty", "Выберите модель из списка, чтобы увидеть её параметры."));
      return;
    }
    const label = element("p", "model-detail-source", `${serviceName(model.service)} · ${model.local ? "Локальная" : "Внешний провайдер"}`);
    const title = element("h3", "model-detail-title", displayName(model));
    const meta = element("p", "model-detail-id", model.id);
    const facts = element("dl", "model-facts");
    facts.append(
      field("Назначение", kinds[model.kind] || model.kind),
      field("Семейство", model.model_family || "Не указано"),
      field("Способ работы", operationName(model)),
      field("Выбор", model.auto_select ? "Участвует в автоподборе" : "Выбирается вручную"),
      field("Качество", model.quality ? `${model.quality}/100 · оценка для автовыбора` : "Не указано"),
      field("RAM", model.estimated_ram_mb ? `≈ ${model.estimated_ram_mb} МБ` : "Не указано"),
      field("VRAM", model.estimated_vram_mb ? `≈ ${model.estimated_vram_mb} МБ` : "Не указано"),
      field("API", model.endpoint || (model.driver === "mock" ? "Не требуется" : "Встроенный worker")),
      field("Ключ", model.api_key_env
        ? (model.credential_ready ? `${model.api_key_env} · доступен` : `${model.api_key_env} · не задан`)
        : "Не требуется"),
      field("Профиль", model.user_added ? "Добавлен в настройках" : "Из конфигурации Yui"),
    );
    if (model.tags?.length) facts.append(field("Метки", model.tags.join(", ")));
    const note = element("p", "model-detail-note",
      model.driver === "mock"
        ? "Тестовый провайдер работает без внешнего сервера."
        : model.local
          ? "Локальный сервер должен быть запущен. Доступность проверится при запросе."
          : "Передача данных этому провайдеру контролируется разрешениями Yui.");
    const action = element("button", "button button--primary model-detail-action",
      isSelected(model) ? "Выбрана" : "Использовать");
    action.type = "button";
    action.disabled = !connected || isSelected(model) || !model.credential_ready;
    action.addEventListener("click", () => void useModel(model));
    detail.append(label, title, meta, facts, note, action);
    if (model.api_key_env && !model.credential_ready) {
      detail.append(element("p", "model-detail-warning",
        `Задайте ${model.api_key_env} в окружении ядра и перезапустите Yui.`));
    }
  }

  function render(): void {
    const visible = filtered();
    list.replaceChildren();
    if (!visible.length) {
      list.append(element("p", "models-empty",
        models.length ? "По этим условиям моделей нет." : "Пока нет добавленных моделей."));
      renderDetail();
      message.textContent = connected ? "Можно добавить OpenAI-совместимую модель." : "Подключите ядро, чтобы увидеть модели.";
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
      if (isSelected(model)) top.append(element("span", "model-card__active", "Выбрана"));
      else if (model.is_default) top.append(element("span", "model-card__default", "По умолчанию"));
      card.append(top, element("span", "model-card__meta",
        `${serviceName(model.service)} · ${model.local ? "локально" : "внешний"}`));
      if (!model.credential_ready) card.append(element("span", "model-card__warning", "Нужен API-ключ"));
      card.addEventListener("click", () => {
        selectedId = model.id;
        render();
      });
      list.append(card);
    }
    message.textContent = `${visible.length} из ${models.filter(model => model.kind === kindFilter.value).length} моделей`;
    renderDetail(visible.find(model => model.id === selectedId));
  }

  async function useModel(model: CatalogModel): Promise<void> {
    const current = options.getClient();
    if (!current) return;
    message.textContent = "Сохраняю выбор…";
    try {
      if (model.kind === "llm") {
        const prefs = options.getInference()?.preferences;
        if (!prefs) throw new Error("Сначала загрузите настройки AI Runtime.");
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
      message.textContent = "Выбор сохранён. Новая модель применится при следующем запросе.";
    } catch (error) {
      message.textContent = `Не удалось выбрать модель: ${String(error)}`;
    }
  }

  async function refresh(): Promise<void> {
    const current = options.getClient();
    if (!current) {
      setConnected(false);
      return;
    }
    message.textContent = "Загружаю модели…";
    try {
      const catalog = await current.models();
      if (options.getClient() !== current) return;
      models = catalog;
      connected = true;
      updateProviderFilter();
      render();
    } catch (error) {
      if (options.getClient() !== current) return;
      message.textContent = `Не удалось загрузить модели: ${String(error)}`;
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

  function updateServices(resetEndpoint: boolean): void {
    const source = sourceInput.value as "local" | "provider";
    const previous = serviceInput.value;
    serviceInput.replaceChildren();
    for (const [value, label] of services[source]) serviceInput.add(new Option(label, value));
    if (!resetEndpoint && Array.from(serviceInput.options).some(option => option.value === previous)) {
      serviceInput.value = previous;
    }
    applyPreset(resetEndpoint);
  }

  function applyPreset(replace: boolean): void {
    const source = sourceInput.value as "local" | "provider";
    const preset = services[source].find(([id]) => id === serviceInput.value);
    if (replace) endpointInput.value = preset?.[2] || "";
    if (source === "provider") {
      if (replace) keyEnvInput.value = serviceInput.value === "openrouter" ? "OPENROUTER_API_KEY"
        : serviceInput.value === "openai" ? "OPENAI_API_KEY" : "";
      keyEnvInput.required = true;
      hint.textContent = "Укажите имя переменной с ключом в окружении ядра. Сам ключ здесь не вводится и не сохраняется.";
    } else {
      if (replace) keyEnvInput.value = "";
      keyEnvInput.required = false;
      hint.textContent = "Локальный API должен слушать 127.0.0.1 или localhost. Ключ оставьте пустым, если он не нужен.";
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
  serviceInput.addEventListener("change", () => applyPreset(true));
  form.addEventListener("submit", (event) => {
    event.preventDefault();
    const current = options.getClient();
    if (!current) return;
    const input: AddModelInput = {
      kind: byId<HTMLSelectElement>("model-add-kind").value as AddModelInput["kind"],
      source: sourceInput.value as AddModelInput["source"],
      service: serviceInput.value,
      endpoint: endpointInput.value.trim(),
      model: byId<HTMLInputElement>("model-add-id").value.trim(),
      api_key_env: keyEnvInput.value.trim(),
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
      message.textContent = "Модель добавлена. Выберите «Использовать», чтобы включить её.";
    }).catch(error => {
      errorText.textContent = String(error);
    }).finally(() => { submit.disabled = false; });
  });

  updateServices(true);
  setConnected(false);
  return { refresh, renderStatus, setConnected };
}
