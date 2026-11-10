import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, readFile, rm, stat, symlink, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { ensureLocalConsumer } from '../local-consumer-auth.mjs';

async function fixture(t) {
  const directory = await mkdtemp(join(tmpdir(), 'runloom-consumer-test-'));
  t.after(() => rm(directory, { recursive: true, force: true }));
  return { baseURL: 'http://127.0.0.1:18081/auth', secretFile: join(directory, 'credential') };
}
function provider({ exists = false, invalid = false } = {}) {
  const calls = [];
  return {
    calls,
    fetch: async (url, options) => {
      calls.push({ url, options });
      assert.equal(options.redirect, 'error');
      if (invalid) return new Response('NEVER_PRINT_PROVIDER_SECRET', { status: 401 });
      return Response.json(url.endsWith('/token') ? { access_token: 'synthetic-token' } : url.includes('?clientId=') ? (exists ? [{ id: 'existing' }] : []) : []);
    },
    setup: async ({ secretFile }) => { calls.push({ setup: true }); await writeFile(secretFile, 'synthetic-test-credential\n', { flag: 'wx', mode: 0o600 }); },
  };
}
test('fresh setup creates once; repeat verifies without admin login, rotation or file changes', async t => {
  const options = await fixture(t), p = provider();
  const first = await ensureLocalConsumer(options, p);
  assert.equal(first.created, true);
  const bytes = await readFile(options.secretFile);
  assert.equal((await stat(options.secretFile)).mode & 0o777, 0o640);
  p.calls.length = 0;
  assert.equal((await ensureLocalConsumer(options, p)).created, false);
  assert.deepEqual(await readFile(options.secretFile), bytes);
  assert.ok(!p.calls.some(call => call.setup || call.url.includes('/master/')));
  assert.equal(p.calls.length, 3);
});
test('a Kubernetes credential can be reused without a host backup or administrator', async t => {
  const p = provider();
  const result = await ensureLocalConsumer({ ...await fixture(t), storedSecret: 'synthetic-test' }, p);
  assert.equal(result.created, false);
  assert.ok(!p.calls.some(call => call.setup || call.url.includes('/master/')));
});
test('an existing identity with a missing credential fails without rotation', async t => {
  const p = provider({ exists: true });
  await assert.rejects(ensureLocalConsumer(await fixture(t), p), /original credential is missing/);
  assert.ok(!p.calls.some(call => call.setup));
});
test('explicit missing files and empty or symlink credentials fail closed', async t => {
  const options = await fixture(t), p = provider();
  await assert.rejects(ensureLocalConsumer({ ...options, managed: false }, p), /missing/);
  await writeFile(options.secretFile, '');
  await assert.rejects(ensureLocalConsumer(options, p), /empty/);
  const linked = options.secretFile + '-link';
  await symlink(options.secretFile, linked);
  await assert.rejects(ensureLocalConsumer({ ...options, secretFile: linked }, p), /symlink/);
  assert.equal(p.calls.length, 0);
});
test('invalid credentials never disclose provider responses or regenerate secrets', async t => {
  const options = await fixture(t), p = provider({ invalid: true });
  await writeFile(options.secretFile, 'original');
  await assert.rejects(ensureLocalConsumer(options, p), error => /HTTP 401/.test(error.message) && !error.message.includes('NEVER_PRINT'));
  assert.equal(await readFile(options.secretFile, 'utf8'), 'original');
  assert.ok(!p.calls.some(call => call.setup));
});
test('explicit credential permissions are preserved and remote identity URLs rejected', async t => {
  const options = await fixture(t), p = provider();
  await writeFile(options.secretFile, 'original', { mode: 0o600 });
  await ensureLocalConsumer({ ...options, managed: false }, p);
  assert.equal((await stat(options.secretFile)).mode & 0o777, 0o600);
  await assert.rejects(ensureLocalConsumer({ ...options, baseURL: 'https://production.example/auth' }, p), /loopback/);
});

test('fresh local identity restores a missing client with the saved credential unchanged', async t => {
  const options = await fixture(t);
  const original = 'saved-local-credential-must-remain-unchanged\n';
  await writeFile(options.secretFile, original, {mode: 0o640});
  let restored = false;
  const calls = [];
  const result = await ensureLocalConsumer(options, {
    fetch: async (url, request) => {
      calls.push(url);
      if (url.endsWith('/realms/runloom/protocol/openid-connect/token')) {
        assert.equal(request.body.get('client_secret'), original.trim());
        return restored ? Response.json({access_token: 'synthetic'}) : new Response('private provider error', {status: 401});
      }
      return Response.json(url.includes('/master/') ? {access_token: 'bootstrap'} : []);
    },
    setup: async ({reuseCredential, secretFile}) => {
      assert.equal(reuseCredential, true);
      assert.equal(await readFile(secretFile, 'utf8'), original);
      restored = true;
    },
  });
  assert.equal(result.created, true);
  assert.equal(await readFile(options.secretFile, 'utf8'), original);
  assert.equal(calls.filter(url => url.includes('/master/')).length, 1);
});

test('invalid saved credentials never replace an existing client', async t => {
  const options = await fixture(t);
  await writeFile(options.secretFile, 'original');
  let setupCalls = 0;
  await assert.rejects(ensureLocalConsumer(options, {
    fetch: async url => {
      if (url.includes('/master/')) return Response.json({access_token: 'bootstrap'});
      if (url.includes('?clientId=')) return Response.json([{id: 'existing-client'}]);
      return new Response('private provider error', {status: 401});
    },
    setup: async () => { setupCalls++; },
  }), /Keycloak client already exists.*no client or credential was changed/);
  assert.equal(setupCalls, 0);
  assert.equal(await readFile(options.secretFile, 'utf8'), 'original');
});

test('invalid explicit or Kubernetes credentials never trigger administrator recovery', async t => {
  const options = await fixture(t);
  await writeFile(options.secretFile, 'original');
  for (const override of [{managed: false}, {storedSecret: 'cluster-owned'}]) {
    const p = provider({invalid: true});
    await assert.rejects(ensureLocalConsumer({...options, ...override}, p), /provisioner credential verification.*HTTP 401/);
    assert.equal(p.calls.length, 1);
    assert.ok(!p.calls.some(call => call.setup || call.url?.includes('/master/')));
  }
});

test('bootstrap administrator authentication failures identify the failed stage', async t => {
  await assert.rejects(ensureLocalConsumer(await fixture(t), provider({invalid: true})), /bootstrap administrator login.*HTTP 401/);
});
