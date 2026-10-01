import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';
import ts from 'typescript';

const main = readFileSync(new URL('../src/main.ts', import.meta.url), 'utf8');
const start = main.indexOf('function addMemory(');
const end = main.indexOf('\ntype ConsentRequest', start);
const code = ts.transpileModule(main.slice(start, end), {
  compilerOptions: { target: ts.ScriptTarget.ES2022 },
}).outputText;

function stage() {
  const nodes = new Map();
  const node = () => ({
    dataset: {}, children: [], textContent: '',
    classList: {
      values: new Set(),
      add(value) { this.values.add(value); },
      remove(value) { this.values.delete(value); },
      toggle(value, on) { on ? this.values.add(value) : this.values.delete(value); },
      contains(value) { return this.values.has(value); },
    },
    append(...children) { this.children.push(...children); },
    prepend(child) { this.children = this.children.filter(item => item !== child); this.children.unshift(child); child.parent = this; },
    replaceChildren() { this.children = []; },
    remove() { this.parent.children = this.parent.children.filter(item => item !== this); },
  });
  const el = id => { if (!nodes.has(id)) nodes.set(id, node()); return nodes.get(id); };
  const client = {
    memories: async () => [{ id: 'mem1', content: 'Пользователь любит чай', category: 'preferences', status: 'active' }],
    audit: async () => [{ id: 'aud1', action: 'provider.call', reason: 'dialog_turn', provider: 'remote-llm', categories: ['current_text'], result: 'ok' }],
  };
  const context = vm.createContext({
    el, document: { createElement: node }, client, identityId: 'owner',
    providerLocal: new Map([['remote-llm', false]]), ledgerLoaded: false,
  });
  vm.runInContext(code, context);
  return { context, el };
}

test('saved memories and remote model calls populate both tabs on connect', async () => {
  const { context, el } = stage();
  await context.refreshMemory();
  await context.loadLedger();
  assert.equal(el('memory-list').children.length, 1);
  assert.equal(el('memory-list').children[0].children[1].textContent, 'Пользователь любит чай');
  assert.equal(el('ledger-list').children.length, 1);
  assert.equal(el('ledger-list').children[0].dataset.remote, 'true');
  assert.match(el('ledger-list').children[0].children[1].textContent, /Передано вовне: current_text/);
  assert.equal(el('memory-empty').classList.contains('pane--hidden'), true);
  assert.equal(el('ledger-empty').classList.contains('pane--hidden'), true);
  await context.loadLedger();
  assert.equal(el('ledger-list').children.length, 1);
});
