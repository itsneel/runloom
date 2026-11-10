import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, readFile, writeFile, stat, rm, symlink } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { ensureLocalWorkflowKey, localWorkflowKeyPath } from '../local-workflow-key.mjs';

async function fixture(t) {
  const directory = await mkdtemp(join(tmpdir(), 'runloom-key-test-'));
  t.after(() => rm(directory, { recursive: true, force: true }));
  return directory;
}
test('local key is created outside the repo, reused without rotation, and privately stored', async t => {
  const directory = await fixture(t);
  const first = await ensureLocalWorkflowKey({}, directory);
  assert.equal(first.created, true);
  assert.equal(first.path, join(directory, '.config/runloom/local/workflow-encryption-key'));
  const original = await readFile(first.path);
  assert.equal(original.length, 32);
  const second = await ensureLocalWorkflowKey({}, directory);
  assert.equal(second.created, false);
  assert.ok(original.equals(await readFile(second.path)), 'startup must preserve key bytes');
  assert.ok(!('value' in second));
  if (process.platform !== 'win32') {
    assert.equal((await stat(first.path)).mode & 0o777, 0o640);
    assert.equal((await stat(join(directory, '.config/runloom/local'))).mode & 0o777, 0o700);
  }
});
test('concurrent creation publishes exactly one complete key', async t => {
  const directory = await fixture(t);
  const results = await Promise.all(Array.from({ length: 8 }, () => ensureLocalWorkflowKey({}, directory)));
  assert.equal(results.filter(result => result.created).length, 1);
  assert.equal((await readFile(results[0].path)).length, 32);
});
test('existing invalid keys are never replaced', async t => {
  const directory = await fixture(t);
  const key = await ensureLocalWorkflowKey({}, directory);
  await writeFile(key.path, 'invalid');
  await assert.rejects(ensureLocalWorkflowKey({}, directory), /not replaced/);
  assert.equal(await readFile(key.path, 'utf8'), 'invalid');
});
test('explicit keys are reused without changing permissions; absent keys are not generated', async t => {
  const directory = await fixture(t);
  const path = join(directory, 'external-key');
  const env = { RUNLOOM_SECRETS_KEY_PATH: path };
  await assert.rejects(ensureLocalWorkflowKey(env, directory), /will not generate/);
  await writeFile(path, Buffer.alloc(32, 42), { mode: 0o600 });
  assert.equal((await ensureLocalWorkflowKey(env, directory)).created, false);
  assert.equal((await stat(path)).mode & 0o777, 0o600);
});
test('managed symlinks, relative overrides and production auto-generation are rejected', async t => {
  const directory = await fixture(t);
  assert.throws(() => localWorkflowKeyPath({ RUNLOOM_ENV: 'production' }, directory), /local-only/);
  assert.throws(() => localWorkflowKeyPath({ RUNLOOM_SECRETS_KEY_PATH: './key' }, directory), /absolute/);
  const key = await ensureLocalWorkflowKey({}, directory);
  const other = join(directory, 'other');
  await writeFile(other, Buffer.alloc(32), { mode: 0o600 });
  await rm(key.path);
  await symlink(other, key.path);
  await assert.rejects(ensureLocalWorkflowKey({}, directory), /symbolic link/);
  assert.equal((await stat(other)).mode & 0o777, 0o600);
});
