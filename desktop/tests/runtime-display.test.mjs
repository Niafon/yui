import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';
import ts from 'typescript';

const main = readFileSync(new URL('../src/main.ts', import.meta.url), 'utf8');
const start = main.indexOf('function renderInferenceDecision(');
const end = main.indexOf('\nasync function refreshInference(', start);
const code = ts.transpileModule(main.slice(start, end), {
  compilerOptions: { target: ts.ScriptTarget.ES2022 },
}).outputText;

function stage() {
  const nodes = new Map();
  const el = id => {
    if (!nodes.has(id)) nodes.set(id, {
      dataset: {}, value: '', checked: false, textContent: '',
      replaceChildren() { this.children = []; },
      append(child) { (this.children ??= []).push(child); },
    });
    return nodes.get(id);
  };
  const context = vm.createContext({
    el, document: { createElement: () => ({}) },
    setInferenceControlsEnabled() {}, updatePrivacyTag() {}, formatPercent: value => `${value}%`,
    lastInferenceStatus: undefined, latestLLMDecision: undefined,
    modelSettings: undefined,
  });
  vm.runInContext(code, context);
  return { context, el };
}

const llm = { kind: 'llm', provider_id: 'deepseek', model_family: 'deepseek/flash', backend: 'remote', reason: 'selected' };
const tts = { kind: 'tts', provider_id: 'speech', model_family: 'silero-v5', backend: 'cpu', reason: 'speech' };
const status = {
  preferences: { mode: 'manual', locked_model: 'deepseek/flash', minimum_quality: 55, allow_auto_downgrade: true },
  models: [{ kind: 'llm', provider_id: 'deepseek', model_family: 'deepseek/flash', backend: 'remote', local: false }],
  telemetry: { cpu_percent: 1, gpu_percent: 2, ram_free_mb: 10, vram_free_mb: 20, game_active: false },
  last_decision: tts,
  last_llm_decision: llm,
};

test('speech selection cannot replace the working LLM on status refresh or live frame', () => {
  const { context, el } = stage();
  context.renderInference(status);
  assert.equal(el('inference-model').value, 'deepseek/flash');
  assert.equal(el('runtime-model').textContent, 'deepseek/flash');
  assert.equal(el('bar-runtime').textContent, 'deepseek/flash · remote');
  context.renderInferenceDecision(tts);
  assert.equal(el('runtime-model').textContent, 'deepseek/flash');
});

test('a new lock is shown as pending until the LLM is actually called', () => {
  const { context, el } = stage();
  context.renderInference({ ...status, last_llm_decision: undefined });
  assert.equal(el('runtime-model').textContent, 'ещё не вызывалась');
  assert.match(el('runtime-reason').textContent, /Выбрана deepseek\/flash/);
  assert.equal(el('bar-runtime').textContent, 'ожидает вызова');
});

test('an older status response does not replace a newer live LLM decision', () => {
  const { context, el } = stage();
  context.renderInferenceDecision({ ...llm, at: '2026-09-22T10:00:00Z' });
  context.renderInference({
    ...status,
    last_llm_decision: { ...llm, provider_id: 'old-local', model_family: 'qwen', at: '2026-09-22T09:00:00Z' },
  });
  assert.equal(el('runtime-model').textContent, 'deepseek/flash');
});

test('a selected external model changes the privacy indicator', () => {
  const source = main.slice(main.indexOf('function updatePrivacyTag('), main.indexOf('\nconst INFERENCE_CONTROL_IDS'));
  const privacyCode = ts.transpileModule(source, {
    compilerOptions: { target: ts.ScriptTarget.ES2022 },
  }).outputText;
  const tag = {};
  const context = vm.createContext({
    el: () => tag,
    providerLocal: new Map([['deepseek', false]]),
    remoteDefaultProvider: false,
    lastInferenceStatus: { models: status.models },
  });
  vm.runInContext(privacyCode, context);
  context.updatePrivacyTag({ preferred_provider: 'deepseek', locked_model: 'deepseek/flash' });
  assert.equal(tag.textContent, 'выбрана внешняя модель');
  assert.match(tag.className, /tag--remote/);
});
