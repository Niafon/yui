import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import vm from 'node:vm';
import test from 'node:test';
import ts from 'typescript';

// Run the actual event handlers without loading WebGL or opening a live session.
const source = await readFile(new URL('../src/main.ts', import.meta.url), 'utf8');
const ast = ts.createSourceFile('main.ts', source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS);
const functions = new Set(['askConsent', 'showConsent', 'askTool', 'showToolResult', 'clearRequests', 'handleFrame']);
const variables = new Set(['consentQueue', 'toolCards', 'completedTools']);
const selected = ast.statements.filter(node =>
  (ts.isFunctionDeclaration(node) && functions.has(node.name?.text)) ||
  (ts.isVariableStatement(node) && node.declarationList.declarations.some(d => variables.has(d.name.getText(ast)))));
const js = ts.transpileModule(selected.map(n => n.getText(ast)).join('\n'), {
  compilerOptions: { target: ts.ScriptTarget.ES2022 },
}).outputText;

class Element {
  textContent = '';
  disabled = false;
  children = [];
  hidden = new Set();
  classList = { add: v => this.hidden.add(v), remove: v => this.hidden.delete(v) };
  append(...children) { this.children.push(...children); }
  after(node) { this.actions = node; }
}

function fixture() {
  const turns = [];
  const requests = [];
  const nodes = new Map();
  const timers = new Map();
  let timerId = 0;
  let stopped = 0;
  const context = vm.createContext({
    client: {
      confirmTool: async (...args) => { requests.push(args); return { result: args[1] ? 'Создано' : 'Отменено' }; },
      resolvePermission: async (...args) => { requests.push(args); },
    },
    sessionId: 'session',
    streamingTurn: new Element(),
    document: { createElement: () => new Element() },
    el: id => { if (!nodes.has(id)) nodes.set(id, new Element()); return nodes.get(id); },
    addTurn: (who, text) => { const node = new Element(); node.textContent = text; turns.push({ who, node }); return node; },
    stopSpeech: () => { stopped++; },
    window: { setTimeout: fn => { timers.set(++timerId, fn); return timerId; } },
    clearTimeout: id => timers.delete(id),
  });
  vm.runInContext(js, context);
  return { context, turns, requests, nodes, timers, stopped: () => stopped };
}

const pending = { id: 'invocation', tool: 'reminder.create', description: 'Создать напоминание', args: { text: 'Позвонить', in_minutes: 5 }, method: 'button', expires_at: '2099-01-01T00:00:00Z' };
const tick = () => new Promise(resolve => setImmediate(resolve));

test('tool confirmation has real controls, sends approval and shows one result', async () => {
  const f = fixture();
  f.context.handleFrame({ type: 'tool.confirm', payload: pending });
  f.context.handleFrame({ type: 'tool.confirm', payload: pending });
  assert.equal(f.turns.length, 1);
  assert.match(f.turns[0].node.textContent, /Позвонить/);
  const [approve, reject] = f.turns[0].node.actions.children;
  assert.equal(approve.textContent, 'Подтвердить');
  approve.onclick();
  await tick();
  assert.deepEqual(f.requests, [['invocation', true, 'session']]);
  assert.equal(approve.disabled, true);
  assert.equal(reject.disabled, true);
  assert.equal(f.turns[1].node.textContent, 'Создано');
  f.context.handleFrame({ type: 'tool.result', payload: { invocation_id: 'invocation', result: 'Создано' } });
  assert.equal(f.turns.length, 2);
  assert.equal(f.timers.size, 0);
});

test('rejection and retry after a failed HTTP request remain usable', async () => {
  const f = fixture();
  f.context.client.confirmTool = async () => { throw new Error('offline'); };
  f.context.askTool(pending);
  const [approve, reject] = f.turns[0].node.actions.children;
  approve.onclick(); await tick();
  assert.equal(approve.disabled, false);
  assert.equal(reject.disabled, false);
  assert.match(f.turns[1].node.textContent, /offline/);
  f.context.client.confirmTool = async (...args) => { f.requests.push(args); return { result: 'Отменено' }; };
  reject.onclick(); await tick();
  assert.deepEqual(f.requests, [['invocation', false, 'session']]);
  assert.equal(f.turns.at(-1).node.textContent, 'Отменено');
});

test('strong factors cannot be replaced with a button and stale requests expire', () => {
  const f = fixture();
  f.context.askTool({ ...pending, method: 'pin' });
  const [approve, reject] = f.turns[0].node.actions.children;
  assert.equal(approve.disabled, true);
  assert.equal(reject.disabled, false);
  [...f.timers.values()][0]();
  assert.equal(reject.disabled, true);
  assert.match(f.turns[0].node.textContent, /истёк/);
});

test('reminder events appear and interruption clears active playback and text', () => {
  const f = fixture();
  f.context.handleFrame({ type: 'proactive.reminder', payload: { text: 'Пора сделать перерыв' } });
  assert.equal(f.turns[0].who, 'Напоминание');
  assert.equal(f.turns[0].node.textContent, 'Пора сделать перерыв');
  f.context.handleFrame({ type: 'barge_in' });
  assert.equal(f.stopped(), 1);
  assert.equal(f.context.streamingTurn, undefined);
});

test('consent requests queue; allow once sends the non-persistent answer', async () => {
  const f = fixture();
  f.context.askConsent({ id: 'first', category: 'medical', provider: 'one' });
  f.context.askConsent({ id: 'second', category: 'contacts', provider: 'two' });
  assert.match(f.nodes.get('consent-text').textContent, /medical/);
  f.nodes.get('consent-allow').onclick(); await tick();
  assert.deepEqual(f.requests, [['first', true, false]]);
  assert.match(f.nodes.get('consent-text').textContent, /contacts/);
  f.nodes.get('consent-deny').onclick(); await tick();
  assert.deepEqual(f.requests[1], ['second', false, true]);
  assert.ok(f.nodes.get('consent').hidden.has('consent--hidden'));
});

test('disconnect disables old tool controls before a different session connects', async () => {
  const f = fixture();
  f.context.askTool(pending);
  const [approve, reject] = f.turns[0].node.actions.children;
  f.context.clearRequests();
  assert.equal(approve.disabled, true);
  assert.equal(reject.disabled, true);
  approve.onclick(); await tick();
  assert.deepEqual(f.requests, []);
});
