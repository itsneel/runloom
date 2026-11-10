// Synthetic credentials and mocked APIs only. Requires the Angular development server.
import assert from 'node:assert/strict';
const { chromium } = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({headless:true, ...(process.env.RUNLOOM_CHROME ? {executablePath:process.env.RUNLOOM_CHROME} : {})});
const page = await browser.newPage({viewport:{width:1920,height:1080}});
const errors = [];
page.on('pageerror', error => errors.push(error.message));
let secrets = [];
let saved;
await page.route('**/api/**', async route => {
  const path = new URL(route.request().url()).pathname;
  const method = route.request().method();
  if (path === '/api/v1/auth/config') return route.fulfill({status:503,json:{}});
  if (path === '/api/v1/health') return route.fulfill({json:{temporal:true,database:true}});
  if (path === '/api/v1/studio/projects') return route.fulfill({json:[{id:'default',name:'General'}]});
  if (path === '/api/v1/studio/orgs/org/teams/team/secrets') return route.fulfill({json:{configured:true,items:secrets}});
  if (path === '/api/v1/studio/orgs/org/teams/team/secrets/billing-api') {
    if (method === 'PUT') { saved = route.request().postDataJSON(); secrets = [{name:'billing-api',updated_at:new Date().toISOString()}]; }
    if (method === 'DELETE') secrets = [];
    return route.fulfill({json:{saved:true}});
  }
  return route.fulfill({json:[]});
});
try {
  await page.goto(process.env.RUNLOOM_UI_URL || 'http://localhost:4200');
  await page.waitForFunction(() => window.ng?.getComponent(document.querySelector('app-root'))?.auth.ready());
  await page.evaluate(() => {
    const shell = window.ng.getComponent(document.querySelector('app-root'));
    shell.auth.bearer = async () => 'synthetic-token';
    shell.auth.profile.set({user:{id:'admin',email:'admin@example.test'},organizations:[{id:'org',name:'Org',role:'owner',teams:[{id:'team',name:'Team',role:'admin'}]}]});
    shell.auth.organizationId.set('org'); shell.auth.teamId.set('team'); shell.auth.signedIn.set(true);
  });
  await page.getByRole('button',{name:'Management',exact:true}).click();
  await page.getByRole('button',{name:'Workflow secrets',exact:true}).click();
  await page.getByLabel('Secret name',{exact:true}).fill('billing-api');
  assert.equal(await page.getByLabel('Secret origin',{exact:true}).count(),0);
  await page.getByLabel('Secret value',{exact:true}).fill('synthetic-value-never-in-workflow');
  assert.equal(await page.getByLabel('Secret value',{exact:true}).getAttribute('type'),'password');
  await page.getByRole('button',{name:'Save / replace secret',exact:true}).click();
  await page.getByText('Secret saved. Its value cannot be retrieved.',{exact:true}).waitFor();
  assert.deepEqual(saved,{value:'synthetic-value-never-in-workflow'});
  assert.equal(await page.getByLabel('Secret value',{exact:true}).inputValue(),'');
  const persisted = await page.evaluate(() => JSON.stringify({local:{...localStorage},session:{...sessionStorage}}));
  assert(!persisted.includes(saved.value));
  const modal = page.locator('runloom-dialogs').getByRole('dialog');
  await page.getByLabel('Secret name',{exact:true}).fill('billing-api');
  await page.getByLabel('Secret value',{exact:true}).fill('synthetic-replacement');
  await page.getByRole('button',{name:'Save / replace secret',exact:true}).click();
  await modal.getByTestId('dialog-cancel').click();
  await modal.waitFor({state:'hidden'});
  assert.equal(saved.value,'synthetic-value-never-in-workflow');
  await page.getByRole('button',{name:'Save / replace secret',exact:true}).click();
  await modal.getByTestId('dialog-confirm').click();
  await page.waitForFunction(() => document.querySelector('[aria-label="Secret name"]').value === '');
  assert.equal(saved.value,'synthetic-replacement');
  const deleteSecret = page.locator('runloom-workflow-secrets').getByRole('button',{name:'Delete',exact:true});
  await deleteSecret.click();
  await modal.getByTestId('dialog-cancel').click();
  await modal.waitFor({state:'hidden'});
  assert.equal(secrets.length,1);
  await deleteSecret.click();
  await modal.getByTestId('dialog-confirm').click();
  await deleteSecret.waitFor({state:'hidden'});
  assert.equal(secrets.length,0);
  // Unsaved secret changes must guard returning to the builder.
  await page.getByLabel('Secret name',{exact:true}).fill('unsaved-secret');
  await page.getByRole('button',{name:'Workflows',exact:true}).click();
  await page.getByRole('dialog').getByTestId('dialog-cancel').click();
  assert.equal(await page.getByLabel('Secret name',{exact:true}).inputValue(),'unsaved-secret');
  await page.getByRole('button',{name:'Workflows',exact:true}).click();
  await page.getByRole('dialog').getByTestId('dialog-confirm').click();
  await page.locator('runloom-studio').waitFor();
  await page.locator('.inspector-head button').click();
  await page.locator('.graph-toolbar button[title="Add node"]').click();
  await page.locator('.picker-row').filter({hasText:'External API'}).click();
  await page.getByLabel('API method',{exact:true}).selectOption('POST');
  await page.getByLabel('API URL',{exact:true}).fill('https://api.example.com/orders');
  await page.getByLabel('API secret name',{exact:true}).fill('billing-api');
  const config = JSON.parse(await page.getByLabel('API request configuration',{exact:true}).inputValue());
  assert.deepEqual(config.secret_headers.Authorization,{secret:'billing-api',prefix:'Bearer '});
  assert(!JSON.stringify(config).includes(saved.value));
  await page.locator('.inspector-head button').click();
  await page.locator('.graph-toolbar button[title="Add node"]').click();
  await page.locator('.picker-row').filter({hasText:'End workflow'}).click();
  await page.getByText('Workflow will end now',{exact:true}).waitFor();
  await page.getByLabel('End output',{exact:true}).fill('{"done":true}');
  const definition = await page.evaluate(() => window.ng.getComponent(document.querySelector('runloom-studio')).definition);
  assert.equal(definition.steps[0].type,'http');
  assert.equal(definition.steps[0].retry.maximum_attempts,1);
  assert.equal(definition.steps[1].type,'end');
  assert.deepEqual(definition.steps[1].with,{done:true});
  assert(!JSON.stringify(definition).includes(saved.value));
  assert.deepEqual(errors,[]);
  console.log('PASS API/End builder controls, secret save without retrieval/persistence, and unsaved-secret navigation guard');
} finally { await browser.close(); }
