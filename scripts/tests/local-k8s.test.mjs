import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, existsSync, statSync } from 'node:fs';
import { command, configuration, execute, executeAction, localOrigin } from '../local-k8s.mjs';

function harness({ present = true, installed = false, values = {}, endpoint = 'https://127.0.0.1:6443', docker = 'unix:///var/run/docker.sock', fail } = {}) {
  const calls = [];
  const effective = { images: {localAdmin: {repository: 'custom-admin', tag: 'verified', pullPolicy: 'Never'}}, development: { enabled: true }, splitServices: { enabled: true }, database: { driver: 'mongodb' }, auth: { publicURL: 'http://localhost:4200' }, ...values };
  const run = (tool, args, capture) => {
    const call = { tool, args, capture };
    if (tool === 'helm' && args.includes('--reset-values')) {
      const path = args[args.indexOf('-f') + 1];
      call.preserved = JSON.parse(readFileSync(path, 'utf8'));
      call.permissions = statSync(path).mode & 0o777;
    }
    calls.push(call);
    if (fail?.(tool, args)) throw new Error('simulated failure');
    if (tool === 'docker' && args[0] === 'context') return docker;
    if (tool === 'kind' && args[0] === 'get') return present ? 'runloom-local' : '';
    if (tool === 'kubectl' && args.includes('view')) return JSON.stringify({ clusters: [{ cluster: { server: endpoint } }] });
    if (tool === 'helm' && args[0] === 'list' && capture) return JSON.stringify(installed ? [{ name: 'runloom', chart: 'runloom-0.1.0' }] : []);
    if (tool === 'helm' && args[0] === 'get') return JSON.stringify(effective);
    return '';
  };
  return { calls, run, invoke: (action, env = {}) => execute(action, configuration(env), run, () => {}, {databases: () => calls.push({tool: 'database-setup', args: []})}) };
}
const of = (h, tool, action) => h.calls.filter(c => c.tool === tool && c.args[0] === action);

test('fresh up orders cluster, independent builds, install, provisioning and connectivity test', async () => {
  const h = harness({ present: false });
  await executeAction('up', configuration({K8S_SERVICE_VALUES:'/local/service-values.yaml'}), { run: h.run, log: () => {}, provision: async () => h.calls.push({ tool: 'provision', args: [] }) });
  assert.equal(of(h, 'kind', 'create').length, 1);
  assert.deepEqual(of(h, 'docker', 'build').map(c => c.args.at(-1)), ['server/assistant','server/auth','server/auth','server/workflows','server/workflows','server/management','server/gateway','studio/projects/host','studio/projects/workflows','studio/projects/management', 'studio/projects/platform-admin', 'docs-site']);
  assert(of(h, 'docker', 'build').some(c => c.args.includes('server/auth/Dockerfile.local-admin')));
  assert(of(h, 'docker', 'build').some(c => c.args.includes('server/workflows/Dockerfile.worker')));
  const images = of(h, 'docker', 'build').map(c => c.args[c.args.indexOf('--tag') + 1]);
  assert.equal(new Set(images.map(image => image.split(':')[1])).size, 1);
  assert.match(images[0], /:dev-\d+-[a-f0-9]+$/);
  const load = of(h, 'kind', 'load')[0];
  assert.deepEqual(load.args.slice(2, 2 + images.length), images);
  const install = of(h, 'helm', 'install')[0];
  assert.ok(install.args.includes('database.driver=mongodb'));
  assert.ok(install.args.includes('--create-namespace'));
  assert.ok(install.args.includes('--wait'));
  assert.ok(install.args.includes('10m'));
  assert.ok(h.calls.indexOf(load) < h.calls.indexOf(install));
  assert.ok(h.calls.indexOf(install) < h.calls.indexOf(of(h, 'helm', 'test')[0]));
  const provision = h.calls.findIndex(call => call.tool === 'provision');
  assert.ok(h.calls.indexOf(install) < provision);
  assert.ok(provision < h.calls.indexOf(of(h, 'helm', 'test')[0]));
  for (const c of h.calls.filter(c => c.tool === 'kubectl' || (c.tool === 'helm' && c.args[0] !== 'lint'))) {
    assert.ok(c.args.includes('kind-runloom-local'), 'explicit context required');
  }
});

