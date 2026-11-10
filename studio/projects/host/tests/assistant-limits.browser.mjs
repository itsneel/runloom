// Real local APIs and persistence. Creates a synthetic full chat; never calls a model.
// By default serves the local production UI build; RUNLOOM_TEST_DEPLOYED=1 uses deployed assets.
import assert from 'node:assert/strict';
import {readFile, stat} from 'node:fs/promises';
import {fileURLToPath} from 'node:url';
import {randomUUID} from 'node:crypto';
const {chromium}=await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const origin=process.env.RUNLOOM_UI_URL || 'http://localhost:4200';
if(!['localhost','127.0.0.1'].includes(new URL(origin).hostname))throw new Error('Local deployment only');
const dist=fileURLToPath(new URL('../dist/browser/',import.meta.url));
const browser=await chromium.launch({headless:true,...(process.env.RUNLOOM_CHROME?{executablePath:process.env.RUNLOOM_CHROME}:{})});
let page;
try {
 page=await browser.newPage({viewport:{width:1440,height:1000}});page.setDefaultTimeout(20000);
 const errors=[];page.on('pageerror',e=>errors.push(e.message));
 if(!process.env.RUNLOOM_TEST_DEPLOYED)await page.route(origin+'/**',async route=>{
  const url=new URL(route.request().url());
  if(url.pathname.startsWith('/auth/') && route.request().isNavigationRequest()){
   const response=await route.fetch({maxRedirects:0,maxRetries:2}),location=response.headers()['location'];
   if(location){const target=new URL(location,url);if(target.origin===origin && !target.pathname.startsWith('/auth/'))return route.fulfill({response,status:200,contentType:'text/html',body:'<script>location.replace('+JSON.stringify(target.href).replaceAll('<','\\u003c')+')</script>'});}
   return route.fulfill({response});
  }
  if(['/api/','/auth/','/remotes/'].some(prefix=>url.pathname.startsWith(prefix)))return route.continue();
  const safe=url.pathname.split('/').filter(p=>p && p!=='.' && p!=='..').join('/'),file=dist+safe;
  if(safe && await stat(file).then(s=>s.isFile()).catch(()=>false))return route.fulfill({body:await readFile(file),contentType:file.endsWith('.js')?'application/javascript':file.endsWith('.css')?'text/css':'application/octet-stream'});
  if(/\.(js|css|svg|json)$/.test(safe))return route.continue();
  return route.fulfill({body:await readFile(dist+'index.html'),contentType:'text/html'});
 });
 await page.goto(origin+'/workflows');
 await page.locator('#username').fill(process.env.RUNLOOM_TEST_EMAIL || 'admin@runloom.test');
 await page.locator('#password').fill(process.env.RUNLOOM_TEST_PASSWORD || 'RunloomLocal!123');
 const signedIn=page.waitForResponse(r=>r.url().endsWith('/api/v1/studio/me') && r.ok());
 await page.locator('#kc-login').click();await signedIn;
 const configReady=page.waitForResponse(r=>new URL(r.url()).pathname==='/api/v1/studio/assistant/config');
 await page.getByRole('button',{name:'Lumi',exact:true}).click();
 const configResponse=await configReady;
 assert.equal((await configResponse.json()).configured,false,'Only run against an unconfigured local provider');
 const authorization=configResponse.request().headers()['authorization'];
 const query=new URL(configResponse.url()).search;
 const api=async(path,body,status)=>{
  const response=await page.request.fetch(origin+'/api/v1/studio/assistant/'+path+query,{method:body?'POST':'GET',headers:{Authorization:authorization},...(body?{data:body}:{})});
  assert.equal(response.status(),status);return response.json();
 };
 const panel=page.getByRole('dialog',{name:'Lumi'}),input=page.getByRole('textbox',{name:'Message Lumi'});
 await input.fill('Draft stays editable');
 const key=await page.evaluate(()=>Object.keys(sessionStorage).find(k=>k.startsWith('runloom-chat-session:')));
 assert.ok(key);const oldID=key.slice(key.lastIndexOf(':')+1);
 await api('conversations',{id:oldID,title:'Limit recovery browser check'},201);
 let latest;
 for(let i=0;i<40;i++)latest=await api('conversations/'+oldID+'/messages',{request_id:randomUUID(),content:'Limit verification '+i,mode:'queue'},202);
 const rejectedID=randomUUID(),secondID=randomUUID();
 const pending=[{id:rejectedID,content:'First unsent',mode:'queue',createdAt:new Date().toISOString(),state:'pending',attempts:0},{id:secondID,content:'Second unsent',mode:'queue',createdAt:new Date().toISOString(),state:'pending',attempts:0}];
 const rejection=await api('conversations/'+oldID+'/messages',{request_id:rejectedID,content:'First unsent',mode:'queue'},409);
 if(process.env.RUNLOOM_TEST_DEPLOYED)assert.equal(rejection.code,'conversation_limit');
 await page.evaluate(({key,pending})=>sessionStorage.setItem(key,JSON.stringify({draft:'Draft stays editable',needsCreate:false,outbox:pending})),{key,pending});
 await page.reload();await page.getByRole('button',{name:'Lumi',exact:true}).click();
 await panel.locator('.delivery-recovery').waitFor();
 assert.match(await panel.locator('.delivery-recovery').innerText(),/40 submissions/);
 assert.equal(await panel.getByText('Not sent · conversation limit reached',{exact:true}).count(),2);
 assert.equal(await panel.getByRole('button',{name:'Move unsent to new chat',exact:true}).isEnabled(),true);
 await input.fill('Draft kept after recovery');
 // Narrow layout still leaves both recovery and dismissal usable.
 await page.setViewportSize({width:844,height:390});
 await panel.getByRole('button',{name:'Move unsent messages to new chat',exact:true}).scrollIntoViewIfNeeded();
 await page.setViewportSize({width:1440,height:1000});
 await page.screenshot({path:'/tmp/runloom-chat-limit.png'});
 await panel.getByRole('button',{name:'Move unsent messages to new chat',exact:true}).click();
 await page.waitForFunction(()=>document.querySelectorAll('.message.from-user').length===2 && !document.querySelector('.pending-message'));
 assert.equal(await input.inputValue(),'Draft kept after recovery');
 const nextID=await panel.getByRole('combobox',{name:'Conversation history'}).inputValue();assert.notEqual(nextID,oldID);
 const moved=await api('conversations/'+nextID,undefined,200);
 assert.deepEqual(moved.jobs.map(j=>j.request_id),[rejectedID,secondID]);
 assert.deepEqual(moved.messages.filter(m=>m.role==='user').map(m=>m.content),['First unsent','Second unsent']);
 const old=await api('conversations/'+oldID,undefined,200);assert.equal(old.jobs.length,40);assert.equal(old.revision,latest.revision);
 await page.reload();await page.getByRole('button',{name:'Lumi',exact:true}).click();
 await page.getByText('Second unsent',{exact:true}).waitFor();assert.equal(await input.inputValue(),'Draft kept after recovery');
 await panel.getByRole('button',{name:'New chat',exact:true}).click();
 await page.waitForFunction(()=>document.querySelectorAll('#assistant-panel .message').length===0);
 assert.equal(await panel.locator('.message').count(),0);assert.equal(await input.inputValue(),'Draft kept after recovery');
 await panel.getByRole('combobox',{name:'Conversation history'}).selectOption(oldID);
 await panel.locator('.delivery-recovery').waitFor();
 await panel.getByRole('button',{name:'Continue in new chat',exact:true}).click();
 await page.waitForFunction(()=>document.querySelectorAll('#assistant-panel .message').length===0);
 assert.equal(await panel.locator('.message').count(),0);
 await panel.getByRole('button',{name:'Close Lumi',exact:true}).click();await panel.waitFor({state:'detached'});
 assert.equal(await page.getByRole('button',{name:'New workflow',exact:true}).isEnabled(),true);
 assert.deepEqual(errors,[]);
 console.log('PASS real 40-turn rejection, visible limit, preserved draft and ordered IDs, safe transfer, reload, intact old history, new chat and dashboard dismissal. No model called.');
} catch(error) {
 await page?.screenshot({path:'/tmp/runloom-chat-limit-failure.png'});
 throw error;
} finally { await browser.close(); }
