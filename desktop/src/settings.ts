/**
 * Settings dialog: category navigation plus the panels that are not part of
 * the conversation flow (character, avatar, voice, privacy grants, devices,
 * interface). Model and runtime panels keep their own modules.
 */

import type { CoreClient, Identity, IdentityPatch } from "./api";
import { importAvatar, listAvatars, removeAvatar } from "./avatar/catalog";
import { parseCharacterCard } from "./character-card";
import { ACCENTS, applyAppearance, onPrefs, prefs, updatePrefs, type Prefs } from "./prefs";

export type SettingsTab = "character" | "avatar" | "voice" | "models" | "inference" | "memory" | "ledger" | "devices" | "interface";

export interface SettingsContext {
  client(): CoreClient | undefined;
  identity(): Identity | undefined;
  onIdentity(identity: Identity): void;
  onTab(tab: SettingsTab): void;
  previewVoice(): Promise<void>;
  lipEngine(): string;
  detachAvatar(): void;
  reconnect(base: string): void;
  coreBase(): string;
}

const TRAITS: Record<string, string> = {
  warmth: "Теплота", curiosity: "Любопытство", playfulness: "Игривость",
  directness: "Прямота", assertiveness: "Настойчивость", calmness: "Спокойствие",
};

const byId = <T extends HTMLElement>(id: string): T => {
  const node = document.getElementById(id);
  if (!node) throw new Error(`missing settings element: ${id}`);
  return node as T;
};

function make<K extends keyof HTMLElementTagNameMap>(tag: K, className = "", text?: string): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function icon(name: string): SVGSVGElement {
  const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  const use = document.createElementNS("http://www.w3.org/2000/svg", "use");
  use.setAttribute("href", `#i-${name}`);
  svg.append(use);
  return svg;
}

export function iconButton(name: string, label: string, onClick: () => void): HTMLButtonElement {
  const button = make("button", "icon-button icon-button--small");
  button.type = "button";
  button.title = label;
  button.setAttribute("aria-label", label);
  button.append(icon(name));
  button.addEventListener("click", onClick);
  return button;
}

/** Binds a range input to a preference with a formatted live value. */
function range(id: string, key: keyof Prefs, format: (value: number) => string): void {
  const input = byId<HTMLInputElement>(id);
  const output = document.getElementById(`${id}-value`);
  const show = () => { if (output) output.textContent = format(Number(input.value)); };
  input.value = String(prefs()[key]);
  show();
  input.addEventListener("input", () => { show(); updatePrefs({ [key]: Number(input.value) } as Partial<Prefs>); });
  onPrefs(next => { if (document.activeElement !== input) { input.value = String(next[key]); show(); } });
}

function check(id: string, key: keyof Prefs): void {
  const input = byId<HTMLInputElement>(id);
  input.checked = Boolean(prefs()[key]);
  input.addEventListener("change", () => updatePrefs({ [key]: input.checked } as Partial<Prefs>));
  onPrefs(next => { input.checked = Boolean(next[key]); });
}

function select(id: string, key: keyof Prefs): void {
  const input = byId<HTMLSelectElement>(id);
  input.value = String(prefs()[key]);
  input.addEventListener("change", () => updatePrefs({ [key]: input.value } as Partial<Prefs>));
  onPrefs(next => { input.value = String(next[key]); });
}

const percent = (value: number) => `${Math.round(value * 100)}%`;
const times = (value: number) => `×${value.toFixed(2)}`;

