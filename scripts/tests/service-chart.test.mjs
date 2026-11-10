import test from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('../../', import.meta.url));
function render(settings = [], success = true) {
  const result = spawnSync('helm', ['template', 'split', 'charts/runloom', ...settings], {cwd: root, encoding: 'utf8'});
  // Rendered manifests can contain generated keys: do not log them on failure.
  assert.equal(result.status === 0, success, 'Unexpected Helm validation result');
  return result.stdout;
}
function deployment(manifest, component) {
  const doc = manifest.split(/\n---\n/).find(doc => doc.includes('kind: Deployment\n') && doc.includes(`  name: split-runloom-${component}\n`));
  assert.ok(doc, `Missing ${component} Deployment`);
  return doc;
}
function config(manifest, component) {
  const doc = manifest.split(/\n---\n/).find(doc => doc.includes('  name: split-runloom-config\n'));
  return doc.split(`  ${component}.yaml: |\n`)[1]?.split(/\n  [a-z]+\.yaml: \|/)[0] || '';
}

test('both database drivers render independently owned deployments and credential mounts', () => {
  for (const driver of ['mongodb', 'postgres']) {
    const manifest = render(['--set', `database.driver=${driver}`, '--set', 'consumer.provisioning.enabled=true', '--set', 'consumer.provisioning.adminURL=http://keycloak:8080/auth', '--set', 'consumer.provisioning.existingSecret=provisioner']);
    for (const role of ['auth', 'workflows', 'management', 'worker']) {
      const owner = role === 'worker' ? 'workflows' : role;
      const doc = deployment(manifest, role);
      assert.ok(doc.includes(`image: "runloom-${role}:local"`));
      assert.ok(doc.includes(`command: ["/runloom-${role}"]`));
      assert.ok(doc.includes(`secretName: runloom-${owner}-database`));
      for (const other of ['auth', 'workflows', 'management'].filter(x => x !== owner)) assert.ok(!doc.includes(`secretName: runloom-${other}-database`));
      assert.ok(doc.includes(`secretName: runloom-${role}-token`));
      assert.ok(doc.includes('name: runloom-service-trust'));
      assert.ok(doc.includes('path: /readyz'));
      assert.equal(doc.includes('path: /readyz/vault'), ['workflows', 'worker'].includes(role));
      assert.equal(doc.includes('name: provisioner'), role === 'auth');
      assert.equal(doc.includes('name: workflow-secrets'), ['workflows', 'worker'].includes(role));
      const cfg = config(manifest, role);
      assert.ok(cfg.includes(`RUNLOOM_DATABASE_NAME: runloom_${owner}`));
      assert.ok(cfg.includes(`RUNLOOM_DATABASE_DRIVER: ${driver}`));
      assert.equal(cfg.includes('TEMPORAL_ADDRESS:'), ['workflows', 'worker'].includes(role));
      assert.equal(cfg.includes('RUNLOOM_KEYCLOAK_PROVISIONER_SECRET_FILE:'), role === 'auth');
      assert.equal(cfg.includes('RUNLOOM_SECRETS_KEY_FILE:'), ['workflows', 'worker'].includes(role));
    }
    const gateway = deployment(manifest, 'api');
    assert.ok(gateway.includes('image: "runloom-gateway:local"'));
    assert.ok(gateway.includes('command: ["/runloom-gateway"]'));
    assert.ok(!gateway.includes('secret:'));
    assert.ok(!gateway.includes('name: service-trust'));
    assert.ok(!/DATABASE|TOKEN|SECRET|TEMPORAL/.test(config(manifest, 'api')));
    for (const role of ['ui', 'workflows-ui', 'management-ui', 'platform-ui']) assert.ok(!deployment(manifest, role).includes('secret:'));
    assert.ok(!manifest.includes('  name: split-runloom-database\n'), 'Removed shared application database must not be created');
  }
});

