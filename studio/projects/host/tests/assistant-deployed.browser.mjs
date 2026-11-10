// Actual deployed assets and services: no request routing or model fixture.
import assert from 'node:assert/strict';
const {chromium}=await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const origin=process.env.RUNLOOM_UI_URL || 'http://localhost:4200';
if(!['localhost','127.0.0.1'].includes(new URL(origin).hostname))throw new Error('Local deployment only');
const browser=await chromium.launch({headless:true,...(process.env.RUNLOOM_CHROME?{executablePath:process.env.RUNLOOM_CHROME}:{})});
try{
 const page=await browser.newPage({viewport:{width:1440,height:1000}}),errors=[];page.setDefaultTimeout(20000);
 page.on('pageerror',e=>errors.push(e.message));
 await page.goto(origin+'/workflows');
 await page.locator('#username').fill(process.env.RUNLOOM_TEST_EMAIL || 'admin@runloom.test');
 await page.locator('#password').fill(process.env.RUNLOOM_TEST_PASSWORD || 'RunloomLocal!123');
 const signedIn=page.waitForResponse(r=>r.url().endsWith('/api/v1/studio/me') && r.ok());
 await page.locator('#kc-login').click();await signedIn;
 const configResponse=page.waitForResponse(r=>new URL(r.url()).pathname==='/api/v1/studio/assistant/config');
 await page.getByRole('button',{name:'Lumi',exact:true}).click();
 const panel=page.getByRole('dialog',{name:'Lumi'});
 await panel.waitFor();await page.waitForTimeout(450);
 assert.equal(await panel.evaluate(el=>el.matches(':modal')),true);
 const bounds=await panel.boundingBox();
 const anchor=await page.locator('.assistant-launcher').boundingBox();
 assert.ok(Math.abs(bounds.x+bounds.width/2-720)<2 && Math.abs(bounds.y+bounds.height-anchor.y-anchor.height)<2,'Deployed assistant must be horizontally centered and bottom-anchored to its launcher');
 const response=await configResponse;assert.equal(response.status(),200);const config=await response.json();assert.equal(config.mode,'reviewed-authoring');
 if(!config.configured){
  const composer=page.getByRole('textbox',{name:'Message Lumi'});
  await panel.evaluate(el=>{
   window.__savedBubble=null;window.__repeatExpansion=0;window.__deployedMessageEntrances=0;
   window.__chatObserver=new MutationObserver(()=>{
    window.__savedBubble ||= el.querySelector('.pending-message');
   });
   window.__chatObserver.observe(el,{childList:true,subtree:true,attributes:true,attributeFilter:['class']});
   el.addEventListener('animationstart',event=>{
    if(event.target===el && event.animationName.endsWith('assistant-expand'))window.__repeatExpansion++;
    if(event.target.matches('.message') && event.animationName.endsWith('message-send-in')){
     window.__deployedMessageEntrances++;
     window.__deployedMessageDuration=parseFloat(getComputedStyle(event.target).animationDuration)*1000;
    }
   });
  });
  const composerY=(await composer.boundingBox()).y;
  await composer.fill('Local deployment verification');await composer.press('Enter');
  await page.getByText('LLM is not configured.',{exact:true}).waitFor();
  await page.waitForFunction(()=>window.__deployedMessageEntrances===1);
  assert.equal(await page.evaluate(()=>window.__deployedMessageDuration),600);
  await page.waitForFunction(()=>!document.querySelector('.message.is-entering'));
  assert.equal(await page.evaluate(()=>window.__deployedMessageEntrances),1,'Fast acknowledgement must not interrupt or replay the message animation');
  assert.equal(await panel.evaluate(el=>!!window.__savedBubble && window.__savedBubble===el.querySelector('.message.from-user') && window.__repeatExpansion===0),true,'Deployed send must keep the bubble and must not re-expand');
  assert.equal((await composer.boundingBox()).y,composerY,'Deployed composer must stay fixed');
  await page.evaluate(()=>window.__chatObserver.disconnect());
  await composer.fill('Next message remains editable');
  assert.equal(await composer.evaluate(e=>document.activeElement===e && !e.disabled),true);
  await composer.press('Escape');await panel.waitFor({state:'detached'});
  await page.keyboard.press('Control+k');await panel.waitFor();
  assert.equal(await composer.inputValue(),'Next message remains editable');
  await page.reload();await page.getByRole('button',{name:'Lumi',exact:true}).click();
  await page.getByText('LLM is not configured.',{exact:true}).waitFor();
  assert.equal(await composer.inputValue(),'Next message remains editable');
 }
 await page.screenshot({path:'/tmp/runloom-authoring-deployed.png'});
 assert.deepEqual(errors,[]);
 console.log('PASS actual deployed Studio federation, Keycloak login, bottom-anchored modal, stable pending/saved bubble and composer, no repeat expansion, keyboard dismissal/reopen, persisted unconfigured reply; no paid model called.');
}catch(error){throw new Error('Deployed browser verification failed: '+String(error.message).split('Call log:')[0]);}finally{await browser.close()}
