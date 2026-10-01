/**
 * Avatar catalog: bundled samples plus models the user imports. Imported
 * files stay in this browser profile (IndexedDB) and never leave the device.
 */

import { createStore, del, get, set } from "idb-keyval";
import type { Live2DSource } from "./live2d";
import type { Avatar } from "./types";

import { BUILTIN_AVATARS, type AvatarEntry, type AvatarKind } from "./builtin";

export { BUILTIN_AVATARS, type AvatarEntry, type AvatarKind };

const store = typeof indexedDB === "undefined" ? undefined : createStore("yui-avatars", "models");
const INDEX_KEY = "index";

async function readIndex(): Promise<AvatarEntry[]> {
  if (!store) return [];
  try { return (await get<AvatarEntry[]>(INDEX_KEY, store)) ?? []; } catch { return []; }
}

export async function listAvatars(): Promise<AvatarEntry[]> {
  return [...BUILTIN_AVATARS, ...(await readIndex())];
}

export async function findAvatar(id: string): Promise<AvatarEntry | undefined> {
  return (await listAvatars()).find(entry => entry.id === id);
}

const MAX_IMPORT = 200 * 1024 * 1024;

/** Imports a .vrm file or a zipped Live2D (Cubism 3/4) model package. */
export async function importAvatar(file: File): Promise<AvatarEntry> {
  if (!store) throw new Error("Хранилище браузера недоступно");
  if (file.size > MAX_IMPORT) throw new Error("Файл больше 200 МБ");
  const lower = file.name.toLowerCase();
  let kind: AvatarKind;
  if (lower.endsWith(".vrm")) {
    const head = new Uint8Array(await file.slice(0, 4).arrayBuffer());
    if (String.fromCharCode(...head) !== "glTF") throw new Error("Это не VRM: нет заголовка glTF");
    kind = "vrm";
  } else if (lower.endsWith(".zip")) {
    // Validate the package before it is stored; the preview URLs are not kept.
    const { urls } = await unpackLive2D(file);
    urls.forEach(url => URL.revokeObjectURL(url));
    kind = "live2d";
  } else {
    throw new Error("Поддерживаются .vrm и .zip с моделью Live2D (.model3.json)");
  }
  const id = `custom-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 7)}`;
  const entry: AvatarEntry = {
    id, kind, url: "", builtin: false,
    label: file.name.replace(/\.(vrm|zip)$/i, "").slice(0, 40) || "Своя модель",
    note: `${kind === "vrm" ? "VRM" : "Live2D"} · ${(file.size / 1048576).toFixed(1)} МБ`,
  };
  await set(`file:${id}`, file, store);
  await set(INDEX_KEY, [...(await readIndex()), entry], store);
  return entry;
}

export async function removeAvatar(id: string): Promise<void> {
  if (!store) return;
  await del(`file:${id}`, store);
  await set(INDEX_KEY, (await readIndex()).filter(entry => entry.id !== id), store);
}

const MIME: Record<string, string> = { png: "image/png", jpg: "image/jpeg", jpeg: "image/jpeg", webp: "image/webp", json: "application/json" };

interface Unpacked { settings: Record<string, unknown> & { url: string }; urls: string[] }

