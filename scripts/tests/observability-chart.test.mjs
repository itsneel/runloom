import test from 'node:test';
import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
const cwd=fileURLToPath(new URL('../../',import.meta.url));
const render=(args=[])=>spawnSync('helm',['template','observe','charts/runloom',...args],{cwd,encoding:'utf8'});
test('observability is opt-in and independently renders for both databases',()=>{
 assert(!render().stdout.includes('name: telemetry'));
 for(const db of ['mongodb','postgres']) {
  const r=render(['--set','observability.enabled=true','--set',`database.driver=${db}`]);
  assert.equal(r.status,0,'render failed; potentially sensitive output withheld');
  assert.equal((r.stdout.match(/- name: telemetry\n/g)||[]).length,6);
  assert.equal((r.stdout.match(/RUNLOOM_TELEMETRY_ENDPOINT:/g)||[]).length,6);
  assert(r.stdout.includes('kind: NetworkPolicy'));
  assert(r.stdout.includes('role: pod'));
  assert(r.stdout.includes('regex: telemetry;metrics'));
  assert(r.stdout.includes('127.0.0.1:4318'));
  assert(r.stdout.includes('helm.sh/resource-policy: keep'));
  for(const d of r.stdout.split('\n---\n').filter(d=>/^kind: Deployment$/m.test(d)&&d.includes('name: telemetry'))) {
   const sidecar=d.slice(d.indexOf('- name: telemetry\n'),d.indexOf('\n      volumes:'));
   assert(!/secret|database|service-token|service-trust/i.test(sidecar),'sidecar mounts application credentials');
   assert(sidecar.includes('readOnlyRootFilesystem: true'));
  }
 }
});
test('HPA targets application containers when sidecars are enabled',()=>{
 const r=render(['--set','observability.enabled=true','--set','autoscaling.workloads.api.enabled=true']);
 assert.equal(r.status,0);
 assert(r.stdout.includes('type: ContainerResource'));
 assert(r.stdout.includes('container: api'));
});
test('external collection does not require bundled Prometheus',()=>{
 const r=render(['--set','observability.enabled=true','--set','observability.prometheus.enabled=false']);
 assert.equal(r.status,0);assert(!r.stdout.includes('kind: PersistentVolumeClaim\nmetadata:\n  name: observe-runloom-prometheus'));
 assert.equal((r.stdout.match(/- name: telemetry\n/g)||[]).length,6);
});
test('disabling Assistant leaves the original five collectors',()=>{
 const r=render(['--set','observability.enabled=true','--set','assistant.enabled=false']);
 assert.equal(r.status,0);
 assert.equal((r.stdout.match(/- name: telemetry\n/g)||[]).length,5);
 assert.equal((r.stdout.match(/RUNLOOM_TELEMETRY_ENDPOINT:/g)||[]).length,5);
});
test('Assistant collector supports operator config and contains no application secrets',()=>{
 const r=render(['--set','observability.enabled=true','--set','observability.collectorConfigMap=custom-collector']);
 assert.equal(r.status,0);
 const d=r.stdout.split('\n---\n').find(d=>/^kind: Deployment$/m.test(d)&&/name: observe-runloom-assistant\n/.test(d));
 assert(d?.includes('name: telemetry'));
 assert(d.includes('checksum/observability:'));
 assert(d.includes('name: custom-collector'));
 assert(r.stdout.includes('values: [api, auth, workflows, management, worker, assistant]'));
});
