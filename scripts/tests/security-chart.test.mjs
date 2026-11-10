import test from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
const cwd = fileURLToPath(new URL('../../', import.meta.url));
function render(args=[]) { return spawnSync('helm',['template','security','charts/runloom',...args],{cwd,encoding:'utf8'}); }
test('payload keyring and Temporal TLS identities mount only into execution workloads',()=>{
 const r=render(['--set','executionSecurity.existingKeyringSecret=execution-keys','--set','temporal.tls.enabled=true','--set','temporal.tls.existingSecret=temporal-identity','--set','temporal.tls.certKey=tls.crt','--set','temporal.tls.keyKey=tls.key']);
 assert.equal(r.status,0,'render failed; output withheld');
 const deployments=r.stdout.split(/\n---\n/).filter(d=>/^kind: Deployment$/m.test(d)&& /app.kubernetes.io\/component: (api|auth|workflows|management|worker|ui|workflows-ui|management-ui)\n/.test(d));
 assert.equal(deployments.length,8);
 for(const d of deployments){
  const execution=/name: security-runloom-(workflows|worker)\n/.test(d);
  assert.equal(d.includes('secretName: execution-keys'),execution);
  assert.equal(d.includes('secretName: temporal-identity'),execution);
 }
 assert(r.stdout.includes('RUNLOOM_PAYLOAD_KEYRING_FILE: /var/run/runloom-payload-keys/keyring.json'));
 assert(r.stdout.includes('TEMPORAL_TLS_ENABLED: "true"'));
 assert(r.stdout.includes('RUNLOOM_ALLOW_LEGACY_PAYLOADS: "false"'));
});
test('invalid security values fail rendering',()=>{
 for(const args of [
  ['--set','temporal.tls.certKey=tls.crt'],
  ['--set','temporal.tls.certKey=tls.crt','--set','temporal.tls.keyKey=tls.key'],
  ['--set','executionSecurity.sensitiveRunRoles[0]=everyone'],
  ['--set','auth.maxAuthAge=invalid'],
  ['--set','temporal.namespacePrefix=invalid/value'],
  ['--set','temporal.retentionDays=0'],
  ['-f','charts/runloom/values-production.yaml','--set','workflowSecrets.existingSecret='],
 ]) assert.notEqual(render(args).status,0,'accepted invalid security configuration');
});

test('org namespace configuration is shared by execution services only for both databases',()=>{
 for(const driver of ['mongodb','postgres']) {
  const r=render(['--set',`database.driver=${driver}`,'--set','temporal.namespacePrefix=runloom-test','--set','temporal.retentionDays=14']);
  assert.equal(r.status,0,'render failed; output withheld');
  assert.equal((r.stdout.match(/TEMPORAL_NAMESPACE_PREFIX: runloom-test/g)||[]).length,2);
  assert.equal((r.stdout.match(/TEMPORAL_RETENTION_DAYS: "14"/g)||[]).length,2);
 }
});
