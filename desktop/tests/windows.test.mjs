import assert from 'node:assert/strict';
import test from 'node:test';
import { load } from './bundle.mjs';

// One in-process "browser": every BroadcastChannel with the same name hears the others.
const channels = new Set();
globalThis.BroadcastChannel = class {
  constructor(name) { this.name = name; channels.add(this); }
  postMessage(data) { for (const other of channels) if (other !== this && other.name === this.name) queueMicrotask(() => other.onmessage?.({ data })); }
  close() { channels.delete(this); }
};
const timers = [];
globalThis.window = {
  setInterval: (fn) => { timers.push(fn); return timers.length; }, clearInterval() {},
  setTimeout: (fn, ms) => setTimeout(fn, ms), addEventListener() {},
};
const { WindowHub } = await load('windows.ts');
const flush = () => new Promise(r => setTimeout(r, 5));

test('the avatar window owns audio while it is open, then main takes over', async () => {
  const main = new WindowHub('main', () => {});
  const chat = new WindowHub('chat', () => {});
  await flush();
  assert.equal(main.ownsAudio, true);
  assert.equal(chat.ownsAudio, false);
  const avatar = new WindowHub('avatar', () => {});
  await flush();
  assert.equal(avatar.ownsAudio, true);
  assert.equal(main.ownsAudio, false, 'lip sync must run where the avatar is rendered');
  assert.equal(main.hasPeer('avatar'), true);
  avatar.close();
  await flush();
  assert.equal(main.ownsAudio, true);
  assert.equal(main.hasPeer('avatar'), false);
  main.close(); chat.close();
});

test('detached windows join the session the main window announces', async () => {
  const main = new WindowHub('main', () => {});
  main.setSession('ses_main');
  const avatar = new WindowHub('avatar', () => {});
  avatar.setSession('ses_old');
  const chat = new WindowHub('chat', () => {});
  assert.equal(await chat.discover(20), 'ses_main', 'main is the session authority');
  main.close(); avatar.close(); chat.close();
});

test('typed user turns reach sibling windows of the same session only', async () => {
  const main = new WindowHub('main', () => {});
  const chat = new WindowHub('chat', () => {});
  main.setSession('ses_1'); chat.setSession('ses_1');
  const seen = [];
  main.onUserText = (session, text) => seen.push([session, text]);
  chat.shareUserText('привет');
  await flush();
  assert.deepEqual(seen, [['ses_1', 'привет']]);
  main.close(); chat.close();
});
