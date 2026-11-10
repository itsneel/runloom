// Production build + real login by default. Optional dev-server synthetic session
// verifies refactors without a running backend. No model calls or saved messages.
import assert from 'node:assert/strict';
import {readFile, stat} from 'node:fs/promises';
import {fileURLToPath} from 'node:url';
const {chromium}=await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const origin=process.env.RUNLOOM_UI_URL || 'http://localhost:4200';
const synthetic=process.env.RUNLOOM_ASSISTANT_SYNTHETIC_SESSION === '1';
if(!['localhost','127.0.0.1'].includes(new URL(origin).hostname))throw new Error('Local UI verification only');
const dist=fileURLToPath(new URL('../dist/browser/',import.meta.url));
const browser=await chromium.launch({headless:true,...(process.env.RUNLOOM_CHROME?{executablePath:process.env.RUNLOOM_CHROME}:{})});
try {
 const page=await browser.newPage({viewport:{width:1440,height:1000}}),errors=[];
 page.setDefaultTimeout(20000);page.on('pageerror',e=>errors.push(e.message));
 let saved={id:'',revision:0,messages:[],jobs:[]};
 await page.route(origin+'/**',async route=>{
  const url=new URL(route.request().url());
  if(url.pathname.startsWith('/api/v1/studio/assistant/')){
   const json=body=>route.fulfill({json:body});
   if(url.pathname.endsWith('/config'))return json({configured:false,mode:'reviewed-authoring'});
   if(url.pathname.endsWith('/conversations') && route.request().method()==='GET')return json({items:saved.id?[{...saved,messages:[],jobs:[]}]:[],next_cursor:''});
   if(url.pathname.endsWith('/conversations')){saved={...saved,...route.request().postDataJSON()};return json(saved);}
   if(url.pathname.endsWith('/messages')){
    const body=route.request().postDataJSON();
    saved={...saved,revision:saved.revision+1,messages:[...saved.messages,{id:'u'+saved.revision,role:'user',content:body.content,created_at:new Date().toISOString(),job_id:'j'+saved.revision},{id:'a'+saved.revision,role:'assistant',content:'LLM is not configured.',created_at:new Date().toISOString(),job_id:'j'+saved.revision}],jobs:[...saved.jobs,{id:'j'+saved.revision,request_id:body.request_id,status:'completed'}]};
    await new Promise(resolve=>setTimeout(resolve,500));return json(saved);
   }
   if(url.pathname.endsWith('/events'))return route.fulfill({contentType:'text/event-stream',body:'data: '+JSON.stringify(saved)+'\n\n'});
   return json(saved);
  }
  if(synthetic && url.pathname.startsWith('/api/')){
   if(url.pathname.endsWith('/auth/config'))return route.fulfill({status:503,json:{}});
   if(url.pathname.endsWith('/health'))return route.fulfill({json:{temporal:true,database:true}});
   return route.fulfill({json:[]});
  }
  if(synthetic)return route.continue();
  if(url.pathname.startsWith('/auth/') && route.request().isNavigationRequest()){
   const response=await route.fetch({maxRedirects:0,maxRetries:2});
   const location=response.headers()['location'];
   if(location){const target=new URL(location,url);if(target.origin===origin && !target.pathname.startsWith('/auth/'))return route.fulfill({response,status:200,contentType:'text/html',body:'<script>location.replace('+JSON.stringify(target.href).replaceAll('<','\\u003c')+')</script>'});}
   return route.fulfill({response});
  }
  if(['/api/','/auth/','/remotes/'].some(prefix=>url.pathname.startsWith(prefix)))return route.continue();
  const safe=url.pathname.split('/').filter(part=>part && part!=='.' && part!=='..').join('/');
  const file=dist+safe;
  if(safe && await stat(file).then(s=>s.isFile()).catch(()=>false))return route.fulfill({body:await readFile(file),contentType:file.endsWith('.js')?'application/javascript':file.endsWith('.css')?'text/css':'application/octet-stream'});
  if(/\.(js|css|svg|json)$/.test(safe))return route.continue();
  return route.fulfill({body:await readFile(dist+'index.html'),contentType:'text/html'});
 });
 await page.goto(origin+'/workflows');
 async function syntheticLogin(){
  await page.waitForFunction(()=>window.ng?.getComponent(document.querySelector('app-root'))?.auth.ready());
  await page.evaluate(()=>{
   const auth=window.ng.getComponent(document.querySelector('app-root')).auth;
   auth.bearer=async()=>'synthetic-token';
   auth.profile.set({user:{id:'tester',email:'test@example.test'},organizations:[{id:'org',name:'Test org',role:'owner',teams:[{id:'team',name:'Test team',role:'admin'}]}]});
   auth.organizationId.set('org');auth.teamId.set('team');auth.signedIn.set(true);
  });
 }
 if(synthetic)await syntheticLogin();
 else{
  await page.locator('#username').fill(process.env.RUNLOOM_TEST_EMAIL || 'admin@runloom.test');
  await page.locator('#password').fill(process.env.RUNLOOM_TEST_PASSWORD || 'RunloomLocal!123');
  const signedIn=page.waitForResponse(r=>r.url().endsWith('/api/v1/studio/me') && r.ok());
  await page.locator('#kc-login').click();await signedIn;
 }
 const launcher=page.getByRole('button',{name:'Lumi',exact:true});
 await launcher.waitFor();
 const launchBounds=await launcher.boundingBox();
 assert.ok(Math.abs(launchBounds.x+launchBounds.width/2-720)<1,'Launcher must be centered at the bottom');
 assert.ok(launchBounds.y>900);
 await page.getByRole('button',{name:'Lumi',exact:true}).click();
 const panel=page.getByRole('dialog',{name:'Lumi'}),input=page.getByRole('textbox',{name:'Message Lumi'});
 await panel.waitFor();
 const originBounds=await panel.evaluate(el=>{
  const animation=el.getAnimations().find(a=>a.animationName?.endsWith('assistant-expand'));
  if(!animation)throw new Error('Expected launcher expansion animation');
  animation.pause();animation.currentTime=0;
  const rect=el.getBoundingClientRect();
  return {x:rect.x,y:rect.y,width:rect.width,height:rect.height};
 });
 for(const key of ['x','y','width','height'])assert.ok(Math.abs(originBounds[key]-launchBounds[key])<2,'Animation must begin at launcher '+key);
 const animationBottoms=await panel.evaluate(el=>{
  const a=el.getAnimations().find(a=>a.animationName?.endsWith('assistant-expand'));
  return [90,180,270].map(time=>{a.currentTime=time;return el.getBoundingClientRect().bottom;});
 });
 for(const bottom of animationBottoms)assert.ok(Math.abs(bottom-launchBounds.y-launchBounds.height)<2,'Expansion bottom must stay anchored throughout animation');
 await panel.evaluate(async el=>{const a=el.getAnimations().find(a=>a.animationName?.endsWith('assistant-expand'));a.play();await a.finished;});
 await panel.waitFor();await input.fill('A draft worth keeping');
 assert.equal(await panel.evaluate(el=>el.matches(':modal')),true);
 await page.waitForTimeout(200);
 const bounds=await panel.boundingBox();
 assert.ok(Math.abs(bounds.x+bounds.width/2-720)<2,'Dialog must be horizontally centered');
 assert.ok(Math.abs(bounds.y+bounds.height-launchBounds.y-launchBounds.height)<2,'Dialog and launcher must share a bottom edge');
 assert.ok(bounds.width>=760,'Desktop chat should be comfortably wide');
 for(let i=0;i<20;i++){
  await page.keyboard.press('Tab');
  assert.equal(await panel.evaluate(el=>el.contains(document.activeElement)),true,'Tab must remain inside the modal');
 }
 await page.keyboard.press('Escape');await panel.waitFor({state:'detached'});
 assert.equal(await page.getByRole('button',{name:'Lumi',exact:true}).evaluate(el=>document.activeElement===el),true);
 await page.keyboard.press('Control+k');await panel.waitFor();
 await panel.evaluate(el=>{
  const animation=el.getAnimations().find(a=>a.animationName?.endsWith('assistant-expand'));
  if(!animation)throw new Error('Expected expansion before interrupted dismissal');
  animation.pause();animation.currentTime=110;
  el.dispatchEvent(new Event('cancel',{cancelable:true}));
 });
 await panel.waitFor({state:'detached'});
 await page.keyboard.press('Control+k');await panel.waitFor();
 assert.equal(await input.inputValue(),'A draft worth keeping');
 await page.getByRole('button',{name:'Plan a workflow',exact:false}).click();
 await page.waitForFunction(()=>document.querySelector('textarea[name="message"]')?.value.includes('clear inputs'));
 assert.match(await input.inputValue(),/clear inputs/);assert.equal(saved.messages.length,0,'Suggestion must not submit');
 for(const theme of ['dark','light']){
  await page.evaluate(theme=>document.documentElement.dataset.theme=theme,theme);
  await page.waitForTimeout(250);
  assert.equal(await page.getByRole('button',{name:'New chat',exact:true}).evaluate(el=>getComputedStyle(el).backgroundColor),theme==='dark'?'rgb(28, 33, 45)':'rgb(255, 255, 255)');
  await page.screenshot({path:'/tmp/runloom-assistant-spotlight-'+theme+'.png'});
 }
 const beforeSend=await panel.boundingBox();
 const footerBefore=await panel.locator('footer').boundingBox();
 await panel.evaluate(el=>{
  window.__assistantSendExpansions=0;
  window.__messageEntrances=0;
  window.__assistantPanelBeforeSend=el;
  el.addEventListener('animationstart',event=>{
   if(event.target===el && event.animationName.endsWith('assistant-expand'))window.__assistantSendExpansions++;
   if(event.target.matches('.message') && event.animationName.endsWith('message-send-in')){
    window.__messageEntrances++;
    window.__messageEntranceDuration=parseFloat(getComputedStyle(event.target).animationDuration)*1000;
   }
  });
 });
 await input.fill('Hello from Spotlight');await input.press('Enter');await page.keyboard.type('Next draft');
 await page.locator('.pending-message').waitFor();
 await page.locator('.pending-message').evaluate(el=>{window.__pendingBubble=el;});
 const pendingBounds=await panel.boundingBox();
 assert.equal((await panel.locator('footer').boundingBox()).y,footerBefore.y,'Pending controls must not shift the composer');
 for(const key of ['x','y','width','height'])assert.ok(Math.abs(pendingBounds[key]-beforeSend[key])<2,'Sending must not resize or re-expand the panel: '+key);
 await page.getByText('LLM is not configured.',{exact:true}).waitFor();
 assert.equal(await page.locator('.message.from-user').filter({hasText:'Hello from Spotlight'}).evaluate(el=>el===window.__pendingBubble),true,'Acknowledgement must update the existing bubble, not replace it');
 assert.equal(await input.inputValue(),'Next draft');
 assert.equal(await input.evaluate(el=>document.activeElement===el),true);
 const afterSend=await panel.boundingBox();
 assert.equal((await panel.locator('footer').boundingBox()).y,footerBefore.y,'Acknowledgement must not shift the composer');
 for(const key of ['x','y','width','height'])assert.ok(Math.abs(afterSend[key]-beforeSend[key])<2,'Response must not resize or re-expand the panel: '+key);
 assert.equal(await panel.evaluate(el=>el===window.__assistantPanelBeforeSend && window.__assistantSendExpansions===0),true,'Sending and acknowledgement must not replay the opening animation');
 assert.equal(await page.evaluate(()=>window.__messageEntrances),1,'Acknowledgement must not replay the message entrance');
 assert.equal(await page.evaluate(()=>window.__messageEntranceDuration),600,'New sends should have a gentle 600ms entrance');
 await page.waitForFunction(()=>!document.querySelector('.message.is-entering'));
 assert.equal(await panel.locator('.message.is-entering').count(),0,'Entrance class is removed after completion');
 await page.mouse.click(10,10);await panel.waitFor({state:'detached'});
 await page.keyboard.press('Control+k');await panel.waitFor();assert.equal(await input.inputValue(),'Next draft');
 await page.waitForFunction(()=>!document.querySelector('#assistant-panel')?.classList.contains('is-opening'));
 assert.equal(await panel.locator('.message.is-entering').count(),0,'History must not animate again on reopen');
 for(const viewport of [{width:390,height:844},{width:844,height:390}]){
  await page.setViewportSize(viewport);await page.waitForTimeout(200);
  const dialogBounds=await panel.boundingBox(),composerBounds=await input.boundingBox();
  assert.ok(dialogBounds.x>=0 && dialogBounds.x+dialogBounds.width<=viewport.width);
  assert.ok(dialogBounds.y>=0 && dialogBounds.y+dialogBounds.height<=viewport.height);
  const anchor=await launcher.boundingBox();
  assert.ok(Math.abs(dialogBounds.y+dialogBounds.height-anchor.y-anchor.height)<2,'Mobile and landscape chat must keep the launcher bottom anchor');
  assert.ok(composerBounds.y>=0 && composerBounds.y+composerBounds.height<viewport.height,'Composer must stay visible');
  assert.equal(await panel.evaluate(el=>el.scrollWidth<=el.clientWidth),true);
  const footerBeforeMobile=await panel.locator('footer').boundingBox();
  await input.fill('Layout check '+viewport.width);await input.press('Enter');
  const pending=page.locator('.pending-message');await pending.waitFor();
  assert.equal((await panel.locator('footer').boundingBox()).y,footerBeforeMobile.y,'Mobile pending controls must not shift the composer');
  await pending.waitFor({state:'detached'});
  assert.equal((await panel.locator('footer').boundingBox()).y,footerBeforeMobile.y,'Mobile acknowledgement must not shift the composer');
  await input.fill('Next draft');
  await page.screenshot({path:'/tmp/runloom-assistant-spotlight-'+viewport.width+'.png'});
  await page.keyboard.press('Escape');await panel.waitFor({state:'detached'});
  const mobileLauncher=await launcher.boundingBox();
  assert.ok(Math.abs(mobileLauncher.x+mobileLauncher.width/2-viewport.width/2)<1,'Launcher must remain centered after resize');
  await launcher.click();await panel.waitFor();
  await page.waitForTimeout(400);
 }
 await page.setViewportSize({width:1440,height:1000});
 await page.emulateMedia({reducedMotion:'reduce'});
 assert.equal(await panel.evaluate(el=>getComputedStyle(el).animationName),'none');
 await page.reload();if(synthetic)await syntheticLogin();
 await page.getByRole('button',{name:'Lumi',exact:true}).click();
 await page.waitForFunction(()=>document.querySelector('textarea[name="message"]')?.value==='Next draft');
 assert.equal(await panel.evaluate(el=>el.getAnimations().length),0,'Reduced motion must skip expansion');
 await input.fill('Reduced motion send');await input.press('Enter');
 await page.locator('.message.from-user:not(.pending-message)').filter({hasText:'Reduced motion send'}).waitFor();
 assert.equal(await panel.locator('.message.is-entering').count(),0,'Reduced motion must skip message entrances');
 assert.deepEqual(errors,[]);
 console.log('PASS Spotlight: shared bottom anchor during expansion and after resize, exact launcher-to-modal expansion, focus trap/return, keyboard/backdrop dismissal, suggestions, light/dark, mobile/landscape, reduced motion, optimistic send and draft restoration. Assistant responses are fixtures.');
} finally {await browser.close();}
