import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
const cwd=fileURLToPath(new URL('../../',import.meta.url));
test('database connection private destinations are opt-in and only configured in workflows and worker',()=>{
 for(const driver of ['mongodb','postgres']){
  const r=spawnSync('helm',['template','connection-check','charts/runloom','--set',`database.driver=${driver}`,'--set','databaseConnections.allowedAddresses[0]=reporting.internal:5432'],{cwd,encoding:'utf8'});
  assert.equal(r.status,0,r.stderr);const fragments=r.stdout.split(/^---$/m);
  const configs=fragments.filter(s=>s.includes('kind: ConfigMap')&&s.includes('RUNLOOM_DATABASE_NODE_ALLOWED_ADDRESSES'));
  assert.equal(configs.length,1);
  const entries=[...configs[0].matchAll(/^  (\w+)\.yaml: \|\n((?:    .*\n|\n)*)/gm)];
  assert.equal(entries.length,5);
  for(const [,name,body]of entries){assert.equal(body.includes('RUNLOOM_DATABASE_NODE_ALLOWED_ADDRESSES'),['workflows','worker'].includes(name));}
  assert.equal((configs[0].match(/reporting.internal:5432/g)||[]).length,2);
 }
 const file=JSON.parse(readFileSync(cwd+'compose/config/database-connections.yaml'));
 assert.equal(file.RUNLOOM_DATABASE_NODE_ALLOWED_ADDRESSES,'');
 const setup=readFileSync(cwd+'scripts/local-services.mjs','utf8');assert.match(setup,/role==='workflows'\|\|role==='worker'.*readJSON\('database-connections'\)/);
});