test('plain k8s-up bootstraps databases, consumer provisioning and admin before success', async () => {
  const h = harness();
  await executeAction('up', configuration({}), {run: h.run, log: () => {},
    databases: () => h.calls.push({tool: 'databases', args: []}),
    provision: async () => h.calls.push({tool: 'consumer', args: []}),
    admin: async () => {
      h.calls.push({tool: 'admin', args: []});
      return {email: 'admin@runloom.test', created: false, default_password: 'synthetic',
        member: {email: 'member@runloom.test', created: false, default_password: 'synthetic', default_org_role: 'member', default_team_role: 'editor'},
        ...Object.fromEntries(['platform_admin', 'platform_staff'].map(role => [role, {email: role + '@example.test', role, created: false, default_password: 'synthetic'}]))};
    },
  });
  assert.equal(of(h, 'docker', 'build').length, 13);
  const index = tool => h.calls.findIndex(c => c.tool === tool);
  const install = h.calls.indexOf(of(h, 'helm', 'install')[0]);
  assert.ok(index('databases') < install);
  assert.ok(install < index('consumer'));
  assert.ok(index('consumer') < index('admin'));
  assert.ok(index('admin') < h.calls.indexOf(of(h, 'helm', 'test')[0]));
});

test('cluster creation is idempotent and does not deploy a chart', () => {
  const h = harness();
  h.invoke('cluster');
  assert.equal(of(h, 'kind', 'create').length, 0);
  assert.equal(h.calls.filter(c => c.tool === 'helm').length, 0);
  assert.ok(h.calls.some(c => c.args.includes('--for=condition=Ready')));
});

test('down deletes only the named local cluster without requiring its Kubernetes API', () => {
  const h = harness({endpoint: 'https://unreachable.example'});
  h.invoke('down');
  assert.deepEqual(of(h, 'kind', 'delete').map(c => c.args), [['delete', 'cluster', '--name', 'runloom-local']]);
  assert.ok(!h.calls.some(c => ['kubectl', 'helm'].includes(c.tool)));
  assert.equal(of(h, 'docker', 'build').length, 0);
  assert.equal(of(h, 'kind', 'create').length, 0);
  const custom = harness();
  execute('down', configuration({K8S_CLUSTER: 'runloom-sandbox'}), (tool, args, capture) =>
    tool === 'kind' && args[0] === 'get' ? 'runloom-sandbox\nother-cluster' : custom.run(tool, args, capture), () => {});
  assert.deepEqual(of(custom, 'kind', 'delete').map(c => c.args), [['delete', 'cluster', '--name', 'runloom-sandbox']]);
});

test('down is idempotent, rejects remote Docker and propagates deletion failures', () => {
  const absent = harness({present: false});
  absent.invoke('down');
  assert.equal(of(absent, 'kind', 'delete').length, 0);
  const remote = harness({docker: 'tcp://remote.example:2375'});
  assert.throws(() => remote.invoke('down'));
  assert.equal(of(remote, 'kind', 'delete').length, 0);
  const failed = harness({fail: (tool, args) => tool === 'kind' && args[0] === 'delete'});
  assert.throws(() => failed.invoke('down'), /simulated failure/);
});

test('upgrade preserves installed values and origin and uses fresh image tags each time', () => {
  const h = harness({ installed: true, values: { database: { driver: 'postgres' }, auth: { publicURL: 'http://localhost:4305' } } });
  h.invoke('upgrade'); h.invoke('up');
  assert.equal(of(h, 'helm', 'install').length, 0);
  const upgrades = of(h, 'helm', 'upgrade');
  assert.equal(upgrades.length, 2);
  for (const c of upgrades) {
    assert.ok(c.args.includes('--reset-values'));
    assert.equal(c.preserved.auth.publicURL, 'http://localhost:4305');
    assert.equal(c.permissions, 0o600);
    assert.equal(existsSync(c.args[c.args.indexOf('-f') + 1]), false);
    assert.ok(c.args.includes('database.driver=postgres'));
    assert.ok(c.args.includes('auth.publicURL=http://localhost:4305'));
  }
  const images = of(h, 'docker', 'build').map(c => c.args[c.args.indexOf('--tag') + 1]);
  assert.notEqual(images[0], images[4]);
});

test('split upgrades remove obsolete database defaults but preserve service references', () => {
  const h = harness({installed: true, values: {
    database: {driver: 'postgres', name: 'runloom', existingSecret: 'old-database', secretKey: 'url'},
    splitServices: {enabled: true, databases: {auth: {existingSecret: 'auth-owner'}}},
    workflowSecrets: {existingSecret: 'keep-vault'},
  }});
  h.invoke('upgrade');
  const saved = of(h, 'helm', 'upgrade')[0].preserved;
  assert.deepEqual(saved.database, {driver: 'postgres'});
  assert.equal(saved.splitServices.databases.auth.existingSecret, 'auth-owner');
  assert.equal(saved.workflowSecrets.existingSecret, 'keep-vault');
});

