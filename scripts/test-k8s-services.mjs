// Local kind only. Browser credentials/tokens and Kubernetes key bytes are never
// printed. Temporary forwards and the private replay key are removed on exit.
import { spawn } from 'node:child_process';
import { mkdtemp, writeFile, rm } from 'node:fs/promises';
import { existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createServer } from 'node:net';
import { setTimeout as delay } from 'node:timers/promises';
import { configuration, execute } from './local-k8s.mjs';
import { command } from './local-k8s-command.mjs';
import { assertFreePort } from './local-k8s-open.mjs';

const children=[];
let directory;
try {
  const c=configuration();
  const plan=execute('open',c); // validates loopback API, kind cluster and Runloom release
  const values=JSON.parse(command(c.helm,['get','values',c.release,'--kube-context',c.context,'-n',c.namespace,'-o','json'],true));
  if(values.development?.enabled!==true) throw new Error('Live local tests require development mode.');
  if(values.executionSecurity?.existingKeyringSecret) throw new Error('This local test runner expects the default development payload key.');
  const studioPort=Number(new URL(plan.origin).port||80);
  await assertFreePort(studioPort);
  const temporalPort=await new Promise((resolve,reject)=>{
    const s=createServer();s.once('error',reject);s.listen(0,'127.0.0.1',()=>{const p=s.address().port;s.close(()=>resolve(p));});
  });
  const keyName=values.workflowSecrets?.existingSecret || values.workflowSecrets?.externalSecret?.targetName || `${plan.fullname}-workflow-key`;
  const keyField=values.workflowSecrets?.secretKey || 'encryption-key';
  const secret=JSON.parse(command(c.kubectl,['--context',c.context,'-n',c.namespace,'get','secret',keyName,'-o','json'],true));
  const key=Buffer.from(secret.data?.[keyField]||'','base64');
  if(key.length!==32) throw new Error('Development replay key is unavailable.');
  directory=await mkdtemp(resolve(tmpdir(),'runloom-namespace-test-'));
  await writeFile(resolve(directory,'workflow-key'),key,{mode:0o600});key.fill(0);
  for(const [service,port,target] of [[`${plan.fullname}-ui`,studioPort,80],[`${plan.fullname}-temporal`,temporalPort,7233]]) {
    const child=spawn(c.kubectl,['--context',c.context,'-n',c.namespace,'port-forward','--address','127.0.0.1',`service/${service}`,`${port}:${target}`],{stdio:'ignore'});
    child.on('error',()=>{});children.push(child);
  }
  let ready=false;
  for(let n=0;n<100;n++) {
    if(children.some(p=>p.exitCode!==null)) throw new Error('A local port-forward failed.');
    try { const r=await fetch(plan.origin+'/api/v1/health',{signal:AbortSignal.timeout(1000)}); if(r.ok){ready=true;break} } catch {}
    await delay(300);
  }
  if(!ready) throw new Error('Local Studio did not become ready.');
  const playwright=fileURLToPath(new URL('../Website/node_modules/playwright/index.mjs',import.meta.url));
  if(!process.env.RUNLOOM_PLAYWRIGHT_MODULE && existsSync(playwright)) process.env.RUNLOOM_PLAYWRIGHT_MODULE=playwright;
  const chrome='/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
  if(!process.env.RUNLOOM_CHROME && process.platform==='darwin' && existsSync(chrome)) process.env.RUNLOOM_CHROME=chrome;
  delete process.env.RUNLOOM_TEST_SERVICE_DATABASE;
  Object.assign(process.env,{RUNLOOM_UI_URL:plan.origin,RUNLOOM_TEST_TEMPORAL_ADDRESS:`localhost:${temporalPort}`,RUNLOOM_TEST_SERVICE_CONFIG_DIR:directory,RUNLOOM_TEST_NAMESPACES_ONLY:'1',RUNLOOM_TEST_NAMESPACE_PREFIX:values.temporal?.namespacePrefix||plan.fullname});
  console.log(`Verifying ${c.context}/${c.namespace}/${c.release} (${values.database.driver}) through ${plan.origin}`);
  await import('./services-smoke.mjs');
} catch(error) {
  console.error(`Local Kubernetes verification failed: ${error.message}`);process.exitCode=1;
} finally {
  for(const child of children) child.kill('SIGTERM');
  if(directory) await rm(directory,{recursive:true,force:true});
}
