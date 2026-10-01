// Open /tests/vad.html with `npm run dev` in a browser started with a fake
// microphone (Chromium: --use-fake-device-for-media-stream
// --use-file-for-fake-audio-capture=speech.wav). Runs the hands-free
// microphone with the Silero engine and reports the utterances it sent.
import { Microphone } from "../src/mic";

const results = document.querySelector("#results")!;
const engine = (new URLSearchParams(location.search).get("engine") ?? "silero") as "silero" | "energy";
const sent: Array<{ type: string; bytes?: number; rate?: number }> = [];
let bytes = 0;
const statuses: string[] = [];
const errors: string[] = [];
const mic = new Microphone(
  () => ({
    sendAudio(pcm: ArrayBuffer) { bytes += pcm.byteLength; },
    send(message: Record<string, unknown>) {
      sent.push({ type: String(message.type), bytes, rate: Number(message.sample_rate ?? 0) });
      bytes = 0;
    },
  }),
  { status: text => statuses.push(text), level: () => undefined, error: message => errors.push(message), speechStart: () => undefined },
  () => ({ sensitivity: 1, bargeIn: false, engine, isAssistantSpeaking: () => false }),
);
await mic.start("handsfree");
await new Promise(resolve => setTimeout(resolve, 15000));
mic.stop(false);
const report = { engine, utterances: sent.filter(item => item.type === "audio.end"), errors, statuses: [...new Set(statuses)] };
results.textContent = JSON.stringify(report, null, 2);
(window as unknown as { vadReport: unknown }).vadReport = report;
