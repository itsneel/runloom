// Synthetic profile and APIs. Tests real federated JavaScript, never starts a workflow.
import assert from 'node:assert/strict';
const {chromium} = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({headless:true,...(process.env.RUNLOOM_CHROME?{executablePath:process.env.RUNLOOM_CHROME}:{})});
try {
  const page = await browser.newPage({viewport:{width:1440,height:1000}});
  page.setDefaultTimeout(15000);
  const errors=[],entries=[];
  page.on('pageerror',error => errors.push(error.message));
  page.on('request',request => {if(request.url().includes('/remoteEntry.js')) entries.push(new URL(request.url()).pathname);});
  await page.route('**/api/**',route => {
    const path = new URL(route.request().url()).pathname;
    if(path==='/api/v1/auth/config') return route.fulfill({status:503,json:{}});
    if(path==='/api/v1/health') return route.fulfill({json:{temporal:true,database:true}});
    if(path==='/api/v1/studio/projects') return route.fulfill({json:[{id:'default',name:'General'}]});
    if(path.endsWith('/edit-lock')) return route.fulfill({json:{locked:false,writing:false,ttl_seconds:60}});
    return route.fulfill({json:[]});
  });
  await page.goto(process.env.RUNLOOM_UI_URL || 'http://localhost:4200');
  await page.waitForFunction(()=>window.ng?.getComponent(document.querySelector('app-root'))?.auth.ready());
  assert.deepEqual(entries,[], 'Signed-out host must not fetch feature remotes');
  await page.evaluate(()=>{
    const shell=window.ng.getComponent(document.querySelector('app-root'));
    shell.auth.bearer=async()=>'synthetic';
    shell.auth.profile.set({user:{id:'test',email:'test@example.test'},organizations:[{id:'org',name:'Acme',role:'owner',teams:[{id:'team',name:'Product',role:'admin'}]}]});
    shell.auth.organizationId.set('org');shell.auth.teamId.set('team');shell.auth.signedIn.set(true);
  });
  await page.waitForFunction(()=>document.querySelector('runloom-studio') || document.querySelector('.failure'));
  if(await page.locator('.failure').count()) {
    const cause=await page.evaluate(async()=>{try {await window.ng.getComponent(document.querySelector('runloom-remote-outlet')).loader.load('workflows');return 'Component mounting failed';}catch(error){return String(error)}});
    assert.fail(cause+'; browser errors: '+errors.join('; '));
  }
  await page.getByRole('button',{name:'New workflow',exact:true}).waitFor();
  assert.deepEqual(entries,['/remotes/workflows/remoteEntry.js']);
  assert.equal(await page.evaluate(()=>window.ng.getComponent(document.querySelector('app-root')).auth === window.ng.getComponent(document.querySelector('runloom-studio')).auth),true,'Host and workflow remote must share one AuthService');
  await page.getByRole('button',{name:'New workflow',exact:true}).click();
  await page.getByLabel('Workflow name',{exact:true}).fill('Federated draft');
  const dashboard = page.getByRole('button',{name:'Workflows',exact:true});
  assert.equal(await dashboard.count(), 1, 'Only one sidebar dashboard entry');
  await dashboard.click();
  await page.getByRole('dialog').getByRole('button',{name:/cancel|keep/i}).click();
  assert.equal(await page.getByLabel('Workflow name',{exact:true}).inputValue(),'Federated draft');
  await dashboard.click();
  await page.getByRole('dialog').getByRole('button',{name:/discard/i}).click();
  await page.getByRole('button',{name:'New workflow',exact:true}).waitFor();
  await page.getByRole('button',{name:'New workflow',exact:true}).click();
  await page.getByLabel('Workflow name',{exact:true}).fill('Federated draft');
  await page.getByRole('button',{name:'Teams & members',exact:true}).click();
  await page.getByRole('dialog').getByRole('button',{name:/cancel|keep/i}).click();
  assert.equal(await page.getByLabel('Workflow name',{exact:true}).inputValue(),'Federated draft');
  assert.equal(entries.length,1,'Canceled switch must not load management');
  let failOnce=true;
  await page.route('**/remotes/management/remoteEntry.js*',route=>{if(failOnce){failOnce=false;return route.fulfill({status:503,body:'Unavailable'})}return route.continue()});
  await page.getByRole('button',{name:'Teams & members',exact:true}).click();
  await page.getByRole('dialog').getByRole('button',{name:/discard/i}).click();
  await page.getByRole('button',{name:'Retry application',exact:true}).click();
  await page.getByRole('heading',{name:'Teams & access',exact:true}).waitFor();
  assert.equal(await page.evaluate(()=>window.ng.getComponent(document.querySelector('app-root')).auth === window.ng.getComponent(document.querySelector('runloom-management')).auth),true);
  await page.getByRole('button',{name:/^Appearance:/}).click();
  await page.getByRole('group',{name:'Color theme'}).getByRole('button',{name:'Dark',exact:true}).click();
  await page.getByRole('button',{name:'Workflows',exact:true}).click();
  await page.locator('runloom-studio').waitFor();
  assert.equal(await page.locator('html').getAttribute('data-theme'),'dark');
  assert.equal(entries.filter(path=>path.includes('/workflows/')).length,1,'Workflow container is cached');
  await page.evaluate(()=>window.ng.getComponent(document.querySelector('app-root')).auth.signedIn.set(false));
  await page.locator('runloom-studio').waitFor({state:'detached'});
  assert.deepEqual(errors,[]);
  console.log('PASS authenticated lazy remotes, shared auth, guarded app switch, remote failure/retry, theme continuity, cached container and sign-out unmount');
} finally {await browser.close();}
