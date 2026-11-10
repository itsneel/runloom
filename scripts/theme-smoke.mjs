// Synthetic session only: no real accounts, credentials or workflow execution.
import assert from 'node:assert/strict';
const { chromium } = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({headless:true, ...(process.env.RUNLOOM_CHROME ? {executablePath:process.env.RUNLOOM_CHROME} : {})});
const base = process.env.RUNLOOM_UI_URL || 'http://localhost:14201';
const context = await browser.newContext({viewport:{width:1440,height:1000}, colorScheme:'dark'});
context.setDefaultTimeout(10000);
const errors = [];
context.on('page', page => page.on('pageerror', error => errors.push(error.message)));
await context.route('**/api/**', route => {
  const path = new URL(route.request().url()).pathname;
  if (path === '/api/v1/auth/config') return route.fulfill({status:503,json:{}});
  if (path === '/api/v1/health') return route.fulfill({json:{temporal:true,database:true}});
  if (path === '/api/v1/studio/projects') return route.fulfill({json:[{id:'default',name:'General'}]});
  if (path.endsWith('/edit-lock')) return route.fulfill({json:{locked:false,writing:false,ttl_seconds:60}});
  return route.fulfill({json:[]});
});
const resolved = (page, theme) => page.waitForFunction(value => document.documentElement.dataset.theme === value, theme);
const ready = page => page.waitForFunction(() => window.ng?.getComponent(document.querySelector('app-root'))?.auth.ready());
async function choose(page, label) {
  await page.getByRole('button', {name:/^Appearance:/}).click();
  await page.getByRole('group', {name:'Color theme'}).getByRole('button', {name:label,exact:true}).click();
}
async function signedIn(page) {
  await ready(page);
  await page.evaluate(() => {
    const shell = window.ng.getComponent(document.querySelector('app-root'));
    shell.error = ''; shell.auth.bearer = async () => 'synthetic';
    shell.auth.profile.set({user:{id:'admin',name:'Alex Morgan',email:'alex@example.test'},organizations:[{id:'org',name:'Acme workspace',role:'owner',teams:[{id:'team',name:'Product',role:'admin'}]}]});
    shell.auth.organizationId.set('org'); shell.auth.teamId.set('team'); shell.auth.signedIn.set(true);
  });
  await page.locator('runloom-studio').waitFor();
}
try {
  const page = await context.newPage();
  // Prevent Angular loading to verify the head script resolves the first paint.
  await page.route('**/main.js', route => route.abort());
  await page.goto(base);
  await resolved(page, 'dark');
  await page.unroute('**/main.js');
  await page.reload();
  await ready(page);
  await choose(page, 'Light');
  await resolved(page, 'light');
  assert.equal(await page.evaluate(() => localStorage.getItem('runloom.theme')), 'light');
  await page.reload();
  await ready(page);
  await resolved(page, 'light');
  await choose(page, 'System');
  await resolved(page, 'dark');
  await page.emulateMedia({colorScheme:'light'});
  await resolved(page, 'light');
  await page.emulateMedia({colorScheme:'dark'});
  await resolved(page, 'dark');
  const other = await context.newPage();
  await other.goto(base); await ready(other);
  await choose(page, 'Light');
  await resolved(other, 'light');
  await choose(page, 'Dark');
  await resolved(other, 'dark');
  await page.emulateMedia({colorScheme:'light'});
  await resolved(page, 'dark');
  await page.emulateMedia({colorScheme:'dark'});
  await other.evaluate(() => localStorage.clear());
  await page.waitForFunction(() => window.ng.getComponent(document.querySelector('app-root')).theme.preference() === 'system');
  await other.close();
  await signedIn(page);
  await page.getByRole('button', {name:'New workflow', exact:true}).click();
  const name = page.getByLabel('Workflow name', {exact:true});
  await name.fill('Unsaved draft stays here');
  await choose(page, 'Light');
  assert.equal(await name.inputValue(), 'Unsaved draft stays here');
  await page.screenshot({path:'/tmp/runloom-mist-builder.png'});
  await choose(page, 'Dark');
  assert.equal(await name.inputValue(), 'Unsaved draft stays here');
  await page.screenshot({path:'/tmp/runloom-midnight-builder.png'});
  await page.getByRole('button', {name:'JSON',exact:true}).first().click();
  assert.equal(await page.locator('.json-modal > textarea').evaluate(el => getComputedStyle(el).backgroundColor), 'rgb(17, 21, 30)');
  await page.screenshot({path:'/tmp/runloom-midnight-json.png'});
  await page.locator('.json-modal').getByRole('button', {name:/close/i}).click();
  await page.getByRole('button', {name:'Management',exact:true}).click();
  await page.getByRole('dialog').waitFor();
  assert.equal(await page.getByRole('dialog').evaluate(el => getComputedStyle(el).backgroundColor), 'rgb(28, 33, 45)');
  await page.screenshot({path:'/tmp/runloom-midnight-dialog.png'});
  await page.getByRole('dialog').getByRole('button', {name:/cancel|keep/i}).click();
  await page.getByRole('button', {name:'Collapse sidebar',exact:true}).click();
  await page.getByRole('button', {name:/^Appearance:/}).focus();
  await page.keyboard.press('Enter');
  await page.getByRole('group', {name:'Color theme'}).waitFor();
  await page.keyboard.press('Escape');
  assert.equal(await page.getByRole('button', {name:/^Appearance:/}).evaluate(el => el === document.activeElement), true);
  await choose(page, 'Light');
  await choose(page, 'Dark');
  await page.setViewportSize({width:390,height:844});
  await page.getByRole('button', {name:'Toggle workspace sidebar',exact:true}).click();
  await page.getByRole('button', {name:/^Appearance:/}).click();
  await page.keyboard.press('Escape');
  assert.equal(await page.getByRole('button', {name:'Close sidebar',exact:true}).isVisible(), true);
  await choose(page, 'Light');
  await choose(page, 'Dark');
  await page.getByRole('button', {name:'Close sidebar',exact:true}).click();
  await page.screenshot({path:'/tmp/runloom-midnight-mobile.png'});
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
  // Storage-denied browsers still render and can change appearance in memory.
  const restricted = await context.newPage();
  await restricted.addInitScript(() => {
    Storage.prototype.getItem = () => { throw new Error('Storage disabled'); };
    Storage.prototype.setItem = () => { throw new Error('Storage disabled'); };
  });
  await restricted.goto(base); await ready(restricted); await resolved(restricted, 'dark');
  await choose(restricted, 'Light'); await resolved(restricted, 'light');
  const invalid = await context.newPage();
  await invalid.addInitScript(() => localStorage.setItem('runloom.theme', 'not-a-theme'));
  await invalid.goto(base); await ready(invalid); await resolved(invalid, 'dark');
  assert.deepEqual(errors, []);
  console.log('PASS pre-Angular theme, light/dark/system, live OS changes, persistence, cross-tab/clear, storage denied, draft preservation, dark JSON/dialogs, collapsed rail, keyboard and mobile');
} finally { await browser.close(); }