test('monolith upgrades stop before builds or mutations', () => {
  const h = harness({installed: true, values: {splitServices: {enabled: false}}});
  assert.throws(() => h.invoke('upgrade'), /removed monolith/);
  assert.equal(of(h, 'docker', 'build').length, 0);
  assert.equal(of(h, 'helm', 'upgrade').length, 0);
});

test('install/upgrade preconditions fail before building or mutating a release', () => {
  for (const [action, opts] of [['install', { installed: true }], ['upgrade', {}], ['install', { present: false }]]) {
    const h = harness(opts);
    assert.throws(() => h.invoke(action));
    assert.equal(of(h, 'docker', 'build').length, 0);
    assert.equal(of(h, 'kind', 'create').length, 0);
  }
});

test('reject remote Docker, remote Kubernetes endpoints, hosted releases and database switches', () => {
  for (const opts of [
    { docker: 'ssh://production' }, { endpoint: 'https://cluster.example.com' },
    { installed: true, values: { development: { enabled: false } } },
    { installed: true, values: { ingress: { enabled: true } } },
    { installed: true, values: { auth: { publicURL: 'https://studio.example.com' } } },
  ]) {
    const h = harness(opts);
    assert.throws(() => h.invoke('up'));
    assert.equal(of(h, 'docker', 'build').length, 0);
  }
  const h = harness({ installed: true });
  assert.throws(() => h.invoke('upgrade', { K8S_DATABASE: 'postgres' }), /database\/PVC/);
  assert.equal(of(h, 'docker', 'build').length, 0);
});

test('forwarding uses the installed origin, loopback binding and custom resource prefix', () => {
  const h = harness({ installed: true, values: { fullnameOverride: 'custom', auth: { publicURL: 'http://localhost:4400' } } });
  h.invoke('run');
  const forward = h.calls.find(c => c.args.includes('port-forward'));
  assert.ok(forward.args.includes('127.0.0.1'));
  assert.ok(forward.args.includes('4400:80'));
  assert.ok(forward.args.includes('service/custom-ui'));
  assert.equal(of(h, 'docker', 'build').length, 0);
  assert.throws(() => h.invoke('run', { K8S_PORT: '4500' }), /login origin/);
  assert.throws(() => h.invoke('upgrade', { K8S_PORT: '4500' }), /login origin/);
  assert.equal(of(h, 'docker', 'build').length, 0);
});

test('open resolves a guarded forwarding plan without building or deploying', () => {
  const h = harness({ installed: true });
  const plan = execute('open', configuration({K8S_SERVICE_VALUES:'/local/service-values.yaml'}), h.run, () => {});
  assert.equal(plan.origin, 'http://localhost:4200');
  assert.deepEqual(plan.localAdminImage, {repository: 'custom-admin', tag: 'verified', pullPolicy: 'Never'});
  assert.ok(plan.forwardArgs.includes('kind-runloom-local'));
  assert.ok(plan.forwardArgs.includes('service/runloom-ui'));
  assert.equal(of(h, 'docker', 'build').length, 0);
  assert.equal(of(h, 'helm', 'upgrade').length, 0);
  assert.throws(() => harness().invoke('open'), /Release does not exist/);
});

test('explicit matching port preserves an installed 127.0.0.1 origin', () => {
  const h = harness({ installed: true, values: { auth: { publicURL: 'http://127.0.0.1:4305' } } });
  h.invoke('upgrade', { K8S_PORT: '4305' });
  assert.ok(of(h, 'helm', 'upgrade')[0].args.includes('auth.publicURL=http://127.0.0.1:4305'));
});

test('PostgreSQL and custom browser ports are supported on a fresh install', () => {
  const h = harness();
  h.invoke('install', { K8S_DATABASE: 'postgres', K8S_PORT: '4500' });
  const install = of(h, 'helm', 'install')[0];
  assert.ok(install.args.includes('database.driver=postgres'));
  assert.ok(install.args.includes('auth.publicURL=http://localhost:4500'));
});

