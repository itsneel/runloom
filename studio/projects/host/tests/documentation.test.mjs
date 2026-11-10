import test from 'node:test';
import assert from 'node:assert/strict';
import { loadTypeScript } from './helpers/load-typescript.mjs';

const { documentationOrigin } = loadTypeScript(
  new URL('../src/app/core/config/documentation.service.ts', import.meta.url),
  {
    modules: { '@angular/core': { Injectable: () => (value) => value } },
    globals: { URL },
  },
);
test('documentation URLs are independent HTTP(S) origins, never scripts, credentials or subpaths', () => {
  assert.equal(documentationOrigin('https://docs.example.com/'), 'https://docs.example.com');
  assert.equal(documentationOrigin('http://localhost:4400'), 'http://localhost:4400');
  for (const value of [
    undefined,
    '/docs',
    'javascript:alert(1)',
    '//example.com',
    'https://user:pass@example.com',
    'https://example.com/docs',
    'https://example.com/?token=x',
    'https://example.com/#x',
  ]) {
    assert.equal(documentationOrigin(value), undefined);
  }
});
