import test from 'node:test';
import assert from 'node:assert/strict';
import { fileURLToPath } from 'node:url';
import { executeLocalStack, validateDockerEndpoint, validateLocalConfig } from '../local-stack.mjs';
function config() {
  const mounted = { environment: {RUNLOOM_CONFIG_FILE: '/etc/runloom/config.yaml'}, configs: [{source: 'base', target: '/etc/runloom/config.yaml'}, {source: 'api', target: '/etc/runloom/config.d/10-api.yaml'}], secrets: [{source: 'database-url', target: '/run/secrets/database-url'}] };
  return { name: 'runloom-local', services: {
    api: { ...structuredClone(mounted), ports: [{ host_ip: '127.0.0.1' }] },
    keycloak: { environment: { KC_HOSTNAME: 'http://localhost:14200/auth' }, ports: [{ target: 8080, published: '18081', host_ip: '127.0.0.1' }] },
    'local-admin': { ...structuredClone(mounted), environment: {...mounted.environment, RUNLOOM_ENV: 'local', RUNLOOM_LOCAL_BOOTSTRAP: '1'} },
  }, configs: {base: {file: fileURLToPath(new URL('../../compose/config/base.mongodb.yaml', import.meta.url))}, api: {file: fileURLToPath(new URL('../../compose/config/api.local.yaml', import.meta.url))}}, networks: { default: {} } };
}
test('local config and Unix/named-pipe Docker endpoints are accepted', () => {
  validateLocalConfig(config()); validateDockerEndpoint('unix:///var/run/docker.sock'); validateDockerEndpoint('npipe:////./pipe/docker_engine');
});