export function initSettings(context: SettingsContext) {
  const dialog = byId<HTMLDialogElement>("settings");
  const tabs = Array.from(dialog.querySelectorAll<HTMLButtonElement>(".tab"));
  let currentTab: SettingsTab = "character";

  function show(tab: SettingsTab): void {
    currentTab = tab;
    for (const button of tabs) {
      const on = button.dataset.tab === tab;
      button.classList.toggle("tab--on", on);
      button.setAttribute("aria-selected", String(on));
      button.tabIndex = on ? 0 : -1;
    }
    for (const button of tabs) byId(`panel-${button.dataset.tab}`).classList.toggle("pane--hidden", button.dataset.tab !== tab);
    context.onTab(tab);
    if (tab === "avatar") void renderAvatars();
    if (tab === "ledger") void renderGrants();
    if (tab === "devices") void renderDevices();
    if (tab === "voice") byId("lip-engine-active").textContent = context.lipEngine();
    if (tab === "character") renderCharacter();
  }

  for (const button of tabs) {
    button.addEventListener("click", () => show(button.dataset.tab as SettingsTab));
    button.addEventListener("keydown", event => {
      const index = tabs.indexOf(button);
      const next = ["ArrowDown", "ArrowRight"].includes(event.key) ? (index + 1) % tabs.length
        : ["ArrowUp", "ArrowLeft"].includes(event.key) ? (index - 1 + tabs.length) % tabs.length
          : event.key === "Home" ? 0 : event.key === "End" ? tabs.length - 1 : -1;
      const target = tabs[next];
      if (!target) return;
      event.preventDefault();
      target.focus();
      target.click();
    });
  }
  byId("settings-close").addEventListener("click", () => dialog.close());
  dialog.addEventListener("click", event => { if (event.target === dialog) dialog.close(); });

  // Character ------------------------------------------------------------
  const form = byId<HTMLFormElement>("character-form");
  const status = byId("character-status");
  const save = byId<HTMLButtonElement>("character-save");
  const traitBox = byId("character-traits");
  const initiative = byId<HTMLInputElement>("character-initiative");
  const initiativeValue = byId("character-initiative-value");
  initiative.addEventListener("input", () => { initiativeValue.textContent = percent(Number(initiative.value)); });

  function renderCharacter(): void {
    const identity = context.identity();
    const connected = Boolean(context.client() && identity);
    save.disabled = !connected;
    for (const control of form.querySelectorAll<HTMLInputElement>("input, select, textarea")) control.disabled = !connected;
    if (!identity) { status.textContent = "Подключите ядро, чтобы изменить персонажа."; return; }
    if (form.dataset.identity === `${identity.id}`) return; // keep unsaved edits
    form.dataset.identity = identity.id;
    byId<HTMLInputElement>("character-name").value = identity.name;
    byId<HTMLSelectElement>("character-mode").value = identity.mode || "normal";
    byId<HTMLTextAreaElement>("character-style").value = identity.speech_style ?? "";
    byId<HTMLInputElement>("character-relationship").value = identity.relationship ?? "";
    byId<HTMLInputElement>("character-voice").value = identity.presentation?.voice_profile ?? "";
    initiative.value = String(identity.initiative ?? 0.5);
    initiativeValue.textContent = percent(Number(initiative.value));
    traitBox.replaceChildren();
    for (const [name, value] of Object.entries(identity.traits ?? {})) {
      const label = make("label", "field");
      const caption = make("span", "field__label", `${TRAITS[name] ?? name} `);
      const output = make("output", "", percent(value));
      caption.append(output);
      const input = make("input");
      input.type = "range"; input.min = "0"; input.max = "1"; input.step = "0.05";
      input.value = String(value); input.dataset.trait = name;
      input.addEventListener("input", () => { output.textContent = percent(Number(input.value)); });
      label.append(caption, input);
      traitBox.append(label);
    }
    status.textContent = "Изменения применятся к следующим ответам.";
  }

  form.addEventListener("submit", event => {
    event.preventDefault();
    const client = context.client();
    const identity = context.identity();
    if (!client || !identity) return;
    const traits: Record<string, number> = {};
    for (const input of traitBox.querySelectorAll<HTMLInputElement>("input[data-trait]")) traits[input.dataset.trait!] = Number(input.value);
    const patch: IdentityPatch = {
      name: byId<HTMLInputElement>("character-name").value.trim() || identity.name,
      mode: byId<HTMLSelectElement>("character-mode").value as IdentityPatch["mode"],
      speech_style: byId<HTMLTextAreaElement>("character-style").value.trim(),
      relationship: byId<HTMLInputElement>("character-relationship").value.trim(),
      voice_profile: byId<HTMLInputElement>("character-voice").value.trim(),
      initiative: Number(initiative.value),
      traits,
    };
    save.disabled = true;
    status.textContent = "Сохраняю…";
    void client.updateIdentity(identity.id, patch).then(updated => {
      if (context.client() !== client) return;
      form.dataset.identity = "";
      context.onIdentity(updated);
      renderCharacter();
      status.textContent = "Сохранено.";
    }).catch(error => { status.textContent = `Не удалось сохранить: ${String(error)}`; })
      .finally(() => { save.disabled = !context.client(); });
  });

  byId<HTMLInputElement>("character-card").addEventListener("change", event => {
    const input = event.target as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;
    void parseCharacterCard(file).then(card => {
      byId<HTMLInputElement>("character-name").value = card.name;
      if (card.style) byId<HTMLTextAreaElement>("character-style").value = card.style;
      if (card.relationship) byId<HTMLInputElement>("character-relationship").value = card.relationship;
      status.textContent = `Карточка «${card.name}» загружена. Проверьте поля и сохраните.`;
    }).catch(error => { status.textContent = String(error instanceof Error ? error.message : error); })
      .finally(() => { input.value = ""; });
  });

  // Avatar ---------------------------------------------------------------
  const grid = byId("avatar-grid");
  const importStatus = byId("avatar-import-status");
  async function renderAvatars(): Promise<void> {
    const entries = await listAvatars();
    grid.replaceChildren();
    for (const entry of entries) {
      const card = make("div", `avatar-card${prefs().avatar === entry.id ? " avatar-card--on" : ""}`);
      const pick = make("button", "avatar-card__pick");
      pick.type = "button";
      pick.setAttribute("role", "radio");
      pick.setAttribute("aria-checked", String(prefs().avatar === entry.id));
      pick.append(make("span", `avatar-card__badge avatar-card__badge--${entry.kind}`, entry.kind === "vrm" ? "3D" : "2D"),
        make("strong", "", entry.label), make("small", "", entry.note ?? ""));
      pick.addEventListener("click", () => { updatePrefs({ avatar: entry.id }); void renderAvatars(); });
      card.append(pick);
      if (!entry.builtin) {
        card.append(iconButton("trash", "Удалить модель", () => {
          if (!confirm(`Удалить «${entry.label}» из этого браузера?`)) return;
          void removeAvatar(entry.id).then(() => {
            if (prefs().avatar === entry.id) updatePrefs({ avatar: "shino" });
            void renderAvatars();
          });
        }));
      }
      grid.append(card);
    }
  }
  byId<HTMLInputElement>("avatar-import").addEventListener("change", event => {
    const input = event.target as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;
    importStatus.textContent = "Импортирую…";
    void importAvatar(file).then(entry => {
      importStatus.textContent = `Добавлено: ${entry.label}`;
      updatePrefs({ avatar: entry.id });
      void renderAvatars();
    }).catch(error => { importStatus.textContent = String(error instanceof Error ? error.message : error); })
      .finally(() => { input.value = ""; });
  });
  range("avatar-scale", "avatarScale", times);
  range("avatar-offset", "avatarOffsetY", value => `${value > 0 ? "+" : ""}${Math.round(value * 100)}`);
  range("avatar-motion", "motion", percent);
  select("avatar-backdrop", "backdrop");
  check("avatar-follow", "followPointer");
  check("avatar-ontop", "avatarOnTop");
  byId("avatar-detach").addEventListener("click", () => { dialog.close(); context.detachAvatar(); });

  // Voice ----------------------------------------------------------------
  range("voice-volume", "volume", percent);
  range("lip-sensitivity", "lipSensitivity", times);
  select("lip-engine", "lipEngine");
  select("mic-mode", "micMode");
  range("vad-sensitivity", "vadSensitivity", times);
  check("barge-in", "bargeIn");
  onPrefs((_, changed) => { if (changed.includes("lipEngine")) byId("lip-engine-active").textContent = context.lipEngine(); });
  const preview = byId<HTMLButtonElement>("voice-preview");
  preview.addEventListener("click", () => {
    preview.disabled = true;
    void context.previewVoice()
      .then(() => { byId("lip-engine-active").textContent = context.lipEngine(); })
      .catch(error => { byId("lip-engine-active").textContent = String(error); })
      .finally(() => { preview.disabled = false; });
  });

  // Privacy grants -------------------------------------------------------
  async function renderGrants(): Promise<void> {
    const client = context.client();
    const list = byId("grants-list");
    const empty = byId("grants-empty");
    if (!client) { list.replaceChildren(); empty.hidden = false; empty.textContent = "Подключите ядро."; return; }
    try {
      const grants = (await client.grants()).filter(grant => !grant.expires_at || Date.parse(grant.expires_at) > Date.now());
      if (context.client() !== client) return;
      list.replaceChildren();
      empty.hidden = grants.length > 0;
      empty.textContent = "Постоянных разрешений нет.";
      for (const grant of grants) {
        const item = make("li");
        item.dataset.remote = String(grant.decision === "allow");
        const text = make("div");
        text.append(make("div", "ledger__meta", `${grant.subject_id || grant.subject_kind} · ${grant.action}`),
          make("div", "", `${grant.decision === "allow" ? "Разрешено" : "Запрещено"}: ${grant.category}`));
        item.append(text, iconButton("trash", "Отозвать", () => {
          void client.revokeGrant(grant.id).then(renderGrants).catch(error => { empty.hidden = false; empty.textContent = String(error); });
        }));
        list.append(item);
      }
    } catch (error) {
      empty.hidden = false;
      empty.textContent = `Не удалось загрузить разрешения: ${String(error)}`;
    }
  }

  // Devices --------------------------------------------------------------
  async function renderDevices(): Promise<void> {
    const client = context.client();
    const list = byId("devices-list");
    const empty = byId("devices-empty");
    if (!client) { list.replaceChildren(); empty.hidden = false; empty.textContent = "Подключите ядро, чтобы увидеть устройства."; return; }
    try {
      const devices = (await client.devices()).filter(device => !device.revoked_at);
      if (context.client() !== client) return;
      list.replaceChildren();
      empty.hidden = devices.length > 0;
      empty.textContent = "Подключённых устройств нет.";
      for (const device of devices) {
        const item = make("li");
        const seen = Date.parse(device.last_seen_at);
        const text = make("div");
        text.append(make("div", "ledger__meta", `${device.kind} · сопряжено ${new Date(device.paired_at).toLocaleDateString()}`),
          make("div", "", `${device.name}${Number.isFinite(seen) && seen > 0 ? ` · был ${new Date(seen).toLocaleString()}` : ""}`));
        item.append(text, iconButton("trash", "Отключить устройство", () => {
          if (!confirm(`Отключить «${device.name}»? Устройству понадобится новое сопряжение.`)) return;
          void client.revokeDevice(device.id).then(renderDevices).catch(error => { empty.hidden = false; empty.textContent = String(error); });
        }));
        list.append(item);
      }
    } catch (error) {
      empty.hidden = false;
      empty.textContent = `Не удалось загрузить устройства: ${String(error)}`;
    }
  }

  // Interface ------------------------------------------------------------
  select("ui-theme", "theme");
  check("ui-enter", "sendOnEnter");
  check("ui-ribbon", "showRibbon");
  const swatches = byId("ui-accents");
  function renderSwatches(): void {
    swatches.replaceChildren();
    for (const colour of ACCENTS) {
      const swatch = make("button", `swatch${prefs().accent === colour ? " swatch--on" : ""}`);
      swatch.type = "button";
      swatch.style.setProperty("--swatch", colour);
      swatch.setAttribute("role", "radio");
      swatch.setAttribute("aria-checked", String(prefs().accent === colour));
      swatch.setAttribute("aria-label", colour);
      swatch.addEventListener("click", () => updatePrefs({ accent: colour }));
      swatches.append(swatch);
    }
  }
  renderSwatches();
  onPrefs((next, changed) => {
    if (changed.some(key => key === "theme" || key === "accent" || key === "backdrop")) applyAppearance(next);
    if (changed.includes("accent")) renderSwatches();
    if (changed.includes("avatar") && currentTab === "avatar") void renderAvatars();
  });
  window.matchMedia("(prefers-color-scheme: light)").addEventListener("change", () => applyAppearance());
  const coreInput = byId<HTMLInputElement>("ui-core");
  byId("ui-reconnect").addEventListener("click", () => context.reconnect(coreInput.value.trim()));

  return {
    open(tab: SettingsTab = currentTab): void {
      coreInput.value = context.coreBase();
      if (!dialog.open) dialog.showModal();
      show(tab);
    },
    get tab(): SettingsTab { return currentTab; },
    refreshCharacter(): void { form.dataset.identity = ""; if (dialog.open && currentTab === "character") renderCharacter(); },
  };
}
