// Local kind bootstrap only; callers first verify Docker and Kubernetes endpoints.
// Credentials are reused, never rotated; only a database-owner job sees admin URLs.
import { randomBytes, createHash } from 'node:crypto';
import { mkdtempSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { resolve } from 'node:path';
import { command } from './local-k8s-command.mjs';
import { mongoDNSGate, mongoProbe } from './local-mongodb.mjs';

const owners = ['auth', 'workflows', 'management'];
const roles = [...owners, 'worker'];
export function localServiceValues(fullname) {
  return {assistant: {enabled: true, databaseSecret: `${fullname}-assistant-database`}, localSetup: {managed: true}, databaseSetup: {enabled: true, existingSecret: `${fullname}-local-bootstrap`}, splitServices: {
    trustConfigMap: `${fullname}-service-trust`,
    databases: Object.fromEntries(owners.map(owner => [owner, {name: `runloom_${owner}`, existingSecret: `${fullname}-${owner}-database`, secretKey: 'url'}])),
    credentials: Object.fromEntries(roles.map(role => [role, `${fullname}-${role}-token`])),
  }};
}

export function withPrivateManifest(object, callback) {
  const directory = mkdtempSync(resolve(tmpdir(), 'runloom-k8s-manifest-'));
  try {
    const path = resolve(directory, 'manifest.json');
    writeFileSync(path, JSON.stringify(object), {mode: 0o600});
    return callback(path);
  } finally { rmSync(directory, {recursive: true, force: true}); }
}

export function databaseResources(c, plan) {
  const {fullname: prefix, database} = plan;
  const mongo = database === 'mongodb';
  const labels = {'app.kubernetes.io/managed-by': 'runloom-local-setup', 'runloom.local/release': c.release};
  const metadata = name => ({name, namespace: c.namespace, labels});
  const selectors = {'runloom.local/database': prefix};
  const port = mongo ? 27017 : 5432;
  const container = {
    name: 'database', image: mongo ? 'mongo:8' : 'postgres:17-alpine', imagePullPolicy: 'IfNotPresent',
    ports: [{name: 'database', containerPort: port}],
    resources: {requests: {cpu: '100m', memory: '256Mi'}, limits: {cpu: '1', memory: '1Gi'}},
    volumeMounts: [{name: 'data', mountPath: mongo ? '/data/db' : '/var/lib/postgresql/data'}, {name: 'credentials', mountPath: '/run/secrets', readOnly: true}],
  };
  if (mongo) {
    container.command = ['bash', '-ec'];
    container.args = [mongoDNSGate + '\ncp /run/secrets/mongodb-replica-key /tmp/replica-key; chmod 400 /tmp/replica-key; chown mongodb:mongodb /tmp/replica-key; exec /usr/local/bin/docker-entrypoint.sh mongod --replSet runloom --bind_ip_all --keyFile /tmp/replica-key'];
    container.env = [{name: 'MONGO_INITDB_ROOT_USERNAME', value: 'runloom_root'}, {name: 'MONGO_INITDB_ROOT_PASSWORD_FILE', value: '/run/secrets/database-root-password'},
      {name: 'RUNLOOM_MONGO_HOST', value: `${prefix}-database`}, {name: 'RUNLOOM_POD_IP', valueFrom: {fieldRef: {fieldPath: 'status.podIP'}}}];
    container.startupProbe = {exec: {command: ['mongosh', '--quiet', '--eval', mongoProbe(`${prefix}-database:27017`, true)]}, periodSeconds: 10, timeoutSeconds: 5, failureThreshold: 30};
    container.readinessProbe = {exec: {command: ['mongosh', '--quiet', '--eval', mongoProbe(`${prefix}-database:27017`)]}, periodSeconds: 10, timeoutSeconds: 5};
    container.livenessProbe = {tcpSocket: {port: 'database'}, periodSeconds: 20, timeoutSeconds: 5, failureThreshold: 6};
  } else {
    container.env = [{name: 'POSTGRES_DB', value: 'postgres'}, {name: 'POSTGRES_USER', value: 'runloom_root'}, {name: 'POSTGRES_PASSWORD_FILE', value: '/run/secrets/database-root-password'}, {name: 'PGDATA', value: '/var/lib/postgresql/data/pgdata'}];
    container.readinessProbe = {exec: {command: ['pg_isready', '-U', 'runloom_root', '-d', 'postgres']}, periodSeconds: 5, timeoutSeconds: 5};
  }
  return [
    {apiVersion: 'v1', kind: 'Service', metadata: metadata(`${prefix}-database`), spec: {clusterIP: 'None', publishNotReadyAddresses: true, selector: selectors, ports: [{name: 'database', port, targetPort: 'database'}]}},
    {apiVersion: 'apps/v1', kind: 'StatefulSet', metadata: metadata(`${prefix}-database`), spec: {
      serviceName: `${prefix}-database`, replicas: 1, selector: {matchLabels: selectors},
      template: {metadata: {labels: selectors}, spec: {automountServiceAccountToken: false, containers: [container], volumes: [{name: 'credentials', secret: {secretName: `${prefix}-local-bootstrap`, items: ['database-root-password', ...(mongo ? ['mongodb-replica-key'] : [])].map(key => ({key, path: key}))}}]}},
      volumeClaimTemplates: [{metadata: {name: 'data'}, spec: {accessModes: ['ReadWriteOnce'], resources: {requests: {storage: '5Gi'}}}}],
    }},
  ];
}

export function provisionLocalDatabases(c, plan, run = command) {
  if (c.context !== `kind-${c.cluster}` || !['mongodb', 'postgres'].includes(plan.database) || !/^[a-z][a-z0-9-]{0,47}$/.test(plan.fullname)) throw new Error('Invalid local database bootstrap target.');
  const kube = ['--context', c.context, '--namespace', c.namespace];
  const get = (kind, name) => {const value = run(c.kubectl, [...kube, 'get', kind, name, '--ignore-not-found', '-o', 'json'], true); return value ? JSON.parse(value) : undefined;};
  const send = (verb, object) => withPrivateManifest(object, path => run(c.kubectl, [...kube, verb, '-f', path], true));
  if (!get('namespace', c.namespace)) send('create', {apiVersion: 'v1', kind: 'Namespace', metadata: {name: c.namespace}});
  const labels = {'app.kubernetes.io/managed-by': 'runloom-local-setup', 'runloom.local/release': c.release};
  const metadata = name => ({name, namespace: c.namespace, labels});
  const owned = object => {
    if (Object.entries(labels).some(([key, value]) => object.metadata?.labels?.[key] !== value)) throw new Error('Existing local database resources are not owned by this setup; refusing to overwrite.');
  };
  const name = `${plan.fullname}-local-bootstrap`;
  let stored = get('secret', name);
  if (!stored) {
    if (get('pvc', `data-${plan.fullname}-database-0`) || get('statefulset', `${plan.fullname}-database`)) throw new Error('Database credentials are missing for existing storage; refusing to generate replacements.');
    const data = {driver: plan.database, 'database-root-password': randomBytes(32).toString('base64url'), 'mongodb-replica-key': randomBytes(512).toString('base64')};
    const url = (user, password, db) => plan.database === 'postgres'
      ? `postgres://${user}:${password}@${plan.fullname}-database:5432/${db}?sslmode=disable`
      : `mongodb://${user}:${password}@${plan.fullname}-database:27017/${db}?replicaSet=runloom&authSource=${db}`;
    data['database-admin-url'] = url('runloom_root', data['database-root-password'], plan.database === 'postgres' ? 'postgres' : 'admin');
    for (const owner of owners) {
      data[`${owner}-database-password`] = randomBytes(32).toString('base64url');
      data[`${owner}-database-url`] = url(`runloom_${owner}`, data[`${owner}-database-password`], `runloom_${owner}`);
    }
    stored = {apiVersion: 'v1', kind: 'Secret', metadata: metadata(name), immutable: true, type: 'Opaque', data: Object.fromEntries(Object.entries(data).map(([key, value]) => [key, Buffer.from(value).toString('base64')]))};
    send('create', stored);
  }
  owned(stored);
  const decode = (object, key) => Buffer.from(object.data?.[key] || '', 'base64').toString('utf8');
  if (decode(stored, 'driver') !== plan.database) throw new Error('Cannot switch an existing local database driver.');
  for (const key of ['database-root-password', 'database-admin-url', ...owners.flatMap(owner => [`${owner}-database-password`, `${owner}-database-url`])]) if (decode(stored, key).length < 32) throw new Error('Stored local database credentials are incomplete; no replacement was generated.');
  const ensureSecret = (name, key, value) => {
    const existing = get('secret', name);
    if (existing) {
      owned(existing);
      if (value && decode(existing, key) !== value) throw new Error('Existing local credential disagrees with bootstrap state; refusing to rotate.');
      if (decode(existing, key).length < 32) throw new Error('Invalid existing local credential.');
      return decode(existing, key);
    }
    if (!value && get('deployment', `${plan.fullname}-auth`)) throw new Error('A workload token is missing from an existing installation; refusing to regenerate.');
    value ||= randomBytes(32).toString('base64url');
    send('create', {apiVersion: 'v1', kind: 'Secret', metadata: metadata(name), immutable: true, type: 'Opaque', data: {[key]: Buffer.from(value).toString('base64')}});
    return value;
  };
  for (const owner of owners) ensureSecret(`${plan.fullname}-${owner}-database`, 'url', decode(stored, `${owner}-database-url`));
  // Additive service bootstrap: do not rotate or rewrite the immutable original
  // bootstrap Secret or the trust registry. Assistant has no internal privileges.
  const assistantName = `${plan.fullname}-assistant-database`;
  const existingAssistant = get('secret', assistantName);
  if (existingAssistant) {
    owned(existingAssistant);
    if (decode(existingAssistant, 'password').length < 32 || !decode(existingAssistant, 'url')) throw new Error('Incomplete assistant database credential; refusing to replace.');
  } else {
    if (get('deployment', `${plan.fullname}-assistant`)) throw new Error('Assistant credentials are missing from an existing installation.');
    const password = randomBytes(32).toString('base64url');
    const url = plan.database === 'postgres'
      ? `postgres://runloom_assistant:${password}@${plan.fullname}-database:5432/runloom_assistant?sslmode=disable`
      : `mongodb://runloom_assistant:${password}@${plan.fullname}-database:27017/runloom_assistant?replicaSet=runloom&authSource=runloom_assistant`;
    send('create', {apiVersion:'v1',kind:'Secret',metadata:metadata(assistantName),immutable:true,type:'Opaque',data:{password:Buffer.from(password).toString('base64'),url:Buffer.from(url).toString('base64')}});
  }
  const trust = Object.fromEntries(roles.map(role => [role, createHash('sha256').update(ensureSecret(`${plan.fullname}-${role}-token`, 'token')).digest('hex')]));
  const trustName = `${plan.fullname}-service-trust`;
  const existingTrust = get('configmap', trustName);
  if (existingTrust) {
    owned(existingTrust);
    const current = JSON.parse(existingTrust.data?.['trust.json'] || '{}');
    if (roles.some(role => current[role] !== trust[role]) || Object.keys(current).length !== roles.length) throw new Error('Existing trust registry does not match the workload tokens; refusing to overwrite.');
  } else send('create', {apiVersion: 'v1', kind: 'ConfigMap', metadata: metadata(trustName), immutable: true, data: {'trust.json': JSON.stringify(trust)}});
  for (const resource of databaseResources(c, plan)) {
    const existing = get(resource.kind.toLowerCase(), resource.metadata.name);
    if (existing) owned(existing);
    send('apply', resource);
  }
  run(c.kubectl, [...kube, 'rollout', 'status', `statefulset/${plan.fullname}-database`, `--timeout=${c.timeout}`]);
  console.log('Local database server and credentials ready. Helm will provision and verify service databases before starting servers.');
}