/** Unzips a Live2D package into object URLs and rewrites model3.json to them. */
async function unpackLive2D(blob: Blob): Promise<Unpacked> {
  const { unzip } = await import("fflate");
  const bytes = new Uint8Array(await blob.arrayBuffer());
  const files = await new Promise<Record<string, Uint8Array>>((resolve, reject) =>
    unzip(bytes, (error, data) => (error ? reject(error) : resolve(data))));
  const names = Object.keys(files).filter(name => !name.startsWith("__MACOSX/"));
  const modelPath = names.find(name => name.toLowerCase().endsWith(".model3.json"));
  if (!modelPath) throw new Error("В архиве нет файла .model3.json (поддерживаются Cubism 3/4)");
  const json = JSON.parse(new TextDecoder().decode(files[modelPath])) as Record<string, unknown>;
  const directory = modelPath.includes("/") ? modelPath.slice(0, modelPath.lastIndexOf("/") + 1) : "";
  const urls: string[] = [];
  const resolve = (relative: unknown): string | undefined => {
    if (typeof relative !== "string") return undefined;
    const parts: string[] = [];
    for (const part of (directory + relative).split("/")) {
      if (part === "..") parts.pop(); else if (part && part !== ".") parts.push(part);
    }
    const data = files[parts.join("/")];
    if (!data) return undefined;
    const extension = relative.split(".").pop()?.toLowerCase() ?? "";
    const url = URL.createObjectURL(new Blob([data as BlobPart], { type: MIME[extension] ?? "application/octet-stream" }));
    urls.push(url);
    return url;
  };
  const refs = (json.FileReferences ?? {}) as Record<string, unknown>;
  const moc = resolve(refs.Moc);
  if (!moc) { urls.forEach(URL.revokeObjectURL); throw new Error("В архиве не найден файл .moc3"); }
  refs.Moc = moc;
  refs.Textures = ((refs.Textures as unknown[]) ?? []).map(resolve).filter(Boolean);
  for (const key of ["Physics", "Pose", "DisplayInfo", "UserData"]) {
    const url = resolve(refs[key]);
    if (url) refs[key] = url; else delete refs[key];
  }
  if (Array.isArray(refs.Expressions)) {
    refs.Expressions = (refs.Expressions as Array<Record<string, unknown>>)
      .map(item => ({ ...item, File: resolve(item.File) })).filter(item => item.File);
  }
  if (refs.Motions && typeof refs.Motions === "object") {
    const motions: Record<string, unknown[]> = {};
    for (const [group, list] of Object.entries(refs.Motions as Record<string, Array<Record<string, unknown>>>)) {
      motions[group] = list.map(item => ({ ...item, File: resolve(item.File), Sound: undefined })).filter(item => item.File);
    }
    refs.Motions = motions;
  }
  json.FileReferences = refs;
  return { settings: { ...json, url: `${location.origin}/imported/${modelPath}` }, urls };
}

/** A loaded avatar plus the object URLs it borrowed. */
export interface MountedAvatar { avatar: Avatar; loaded: boolean; dispose(): void }

export async function mountAvatar(entry: AvatarEntry, canvas: HTMLCanvasElement): Promise<MountedAvatar> {
  const urls: string[] = [];
  // Renderers load on demand: a Live2D user never downloads three.js and vice versa.
  let avatar: Avatar;
  let loadLive2D: ((source: Live2DSource) => Promise<boolean>) | undefined;
  if (entry.kind === "vrm") {
    const { VRMAvatar } = await import("./vrm");
    avatar = new VRMAvatar(canvas);
  } else {
    const { Live2DAvatar, LIVE2D_PROFILES } = await import("./live2d");
    const live2d = new Live2DAvatar(canvas, LIVE2D_PROFILES[entry.id] ? entry.id : "");
    avatar = live2d;
    loadLive2D = source => live2d.loadSource(source);
  }
  const dispose = () => { avatar.destroy(); urls.forEach(url => URL.revokeObjectURL(url)); };
  try {
    let source: Live2DSource = entry.url;
    if (!entry.builtin) {
      if (!store) throw new Error("Хранилище браузера недоступно");
      const blob = await get<Blob>(`file:${entry.id}`, store);
      if (!blob) throw new Error("Файл модели не найден. Импортируйте его заново.");
      if (entry.kind === "vrm") {
        source = URL.createObjectURL(blob);
        urls.push(source);
      } else {
        const unpacked = await unpackLive2D(blob);
        urls.push(...unpacked.urls);
        source = unpacked.settings;
      }
    }
    const loaded = loadLive2D ? await loadLive2D(source) : await avatar.load(source as string);
    return { avatar, loaded, dispose };
  } catch (error) {
    console.error("Avatar mount failed", error);
    return { avatar, loaded: false, dispose };
  }
}
