/**
 * Character card import (SillyTavern / Character Card V2 and V3). A card is
 * JSON, either standalone or embedded in a PNG "chara"/"ccv3" tEXt chunk as
 * base64. Only text fields are read; nothing is executed or fetched.
 */

export interface CardFields { name: string; style: string; relationship: string; greeting: string }

interface RawCard {
  name?: string; description?: string; personality?: string; scenario?: string; first_mes?: string;
  data?: RawCard;
}

const clip = (text: string, limit: number) => (text.length > limit ? `${text.slice(0, limit - 1).trimEnd()}…` : text);
const clean = (text: unknown) => (typeof text === "string" ? text.replace(/\{\{char\}\}/gi, "").replace(/\s+/g, " ").trim() : "");

function readPngText(bytes: Uint8Array): string | undefined {
  const signature = [137, 80, 78, 71, 13, 10, 26, 10];
  if (!signature.every((value, index) => bytes[index] === value)) return undefined;
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  const latin1 = new TextDecoder("latin1");
  let found: Record<string, string> = {};
  for (let offset = 8; offset + 8 <= bytes.length;) {
    const length = view.getUint32(offset);
    const type = latin1.decode(bytes.subarray(offset + 4, offset + 8));
    const data = bytes.subarray(offset + 8, offset + 8 + length);
    if (type === "tEXt") {
      const split = data.indexOf(0);
      if (split > 0) found[latin1.decode(data.subarray(0, split))] = latin1.decode(data.subarray(split + 1));
    }
    if (type === "IEND") break;
    offset += 12 + length;
  }
  const encoded = found.ccv3 ?? found.chara;
  if (!encoded) return undefined;
  const binary = atob(encoded);
  return new TextDecoder().decode(Uint8Array.from(binary, char => char.charCodeAt(0)));
}

export async function parseCharacterCard(file: File): Promise<CardFields> {
  if (file.size > 20 * 1024 * 1024) throw new Error("Файл карточки больше 20 МБ");
  const bytes = new Uint8Array(await file.arrayBuffer());
  const text = readPngText(bytes) ?? new TextDecoder().decode(bytes);
  let raw: RawCard;
  try { raw = JSON.parse(text) as RawCard; } catch { throw new Error("Не удалось прочитать карточку: нужен .json или .png с данными персонажа"); }
  const card = raw.data && typeof raw.data === "object" ? { ...raw, ...raw.data } : raw;
  const name = clip(clean(card.name), 40);
  if (!name) throw new Error("В карточке нет имени персонажа");
  const style = clip([clean(card.personality), clean(card.description)].filter(Boolean).join(". "), 600);
  return { name, style, relationship: clip(clean(card.scenario), 120), greeting: clean(card.first_mes) };
}
