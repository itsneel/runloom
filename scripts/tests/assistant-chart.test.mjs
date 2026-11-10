import test from 'node:test';
import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
const cwd=fileURLToPath(new URL('../../',import.meta.url));
function render(args=[]){const r=spawnSync('helm',['template','assistant-check','charts/runloom',...args],{cwd,encoding:'utf8'});assert.equal(r.status,0,'Helm rendering failed; output withheld');return r.stdout;}
test('assistant owns an isolated database and has no execution or internal-service credentials',()=>{
 for(const driver of ['postgres','mongodb']){
  const rendered=render(['--set','database.driver='+driver]);
  const deployment=rendered.split('\n---\n').find(doc=>/^kind: Deployment$/m.test(doc) && /name: assistant-check-runloom-assistant\n/.test(doc));
  assert(deployment);
  assert(deployment.includes('secretName: runloom-assistant-database'));
  assert(deployment.includes('automountServiceAccountToken: false'));
  assert(deployment.includes('readOnlyRootFilesystem: true'));
  assert(!/service-token|service-trust|workflow-secrets|payload-keyring/.test(deployment));
  assert(rendered.includes('RUNLOOM_ASSISTANT_URL: http://assistant-check-runloom-assistant:8080'));
  const config=rendered.split('\n---\n').find(doc=>/^kind: ConfigMap$/m.test(doc) && doc.includes('name: assistant-check-runloom-assistant-config'));
  assert(config?.includes('RUNLOOM_WORKFLOWS_URL: http://assistant-check-runloom-workflows:8080'));
 }
});
test('provider Secret mounts only into assistant; disabling assistant removes its route and resources',()=>{
 const rendered=render(['--set','assistant.apiKeySecret=provider-only']);
 const users=rendered.split('\n---\n').filter(doc=>/^kind: Deployment$/m.test(doc) && doc.includes('secretName: provider-only'));
 assert.equal(users.length,1);assert(users[0].includes('name: assistant-check-runloom-assistant'));
 const disabled=render(['--set','assistant.enabled=false']);
 assert(!disabled.includes('name: assistant-check-runloom-assistant'));
 assert(!disabled.includes('RUNLOOM_ASSISTANT_URL'));
});
test('bootstrap includes assistant owner credentials without exposing admin access to the assistant',()=>{
 const rendered=render(['--set','databaseSetup.enabled=true','--set','databaseSetup.existingSecret=bootstrap']);
 const job=rendered.split('\n---\n').find(doc=>/^kind: Job$/m.test(doc) && doc.includes('database-setup'));
 assert(job?.includes('path: assistant-database-url'));
 assert(job?.includes('path: assistant-database-password'));
 assert(rendered.includes('RUNLOOM_ASSISTANT_PROVIDER: disabled'));
 for(const setting of ['assistant.provider=unknown','assistant.maxTokens=0','assistant.replicas=0']){
  assert.notEqual(spawnSync('helm',['template','test','charts/runloom','--set',setting],{cwd,encoding:'utf8'}).status,0);
 }
});
