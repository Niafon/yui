/**
 * Loads the wLipSync MFCC worklet. Its processor and WASM are served as
 * same-origin files (not the inlined data: URL build) so the Tauri content
 * policy, which only allows 'self' scripts, accepts them.
 */
import processorUrl from "wlipsync/audio-processor.js?url";
import wasmUrl from "wlipsync/wlipsync.wasm?url";
import type { LipSyncNode } from "./speech";

const PROFILE_URL = "/assets/lipsync/profile.json";
let wasm: Promise<WebAssembly.Module> | undefined;
const registered = new WeakSet<BaseAudioContext>();

async function compileWasm(): Promise<WebAssembly.Module> {
  const response = await fetch(wasmUrl);
  if (!response.ok) throw new Error(`wlipsync.wasm: ${response.status}`);
  // compileStreaming needs application/wasm; some static servers differ.
  return WebAssembly.compile(await response.arrayBuffer());
}

export async function loadWLipSync(context: AudioContext): Promise<LipSyncNode> {
  if (!context.audioWorklet) throw new Error("AudioWorklet needs a secure context (localhost or HTTPS)");
  const lib = await import("wlipsync/wlipsync.js");
  wasm ??= compileWasm().catch(error => { wasm = undefined; throw error; });
  lib.configuration.wasmModule = await wasm;
  if (!registered.has(context)) {
    await context.audioWorklet.addModule(processorUrl);
    registered.add(context);
  }
  const profile = await fetch(PROFILE_URL).then(response => {
    if (!response.ok) throw new Error(`lip-sync profile: ${response.status}`);
    return response.json();
  });
  return await lib.createWLipSyncNode(context, profile) as unknown as LipSyncNode;
}
