import assert from 'node:assert/strict';
import {readFile, stat} from 'node:fs/promises';
import {fileURLToPath} from 'node:url';
const {chromium}=await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const origin=process.env.RUNLOOM_UI_URL || 'http://localhost:4200';
const api=process.env.RUNLOOM_ASSISTANT_TEST_URL;
const unconfigured=process.env.RUNLOOM_ASSISTANT_TEST_DISABLED==='1';
if(!api || !['127.0.0.1','localhost'].includes(new URL(api).hostname))throw new Error('Requires isolated assistant test server');
const dist=fileURLToPath(new URL('../dist/browser/',import.meta.url));
const browser=await chromium.launch({headless:true,...(process.env.RUNLOOM_CHROME?{executablePath:process.env.RUNLOOM_CHROME}:{})});
let page, assistantRequest;
try{
 page=await browser.newPage({viewport:{width:1440,height:1000}});page.setDefaultTimeout(15000);const errors=[];page.on('pageerror',error=>{errors.push(error.message);console.error('Browser runtime:',error.message)});
 page.on('request',request=>{if(request.url().includes('/api/v1/studio/assistant/'))assistantRequest=request;});
 await page.context().grantPermissions(['local-network-access'],{origin});
 page.on('console',message=>{if(message.type()==='error' && !message.text().includes('/auth/'))console.error('Browser console:',message.text().slice(0,500));});
 await page.addInitScript(({api})=>{
   const original=window.fetch.bind(window);
   window.__chatFaults={delayNextMessage:0,loseNextAck:false,cutNextStream:false,failEvents:0,blockNextMessage:false,requests:[]};
   window.fetch=(input,init)=>{
     const url=new URL(typeof input==='string'?input:input instanceof URL?input.href:input.url,window.location.origin);
     if(url.pathname.startsWith('/api/v1/studio/assistant/'))return (async()=>{
       const faults=window.__chatFaults;
       if(url.pathname.endsWith('/messages')){
         faults.requests.push(JSON.parse(init.body));
         if(faults.blockNextMessage){faults.blockNextMessage=false;throw new TypeError('Synthetic offline submission');}
         const delay=faults.delayNextMessage;faults.delayNextMessage=0;
         const response=await original(api+url.pathname+url.search,init);
         if(delay)await new Promise(resolve=>setTimeout(resolve,delay));
         if(faults.loseNextAck){faults.loseNextAck=false;await response.text();throw new TypeError('Synthetic lost acknowledgement');}
         return response;
       }
       if(url.pathname.endsWith('/events')){
         if(faults.failEvents>0){faults.failEvents--;return new Response('',{status:503});}
         const response=await original(api+url.pathname+url.search,init);
         if(faults.cutNextStream && response.ok){
           faults.cutNextStream=false;
           const reader=response.body.getReader();
           return new Response(new ReadableStream({async start(controller){
             try {const first=await reader.read();if(first.value)controller.enqueue(first.value);
               await new Promise(resolve=>setTimeout(resolve,100));await reader.cancel();controller.error(new TypeError('Synthetic SSE disconnect'));
             }catch(error){controller.error(error)}
           }}),{headers:response.headers,status:response.status});
         }
         return response;
       }
       return original(api+url.pathname+url.search,init);
     })();
     return original(input,init);
   };
 },{api});
 await page.route(origin+'/**',async route=>{
  const url=new URL(route.request().url());
  if(url.pathname.startsWith('/api/v1/studio/assistant/'))return route.continue({url:api+url.pathname+url.search});
  // Redirect chains bypass Playwright routing for subsequent requests. Convert
  // only the local login return into a new navigation so it loads this build.
  if(url.pathname.startsWith('/auth/') && route.request().isNavigationRequest()){
    let response;
    try { response=await route.fetch({maxRedirects:0,maxRetries:2}); }
    catch { console.error('Local identity request failed; inspect cluster health. Request headers are intentionally omitted.'); return route.abort(); }
    const location=response.headers()['location'];
    if(location){const target=new URL(location,url);if(target.origin===origin && !target.pathname.startsWith('/auth/'))return route.fulfill({response,status:200,contentType:'text/html',body:'<script>location.replace('+JSON.stringify(target.href).replaceAll('<','\\u003c')+')</script>'});}
    return route.fulfill({response});
  }
  const remote=url.pathname.match(/^\/remotes\/(workflows|management)\/([^/]+)$/);
  if(remote){
    const file=fileURLToPath(new URL('../../'+remote[1]+'/dist/browser/'+remote[2],import.meta.url));
    if(await stat(file).then(s=>s.isFile()).catch(()=>false))return route.fulfill({body:await readFile(file),contentType:file.endsWith('.js')?'application/javascript':file.endsWith('.css')?'text/css':'application/octet-stream'});
  }
  if(url.pathname.startsWith('/api/') || url.pathname.startsWith('/auth/') || url.pathname.startsWith('/remotes/'))return route.continue();
  const safe=url.pathname.split('/').filter(part=>part && part!=='.' && part!=='..').join('/');
  const file=dist+safe;
  if(safe && await stat(file).then(s=>s.isFile()).catch(()=>false))return route.fulfill({body:await readFile(file),contentType:file.endsWith('.js')?'application/javascript':file.endsWith('.css')?'text/css':'application/octet-stream'});
  if (/\.(js|css|svg|json)$/.test(safe)) return route.continue();
  return route.fulfill({body:await readFile(dist+'index.html'),contentType:'text/html'});
 });
 await page.goto(origin+'/workflows');
 await page.locator('#username').fill(process.env.RUNLOOM_TEST_EMAIL || 'admin@runloom.test');
 await page.locator('#password').fill(process.env.RUNLOOM_TEST_PASSWORD || 'RunloomLocal!123');
 const signedIn=page.waitForResponse(response=>response.url().endsWith('/api/v1/studio/me') && response.ok());
 await page.locator('#kc-login').click();
 await signedIn;
 await page.reload();
 await page.getByRole('button',{name:'Switch organization or team',exact:true}).click();
 await page.getByRole('combobox',{name:'Organization',exact:true}).waitFor();
 const organizations=page.getByRole('combobox',{name:'Organization',exact:true});
 const orgOptions=await organizations.locator('option').allTextContents();
 if(orgOptions.includes('Runloom'))await organizations.selectOption({label:'Runloom'});
 await page.getByRole('button',{name:'Lumi',exact:true}).click();
 const input=page.getByRole('textbox',{name:'Message Lumi'});
 await input.fill('Unsent first draft');
 await page.reload();
 await page.getByRole('button',{name:'Lumi',exact:true}).click();
 await page.waitForFunction(()=>document.querySelector('textarea[name="message"]')?.value==='Unsent first draft');
 await page.evaluate(()=>{window.__chatFaults.delayNextMessage=1200;window.__chatFaults.cutNextStream=true;});
 await input.fill('Help me plan an onboarding workflow');
 await input.press('Enter');
 await page.keyboard.type('A follow-up draft');
 await page.locator('.pending-message').getByText('Help me plan an onboarding workflow',{exact:true}).waitFor();
 assert.equal(await input.evaluate(element=>document.activeElement===element && !element.readOnly && !element.disabled),true);
 assert.equal(await input.inputValue(),'A follow-up draft');
 const reply=unconfigured?'LLM is not configured.':'Test response: we can plan the onboarding steps together.';
 await page.getByText(reply,{exact:true}).waitFor();
 await page.getByText('Saved',{exact:true}).waitFor();
 assert.equal(await input.evaluate(element=>document.activeElement===element),true,'Enter must preserve composer focus');
 // Acknowledgement and reconnect must not erase text entered during submission.
 assert.equal(await input.inputValue(),'A follow-up draft');
 await page.reload();
 await page.getByRole('button',{name:'Lumi',exact:true}).click();
 await page.getByText(reply,{exact:true}).waitFor();
 assert.equal(await input.inputValue(),'A follow-up draft','Draft survives refresh');
 await input.fill('Now think about a reminder');
 await page.getByRole('button',{name:'Send',exact:true}).click();
 assert.equal(await input.evaluate(element=>document.activeElement===element),true,'Send must return focus to the composer');
 if(unconfigured){
  await page.getByText(reply,{exact:true}).nth(1).waitFor();
  await page.locator('.jobs summary').click();
  assert.equal(await page.getByText('Configuration notice · completed',{exact:true}).count(),2);
  assert.equal(await page.getByText('No model called',{exact:true}).count(),2);
 }else{
  await input.fill('Queued follow-up');
  await page.getByRole('button',{name:'Queue message',exact:true}).click();
  await page.locator('.message:not(.pending-message)').filter({has:page.getByText('Queued follow-up',{exact:true})}).waitFor();
  await input.fill('Urgent priority');
  await page.getByRole('button',{name:'Stop and send',exact:true}).click();
  await page.locator('.jobs summary').click();
  await page.getByText('Planning · cancelled',{exact:true}).waitFor();
  // Keep the reader's position while the remaining queue streams.
  const pane=page.locator('.messages');
  await pane.evaluate(el=>{el.scrollTop=0;});
  await page.waitForTimeout(600);
  assert.ok(await pane.evaluate(el=>el.scrollTop<5),'Streaming must not pull the reader to the bottom');
  await page.getByRole('button',{name:'Close Lumi',exact:true}).click();
  await page.waitForTimeout(600);
  await page.getByRole('button',{name:'Lumi',exact:true}).click();
  await page.getByText('Saved',{exact:true}).waitFor();
  const states=await page.locator('.job strong').allTextContents();
  assert.deepEqual(states,['Planning · completed','Planning · cancelled','Planning · completed','Planning · completed']);
 }
 // The server commits, but the POST acknowledgement is lost. Reload reconciles
 // the same request ID with durable history, without duplicating the turn.
 await page.evaluate(()=>{window.__chatFaults.loseNextAck=true;});
 await input.fill('Recover committed message');
 await input.press('Enter');
 await page.getByText('Delivery not confirmed',{exact:true}).waitFor();
 await input.fill('Draft retained across reconnect');
 await page.reload();
 await page.getByRole('button',{name:'Lumi',exact:true}).click();
 await page.locator('.message:not(.pending-message)').getByText('Recover committed message',{exact:true}).waitFor();
 assert.equal(await page.getByText('Recover committed message',{exact:true}).count(),1);
 assert.equal(await input.inputValue(),'Draft retained across reconnect');
 await page.getByText('Saved',{exact:true}).waitFor();
 // This message never reaches the server before refresh; the tab outbox must
 // resend it with its original idempotency key after restoring.
 await page.evaluate(()=>{window.__chatFaults.blockNextMessage=true;});
 await input.fill('Recover unsent message');
 await input.press('Enter');
 await page.getByText('Delivery not confirmed',{exact:true}).waitFor();
 const originalID=await page.evaluate(()=>window.__chatFaults.requests.at(-1).request_id);
 await page.reload();
 await page.getByRole('button',{name:'Lumi',exact:true}).click();
 await page.locator('.message:not(.pending-message)').getByText('Recover unsent message',{exact:true}).waitFor();
 assert.equal(await page.evaluate(()=>window.__chatFaults.requests.at(-1).request_id),originalID);
 assert.equal(await page.getByText('Recover unsent message',{exact:true}).count(),1);
 await page.getByText('Saved',{exact:true}).waitFor();
 // Read existing workflows only; never mutate the user's workflow definitions.
 // Chat data still lives in the disposable test DB.
 const params=new URL(assistantRequest.url()).searchParams;
 const headers={Authorization:assistantRequest.headers()['authorization'],'X-Runloom-Team':params.get('team_id')};
 const definitionsResponse=await page.request.get(origin+'/api/v1/studio/definitions',{headers});
 assert.equal(definitionsResponse.status(),200);
 const definitions=await definitionsResponse.json();
 assert(definitions.length,'Workflow-scope browser check needs one saved workflow in the selected local team');
 const parent=definitions[0];
 const parentDetails=page.getByRole('row').filter({has:page.getByText(parent.id,{exact:true})}).getByRole('button',{name:'Details for '+parent.name,exact:true});
 await page.getByRole('button',{name:'Close Lumi',exact:true}).click();
 const projectsResponse=await page.request.get(origin+'/api/v1/studio/projects',{headers});
 const projects=await projectsResponse.json();
 const project=projects.find(p=>p.id===(parent.project_id || 'default'));
 if(project)await page.getByLabel('Browse project',{exact:true}).selectOption(project.id);
 await parentDetails.click();
 await page.getByRole('button',{name:'Lumi',exact:true}).click();
 await page.getByText('Scope & guardrails',{exact:true}).click();
 await page.getByTestId('workflow-chat-scope').waitFor();
 await input.fill('Scoped parent planning');await input.press('Enter');
 await page.locator('.message:not(.pending-message)').getByText('Scoped parent planning',{exact:true}).waitFor();
 await page.getByText('Saved',{exact:true}).waitFor();
 assert.equal(new URL(assistantRequest.url()).searchParams.get('workflow_id'),parent.id);
 assert.equal(await page.locator('.message').getByText('Recover unsent message',{exact:true}).count(),0,'General conversation leaked into workflow details');
 await input.fill('Parent-only draft');
 await page.getByRole('button',{name:'Close Lumi',exact:true}).click();
 await page.getByRole('button',{name:'Workflows',exact:true}).click();
 await page.getByRole('button',{name:'Lumi',exact:true}).click();
 await page.locator('.message').getByText('Recover unsent message',{exact:true}).waitFor();
 assert.equal(await page.locator('.message').getByText('Scoped parent planning',{exact:true}).count(),0,'Workflow conversation leaked into general history');
 await page.getByRole('button',{name:'Close Lumi',exact:true}).click();
 await parentDetails.click();
 await page.getByRole('button',{name:'Lumi',exact:true}).click();
 await page.locator('.message').getByText('Scoped parent planning',{exact:true}).waitFor();
 assert.equal(await input.inputValue(),'Parent-only draft');
 if(process.env.RUNLOOM_ASSISTANT_TEST_AUTHORING==='1' && !unconfigured){
  await page.getByRole('button',{name:'Close Lumi',exact:true}).click();
  await page.getByRole('button',{name:'Workflows',exact:true}).click();
  const id='assistant-smoke-'+crypto.randomUUID(),name='Assistant guardrail smoke '+new Date().toISOString();
  const saved=await page.request.post(origin+'/api/v1/studio/definitions',{headers,data:{schema_version:'1.0',id,name,kind:'main',trigger:{type:'manual'},input_schema:{},steps:[{id:'done',type:'end'}]}});
  assert.equal(saved.status(),200,'Create isolated smoke parent');
  await page.reload();
  const defaultProject=projects.find(p=>p.id==='default');
  if(defaultProject)await page.getByLabel('Browse project',{exact:true}).selectOption(defaultProject.id);
  await page.getByRole('row').filter({has:page.getByText(id,{exact:true})}).getByRole('button',{name:'Details for '+name,exact:true}).click();
  await page.getByRole('button',{name:'Lumi',exact:true}).click();
  await input.fill('Create guarded smoke child');await input.press('Enter');
  const proposal=page.getByRole('region',{name:'Child workflow proposal'});
  await proposal.getByText('Guarded smoke child',{exact:true}).waitFor();
  let current=await (await page.request.get(origin+'/api/v1/studio/definitions/'+id+'?version=0',{headers})).json();
  assert.equal(current.steps.length,1,'Model output must not write workflows');
  await page.getByRole('button',{name:'Close Lumi',exact:true}).click();
  await page.getByRole('button',{name:'Edit workflow',exact:true}).click();
  await page.getByRole('button',{name:'Lumi',exact:true}).click();
  const apply=page.getByRole('button',{name:'Apply draft changes',exact:true});assert.equal(await apply.isDisabled(),true,'Active edit must block apply');
  await page.getByRole('button',{name:'Close Lumi',exact:true}).click();
  await page.getByRole('button',{name:'Finish editing',exact:true}).click();
  await page.getByRole('button',{name:'Lumi',exact:true}).click();
  await apply.click();
  await proposal.getByText(/Applied: child-/).waitFor();
  current=await (await page.request.get(origin+'/api/v1/studio/definitions/'+id+'?version=0',{headers})).json();
  assert.equal(current.steps.length,2);assert.equal(current.steps[0].type,'subworkflow');assert.equal(current.steps[0].version ?? 0,0);assert.equal(current.steps[1].id,'done');
  const child=await (await page.request.get(origin+'/api/v1/studio/definitions/'+current.steps[0].definition_id+'?version=0',{headers})).json();
  assert.equal(child.kind,'child');assert.equal(child.steps[0].type,'wait');
  assert.equal((await page.request.get(origin+'/api/v1/studio/definitions/'+child.id,{headers})).status(),404,'Child must remain unpublished');
  const captured=assistantRequest;
  await page.getByRole('button',{name:'Close Lumi',exact:true}).click();
  await page.getByRole('button',{name:'Workflows',exact:true}).click();
  await page.getByRole('row').filter({has:page.getByText(id,{exact:true})}).getByRole('button',{name:'Details for '+name,exact:true}).click();
  await page.getByRole('button',{name:'Lumi',exact:true}).click();
  await page.getByText(/Applied: child-/).waitFor();
  const capturedURL=new URL(captured.url()),conversationPath=capturedURL.pathname.match(/^(.*\/conversations\/[^/]+)/)?.[1];
  if(conversationPath){
   const conversationURL=api+conversationPath+'?'+capturedURL.searchParams;
   const conversation=await (await page.request.get(conversationURL,{headers:{Authorization:captured.headers()['authorization']}})).json();
   const job=conversation.jobs?.find(j=>j.applied);
   if(job){const repeated=await page.request.post(api+'/api/v1/studio/assistant/conversations/'+conversation.id+'/jobs/'+job.id+'/apply?'+new URL(captured.url()).searchParams,{headers:{Authorization:captured.headers()['authorization']},data:{approved:true,proposal_hash:job.proposal_hash}});assert.equal(repeated.status(),200);}
  }
  console.log('PASS reviewed child creation, edit-lock UI guard, atomic parent attachment, draft-only persistence and reload. Smoke parent: '+id);
 }
 await page.setViewportSize({width:390,height:844});
 const box=await page.locator('#assistant-panel').boundingBox();assert(box && box.x>=0 && box.width<=390);
 await page.screenshot({path:process.env.RUNLOOM_ASSISTANT_SCREENSHOT || '/tmp/runloom-assistant-mobile.png'});
 assert.deepEqual(errors,[]);
 console.log('PASS browser: optimistic messages, editable drafts, focus, SSE reconnect, lost acknowledgement, unsent reload recovery, '+(unconfigured?'unconfigured notice':'durable queue, stop-and-send, stable scrolling')+', workflow/general history and draft isolation, mobile layout, no runtime errors.');
}catch(error){if(page){await page.screenshot({path:'/tmp/runloom-assistant-error.png'});console.error('Assistant elements:',await page.locator('runloom-assistant').count());console.error('Loaded scripts:',await page.locator('script[src]').evaluateAll(nodes=>nodes.map(node=>node.getAttribute('src'))));console.error('Runtime resources:',await page.evaluate(()=>performance.getEntriesByType('resource').filter(r=>r.name.endsWith('.js')).map(r=>new URL(r.name).pathname)))}throw error}finally{await browser.close();}
