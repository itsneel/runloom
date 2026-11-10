import test from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { readFileSync } from 'node:fs';

const root = fileURLToPath(new URL('../../', import.meta.url));
function render(args = []) {
  const result = spawnSync('helm', ['template', 'vault-check', 'charts/runloom', ...args], { cwd: root, encoding: 'utf8' });
  // Never include rendered Secrets in test output, including failure diagnostics.
  assert.ok(result.status === 0, 'Helm rendering failed');
  return result.stdout;
}
function verifyMounts(rendered, expectedName) {
  const deployments = rendered.split(/\n---\n/).filter(doc => /^kind: Deployment$/m.test(doc));
  assert.ok(deployments.length >= 5);
  assert.equal(deployments.filter(doc => /name: vault-check-runloom-(workflows|worker)\n/.test(doc)).length, 2);
  for (const doc of deployments) {
    const backend = /name: vault-check-runloom-(workflows|worker)\n/.test(doc);
    assert.equal(doc.includes('name: RUNLOOM_CONFIG_FILE'), /name: vault-check-runloom-(api|auth|workflows|management|assistant|worker)\n/.test(doc));
    assert.equal(doc.includes('name: workflow-secrets'), backend && !!expectedName);
    if (backend && expectedName) {
      assert.ok(doc.includes(`secretName: ${expectedName}`));
      assert.ok(/name: workflow-secrets\s+mountPath: \/var\/run\/runloom-workflow-secrets\s+readOnly: true/.test(doc));
      assert.ok(/secretName: [^\n]+\s+defaultMode: 0440/.test(doc));
      assert.ok(rendered.includes('RUNLOOM_SECRETS_KEY_FILE: /var/run/runloom-workflow-secrets/encryption-key'));
    }
  }
}
test('development creates a retained 32-byte key mounted only into Workflows and worker', () => {
  const rendered = render();
  const key = rendered.split(/\n---\n/).find(doc => doc.includes('# Source: runloom/templates/workflow-key.yaml'));
  assert.ok(key, 'development key resource missing');
  assert.ok(key.includes('helm.sh/resource-policy: keep'));
  const encoded = key.match(/"encryption-key": "([A-Za-z0-9+/=]+)"/)?.[1];
  assert.ok(encoded && Buffer.from(encoded, 'base64').length === 32, 'key must be exactly 32 bytes');
  verifyMounts(rendered, 'vault-check-runloom-workflow-key');
});

test('connected upgrades refuse to silently replace a missing key on existing installations', () => {
  const source = readFileSync(new URL('../../charts/runloom/templates/workflow-key.yaml', import.meta.url), 'utf8');
  assert.ok(source.includes('lookup "apps/v1" "Deployment"'));
  assert.ok(source.includes('refusing to generate a replacement key'));
});
test('an explicit key overrides automatic provisioning; hosted defaults never generate keys', () => {
  for (const args of [[], ['-f', 'charts/runloom/values-production.yaml']]) {
    const rendered = render([...args, '--set', 'workflowSecrets.existingSecret=operator-key', '--set', 'workflowSecrets.secretKey=custom-key']);
    assert.ok(!rendered.includes('# Source: runloom/templates/workflow-key.yaml'));
    verifyMounts(rendered, 'operator-key');
    assert.equal((rendered.match(/key: custom-key/g) || []).length, 2);
  }
  const hosted = render(['-f', 'charts/runloom/values-production.yaml']);
  assert.ok(!hosted.includes('# Source: runloom/templates/workflow-key.yaml'));
  verifyMounts(hosted, 'runloom-vault-key');
});

test('external secrets emit provider references, retain an immutable key and share its mount', () => {
  for (const hosted of [false, true]) {
    const rendered = render([
      ...(hosted ? ['-f', 'charts/runloom/values-production.yaml'] : []),
      '--set', 'workflowSecrets.existingSecret=',
      '--set', 'workflowSecrets.externalSecret.enabled=true',
      '--set', 'workflowSecrets.externalSecret.targetName=externally-managed-key',
      '--set', 'workflowSecrets.externalSecret.secretStoreRef.name=platform-store',
      '--set', 'workflowSecrets.externalSecret.secretStoreRef.kind=SecretStore',
      '--set', 'workflowSecrets.externalSecret.remoteRef.key=runloom/vault',
      '--set', 'workflowSecrets.externalSecret.remoteRef.property=key',
    ]);
    assert.ok(!rendered.includes('# Source: runloom/templates/workflow-key.yaml'));
    const external = rendered.split(/\n---\n/).find(doc => doc.includes('kind: ExternalSecret'));
    assert.ok(external);
    for (const fragment of ['apiVersion: external-secrets.io/v1', 'refreshPolicy: CreatedOnce', 'creationPolicy: Orphan', 'deletionPolicy: Retain', 'immutable: true', 'name: "platform-store"', 'kind: "SecretStore"', 'key: "runloom/vault"', 'property: "key"', 'decodingStrategy: "Base64"']) assert.ok(external.includes(fragment), fragment);
    verifyMounts(rendered, 'externally-managed-key');
  }
});

test('external mode fails for missing references, ambiguous ownership and invalid decoding', () => {
  const base = ['--set', 'workflowSecrets.externalSecret.enabled=true'];
  for (const args of [
    base,
    [...base, '--set', 'workflowSecrets.externalSecret.secretStoreRef.name=store'],
    [...base, '--set', 'workflowSecrets.existingSecret=other-key'],
    [...base, '--set', 'workflowSecrets.externalSecret.remoteRef.decodingStrategy=Auto'],
    [...base, '--set', 'workflowSecrets.externalSecret.secretStoreRef.kind=Invalid'],
  ]) {
    const result = spawnSync('helm', ['template', 'check', 'charts/runloom', ...args], { cwd: root, encoding: 'utf8' });
    assert.ok(result.status !== 0, 'invalid external secret configuration accepted');
  }
});
