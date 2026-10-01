import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';
import ts from 'typescript';

import { load } from './bundle.mjs';

const { CoreClient } = await load('api.ts');

test('tool approval uses the pending endpoint and an honest button factor', async (t) => {
  const original = globalThis.fetch;
  t.after(() => { globalThis.fetch = original; });
  const calls = [];
  globalThis.fetch = async (url, options) => {
    calls.push({ url, options });
    return new Response(JSON.stringify({ result: 'done', approved: true }), { status: 200 });
  };
  const client = new CoreClient('http://localhost:8765', 'test-token');
  await client.confirmTool('tool/id', true, 'session');
  assert.equal(calls[0].url, 'http://localhost:8765/v1/tools/pending/tool%2Fid');
  assert.equal(calls[0].options.headers.Authorization, 'Bearer test-token');
  assert.deepEqual(JSON.parse(calls[0].options.body), { approved: true, method: 'button', session_id: 'session' });
  await client.pendingTools();
  assert.equal(calls[1].url, 'http://localhost:8765/v1/tools/pending');
});

class Socket extends EventTarget {
  static OPEN = 1;
  static instances = [];
  readyState = 1;
  sent = [];
  constructor(url) { super(); this.url = url; Socket.instances.push(this); }
  close() { this.readyState = 3; }
  send(value) { this.sent.push(value); }
  message(value) { this.dispatchEvent(new MessageEvent('message', { data: value })); }
}

function setup(t) {
  const original = globalThis.WebSocket;
  globalThis.WebSocket = Socket;
  t.after(() => { globalThis.WebSocket = original; });
  Socket.instances = [];
  return new CoreClient('http://localhost:8765', 'test');
}

test('reconnecting ignores delayed frames and close events from old sockets', (t) => {
  const client = setup(t);
  const frames = [];
  let closed = 0;
  client.connect('old', f => frames.push(f), () => closed++);
  const [oldControl, oldData] = Socket.instances;
  client.connect('new', f => frames.push(f), () => closed++);
  assert.equal(oldControl.readyState, 3);
  assert.equal(oldData.readyState, 3);
  oldData.message('{"type":"old"}');
  oldControl.dispatchEvent(new Event('close'));
  Socket.instances[3].message('{"type":"new"}');
  assert.deepEqual(frames, [{ type: 'new' }]);
  assert.equal(closed, 0);
});

test('losing either plane closes its peer and reports disconnection once', (t) => {
  const client = setup(t);
  let closed = 0;
  client.connect('session', () => {}, () => closed++);
  const [control, data] = Socket.instances;
  control.dispatchEvent(new Event('close'));
  assert.equal(data.readyState, 3);
  data.dispatchEvent(new Event('close'));
  assert.equal(closed, 1);
  assert.throws(() => client.sendText('hello'), /Соединение/);
});

test('explicit close suppresses callbacks and rejects subsequent sends', (t) => {
  const client = setup(t);
  let callbacks = 0;
  client.connect('session', () => callbacks++, () => callbacks++);
  client.sendText('hello');
  assert.deepEqual(JSON.parse(Socket.instances[1].sent[0]), { type: 'text', text: 'hello' });
  client.close();
  for (const socket of Socket.instances) {
    socket.message('{"type":"late"}');
    socket.dispatchEvent(new Event('close'));
  }
  assert.equal(callbacks, 0);
  assert.throws(() => client.sendAudio(new ArrayBuffer(2)), /Соединение/);
});
