import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import vm from 'node:vm';
import test from 'node:test';
import ts from 'typescript';

const source = await readFile(new URL('../src/main.ts', import.meta.url), 'utf8');
const ast = ts.createSourceFile('main.ts', source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS);
const selected = ast.statements.filter(node =>
  (ts.isFunctionDeclaration(node) && node.name?.text === 'renderPairingQr') ||
  (ts.isVariableStatement(node) && node.declarationList.declarations.some(d => d.name.getText(ast) === 'pairingCode')));
const js = ts.transpileModule(selected.map(node => node.getText(ast)).join('\n'), {
  compilerOptions: { target: ts.ScriptTarget.ES2022 },
}).outputText;

test('pairing QR uses a reachable HTTPS address and contains only the one-time code', async () => {
  const nodes = new Map([
    ['pair-qr', { hidden: true }],
    ['pair-qr-help', { textContent: '' }],
    ['pair-host', { value: 'https://192.168.1.15:8765' }],
  ]);
  const rendered = [];
  const context = vm.createContext({
    URL, JSON,
    el: id => nodes.get(id),
    QRCode: { toCanvas: async (_canvas, payload) => rendered.push(JSON.parse(payload)) },
    localStorage: { setItem() {} },
  });
  vm.runInContext(js, context);
  vm.runInContext('pairingCode = "ABCD-EFGH"', context);
  await context.renderPairingQr();
  assert.equal(nodes.get('pair-qr').hidden, false);
  assert.equal(rendered.length, 1);
  assert.equal(rendered[0].host, 'https://192.168.1.15:8765');
  assert.equal(rendered[0].code, 'ABCD-EFGH');
  assert.deepEqual(Object.keys(rendered[0]).sort(), ['code', 'host', 'name', 'v']);

  nodes.get('pair-host').value = 'http://192.168.1.15:8765';
  await context.renderPairingQr();
  assert.equal(nodes.get('pair-qr').hidden, true);
  assert.equal(rendered.length, 1);
});
