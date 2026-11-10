// Local-only collection check; does not authenticate, start workflows or read data.
import assert from 'node:assert/strict';
import {setTimeout as delay} from 'node:timers/promises';
const api=process.env.RUNLOOM_UI_URL||'http://localhost:34200';
const prom=process.env.RUNLOOM_PROMETHEUS_URL||'http://localhost:39090';
for(const value of [api,prom]) assert(['localhost','127.0.0.1'].includes(new URL(value).hostname),'Local verification only');
const query=async(q)=>{
 const response=await fetch(prom+'/api/v1/query?'+new URLSearchParams({query:q}),{signal:AbortSignal.timeout(5000)});
 assert(response.ok,'Prometheus query failed');const data=await response.json();assert.equal(data.status,'success');return data.data.result;
};
const marker='never-export-this-private-input';
const ids=new Set();
const spoofed='0123456789abcdef0123456789abcdef';
for(const path of ['/api/v1/studio/me','/api/v1/studio/assistant/conversations']) {
 for(let i=0;i<3;i++) {
  const response=await fetch(api+path+'?secret='+marker,{headers:{Authorization:'Bearer '+marker,'X-Runloom-Request-ID':spoofed},signal:AbortSignal.timeout(10000)});
  assert.equal(response.status,401,'Invalid identity accepted');
  const id=response.headers.get('x-runloom-request-id');
  assert.match(id||'',/^[0-9a-f]{32}$/,'Response missing canonical request ID');
  assert(id!==spoofed&&!ids.has(id),'Gateway trusted caller ID or reused another request ID');
  ids.add(id);await response.arrayBuffer();
 }
}
let ready=false;
for(let i=0;i<24;i++) {
 try {
  const up=await query('up{job="runloom"}');
  const counts=await query('runloom_http_requests_total');
  const hist=await query('http_server_request_duration_seconds_count');
  const active=await query('http_server_active_requests');
  const required=['api','auth','workflows','management','worker','assistant'];
  assert(!JSON.stringify([counts,hist,active]).includes(marker),'Sensitive payload in metrics');
  assert(![...ids].some(id=>JSON.stringify([counts,hist,active]).includes(id)),'Request ID in metric labels');
  if(required.every(c=>up.some(s=>s.metric.component===c&&Number(s.value[1])===1)&&[counts,hist,active].every(samples=>samples.some(s=>s.metric.component===c)))&&['api','auth','assistant'].every(c=>counts.some(s=>s.metric.component===c&&s.metric.http_response_status_code==='401'&&Number(s.value[1])>0))) {
   ready=true;break;
  }
 } catch(error) {if(error.name==='AssertionError')throw error;}
 await delay(2500);
}
assert(ready,'Six healthy targets and API latency/count/active metrics were not available within 60 seconds');
console.log('PASS: six collector targets including Lumi, API counts/latency/active metrics, 401 recording, canonical response IDs, and no payload markers or request IDs in metrics.');
