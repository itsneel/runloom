import test from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { browserCommand, websiteOptions, assertFreePort, waitForURL, bootstrapJob, connectionInfo, ensureLocalAdmin } from '../local-k8s-open.mjs';
import { readFileSync } from 'node:fs';

const member = {email: 'member@runloom.test', default_password: 'public-member-default', created: true, default_org_role: 'member', default_team_role: 'editor'};
const platform = Object.fromEntries(['platform_admin', 'platform_staff'].map(role => [role, {email: `${role.replace('_', '-')}@runloom.test`, default_password: 'public-platform-default', created: true, role}]));

test('actual CLI open entry point resolves imports and reaches launcher validation', () => {
  const result = spawnSync(process.execPath, [
    '--import', fileURLToPath(new URL('./fixtures/k8s-cli-preload.mjs', import.meta.url)),
    fileURLToPath(new URL('../local-k8s.mjs', import.meta.url)), 'open',
  ], {
    encoding: 'utf8', timeout: 10000,
    env: { ...process.env, DOCKER: 'docker', KIND: 'kind', KUBECTL: 'kubectl', HELM: 'helm',
      DOCKER_HOST: '', DOCKER_CONTEXT: '', RUNLOOM_ENV: 'local', K8S_CLUSTER: 'runloom-local',
      K8S_NAMESPACE: 'runloom', K8S_RELEASE: 'runloom', K8S_PORT: '', K8S_DATABASE: '',
      K8S_TIMEOUT: '10m', K8S_WEBSITE_PORT: '0' },
  });
  assert.equal(result.status, 1, result.stderr);
  assert.match(result.stderr, /K8S_WEBSITE_PORT must be between/);
  assert.doesNotMatch(result.stderr, /unsettled top-level await/);
});

test('local admin Job references installed database settings, never resets passwords, and expires', () => {
  const job = bootstrapJob({ namespace: 'runloom', fullname: 'custom-runloom', databaseSecretKey: 'url' }, 'bootstrap:test', 'bootstrap-test');
  const pod = job.spec.template.spec;
  assert.equal(pod.automountServiceAccountToken, false);
  assert.equal(job.spec.backoffLimit, 2);
  assert.ok(job.spec.ttlSecondsAfterFinished > 0);
  assert.deepEqual(pod.containers[0].args, ['--json']);
  assert.equal(pod.volumes.find(v => v.name === 'database').secret.secretName, 'custom-runloom-auth-database');
  assert.deepEqual(pod.containers[0].command, ['/runloom-local-admin']);
  assert.equal(pod.volumes.find(v => v.name === 'configuration').configMap.items[0].key, 'auth.yaml');
  assert.ok(!pod.containers[0].envFrom);
  assert.ok(!JSON.stringify(job).includes('RunloomLocal!123'));
});

test('administrator bootstrap waits for rollouts and uses the configured dedicated image', async () => {
  const calls = [];
  const run = (tool, args) => {
    calls.push(args);
    assert(!args.includes('get'), 'must not discover the Auth runtime image');
    if (args.includes('create')) {
      const job = JSON.parse(readFileSync(args[args.indexOf('-f')+1], 'utf8'));
      assert.equal(job.spec.template.spec.containers[0].image, 'registry.example/admin:verified');
      assert.equal(job.spec.template.spec.containers[0].imagePullPolicy, 'Never');
      assert.equal(calls.filter(c => c.includes('rollout')).length, 2);
    }
    if (args.includes('logs')) return JSON.stringify({email: 'admin@runloom.test', organization: 'Runloom', team: 'General', created: true, default_password: 'RunloomLocal!123', member, ...platform});
    return '';
  };
  await ensureLocalAdmin({kubectl: 'kubectl', context: 'kind-runloom-local', namespace: 'runloom', timeout: '5m'}, {fullname: 'runloom', namespace: 'runloom', localAdminImage: {repository: 'registry.example/admin', tag: 'verified', pullPolicy: 'Never'}}, run);
  assert.ok(calls[0].includes('deployment/runloom-keycloak'));
  assert.ok(calls[1].includes('deployment/runloom-auth'));
  assert.equal(calls.some(c => c.includes('--reset-password')), false);
});

