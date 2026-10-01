/** Bundled avatars. Kept free of renderer imports so any module can use it. */

export type AvatarKind = "vrm" | "live2d";

export interface AvatarEntry {
  id: string;
  label: string;
  kind: AvatarKind;
  /** URL for bundled models; empty for imported ones. */
  url: string;
  builtin: boolean;
  note?: string;
}

export const BUILTIN_AVATARS: AvatarEntry[] = [
  { id: "shino", label: "Shino", kind: "vrm", url: "/assets/vrm/shino.vrm", builtin: true, note: "VRoid · CC0" },
  { id: "victoria", label: "Victoria", kind: "vrm", url: "/assets/vrm/victoria.vrm", builtin: true, note: "VRoid · CC0" },
  { id: "orion", label: "Orion", kind: "vrm", url: "/assets/vrm/default.vrm", builtin: true, note: "VRM · CC0" },
  { id: "hiyori", label: "Hiyori", kind: "live2d", url: "/assets/live2d/hiyori/Hiyori.model3.json", builtin: true, note: "Live2D sample" },
  { id: "natori", label: "Natori", kind: "live2d", url: "/assets/live2d/natori/Natori.model3.json", builtin: true, note: "Live2D sample" },
  { id: "haru", label: "Haru", kind: "live2d", url: "/assets/live2d/haru/Haru.model3.json", builtin: true, note: "Live2D sample" },
  { id: "mao", label: "Mao", kind: "live2d", url: "/assets/live2d/mao/Mao.model3.json", builtin: true, note: "Live2D sample · гласные" },
];

export const DEFAULT_AVATAR = "shino";
const LEGACY: Record<string, string> = { live2d: "haru", vrm: "orion" };

export function normalizeAvatarId(id: string | null | undefined): string {
  if (!id) return DEFAULT_AVATAR;
  return LEGACY[id] ?? id;
}

