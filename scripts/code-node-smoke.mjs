// Mocked Studio regression. Code is sent to the API, never evaluated in the browser.
import assert from 'node:assert/strict';
const { chromium } = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({headless:true, ...(process.env.RUNLOOM_CHROME ? {executablePath:process.env.RUNLOOM_CHROME} : {})});
const page = await browser.newPage({viewport:{width:1920,height:1080}});
const errors = [];
page.on('pageerror', error => errors.push(error.message));
let tested, delayed, release;
await page.route('**/api/**', async route => {
  const path = new URL(route.request().url()).pathname;
  if (path === '/api/v1/auth/config') return route.fulfill({status:503,json:{}});
  if (path === '/api/v1/health') return route.fulfill({json:{temporal:true,database:true}});
  if (path === '/api/v1/studio/projects') return route.fulfill({json:[{id:'default',name:'General'}]});
  if (path === '/api/v1/studio/code/test') {
    tested = route.request().postDataJSON();
    if (delayed) await new Promise(resolve => { release=resolve; });
    return route.fulfill(tested.code.source.includes('throw') ? {status:422,json:{detail:'Error: example failure'}} : {json:{output:{message:'Hello Runloom'}}});
  }
  return route.fulfill({json:[]});
});
try {
  await page.goto(process.env.RUNLOOM_UI_URL || 'http://localhost:14201');
  await page.waitForFunction(() => window.ng?.getComponent(document.querySelector('app-root'))?.auth.ready());
  await page.evaluate(() => {
    const shell = window.ng.getComponent(document.querySelector('app-root'));
    shell.auth.bearer = async () => 'synthetic-token';
    shell.auth.profile.set({user:{id:'admin',email:'admin@example.test'},organizations:[{id:'org',name:'Org',role:'owner',teams:[{id:'team',name:'Team',role:'admin'}]}]});
    shell.auth.organizationId.set('org'); shell.auth.teamId.set('team'); shell.auth.signedIn.set(true);
  });
  await page.locator('runloom-studio').waitFor();
  await page.getByRole('button',{name:'New workflow',exact:true}).click();
  await page.locator('.inspector-head button').click();
  await page.locator('.graph-toolbar button[title="Add node"]').click();
  await page.locator('.picker-row').filter({hasText:'Custom Code'}).click();
  const source='return {message: `Hello ${input.name}`};';
  await page.getByLabel('JavaScript code',{exact:true}).fill(source);
  await page.getByLabel('JavaScript mapped input',{exact:true}).fill('{"name":"${input.customer.name}"}');
  await page.getByLabel('JavaScript sample input',{exact:true}).fill('{"name":"Runloom"}');
  await page.getByRole('button',{name:'Test JavaScript',exact:true}).click();
  await page.getByLabel('JavaScript test result',{exact:true}).waitFor();
  assert.deepEqual(tested,{code:{runtime:'quickjs-ng-0.11.0-v1',source},input:{name:'Runloom'}});
  assert.equal(JSON.parse(await page.getByLabel('JavaScript test result',{exact:true}).innerText()).message,'Hello Runloom');
  await page.getByLabel('JavaScript code',{exact:true}).fill('throw new Error("example failure")');
  assert.equal(await page.getByLabel('JavaScript test result',{exact:true}).count(),0);
  await page.getByRole('button',{name:'Test JavaScript',exact:true}).click();
  await page.getByText('Error: example failure',{exact:true}).waitFor();
  await page.getByLabel('JavaScript sample input',{exact:true}).fill('[]');
  await page.getByRole('button',{name:'Test JavaScript',exact:true}).click();
  await page.getByText('Sample input must be a JSON object.',{exact:true}).waitFor();
  await page.getByLabel('JavaScript sample input',{exact:true}).fill('{}');
  await page.getByLabel('JavaScript code',{exact:true}).fill(source);
  delayed=true;
  await page.getByRole('button',{name:'Test JavaScript',exact:true}).click();
  await page.getByRole('button',{name:'Testing JavaScript…',exact:true}).waitFor();
  await page.getByLabel('JavaScript code',{exact:true}).fill('return {updated:true};');
  while (!release) await new Promise(resolve=>setTimeout(resolve,10));
  release();
  await page.getByRole('button',{name:'Test JavaScript',exact:true}).waitFor();
  assert.equal(await page.getByLabel('JavaScript test result',{exact:true}).count(),0,'stale preview must not replace edited code results');
  const definition=await page.evaluate(()=>window.ng.getComponent(document.querySelector('runloom-studio')).definition);
  assert.equal(definition.steps[0].type,'code');
  assert.equal(definition.steps[0].code.source,'return {updated:true};');
  assert.deepEqual(definition.steps[0].with,{name:'${input.customer.name}'});
  await page.getByRole('button',{name:'Management',exact:true}).click();
  await page.getByRole('dialog').waitFor();
  await page.getByTestId('dialog-cancel').click();
  assert.equal(await page.getByLabel('JavaScript code',{exact:true}).inputValue(),'return {updated:true};');
  assert.deepEqual(errors,[]);
  console.log('PASS JavaScript node creation, literal source, mappings, API preview, errors, stale-response protection and unsaved-change guard');
} finally { await browser.close(); }
