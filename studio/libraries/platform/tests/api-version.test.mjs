import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

test('platform uses versioned login/profile endpoints and attaches human tokens only to Studio', () => {
  const service = readFileSync(new URL('../src/auth.service.ts', import.meta.url), 'utf8');
  const interceptor = readFileSync(new URL('../src/auth.interceptor.ts', import.meta.url), 'utf8');
  assert.ok(service.includes("fetch('/api/v1/auth/config')"));
  assert.ok(service.includes("'/api/v1/studio/me'"));
  assert.ok(interceptor.includes("!request.url.startsWith('/api/v1/studio/')"));
});
