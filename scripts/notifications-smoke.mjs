// Mocked Studio UI: no workflow writes, credentials or external side effects.
import assert from 'node:assert/strict';
const { chromium } = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({headless:true,...(process.env.RUNLOOM_CHROME ? {executablePath:process.env.RUNLOOM_CHROME}:{})});
try {
  const page = await browser.newPage({viewport:{width:1440,height:1000},reducedMotion:'reduce',colorScheme:process.env.RUNLOOM_COLOR_SCHEME || 'light'});
  const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.clock.install();
  await page.route('**/api/**',route=>{
    const path=new URL(route.request().url()).pathname;
    if(path==='/api/v1/auth/config')return route.fulfill({status:503,json:{}});
    if(path==='/api/v1/health')return route.fulfill({json:{temporal:true,database:true}});
    if(path==='/api/v1/studio/projects')return route.fulfill({json:[{id:'default',name:'General'}]});
    if(path.endsWith('/edit-lock'))return route.fulfill({json:{locked:false,writing:false,ttl_seconds:60}});
    return route.fulfill({json:[]});
  });
  await page.goto(process.env.RUNLOOM_UI_URL || 'http://localhost:14201');
  await page.waitForFunction(()=>window.ng?.getComponent(document.querySelector('app-root'))?.auth.ready());
  await page.evaluate(()=>{
    const shell=window.ng.getComponent(document.querySelector('app-root'));
    shell.error='';shell.auth.bearer=async()=>'synthetic-token';
    shell.auth.profile.set({user:{id:'admin',email:'admin@example.test'},organizations:[{id:'org',name:'Org',role:'owner',teams:[{id:'team',name:'Team',role:'admin'}]}]});
    shell.auth.organizationId.set('org');shell.auth.teamId.set('team');shell.auth.signedIn.set(true);
  });
  await page.locator('runloom-studio').waitFor();
  await page.getByRole('button',{name:'New workflow',exact:true}).click();
  const notify=async(message,error=false)=>{
    await page.evaluate(({message,error})=>{const app=window.ng.getComponent(document.querySelector('runloom-studio'));app.notify(message,error);window.ng.applyChanges(app);},{message,error});
  };
  const toast=page.locator('runloom-notice.toast');
  await page.getByLabel('Workflow name',{exact:true}).focus();
  await notify('Workflow saved. Your changes are ready to publish.');
  assert.equal(await toast.getByRole('status').count(),1);
  assert.equal(await page.getByLabel('Workflow name',{exact:true}).evaluate(e=>e===document.activeElement),true,'toast must not steal focus');
  const rect=await toast.boundingBox();assert(rect.x>900 && rect.y>800,'toast should not cover the toolbar');
  await toast.hover();await page.clock.runFor(8000);assert.equal(await toast.count(),1,'hover pauses dismissal');
  await toast.getByRole('button',{name:'Dismiss message'}).focus();await page.mouse.move(10,10);await page.clock.runFor(8000);
  assert.equal(await toast.count(),1,'keyboard focus pauses dismissal');
  await page.getByLabel('Workflow name',{exact:true}).focus();await page.clock.runFor(6100);
  await toast.waitFor({state:'detached'});
  await notify('Saved again');await page.clock.runFor(4000);await notify('Saved again');await page.clock.runFor(3000);
  assert.equal(await toast.count(),1,'same message must restart its own timer');await page.clock.runFor(3100);await toast.waitFor({state:'detached'});
  await notify('Unable to save. Your changes are still here. Reconnect and try again.',true);
  assert.equal(await toast.getByRole('alert').count(),1);await page.clock.runFor(20000);assert.equal(await toast.count(),1,'errors must remain visible');
  await toast.getByRole('button',{name:'Dismiss message'}).focus();await page.keyboard.press('Enter');await toast.waitFor({state:'detached'});
  await page.getByRole('button',{name:'Schedules',exact:true}).click();
  await notify('Choose a published workflow before creating a schedule.',true);
  const inline=page.getByRole('dialog',{name:'Workflow schedules'}).locator('runloom-notice');
  await inline.waitFor();assert.equal(await toast.count(),0);assert.equal(await inline.evaluate(e=>getComputedStyle(e).position),'static');
  await inline.getByRole('button',{name:'Dismiss schedule message'}).click();await inline.waitFor({state:'detached'});
  await page.getByRole('button',{name:'Close schedules',exact:true}).click();
  await page.getByRole('dialog',{name:'Workflow schedules'}).waitFor({state:'detached'});
  await page.evaluate(()=>{
    const app=window.ng.getComponent(document.querySelector('runloom-studio'));
    app.definitions=[{id:app.definition.id}];app.edit.editLost=true;window.ng.applyChanges(app);
  });
  await page.getByText('Editing paused',{exact:true}).waitFor();
  await page.getByRole('button',{name:'View unsaved JSON',exact:true}).waitFor();
  await notify('Could not save this workflow. Your unsaved changes are preserved.',true);
  if(process.env.RUNLOOM_SCREENSHOT_PATH)await page.screenshot({path:process.env.RUNLOOM_SCREENSHOT_PATH});
  await page.setViewportSize({width:390,height:844});
  await notify('A long validation message: '+ 'workflow-node-with-a-long-name-'.repeat(40),true);
  const mobile=await toast.boundingBox();assert(mobile.x>=0 && mobile.x+mobile.width<=390 && mobile.y>=0 && mobile.y+mobile.height<=844,'notification must fit mobile viewport');
  assert.equal(await toast.locator('.notice-icon').getAttribute('aria-hidden'),'true');
  assert.equal(await toast.evaluate(e=>getComputedStyle(e).animationName),'none');
  await notify('<img src=x onerror=alert(1)>',true);assert.equal(await toast.locator('img').count(),0,'message must render as text');
  assert.deepEqual(errors,[]);
  console.log('PASS notification placement, accessibility, auto-dismiss, hover/focus pause, repeated messages, persistent errors, schedule scope, recovery notice, mobile and escaped content');
}finally{await browser.close();}