function harness({ supplied = '', mounted = '', invalid = false, keyError = false, fresh = false, consumerError = false, consumerPath = '', mountedConsumer = '' } = {}) {
  const calls = [], keyRequests = [], consumerRequests = [];
  const model = config();
  if (fresh) model.name = 'runloom-fresh';
  if (invalid) model.name = 'production';
  return {
    calls, keyRequests, consumerRequests,
    run: async action => executeLocalStack(action, {}, {
      run: (args, capture, env, input) => {
        calls.push({ args, env, input });
        if (args[0] === 'context') return 'unix:///var/run/docker.sock';
        if (args.includes('ps')) return mounted || mountedConsumer ? 'abcdef012345' : '';
        if (args[0] === 'inspect') return JSON.stringify([{ Mounts: [
          ...(mounted ? [{ Type: 'bind', Source: mounted, Destination: '/run/secrets/workflow-key' }] : []),
          ...(mountedConsumer ? [{ Type: 'bind', Source: mountedConsumer, Destination: '/run/secrets/runloom-provisioner' }] : []),
        ] }]);
        if (args.includes('--environment')) return `RUNLOOM_SECRETS_KEY_PATH=${supplied}\nRUNLOOM_PROVISIONER_SECRET_PATH=${consumerPath}\n`;
        if (args.includes('config')) return JSON.stringify(model);
        return '';
      },
      ensureKey: async env => {
        keyRequests.push(env);
        if (keyError) throw new Error('invalid key');
        return { path: supplied || '/private/local/workflow-key', gid: '1234', created: !supplied };
      },
      ensureConsumer: async options => {
        consumerRequests.push(options);
        calls.push({ args: ['ensure-consumer'] });
        if (consumerError) throw new Error('consumer setup failed');
        return { path: options.secretFile, gid: '1234', created: true };
      },
    }),
  };
}
test('local and fresh startup automatically mount the managed key before building or starting services', async () => {
  for (const fresh of [false, true]) {
    const h = harness({ fresh });
    await h.run(fresh ? 'fresh-up' : 'up');
    assert.equal(h.keyRequests.length, 1);
    const launch = h.calls.find(call => call.args.includes('--build'));
    assert.ok(launch.args.includes('--project-directory'));
    assert.ok(launch.args.includes('compose/compose.yaml'));
    assert.ok(launch.args.includes('compose/compose.local.yaml'));
    assert.ok(launch.args.includes('compose/compose.secrets.yaml'));
    assert.ok(launch.args.includes(fresh ? 'runloom-fresh' : 'runloom-local'));
    assert.equal(launch.env.RUNLOOM_SECRETS_KEY_PATH, '/private/local/workflow-key');
    assert.equal(launch.env.RUNLOOM_SECRETS_KEY_GID, '1234');
    assert.ok(launch.args.includes('compose/compose.consumer.yaml'));
    assert.equal(launch.env.RUNLOOM_PROVISIONER_SECRET_GID, '1234');
    assert.match(launch.input, /group_add: !override \["1234"\]/);
    assert.equal(h.consumerRequests.length, 1);
    assert.ok(h.calls.findIndex(call => call.args.includes('run')) < h.calls.findIndex(call => call.args.includes('ensure-consumer')));
    assert.ok(h.calls.findIndex(call => call.args.includes('ensure-consumer')) < h.calls.indexOf(launch));
  }
});
test('env-file key overrides are respected; config, down and admin do not create keys', async () => {
  const external = harness({ supplied: '/external/key' });
  await external.run('up');
  assert.equal(external.keyRequests[0].RUNLOOM_SECRETS_KEY_PATH, '/external/key');
  const previous = harness({ mounted: '/previous/key' });
  await previous.run('up');
  assert.equal(previous.keyRequests[0].RUNLOOM_SECRETS_KEY_PATH, '/previous/key');
  for (const action of ['config', 'down', 'admin', 'reset']) {
    const h = harness(); await h.run(action);
    assert.equal(h.keyRequests.length, 0);
    assert.equal(h.consumerRequests.length, 0);
  }
});
test('consumer errors stop application launch and explicitly supplied files are preserved', async () => {
  const failed = harness({ consumerError: true });
  await assert.rejects(failed.run('up'), /consumer setup failed/);
  assert.ok(!failed.calls.some(call => call.args.includes('--build')));
  const supplied = harness({ consumerPath: '/external/provisioner' });
  await supplied.run('up');
  assert.equal(supplied.consumerRequests[0].secretFile, '/external/provisioner');
  assert.equal(supplied.consumerRequests[0].managed, false);
});
test('a previous provisioner mount is retained when the next shell omits the override', async () => {
  const h = harness({ mountedConsumer: '/previous/provisioner' });
  await h.run('up');
  assert.equal(h.consumerRequests[0].secretFile, '/previous/provisioner');
  assert.equal(h.consumerRequests[0].managed, false);
  const launch = h.calls.find(call => call.args.includes('--build'));
  assert.equal(launch.env.RUNLOOM_PROVISIONER_SECRET_PATH, '/previous/provisioner');
});
test('unsafe configuration and invalid keys stop startup before container mutations', async () => {
  for (const options of [{ invalid: true }, { keyError: true }]) {
    const h = harness(options);
    await assert.rejects(h.run('up'));
    assert.ok(!h.calls.some(call => call.args.includes('up') || call.args.includes('build') || call.args.includes('run')));
    if (options.invalid) assert.equal(h.keyRequests.length, 0);
  }
});
test('remote Docker endpoints are rejected', () => {
  for (const value of ['ssh://prod', 'tcp://localhost:2375', '', undefined]) assert.throws(() => validateDockerEndpoint(value));
});
test('public exposure, remote identity and mismatched storage fail closed', () => {
  for (const change of [
    c => { c.name = 'production'; },
    c => { c.services.api.ports[0].host_ip = '0.0.0.0'; },
    c => { c.services.api.network_mode = 'host'; },
    c => { c.networks.default.external = true; },
    c => { c.services.keycloak.environment.KC_HOSTNAME = 'https://example.com/auth'; },
    c => { c.services['local-admin'].secrets[0].source = 'other'; },
    c => { c.services['local-admin'].environment.RUNLOOM_ENV = 'production'; },
  ]) { const c = config(); change(c); assert.throws(() => validateLocalConfig(c)); }
});
