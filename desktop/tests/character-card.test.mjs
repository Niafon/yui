import assert from 'node:assert/strict';
import test from 'node:test';
import { deflateSync } from 'node:zlib';
import { load } from './bundle.mjs';

const { parseCharacterCard } = await load('character-card.ts');
const file = (bytes, name) => ({ name, size: bytes.length, arrayBuffer: async () => bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) });

function crc32(buf) {
  let c, crc = 0xffffffff;
  for (const byte of buf) { c = (crc ^ byte) & 0xff; for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1; crc = (crc >>> 8) ^ c; }
  return (crc ^ 0xffffffff) >>> 0;
}
function chunk(type, data) {
  const out = Buffer.alloc(12 + data.length);
  out.writeUInt32BE(data.length, 0); out.write(type, 4, 'latin1'); data.copy(out, 8);
  out.writeUInt32BE(crc32(Buffer.concat([Buffer.from(type, 'latin1'), data])), 8 + data.length);
  return out;
}
function png(text) {
  const header = Buffer.alloc(13); header.writeUInt32BE(1, 0); header.writeUInt32BE(1, 4); header[8] = 8; header[9] = 2;
  return new Uint8Array(Buffer.concat([
    Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), chunk('IHDR', header),
    chunk('tEXt', Buffer.concat([Buffer.from('chara\0', 'latin1'), Buffer.from(Buffer.from(JSON.stringify(text)).toString('base64'), 'latin1')])),
    chunk('IDAT', deflateSync(Buffer.from([0, 0, 0, 0]))), chunk('IEND', Buffer.alloc(0)),
  ]));
}

test('V2 card in a PNG fills name, style and relationship', async () => {
  const card = { spec: 'chara_card_v2', data: { name: 'Мику', personality: 'весёлая', description: '{{char}} любит петь', scenario: 'подруга по учёбе', first_mes: 'Привет!' } };
  const result = await parseCharacterCard(file(png(card), 'miku.png'));
  assert.deepEqual(result, { name: 'Мику', style: 'весёлая. любит петь', relationship: 'подруга по учёбе', greeting: 'Привет!' });
});

test('plain JSON cards work and long text is clipped to core limits', async () => {
  const bytes = new TextEncoder().encode(JSON.stringify({ name: 'A'.repeat(80), description: 'x'.repeat(900) }));
  const result = await parseCharacterCard(file(bytes, 'card.json'));
  assert.equal(result.name.length, 40);
  assert.equal(result.style.length, 600);
});

test('files without a card are rejected with a readable error', async () => {
  await assert.rejects(parseCharacterCard(file(new TextEncoder().encode('not json'), 'x.json')), /карточку/);
  await assert.rejects(parseCharacterCard(file(new TextEncoder().encode('{"description":"no name"}'), 'x.json')), /имени/);
});
