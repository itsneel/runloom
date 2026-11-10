import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, statSync, existsSync } from 'node:fs';
import { localServiceValues, databaseResources, provisionLocalDatabases } from '../local-k8s-services.mjs';

const c = {kubectl: 'kubectl', namespace: 'test-runloom', release: 'test', cluster: 'runloom-local', context: 'kind-runloom-local', timeout: '5m'};
const plan = {fullname: 'test-runloom', database: 'mongodb', tag: 'test'};
function harness() {
  const objects = new Map(), calls = [], paths = [];
  const key = (kind, name) => `${kind.toLowerCase()}/${name}`;
  const run = (tool, args) => {
    calls.push(args);
    assert.equal(args[args.indexOf('--context') + 1], c.context);
    if (args.includes('get')) {
      const i = args.indexOf('get');
      const object = objects.get(key(args[i+1], args[i+2]));
      return object ? JSON.stringify(object) : '';
    }
    if (args.includes('-f')) {
      const path = args[args.indexOf('-f')+1]; paths.push(path);
      assert.equal(statSync(path).mode & 0o777, 0o600);
      const object = JSON.parse(readFileSync(path, 'utf8'));
      if (args.includes('create')) assert.ok(!objects.has(key(object.kind, object.metadata.name)), 'Must not replace an existing resource');
      objects.set(key(object.kind, object.metadata.name), object);
    }
    return '';
  };
  return {objects, calls, paths, run};
}

test('fresh local setup provisions distinct credentials and repeat setup preserves them', () => {
  for (const driver of ['mongodb', 'postgres']) {
    const h = harness();
    provisionLocalDatabases(c, {...plan, database: driver}, h.run);
    const secrets = [...h.objects.entries()].filter(([key]) => key.startsWith('secret/'));
    assert.equal(secrets.length, 9);
    assert.ok(h.objects.has('secret/test-runloom-assistant-database'));
    assert.equal(new Set(secrets.filter(([key]) => key.endsWith('-token')).map(([, object]) => object.data.token)).size, 4);
    for (const [, secret] of secrets) assert.equal(secret.immutable, true);
    const original = JSON.stringify(secrets);
    provisionLocalDatabases(c, {...plan, database: driver}, h.run);
    assert.equal(JSON.stringify([...h.objects.entries()].filter(([key]) => key.startsWith('secret/'))), original);
    for (const path of h.paths) assert.equal(existsSync(path), false);
    const values = localServiceValues(plan.fullname);
    assert.deepEqual(values.databaseSetup, {enabled: true, existingSecret: 'test-runloom-local-bootstrap'});
    assert.ok(![...h.objects.keys()].some(key => key.startsWith('job/')), 'Helm owns database setup Jobs');
    for (const owner of ['auth', 'workflows', 'management']) {
      const secret = h.objects.get(`secret/${values.splitServices.databases[owner].existingSecret}`);
      const url = new URL(Buffer.from(secret.data.url, 'base64').toString('utf8'));
      assert.equal(url.hostname, 'test-runloom-database');
      assert.equal(url.pathname, `/runloom_${owner}`);
      assert.equal(url.username, `runloom_${owner}`);
    }
  }
});

test('existing storage without credentials, changed driver and unowned resources fail closed', () => {
  const missing = harness(); missing.objects.set('pvc/data-test-runloom-database-0', {});
  assert.throws(() => provisionLocalDatabases(c, plan, missing.run), /credentials are missing/);
  assert.equal([...missing.objects.keys()].some(key => key.startsWith('secret/')), false);
  const changed = harness(); provisionLocalDatabases(c, plan, changed.run);
  assert.throws(() => provisionLocalDatabases(c, {...plan, database: 'postgres'}, changed.run), /Cannot switch/);
  changed.objects.get('secret/test-runloom-auth-token').metadata.labels = {};
  assert.throws(() => provisionLocalDatabases(c, plan, changed.run), /not owned/);
});

test('tampered credentials and trust hashes are rejected without rotation', () => {
  for (const what of ['secret', 'trust']) {
    const h = harness(); provisionLocalDatabases(c, plan, h.run);
    if (what === 'secret') h.objects.get('secret/test-runloom-auth-database').data.url = Buffer.from('unrelated-database-url-that-must-not-be-used').toString('base64');
    else h.objects.get('configmap/test-runloom-service-trust').data['trust.json'] = '{}';
    const before = JSON.stringify([...h.objects]);
    assert.throws(() => provisionLocalDatabases(c, plan, h.run), /disagrees|does not match/);
    assert.equal(JSON.stringify([...h.objects]), before);
  }
});

test('database workloads only receive root passwords and replica keys', () => {
  for (const driver of ['mongodb', 'postgres']) {
    const resources = databaseResources(c, {...plan, database: driver});
    const pod = resources.find(r => r.kind === 'StatefulSet').spec.template.spec;
    assert.equal(pod.automountServiceAccountToken, false);
    assert.ok(pod.containers[0].env.some(e => e.name.endsWith('PASSWORD_FILE')));
    assert.ok(!pod.containers[0].env.some(e => e.name.endsWith('PASSWORD')));
    assert.ok(pod.volumes[0].secret.items.every(i => ['database-root-password', 'mongodb-replica-key'].includes(i.key)));
  }
});

test('MongoDB startup gates DNS, initializes safely and retains the same volume identity', () => {
  const resources = databaseResources(c, plan);
  const service = resources.find(r => r.kind === 'Service');
  const stateful = resources.find(r => r.kind === 'StatefulSet');
  const container = stateful.spec.template.spec.containers[0];
  assert.equal(service.spec.publishNotReadyAddresses, true);
  assert.equal(service.spec.clusterIP, 'None');
  assert.equal(stateful.spec.serviceName, 'test-runloom-database');
  assert.equal(stateful.spec.volumeClaimTemplates[0].metadata.name, 'data');
  assert(container.args[0].indexOf('getent') < container.args[0].indexOf('exec /usr/local'));
  assert.equal(container.env.find(e => e.name === 'RUNLOOM_POD_IP').valueFrom.fieldRef.fieldPath, 'status.podIP');
  assert.equal(container.startupProbe.failureThreshold * container.startupProbe.periodSeconds, 300);
  assert(container.startupProbe.exec.command.at(-1).includes('error.code === 94'));
  assert(!container.readinessProbe.exec.command.at(-1).includes('rs.initiate'));
  assert.equal(container.livenessProbe.tcpSocket.port, 'database');
  const postgres = databaseResources(c, {...plan, database: 'postgres'})[1].spec.template.spec.containers[0];
  assert.equal(postgres.startupProbe, undefined);
});
