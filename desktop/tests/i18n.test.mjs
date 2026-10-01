import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';
import { load } from './bundle.mjs';

const { dictionary, t, setLang, has } = await load('i18n.ts');
const placeholders = text => [...text.matchAll(/\{(\w+)\}/g)].map(m => m[1]).sort();

test('every string has both languages with matching placeholders', () => {
  for (const [key, [ru, en]] of Object.entries(dictionary)) {
    assert.ok(ru.trim() && en.trim(), `${key} is empty`);
    assert.deepEqual(placeholders(ru), placeholders(en), `${key} placeholders differ`);
    assert.ok(!/[Ѐ-ӿ]/.test(en), `${key} English text contains Cyrillic`);
  }
});

test('markup only references existing keys', () => {
  for (const file of ['../index.html', '../avatar.html']) {
    const html = readFileSync(new URL(file, import.meta.url), 'utf8');
    for (const [, key] of html.matchAll(/data-i18n(?:-placeholder|-title|-aria)?="([\w.]+)"/g)) {
      assert.ok(has(key), `${file}: unknown key ${key}`);
    }
  }
});

test('switching language changes text and fills parameters', () => {
  setLang('en');
  assert.equal(t('models.count', { visible: 2, total: 5 }), '2 of 5 models');
  setLang('ru');
  assert.equal(t('models.count', { visible: 2, total: 5 }), '2 из 5 моделей');
});
