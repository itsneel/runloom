import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { tmpdir } from 'node:os';
import { fileURLToPath } from 'node:url';
import test from 'node:test';
import { oneTimeGreeting } from './one-time-greeting.mjs';

test('one-time request uses a future instant, pinned version and paused default', () => {
  const value = oneTimeGreeting(5, new Date('2030-01-01T00:00:00Z'));
  assert.equal(value.at, '2030-01-01T00:05:00.000Z');
  assert.equal(value.version, 1);
  assert.equal(value.definition_id, 'parent-with-subworkflow');
  assert.equal(value.paused, true);
  assert.deepEqual(value.input, { name: 'Avery' });
  assert.equal('interval_seconds' in value, false);
  assert.equal('cron' in value, false);
});

test('invalid delays fail without emitting a request', () => {
  for (const delay of [0, -1, 1.5, 1441, NaN, Infinity]) assert.throws(() => oneTimeGreeting(delay));
  const result = spawnSync(process.execPath, [fileURLToPath(new URL('./one-time-greeting.mjs', import.meta.url)), 'bad'], { encoding: 'utf8' });
  assert.equal(result.status, 1);
  assert.equal(result.stdout, '');
});

test('CLI works outside the repository and emits JSON only', () => {
  const before = Date.now();
  const value = JSON.parse(execFileSync(process.execPath, [fileURLToPath(new URL('./one-time-greeting.mjs', import.meta.url))], { cwd: tmpdir(), encoding: 'utf8' }));
  assert.ok(Date.parse(value.at) >= before + 5 * 60000);
  assert.ok(Date.parse(value.at) <= Date.now() + 5 * 60000);
});
