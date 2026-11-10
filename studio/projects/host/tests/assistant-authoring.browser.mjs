// Real federated UI with synthetic identity and mocked APIs. No model calls or live writes.
import assert from 'node:assert/strict';
const {chromium} = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.RUNLOOM_UI_URL || 'http://127.0.0.1:4300';
assert(['localhost','127.0.0.1'].includes(new URL(base).hostname));
const browser = await chromium.launch({headless:true,...(process.env.RUNLOOM_CHROME ? {executablePath:process.env.RUNLOOM_CHROME} : {})});
const page = await browser.newPage({viewport:{width:1500,height:1000}});
page.setDefaultTimeout(20000);
let root={schema_version:'1.0',id:'root',name:'Existing workflow',kind:'main',version:0,trigger:{type:'manual'},steps:[{id:'wait',type:'wait',seconds:2}]};
const chats=new Map();let applies=0;const errors=[];
page.on('pageerror',e=>errors.push(e.message));
try {
 await page.route('**/api/**',route=>{
  const url=new URL(route.request().url()),path=url.pathname,method=route.request().method();
  const json=value=>route.fulfill({json:value});
  if(path.startsWith('/api/v1/studio/assistant/')) {
   const workflow=url.searchParams.get('workflow_id')||'';
   if(path.endsWith('/config'))return json({configured:true,workflow_changes_enabled:true});
   if(path.endsWith('/conversations')){
    if(method==='GET')return json({items:[...chats.values()].filter(c=>c.workflow===workflow),next_cursor:''});
    const body=route.request().postDataJSON();const c={id:body.id,workflow,revision:1,messages:[],jobs:[]};chats.set(c.id,c);return json(c);
   }
   const id=path.split('/conversations/')[1]?.split('/')[0],c=chats.get(id);
   if(path.endsWith('/messages')){
    const body=route.request().postDataJSON(),operation=workflow?'update':'create',jobID=workflow?'update-job':'create-job';
    const definition=workflow?{...root,name:'Enhanced workflow',steps:[{id:'wait',type:'wait',seconds:7}]}:{schema_version:'1.0',id:'workflow-create-job',name:'New workflow',kind:'main',version:0,trigger:{type:'manual'},steps:[{id:'done',type:'end'}]};
    assert.equal(body.save_draft,true);
    const job={save_draft:true,id:jobID,request_id:body.request_id,status:'completed',model:'synthetic',proposal_hash:'review-hash',proposal:{operation,name:definition.name,steps:[],before_step_id:'',definition},authoring_context:{workflows:workflow?[{id:'root',definition:root}]:[]}};
    c.jobs=[job];c.revision++;c.messages=[{id:'u',role:'user',job_id:jobID,content:body.content,created_at:new Date().toISOString()},{id:'a',role:'assistant',job_id:jobID,content:'Review this draft proposal.',created_at:new Date().toISOString()}];return json(c);
   }
   if(path.endsWith('/apply')){
    const body=route.request().postDataJSON();assert.equal(body.approved,true);assert.equal(body.proposal_hash,'review-hash');applies++;
    const job=c.jobs[0];job.applied={request_id:job.id,parent_id:job.proposal.definition.id,child_id:'',draft_only:true};
    if(workflow)root=job.proposal.definition;c.revision++;return json(c);
   }
   if(path.endsWith('/events'))return route.fulfill({contentType:'text/event-stream',body:'data: '+JSON.stringify(c)+'\n\n'});
   return json(c);
  }
  if(path.endsWith('/auth/config'))return route.fulfill({status:503,json:{}});
  if(path.endsWith('/projects'))return json([{id:'default',name:'General'}]);
  if(path.endsWith('/definitions'))return json([{id:'root',name:root.name,project_id:'default',has_draft:true,latest_version:0}]);
  if(path.endsWith('/definitions/root'))return json(root);
  if(path.endsWith('/edit-lock'))return json({locked:false,ttl_seconds:60});
  if(path.endsWith('/validate'))return json({valid:true,definition:root});
  if(path.endsWith('/health'))return json({temporal:true,database:true});
  return json([]);
 });
 await page.goto(base+'/workflows');
 await page.waitForFunction(()=>window.ng?.getComponent(document.querySelector('app-root'))?.auth.ready());
 await page.evaluate(()=>{const auth=window.ng.getComponent(document.querySelector('app-root')).auth;auth.bearer=async()=>'synthetic-token';auth.profile.set({user:{id:'tester',email:'test@example.test'},organizations:[{id:'org',name:'Test',role:'owner',teams:[{id:'team',name:'Team',role:'admin'}]}]});auth.organizationId.set('org');auth.teamId.set('team');auth.signedIn.set(true);});
 const launcher=page.getByRole('button',{name:'Lumi',exact:true});
 await launcher.click();await page.getByLabel('Message Lumi').fill('Create a workflow that ends.');await page.getByLabel('Message Lumi').press('Enter');
 await page.getByText('New workflow in the default project.',{exact:false}).waitFor();
 await page.getByText('Draft saved: workflow-create-job.',{exact:false}).waitFor();assert.equal(applies,1);
 await page.keyboard.press('Escape');
 await page.getByRole('button',{name:'Details for Existing workflow',exact:true}).click();
 await launcher.click();await page.getByLabel('Message Lumi').fill('Fix the wait duration to seven seconds.');await page.getByLabel('Message Lumi').press('Enter');
 await page.getByText('Changes to the selected workflow draft.',{exact:false}).waitFor();
 await page.getByText('View workflow before changes',{exact:true}).click();
 assert(await page.getByText('Workflow contract (JSON)',{exact:true}).isVisible());
 // Saving happens automatically, without an Apply click.
 await page.getByText('Draft saved: root.',{exact:false}).waitFor();
 assert.equal(applies,2);assert.equal(root.steps[0].seconds,7);
 await page.keyboard.press('Escape');
 await page.waitForFunction(()=>document.querySelector('input[aria-label="Workflow name"]')?.value==='Enhanced workflow');
 assert.deepEqual(errors,[]);
 console.log('PASS automatic workflow draft creation and scoped enhancement without Apply clicks, before/after inspection and builder refresh. APIs/model mocked.');
} finally {await browser.close();}
