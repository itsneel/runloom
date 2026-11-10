// Local-only live verification. Tokens remain in memory; synthetic records are retained.
import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { setTimeout as delay } from 'node:timers/promises';
const base = (process.env.RUNLOOM_UI_URL || 'http://localhost:34200').replace(/\/$/,'');
assert(['localhost','127.0.0.1'].includes(new URL(base).hostname),'Local verification only');
const { chromium } = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({headless:true,...(process.env.RUNLOOM_CHROME?{executablePath:process.env.RUNLOOM_CHROME}:{})});
let phase='login', revoke, cancelConsumer;
try {
  const page=await browser.newPage();
  await page.goto(base);
  await page.locator('#username').fill('admin@runloom.test');
  await page.locator('#password').fill('RunloomLocal!123');
  const profile=page.waitForResponse(r=>new URL(r.url()).pathname==='/api/v1/studio/me'&&r.ok());
  await page.locator('#kc-login').click();
  const response=await profile, me=await response.json();
  const token=(await response.request().allHeaders()).authorization;
  const org=me.organizations.find(o=>o.name==='Runloom');
  let team=org?.teams.find(t=>t.name==='General');
  assert(token&&org&&team,'Default local owner/team missing');
  assert.equal(org.role,'owner');assert.equal(team.role,'admin');
  const request=async(path,body,method=body===undefined?'GET':'POST',credential=token,extra={})=>{
    const r=await fetch(base+path,{method,headers:{'Content-Type':'application/json',Authorization:credential,'X-Runloom-Team':team.id,...extra},body:body===undefined?undefined:JSON.stringify(body),signal:AbortSignal.timeout(15000)});
    if(!r.ok) { const failure=await r.json().catch(()=>({})); const code=failure.error?.code; assert.fail(`${method} ${path}: HTTP ${r.status}${typeof code==='string'&&/^[a-z_]+$/.test(code)?' '+code:''}`); }
    return r.json();
  };
  phase='internal endpoint rejection';
  assert([404,405].includes((await fetch(base+'/internal/v1/identity/view',{method:'POST'})).status),'Internal route is publicly accessible');
  const suffix=randomUUID();
  // Isolate verification from pre-migration audit records and previously spent
  // budgets. Check the default owner above, but never alter their real workspace.
  team={...team,...await request(`/api/v1/studio/orgs/${org.id}/teams`,{name:'Service verification '+suffix})};
  const prefix=`/api/v1/studio/orgs/${org.id}/teams/${team.id}`;
  phase='secret storage';
  const secretName='smoke-'+suffix;
  await request(prefix+'/secrets/'+secretName,{value:'synthetic-value'},'PUT');
  const secrets=await request(prefix+'/secrets');
  assert(secrets.configured&&secrets.items.some(s=>s.name===secretName));
  assert(!JSON.stringify(secrets).includes('synthetic-value'),'Secret leaked');
  await request(prefix+'/secrets/'+secretName,undefined,'DELETE');
  const audit=await request(`/api/v1/studio/orgs/${org.id}/audit`);
  assert(audit.some(e=>e.action==='secret.saved'&&e.target===team.id+'/'+secretName),'Cross-service secret audit missing');
  phase='management reads';
  for(const path of [prefix+'/llm-budget-policies',`/api/v1/studio/orgs/${org.id}/llm-analytics`,'/api/v1/studio/prompts']) await request(path);
  phase='prompt and budget ownership';
  const promptID='prompt-'+suffix;
  const prompt=await request(`/api/v1/studio/prompts/${promptID}/publish`,{id:promptID,name:'Service boundary prompt',template:{text:'Return a short greeting.'}});
  assert.equal(prompt.version,1); assert(prompt.hash);
  assert.equal((await request(`/api/v1/studio/prompts/${promptID}?version=1`)).hash,prompt.hash);
  const policyID=suffix;
  await request(prefix+'/llm-budget-policies/'+policyID,{id:policyID,name:'Smoke disabled policy',enabled:false,soft_usd:'5',hard_usd:'10',reason:'Verify independent Management storage'},'PUT');
  assert((await request(prefix+'/llm-budget-policies')).items.some(p=>p.id===policyID));
  const llmID='prompt-ref-'+suffix;
  await request('/api/v1/studio/definitions',{schema_version:'1.0',id:llmID,name:'Pinned prompt reference',version:0,trigger:{type:'manual'},input_schema:{type:'object'},steps:[{id:'llm',type:'llm',model:'not-executed',prompt_ref:{id:promptID,version:1}}],output:'${steps.llm.output}'});
  await request(`/api/v1/studio/definitions/${llmID}/publish`,{});
  phase='browser routes';
  for(const path of ['/management/teams','/workflows']) {
    await page.goto(base+path);
    await page.waitForLoadState('networkidle');
    assert(!new URL(page.url()).pathname.startsWith('/auth/'),'Session lost between apps');
  }
  phase='workflow execution';
  process.env.RUNLOOM_UI_URL=base;process.env.RUNLOOM_ACCESS_TOKEN=token.replace(/^Bearer /,'');process.env.RUNLOOM_TEAM_ID=team.id;
  process.env.RUNLOOM_TEST_NAMESPACE=(process.env.RUNLOOM_TEST_NAMESPACE_PREFIX||'runloom-local')+'-'+org.id;
  await import('./smoke.mjs');
  phase='service account provisioning';
  const definition='consumer-smoke-'+suffix;
  await request('/api/v1/studio/definitions',{schema_version:'1.0',id:definition,name:'Consumer service smoke',version:0,trigger:{type:'manual'},input_schema:{type:'object'},steps:[{id:'pause',type:'pause',message:'Consumer cancellation test'}],output:{ok:true}});
  await request(`/api/v1/studio/definitions/${definition}/publish`,{});
  const account=await request(prefix+'/service-accounts',{name:'Service split smoke',scopes:['workflows:read','runs:start','runs:read','runs:cancel'],grants:[{definition_id:definition,versions:[1],input_fields:[],output_fields:['ok']}],requests_per_minute:120,max_concurrent_runs:2});
  assert(account.client_id&&account.client_secret,'One-time client credential missing');
  revoke=()=>request(prefix+'/service-accounts/'+account.client_id+'/revoke',{});
  const machineLogin=await fetch(base+'/auth/realms/runloom/protocol/openid-connect/token',{method:'POST',body:new URLSearchParams({grant_type:'client_credentials',client_id:account.client_id,client_secret:account.client_secret})});
  assert(machineLogin.ok,'Client credentials login failed');
  const machine='Bearer '+(await machineLogin.json()).access_token;
  account.client_secret='';
  phase='consumer boundaries and idempotency';
  assert.equal((await fetch(base+'/api/v1/studio/me',{headers:{Authorization:machine}})).status,401);
  assert.equal((await fetch(base+'/api/v1/platform/me',{headers:{Authorization:machine}})).status,401);
  assert.equal((await fetch(base+'/api/v1/platform/staff',{headers:{Authorization:machine}})).status,401);
  await request('/api/v1/workflows',undefined,'GET',machine);
  const input={definition_id:definition,version:1,input:{}};
  const headers={'Idempotency-Key':suffix};
  const first=await request('/api/v1/runs',input,'POST',machine,headers);
  cancelConsumer=()=>request('/api/v1/runs/'+first.id+'/cancel',{},'POST',machine);
  const again=await request('/api/v1/runs',input,'POST',machine,headers);
  assert.equal(first.id,again.id);
  const run=first.id||first.workflow_id;assert(run,'Consumer run ID missing');
  await request('/api/v1/runs/'+run,undefined,'GET',machine);
  await request('/api/v1/runs/'+run+'/cancel',{},'POST',machine);
  cancelConsumer=undefined;
  phase='revocation';
  await revoke();revoke=undefined;
  await delay(100);
  assert([401,403].includes((await fetch(base+'/api/v1/workflows',{headers:{Authorization:machine}})).status),'Revoked account still authorized');
  if (process.env.RUNLOOM_TEST_SERVICE_DATABASE || process.env.RUNLOOM_TEST_NAMESPACES_ONLY === '1') {
    phase='live Temporal and deployed Management accounting';
    const synthetic=await request(`/api/v1/studio/orgs/${org.id}/teams`,{name:'LLM verification '+suffix});
    const {spawnSync}=await import('node:child_process');
    const {fileURLToPath}=await import('node:url');
    const namespaceOnly=process.env.RUNLOOM_TEST_NAMESPACES_ONLY === '1';
    const pattern=namespaceOnly?'^TestOrganizationNamespacesLive$':'^Test(ServiceLLMUsageLive|SecurityStageOneLive|OrganizationNamespacesLive|ProtectedDatabaseAndAtomicAudit)$';
    const result=spawnSync('go',['test','./internal/services/tests','./internal/store','-run',pattern,'-count=1','-timeout','300s'],{
      cwd:fileURLToPath(new URL('../server/workflows/',import.meta.url)),stdio:'inherit',
      env:{...process.env,RUNLOOM_TEST_TEAM_ID:synthetic.id,RUNLOOM_TEST_ORG_ID:org.id,RUNLOOM_TEST_AUTHORIZATION:token},
    });
    assert.equal(result.status,0,'Live Temporal/Management verification failed');
    console.log('PASS: automatic org namespaces, searchable tags, cross-org rejection, child/draft/schedule execution, persisted metadata and encrypted replay.');
    if(!namespaceOnly) {
      console.log('PASS: encrypted database payloads, encrypted Temporal history, encrypted replay, durable request receipts and cross-team rejection.');
      console.log('PASS: deployed Management usage API, real Temporal parent/child retries, replay, hard-budget pause and non-rebilling resume.');
    }
  }
  console.log('PASS: browser login, owner/team, independent app routes, secrets, management reads, workflow execution, service-account provisioning, consumer idempotency/cancel and revocation.');
} catch(error) {
  console.error(`Service smoke failed during ${phase}: ${error.name}${error.name==='AssertionError'?': '+error.message:''}`);process.exitCode=1;
} finally {
  if(cancelConsumer) await cancelConsumer().catch(()=>{});
  if(revoke) await revoke().catch(()=>{});
  delete process.env.RUNLOOM_ACCESS_TOKEN;
  await browser.close();
}
