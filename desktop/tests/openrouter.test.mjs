import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';
import ts from 'typescript';

const main = readFileSync(new URL('../src/main.ts', import.meta.url), 'utf8');
const start = main.indexOf('async function saveInferencePreferences(');
const end = main.indexOf('\nfunction ', start);
const code = ts.transpileModule(main.slice(start, end), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText;

test('selecting a remote model locks its provider; switching back clears it', async () => {
  let submitted;
  const fields = {
    'inference-mode': { value: 'auto' }, 'inference-model': { value: 'deepseek/deepseek-v4.1-flash' },
    'inference-quality': { value: '55' }, 'inference-downgrade': { checked: true }, 'runtime-reason': {},
  };
  const context = vm.createContext({
    inferenceAvailable: true, el: id => fields[id], renderInference() {},
    lastInferenceStatus: { models: [
      { kind: 'llm', local: false, provider_id: 'openrouter-deepseek-flash', model_family: 'deepseek/deepseek-v4.1-flash' },
      { kind: 'llm', local: true, provider_id: 'local', model_family: 'qwen' },
    ] },
    client: { updateInferencePreferences: async p => { submitted = p; return {}; } },
  });
  vm.runInContext(code, context);
  await context.saveInferencePreferences();
  assert.equal(submitted.mode, 'manual');
  assert.equal(submitted.preferred_provider, 'openrouter-deepseek-flash');
  fields['inference-model'].value = 'qwen';
  await context.saveInferencePreferences();
  assert.equal(submitted.preferred_provider, '');
  assert.equal(submitted.mode, 'auto');
});
