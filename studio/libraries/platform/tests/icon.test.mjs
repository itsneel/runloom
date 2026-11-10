import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import test from 'node:test';

test('icons respect consumer sizes without a fixed flex basis or baseline gap', () => {
  const css = readFileSync(new URL('../src/ui-icon.scss', import.meta.url), 'utf8');
  assert.match(css, /flex:\s*none/);
  assert.doesNotMatch(css, /flex:\s*0\s+0\s+\d/);
  assert.match(css, /width:\s*var\(--rl-icon-size,\s*20px\)/);
  assert.match(css, /height:\s*var\(--rl-icon-size,\s*20px\)/);
  assert.match(css, /svg\s*\{\s*display:\s*block/);
});