test('database bootstrap is a blocking install/upgrade hook with admin access confined to the Job', () => {
  for (const driver of ['mongodb', 'postgres']) {
    const manifest = render(['--set', `database.driver=${driver}`, '--set', 'databaseSetup.enabled=true', '--set', 'databaseSetup.existingSecret=bootstrap-secret', '--set', 'images.database-setup.tag=bootstrap-test']);
    const job = manifest.split(/\n---\n/).find(doc => doc.includes('kind: Job\n') && doc.includes('name: split-runloom-database-setup\n'));
    assert.ok(job);
    for (const fragment of ['helm.sh/hook: pre-install,pre-upgrade', 'backoffLimit: 2', 'activeDeadlineSeconds: 300', 'ttlSecondsAfterFinished: 86400', 'automountServiceAccountToken: false', 'runAsNonRoot: true', 'defaultMode: 0440', 'name: "bootstrap-secret"', 'image: "runloom-database-setup:bootstrap-test"']) assert.ok(job.includes(fragment), fragment);
    for (const role of ['auth', 'workflows', 'management']) assert.ok(job.includes(`path: ${role}-database-url`));
    assert.ok(!job.includes('database-root-password'));
    assert.ok(!job.includes('mongodb-replica-key'));
    for (const role of ['auth', 'workflows', 'management', 'worker', 'api']) assert.ok(!deployment(manifest, role).includes('bootstrap-secret'));
    const cfg = manifest.split(/\n---\n/).find(doc => doc.includes('name: split-runloom-database-setup-config\n'));
    assert.ok(cfg.includes('helm.sh/hook-weight: "-10"'));
    assert.ok(cfg.includes(driver));
  }
  assert.ok(!render().includes('kind: Job\n'));
  render(['--set', 'databaseSetup.enabled=true'], false);
  render(['--set', 'databaseSetup.enabled=true', '--set', 'databaseSetup.existingSecret=bootstrap', '--set', 'splitServices.databases.auth.name=custom_auth'], false);
});

test('service scaling overrides are independent and inherit resource defaults', () => {
  const manifest = render(['--set', 'splitServices.auth.replicas=2', '--set', 'splitServices.management.resources.requests.cpu=250m']);
  assert.ok(deployment(manifest, 'auth').includes('replicas: 2'));
  assert.ok(deployment(manifest, 'workflows').includes('replicas: 1'));
  assert.ok(deployment(manifest, 'workflows').includes('cpu: 100m'));
  assert.ok(deployment(manifest, 'management').includes('cpu: 250m'));
  assert.ok(deployment(manifest, 'management').includes('memory: 128Mi'));
});

test('development identity bounds JVM memory and CPU and permits operator overrides', () => {
  const defaults = deployment(render(), 'keycloak');
  assert.ok(defaults.includes('name: JAVA_OPTS_KC_HEAP, value: "-Xms128m -Xmx384m"'));
  assert.ok(defaults.includes('name: JAVA_OPTS_APPEND, value: "-XX:ActiveProcessorCount=2"'));
  const custom = deployment(render(['--set-string', 'development.keycloakJavaOptsHeap=-Xms128m -Xmx512m', '--set-string', 'development.keycloakJavaOptsAppend=-XX:ActiveProcessorCount=1']), 'keycloak');
  assert.ok(custom.includes('value: "-Xms128m -Xmx512m"'));
  assert.ok(custom.includes('value: "-XX:ActiveProcessorCount=1"'));
  assert.ok(!render(['-f', 'charts/runloom/values-production.yaml']).includes('JAVA_OPTS_KC_HEAP'));
});

test('hosted renders use external dependencies for both database engines', () => {
  for (const driver of ['mongodb', 'postgres']) {
    const manifest = render(['-f', 'charts/runloom/values-production.yaml', '--set', `database.driver=${driver}`]);
    assert.ok(!manifest.includes('kind: StatefulSet'));
    assert.ok(config(manifest, 'worker').includes('TEMPORAL_ADDRESS: temporal-frontend.temporal.svc.cluster.local:7233'));
    assert.ok(config(manifest, 'auth').includes('RUNLOOM_OIDC_ISSUER: https://accounts.example.com/auth/realms/runloom'));
    assert.ok(config(manifest, 'auth').includes('RUNLOOM_KEYCLOAK_PROVISIONER_SECRET_FILE:'));
  }
  render(['-f', 'charts/runloom/values-production.yaml', '--set', 'consumer.provisioning.enabled=false'], false);
});

test('chart rejects monolith keys, missing references, shared credentials and invalid images', () => {
  for (const setting of [
    'splitServices.enabled=false',
    'splitServices.trustConfigMap=',
    'splitServices.databases.auth.existingSecret=',
    'splitServices.databases.auth.secretKey=',
    'splitServices.databases.auth.name=invalid-name',
    'splitServices.databases.management.name=runloom_auth',
    'splitServices.databases.management.existingSecret=runloom-auth-database',
    'splitServices.credentials.worker=runloom-auth-token',
    'splitServices.credentials.auth=',
    'splitServices.auth.replicas=0',
    'images.backend.repository=removed-monolith',
    'database.existingSecret=removed-shared-database',
    ...['auth', 'workflows', 'management', 'gateway'].flatMap(role => [`images.${role}.repository=`, `images.${role}.pullPolicy=invalid`]),
  ]) render(['--set', setting], false);
});