test('missing admin image fails before any cluster operation; job failure propagates', async () => {
  await assert.rejects(ensureLocalAdmin({}, {}, () => assert.fail('must not call tools')), /make k8s-upgrade/);
  const plan = {fullname: 'runloom', namespace: 'runloom', localAdminImage: {repository: 'admin', tag: 'test', pullPolicy: 'IfNotPresent'}};
  await assert.rejects(ensureLocalAdmin({kubectl: 'kubectl'}, plan, (_tool, args) => {
    if (args.includes('wait')) throw new Error('job failed');
    if (args.includes('logs')) assert.fail('failed setup must not report success');
    return '';
  }), /job failed/);
});

test('connection output distinguishes a newly created password from a preserved unknown password', () => {
  const plan = { origin: 'http://localhost:4200', context: 'kind-runloom-local', namespace: 'runloom' };
  const account = { email: 'admin@runloom.test', default_password: 'public-local-default', created: true, member, ...platform };
  assert.match(connectionInfo(plan, { url: 'http://127.0.0.1:4300' }, account), /admin@runloom.test.*public-local-default/);
  assert.match(connectionInfo(plan, { url: 'http://127.0.0.1:4300' }, { ...account, created: false }), /Existing passwords are preserved/);
  assert.match(connectionInfo(plan, { url: 'http://127.0.0.1:4300' }, account), /member@runloom.test.*member \/ editor/);
  assert.match(connectionInfo(plan, { url: 'http://127.0.0.1:4300' }, {...account, member: {...member, created: false}}), /existing member roles and dedicated platform access decisions are preserved/);
  assert.throws(() => connectionInfo(plan, {}, {...account, member: undefined}), /team-member setup/);
  assert.match(connectionInfo(plan, {}, account), /platform-admin@runloom.test.*platform_admin/);
  assert.match(connectionInfo(plan, {}, account), /platform-staff@runloom.test.*platform_staff/);
  assert.match(connectionInfo(plan, {}, account), /\| admin\s+\| Keycloak admin.*development-only/);
  assert.match(connectionInfo(plan, {}, account), /Keycloak admin: http:\/\/localhost:4200\/auth\/admin\/master\/console\//);
  assert.match(connectionInfo(plan, {}, {...account, platform_staff: {...platform.platform_staff, role: '', created: false}}), /none \(revoked\)/);
  assert.throws(() => connectionInfo(plan, {}, {...account, platform_staff: undefined}), /platform account setup/);
});

test('website defaults and custom ports do not collide with Studio', () => {
  assert.deepEqual(websiteOptions('http://localhost:4200', {}), { port: 4300, studioPort: 4200, url: 'http://127.0.0.1:4300', browser: true });
  assert.equal(websiteOptions('http://localhost:4300', { K8S_WEBSITE_PORT: '4400', K8S_OPEN_BROWSER: '0' }).browser, false);
  for (const port of ['4300', '0', '65536', 'x']) assert.throws(() => websiteOptions('http://localhost:4300', { K8S_WEBSITE_PORT: port }));
});

test('browser launching uses OS commands without a shell', () => {
  const url = 'http://127.0.0.1:4300';
  assert.deepEqual(browserCommand(url, 'darwin'), ['open', [url]]);
  assert.deepEqual(browserCommand(url, 'linux'), ['xdg-open', [url]]);
  assert.deepEqual(browserCommand(url, 'win32'), ['rundll32', ['url.dll,FileProtocolHandler', url]]);
});

test('occupied ports fail safely; readiness waits for content and supports cancellation', async () => {
  const server = createServer((request, response) => { response.end('ready'); });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const port = server.address().port;
  try {
    await assert.rejects(assertFreePort(port), /occupied/);
    await waitForURL(`http://127.0.0.1:${port}`, async response => await response.text() === 'ready', new AbortController().signal, 1000);
    const controller = new AbortController();
    controller.abort();
    await assert.rejects(waitForURL(`http://127.0.0.1:${port}`, () => true, controller.signal), /abort/i);
  } finally {
    await new Promise(resolve => server.close(resolve));
  }
  await assertFreePort(port);
});
