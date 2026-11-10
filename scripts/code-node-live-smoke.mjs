// Local-only integration test. Creates one unique sample workflow and two runs;
// retains them for inspection. No secrets/tokens are logged or persisted.
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
const { chromium } = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.RUNLOOM_UI_URL || 'http://localhost:14200';
assert(['localhost','127.0.0.1'].includes(new URL(base).hostname),'Local test only');
const browser = await chromium.launch({headless:true,...(process.env.RUNLOOM_CHROME ? {executablePath:process.env.RUNLOOM_CHROME}:{})});
try {
  const page=await browser.newPage({viewport:{width:1600,height:1000}});
  const errors=[];
  page.on('pageerror',error=>errors.push(error.name));
  await page.goto(base);
  await page.locator('#username').fill('admin@runloom.test');
  await page.locator('#password').fill('RunloomLocal!123');
  const ready=page.waitForResponse(response=>new URL(response.url()).pathname==='/api/v1/studio/me' && response.ok());
  await page.locator('#kc-login').click();
  const response=await ready;
  const me=await response.json();
  const org=me.organizations.find(org=>org.name==='Runloom');
  const team=org.teams.find(team=>team.name==='General');
  const headers={authorization:(await response.request().allHeaders()).authorization,'X-Runloom-Team':team.id};
  assert(headers.authorization,'Expected interactive Studio token');
  await page.locator('runloom-studio').waitFor();
  const api=async(method,path,data,expected=200)=>{
    const response=await page.request.fetch(`${base}${path}`,{method,headers,...(data===undefined?{}:{data})});
    assert.equal(response.status(),expected,`${method} ${path}: ${response.status()}`);
    return response.json();
  };
  const code={runtime:'quickjs-ng-0.11.0-v1',source:'return {message: `Hello ${input.name}`};'};
  assert.deepEqual(await api('POST','/api/v1/studio/code/test',{code,input:{name:'Runloom'}}),{output:{message:'Hello Runloom'}});
  const failure=await api('POST','/api/v1/studio/code/test',{code:{...code,source:'const n: number = 1; return n;'},input:{}},422);
  assert.match(failure.detail,/SyntaxError/);
  const definition=JSON.parse(await readFile(new URL('../examples/javascript-transform.json',import.meta.url),'utf8'));
  definition.id=`javascript-sample-${Date.now()}`;
  const input=JSON.parse(await readFile(new URL('../examples/inputs/order.json',import.meta.url),'utf8'));
  await api('POST','/api/v1/studio/definitions',definition);
  const published=await api('POST',`/api/v1/studio/definitions/${definition.id}/publish`,{});
  assert.equal(published.version,1);
  // Change draft after publishing: version 1 must retain the original source.
  const draft=structuredClone(definition);
  draft.steps[0].code.source='return {total: 999, itemCount: 0};';
  await api('POST','/api/v1/studio/definitions',draft);
  const started=await api('POST','/api/v1/studio/runs',{definition_id:definition.id,version:1,input},201);
  let status;
  for(let attempt=0;attempt<60;attempt++) {
    status=await api('GET',`/api/v1/studio/runs/${started.workflow_id}`);
    if(status.temporal_status!=='running') break;
    await new Promise(resolve=>setTimeout(resolve,500));
  }
  assert.equal(status.temporal_status,'completed',JSON.stringify(status.state));
  assert.deepEqual(status.state.output,{order:{total:40,itemCount:2}});
  assert.equal(status.state.steps.total.status,'completed');
  const history=await api('GET',`/api/v1/studio/runs/${started.workflow_id}/history`);
  assert.deepEqual(history.run.input,input);
  const commits=await api('GET',`/api/v1/studio/definitions/${definition.id}/history`);
  assert(commits.commits.some(commit=>commit.kind==='publish' && commit.published_version===1));
  // Publish a runaway script and ensure its run fails cleanly.
  draft.steps[0].code.source='while (true) {}';
  await api('POST','/api/v1/studio/definitions',draft);
  await api('POST',`/api/v1/studio/definitions/${definition.id}/publish`,{});
  const runaway=await api('POST','/api/v1/studio/runs',{definition_id:definition.id,version:2,input},201);
  for(let attempt=0;attempt<60;attempt++) {
    status=await api('GET',`/api/v1/studio/runs/${runaway.workflow_id}`);
    if(status.temporal_status!=='running') break;
    await new Promise(resolve=>setTimeout(resolve,500));
  }
  assert.equal(status.temporal_status,'failed');
  const failedHistory=await api('GET',`/api/v1/studio/runs/${runaway.workflow_id}/history`);
  assert.equal(failedHistory.run.temporal_status,'failed');
  assert.match(JSON.stringify(status.state),/time limit/);
  // Leave the latest published sample usable, not the deliberate failure case.
  await api('POST','/api/v1/studio/definitions',definition);
  await api('POST',`/api/v1/studio/definitions/${definition.id}/publish`,{});
  await page.reload();
  await page.locator('runloom-studio').waitFor();
  assert.deepEqual(errors,[]);
  console.log(`PASS authenticated preview, TypeScript rejection, pinned source, Temporal execution, timeout, run history and publishing. Sample: ${definition.id} (v3)`);
} catch(error) {
  console.error(`JavaScript live verification failed: ${error.message?.replace(/https?:\/\/\S+/g,'[url]') || error.name}`);
  process.exitCode=1;
} finally { await browser.close(); }
