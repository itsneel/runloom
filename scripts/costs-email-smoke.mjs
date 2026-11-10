// Mocked Angular development-build checks; no real email or paid LLM calls.
import assert from 'node:assert/strict';
const { chromium }=await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const browser=await chromium.launch({headless:true,...(process.env.RUNLOOM_CHROME?{executablePath:process.env.RUNLOOM_CHROME}:{})});
const page=await browser.newPage({viewport:{width:1920,height:1080}});
const errors=[];page.on('pageerror',error=>errors.push(error.message));
let mode='priced',queries=[];
const row={id:'request-1',workflow_id:'run-a',temporal_run_id:'temporal-a',definition_id:'sample',definition_version:1,step_id:'ask',attempt:1,provider:'local-test',model:'test-model',requested_model:'test-model',started_at:'2026-10-01T00:00:00Z',status:'succeeded',accounting:'estimated',input_tokens:1000,output_tokens:500,cached_tokens:0,reasoning_tokens:100,duration_ms:250,cost_usd:'0.002000000000',pricing:{version:'test-v1'}};
const summary={estimated_usd:'0.004000000000',direct_usd:'0.002000000000',child_usd:'0.002000000000',requests:2,input_tokens:2000,output_tokens:1000,unknown:0,unpriced:0,pending:0,complete:true,truncated:false,models:['test-model'],nodes:[{workflow_id:'run-a',step_id:'ask',requests:2,incomplete:0,estimated_usd:'0.004000000000'}]};
await page.route('**/api/**',async route=>{
  const url=new URL(route.request().url());
  if(url.pathname==='/api/v1/auth/config')return route.fulfill({status:503,json:{}});
  if(url.pathname==='/api/v1/health')return route.fulfill({json:{temporal:true,database:true}});
  if(url.pathname==='/api/v1/studio/projects')return route.fulfill({json:[{id:'default',name:'General'}]});
  if(url.pathname.endsWith('/llm-usage')){
    queries.push(url.searchParams);
    if(mode==='error')return route.fulfill({status:503,json:{detail:'unavailable'}});
    const unknown=mode==='unknown',filtered=!!url.searchParams.get('model'),more=!!url.searchParams.get('after');
    return route.fulfill({json:{summary:unknown?{...summary,estimated_usd:'0',unknown:2,complete:false}:summary,entries:filtered?[]:[{...row,id:more?'request-2':'request-1',attempt:more?2:1,...(unknown?{accounting:'unknown',cost_usd:undefined}:{})}],next_cursor:more||filtered?'':'cursor-1',currency:'USD',scope:'run_and_descendants'}});
  }
  return route.fulfill({json:[]});
});
try{
  await page.goto(process.env.RUNLOOM_UI_URL || 'http://localhost:14201');
  await page.waitForFunction(()=>window.ng?.getComponent(document.querySelector('app-root'))?.auth.ready());
  await page.evaluate(()=>{
    const root=window.ng.getComponent(document.querySelector('app-root'));
    root.auth.bearer=async()=>'synthetic-token';root.auth.profile.set({user:{id:'admin',email:'admin@example.test'},organizations:[{id:'org',name:'Org',role:'owner',teams:[{id:'team',name:'Team',role:'admin'}]}]});
    root.auth.organizationId.set('org');root.auth.teamId.set('team');root.auth.signedIn.set(true);
  });
  await page.locator('runloom-studio').waitFor();
  await page.getByRole('button',{name:'New workflow',exact:true}).click();
  await page.locator('.inspector-head button').click();
  await page.locator('.graph-toolbar button[title="Add node"]').click();
  await page.locator('.picker-row').filter({hasText:'Send a message using configured SMTP'}).click();
  const recipients='first@example.test, second@example.test\n${input.email}\n';
  await page.getByLabel('Email To',{exact:true}).fill(recipients);
  await page.getByLabel('Email subject',{exact:true}).fill('Hello ${input.name}');
  await page.getByLabel('Email text',{exact:true}).fill('A plain text message');
  await page.getByLabel('Email HTML',{exact:true}).fill('<p>A message</p>');
  assert.equal(await page.getByLabel('Email To',{exact:true}).inputValue(),recipients);
  const email=await page.evaluate(()=>window.ng.getComponent(document.querySelector('runloom-studio')).definition.steps[0].email);
  assert.deepEqual(email.to,['first@example.test','second@example.test','${input.email}']);
  assert.equal(email.subject,'Hello ${input.name}');assert.equal(email.html,'<p>A message</p>');
  await page.evaluate(summary=>{
    const app=window.ng.getComponent(document.querySelector('runloom-studio'));
    app.testRun.runOpen=true;app.testRun.run={workflow_id:'run-a',temporal_status:'completed',state:{status:'completed',steps:{},output:{}}};
    app.recentRuns=[{workflow_id:'run-a',definition_id:'sample',definition_version:1,temporal_status:'completed',created_at:'2026-10-01T00:00:00Z',trigger_type:'manual',llm_cost:summary},{workflow_id:'run-b',definition_id:'other',definition_version:2,temporal_status:'completed',created_at:'2026-09-01T00:00:00Z',trigger_type:'child_test'}];
    window.ng.applyChanges(app);
  },summary);
  const costs=page.locator('runloom-run-costs');
  await costs.getByText('$0.004',{exact:true}).waitFor();
  await costs.getByRole('button',{name:'Load more requests'}).click();
  await costs.getByText(/Attempt 2/).waitFor();
  assert.equal(queries.at(-1).get('after'),'cursor-1');
  await costs.getByLabel('Cost model filter').fill('missing');
  await costs.getByRole('button',{name:'Apply filters'}).click();
  await costs.getByText('No requests match these filters.').waitFor();
  assert.equal(queries.at(-1).get('model'),'missing');
  assert.equal(await costs.locator('.total').innerText(),'$0.004');
  await costs.getByLabel('Cost model filter').fill('');mode='unknown';
  await costs.getByRole('button',{name:'Refresh costs'}).click();
  await page.waitForFunction(()=>document.querySelector('runloom-run-costs .total')?.textContent==='Unknown');
  mode='error';await costs.getByRole('button',{name:'Refresh costs'}).click();
  await costs.getByRole('alert').waitFor();
  assert.equal(await costs.locator('.total').count(),0,'unavailable data must not show zero');
  await page.locator('.history-browser details > summary').click();
  await page.getByLabel('Run model filter',{exact:true}).fill('test-model');
  await page.waitForFunction(()=>document.querySelectorAll('.history-list button').length===1);
  assert.equal(await page.locator('.history-list button').count(),1);
  await page.getByLabel('Run model filter',{exact:true}).fill('');
  await page.getByLabel('Run test filter',{exact:true}).selectOption('test');
  await page.waitForFunction(()=>document.querySelector('.history-list button')?.textContent.includes('other'));
  assert.equal(await page.locator('.history-list button').count(),1);
  assert.match(await page.locator('.history-list button').innerText(),/other/);
  assert.deepEqual(errors,[]);
  console.log('PASS Email builder/mappings, cost summary, attempts, pagination, filters, unknown/unavailable states and history filters');
}finally{await browser.close();}
