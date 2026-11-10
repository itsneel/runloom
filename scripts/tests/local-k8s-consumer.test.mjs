import test from 'node:test';
import assert from 'node:assert/strict';
import { provisionLocalConsumer, provisioningSettings } from '../local-k8s-consumer.mjs';
import { configuration } from '../local-k8s.mjs';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

test('local provisioner configuration uses an API-only Secret and bundled Keycloak', () => {
  const settings = provisioningSettings({}, { fullname: 'custom-runloom' });
  assert.deepEqual(settings, { clientID: 'runloom-provisioner', existingSecret: 'custom-runloom-provisioner', secretKey: 'client-secret', adminURL: 'http://custom-runloom-keycloak:8080/auth' });
  assert.deepEqual(provisioningSettings({ consumer: { provisioning: settings } }, { fullname: 'custom-runloom' }), settings);
});

test('startup creates the API Secret only after verification; subsequent startup makes no Helm change', async () => {
  for (const exists of [false, true]) {
    const calls = [];
    const settings = provisioningSettings({}, { fullname: 'runloom' });
    await provisionLocalConsumer(configuration({}), { fullname: 'runloom' }, {
      run: (tool, args, capture) => {
        calls.push({ tool, args });
        assert.ok(args.includes('kind-runloom-local'));
        if (tool === 'helm' && args[0] === 'get') return JSON.stringify({ consumer: { provisioning: exists ? { ...settings, enabled: true } : {} } });
        if (args.includes('--ignore-not-found')) return exists ? JSON.stringify({ data: { 'client-secret': Buffer.from('synthetic').toString('base64') } }) : '';
        if (args.includes('create')) assert.equal(capture, true);
        return '';
      },
      forward: async (c, fullname, callback) => callback('http://127.0.0.1:31234/auth'),
      ensureConsumer: async options => { assert.equal(options.storedSecret, exists ? 'synthetic' : undefined); calls.push({ tool: 'verify', args: [] }); },
    });
    const mutations = calls.filter(call => call.args.includes('create') || call.args[0] === 'upgrade');
    assert.equal(mutations.length, exists ? 0 : 2);
    if (!exists) assert.ok(calls.indexOf(mutations[0]) > calls.findIndex(call => call.tool === 'verify'));
  }
});

test('failed credential verification never mounts a credential or upgrades Helm', async () => {
  const calls = [];
  await assert.rejects(provisionLocalConsumer(configuration({}), { fullname: 'runloom' }, {
    run: (tool, args) => { calls.push(args); return tool === 'helm' ? '{}' : ''; },
    forward: async (c, name, action) => action('http://127.0.0.1:31234/auth'),
    ensureConsumer: async () => { throw new Error('invalid credential'); },
  }), /invalid credential/);
  assert.ok(!calls.some(args => args.includes('create') || args[0] === 'upgrade'));
});

test('repair CLI has no circular top-level-await and reaches local-only guards', () => {
  const result = spawnSync(process.execPath, [fileURLToPath(new URL('../local-k8s-consumer.mjs', import.meta.url))], {
    encoding: 'utf8', timeout: 5000, env: { ...process.env, RUNLOOM_ENV: 'production' },
  });
  assert.equal(result.status, 1);
  assert.match(result.stderr, /Local Kubernetes requires/);
  assert.ok(!result.stderr.includes('unsettled'));
});
test('operator-supplied client, credential and provider references are never silently overridden', () => {
  for (const key of ['clientID', 'existingSecret', 'secretKey', 'adminURL']) {
    assert.throws(() => provisioningSettings({ consumer: { provisioning: { [key]: 'custom' } } }, { fullname: 'runloom' }), /Custom provisioning/);
  }
});
