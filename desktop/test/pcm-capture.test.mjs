import assert from "node:assert/strict";
import fs from "node:fs";
import vm from "node:vm";

const source = fs.readFileSync(new URL("../public/pcm-capture.js", import.meta.url), "utf8");
let registered;
class Processor {
  constructor() { this.port = { postMessage(message, transfer) { registered.message = message; registered.transfer = transfer; } }; }
}
const context = vm.createContext({
  AudioWorkletProcessor: Processor,
  registerProcessor(name, ctor) { registered = { name, ctor }; },
});
vm.runInContext(source, context, { filename: "pcm-capture.js" });

assert.equal(registered.name, "pcm-capture");
const processor = new registered.ctor();
assert.equal(processor.process([[[0, 0.5, -1, 2]]]), true);
assert.deepEqual(Array.from(new Int16Array(registered.message)), [0, 16384, -32767, 32767]);
assert.equal(registered.transfer.length, 1);
assert.equal(processor.process([[[]]]), true);
assert.equal(registered.message.byteLength, 0);
console.log("pcm-capture: PASS");