test('status is read-only and a build failure never reaches Helm install', () => {
  const h = harness();
  h.invoke('status');
  assert.equal(of(h, 'helm', 'install').length, 0);
  assert.equal(of(h, 'docker', 'build').length, 0);
  const failed = harness({ fail: (tool, args) => tool === 'docker' && args[0] === 'build' });
  assert.throws(() => failed.invoke('up'), /simulated failure/);
  assert.equal(of(failed, 'helm', 'install').length, 0);
  assert.equal(of(failed, 'kind', 'load').length, 0);
});

test('failed Helm operations and connectivity tests propagate instead of reporting success', async () => {
  for (const action of ['install', 'test']) {
    const h = harness({ fail: (tool, args) => tool === 'helm' && args[0] === action });
    await assert.rejects(executeAction('up', configuration({K8S_SERVICE_VALUES:'/local/service-values.yaml'}), { run: h.run, log: () => {}, provision: async () => {} }), /simulated failure/);
    if (action === 'install') assert.equal(of(h, 'helm', 'test').length, 0);
  }
});

test('all deployment actions require provisioning; failure prevents ready messages and smoke tests', async () => {
  for (const action of ['up', 'install', 'upgrade']) {
    const h = harness({ installed: action === 'upgrade' });
    const logs = [];
    await assert.rejects(executeAction(action, configuration({K8S_SERVICE_VALUES:'/local/service-values.yaml'}), {
      run: h.run, log: message => logs.push(message),
      provision: async () => { throw new Error('provisioner unavailable'); },
    }), /provisioner unavailable/);
    assert.equal(of(h, 'helm', 'test').length, 0);
    assert.ok(!logs.some(message => message.includes('Runloom ready')));
  }
});

test('configuration rejects invalid inputs and public origins', () => {
  for (const env of [
    { K8S_CLUSTER: 'prod;echo bad' }, { K8S_RELEASE: '-bad' }, { K8S_NAMESPACE: '../other' },
    { K8S_DATABASE: 'file' }, { K8S_PORT: '0' }, { K8S_PORT: '65536' },
    { K8S_TIMEOUT: 'forever' }, { RUNLOOM_ENV: 'production' },
  ]) assert.throws(() => configuration(env));
  for (const origin of ['https://localhost:4200', 'http://0.0.0.0:4200', 'http://localhost:4200/path', 'http://user:pass@localhost:4200']) assert.throws(() => localOrigin(origin));
  assert.equal(configuration({K8S_SERVICE_VALUES:'/local/service-values.yaml'}).context, 'kind-runloom-local');
});

test('release discovery and status use Helm 3/4-compatible status filters, not list --all', () => {
  for (const action of ['up', 'status']) {
    const h = harness();
    h.invoke(action);
    for (const call of of(h, 'helm', 'list')) {
      assert.ok(!call.args.includes('--all'));
      for (const status of ['--deployed', '--failed', '--pending', '--uninstalled', '--superseded', '--uninstalling']) assert.ok(call.args.includes(status));
      if (call.capture) assert.ok(call.args.includes('^runloom$'));
    }
  }
  const h = harness({ installed: true });
  h.invoke('upgrade');
  assert.ok(of(h, 'helm', 'get')[0].args.includes('--all'), 'get values --all remains valid and needed');
});

test('captured failures expose safe diagnostics without printing configuration or credentials', () => {
  for (const [stderr, expected] of [
    ['Error: unknown flag: --all', /Unsupported flag --all/],
    ['Error: Kubernetes cluster unreachable: connection refused', /cluster is unreachable/],
    ['Error: forbidden', /Access was denied/],
    ['Error: context deadline exceeded', /timed out/],
    ['unexpected failure', /captured configuration is not printed/],
  ]) {
    assert.throws(() => command('helm', ['list'], true, () => ({ status: 1, stdout: 'SECRET_VALUE', stderr: `${stderr}\nSECRET_VALUE` })), error => {
      assert.match(error.message, expected);
      assert.ok(!error.message.includes('SECRET_VALUE'));
      return true;
    });
  }
});

test('logs uses the guarded local namespace without requiring Helm or provisioning', async () => {
  const h = harness();
  let plan;
  await executeAction('logs', configuration({}), {run: h.run, log: () => {}, logs: async value => {plan = value;}});
  assert.deepEqual(plan, {kubectl: 'kubectl', context: 'kind-runloom-local', namespace: 'runloom'});
  assert.equal(h.calls.filter(c => c.tool === 'helm').length, 0);
  assert.equal(of(h, 'docker', 'build').length, 0);
  assert.throws(() => harness({endpoint: 'https://remote.example'}).invoke('logs'), /non-loopback/);
  assert.throws(() => harness({present: false}).invoke('logs'), /does not exist/);
});
