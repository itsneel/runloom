import test from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
const root=fileURLToPath(new URL('../../',import.meta.url));

test('Compose assigns independent contexts and images to each owner',()=>{
 const args=['compose','--project-directory',root,'-f','compose/compose.yaml','-f','compose/compose.local.yaml','-f','compose/compose.secrets.yaml','-f','compose/compose.consumer.yaml','-f','compose/compose.services.yaml','--profile','local-setup','config','--format','json'];
 const result=spawnSync('docker',args,{cwd:root,encoding:'utf8',env:{...process.env,RUNLOOM_SERVICES_CONFIG_DIR:'/private/synthetic',RUNLOOM_SECRETS_KEY_PATH:'/private/synthetic/key',RUNLOOM_PROVISIONER_SECRET_PATH:'/private/synthetic/provisioner'}});
 assert.equal(result.status,0,'Compose render failed; configuration not printed');
 const c=JSON.parse(result.stdout);
 const contexts={auth:'server/auth',workflows:'server/workflows',management:'server/management',worker:'server/workflows',api:'server/gateway','database-setup':'tools/database-setup','local-admin':'server/auth'};
 for(const [role,path]of Object.entries(contexts))assert.equal(c.services[role].build.context,root+path);
 assert.equal(new Set(['auth','workflows','worker','management','api'].map(n=>c.services[n].image)).size,5);
 assert.equal(c.services.worker.build.dockerfile,'Dockerfile.worker');
 assert.equal(c.services.workflows.build.dockerfile,'Dockerfile');
 assert.deepEqual(c.services.api.secrets,undefined);
 assert.deepEqual(c.services['local-admin'].entrypoint,['/runloom-local-admin']);
 assert.equal(c.services['local-admin'].build.dockerfile,'Dockerfile.local-admin');
 assert.equal(c.services['local-admin'].image,'runloom-local-admin:local');
 assert.notEqual(c.services['local-admin'].image,c.services.auth.image);
 for (const [role,binary,url] of [
  ['api','gateway','http://127.0.0.1:8080/readyz'],
  ['auth','auth','http://127.0.0.1:8080/readyz'],
  ['management','management','http://127.0.0.1:8080/readyz'],
  ['assistant','assistant','http://127.0.0.1:8080/readyz'],
  ['workflows','workflows','http://127.0.0.1:8080/readyz/vault'],
  ['worker','worker','http://127.0.0.1:8081/readyz/vault'],
 ]) {
  assert.deepEqual(c.services[role].healthcheck.test,['CMD','/runloom-'+binary,'healthcheck',url]);
  assert(c.services[role].healthcheck.interval);
  assert(c.services[role].healthcheck.timeout);
  assert.equal(c.services[role].healthcheck.retries,role==='worker'?6:30);
 }
});

test('vendored authentication SDK matches the independent pinned release',()=>{
 const sdk=readFileSync(root+'sdk/go-auth/verifier.go','utf8');
 for(const role of ['auth','workflows','management']){
  assert.equal(readFileSync(root+`server/${role}/vendor/github.com/example/runloom/sdk/go-auth/verifier.go`,'utf8'),sdk);
  assert(!/^replace /m.test(readFileSync(root+`server/${role}/go.mod`,'utf8')));
 }
});

test('Helm selects independent images and routes worker accounting to Management',()=>{
 for(const driver of ['mongodb','postgres']){
  const result=spawnSync('helm',['template','split','charts/runloom','--set',`database.driver=${driver}`],{cwd:root,encoding:'utf8'});
  assert.equal(result.status,0,'Helm render failed; configuration not printed');
  assert(!result.stdout.includes('/runloom-local-admin'), 'Plain Helm must not automatically seed default accounts');
  assert(!result.stdout.includes('/runloom-healthcheck'));
  assert(result.stdout.includes('command: ["/runloom-gateway"]'));
  assert(result.stdout.includes('args: ["healthcheck", "http://split-runloom-ui/api/v1/health"]'));
  for(const role of ['auth','workflows','worker','management','gateway'])assert(result.stdout.includes(`image: "runloom-${role}:local"`));
  const worker=result.stdout.split('worker.yaml: |')[1]?.split('auth.yaml: |')[0];
  assert(worker?.includes('RUNLOOM_MANAGEMENT_URL: http://split-runloom-management:8080'));
  assert(worker?.includes('RUNLOOM_DATABASE_NAME: runloom_workflows'));
 }
});
