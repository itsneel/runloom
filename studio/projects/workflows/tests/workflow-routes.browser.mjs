// Requires the disposable TestDeletionBrowserAPI fixture and independent host/workflows dev servers.
import assert from 'node:assert/strict';
const {chromium} = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.RUNLOOM_UI_URL || 'http://127.0.0.1:4300';
const api = 'http://127.0.0.1:17395';
assert(['localhost', '127.0.0.1'].includes(new URL(base).hostname));
const browser = await chromium.launch({headless: true, ...(process.env.RUNLOOM_CHROME ? {executablePath: process.env.RUNLOOM_CHROME} : {})});
const page = await browser.newPage({viewport: {width: 1500, height: 1000}});
page.setDefaultTimeout(15000);
const errors = [];
page.on('pageerror', error => errors.push(error.message));
async function signIn() {
  await page.waitForFunction(() => window.ng?.getComponent(document.querySelector('app-root'))?.auth.ready());
  await page.evaluate(() => {
    const auth = window.ng.getComponent(document.querySelector('app-root')).auth;
    auth.bearer = async () => 'synthetic-token';
    auth.profile.set({user: {id: 'tester', email: 'test@example.test'}, organizations: [{id: 'org', name: 'Fixture', role: 'owner', teams: [{id: 'team', name: 'Team', role: 'admin'}]}]});
    auth.organizationId.set('org'); auth.teamId.set('team'); auth.signedIn.set(true);
  });
}
async function pathIs(path) { await page.waitForURL(url => url.pathname === path); }
async function highlighted(name) {
  assert.equal(await page.getByRole('navigation', {name: 'Workspace'}).getByRole('button', {name, exact: true}).getAttribute('aria-current'), 'page');
}
try {
  await page.route('**/api/**', async route => {
    const url = new URL(route.request().url());
    if (url.pathname.endsWith('/auth/config')) return route.fulfill({status: 503, json: {}});
    if (url.pathname.endsWith('/health')) return route.fulfill({json: {temporal: false, database: true}});
    return route.fulfill({response: await route.fetch({
      url: api + url.pathname + url.search,
      headers: {...route.request().headers(), 'x-runloom-actor': 'browser-tester'},
    })});
  });
  const projects = await (await fetch(api + '/api/v1/studio/projects')).json();
  const full = projects.find(project => project.name === 'Full fixture').id;
  const projectPath = '/workflows/projects/' + full;
  await page.goto(base + '/workflows/projects'); await signIn();
  await page.getByRole('button', {name: 'Open project Full fixture', exact: true}).click();
  await pathIs(projectPath);
  await page.getByRole('button', {name: 'Details for assigned', exact: true}).waitFor();
  await highlighted('Workflows');
  await page.reload(); await signIn();
  await page.getByRole('button', {name: 'Details for assigned', exact: true}).waitFor();
  await pathIs(projectPath);
  await page.getByRole('combobox', {name: 'Browse project'}).selectOption('default');
  await pathIs('/workflows/projects/default');
  await page.getByRole('button', {name: 'Details for editable', exact: true}).waitFor();
  await page.goBack();
  await pathIs(projectPath);
  await page.getByRole('button', {name: 'Details for assigned', exact: true}).click();
  await pathIs('/workflows/definitions/assigned');
  await page.getByLabel('Workflow name', {exact: true}).waitFor();
  await highlighted('Workflows');
  await page.reload(); await signIn();
  await page.getByLabel('Workflow name', {exact: true}).waitFor();
  assert.equal(await page.getByLabel('Workflow name', {exact: true}).inputValue(), 'assigned');
  await page.getByRole('button', {name: 'Edit workflow', exact: true}).click();
  await page.getByLabel('Workflow name', {exact: true}).fill('Unsaved route test');
  await page.goBack();
  await page.getByRole('dialog').getByRole('button', {name: 'Keep editing', exact: true}).click();
  await pathIs('/workflows/definitions/assigned');
  assert.equal(await page.getByLabel('Workflow name', {exact: true}).inputValue(), 'Unsaved route test');
  await page.goBack();
  await page.getByRole('dialog').getByRole('button', {name: 'Discard changes', exact: true}).click();
  await pathIs(projectPath);
  await page.getByRole('button', {name: 'Details for assigned', exact: true}).waitFor();
  await page.goForward();
  await pathIs('/workflows/definitions/assigned');
  await page.getByLabel('Workflow name', {exact: true}).waitFor();
  assert.equal(await page.getByLabel('Workflow name', {exact: true}).inputValue(), 'assigned');

  // Changing only a definition parameter must guard and remount too.
  await page.getByRole('button', {name: 'Edit workflow', exact: true}).click();
  await page.getByLabel('Workflow name', {exact: true}).fill('Keep this change');
  await page.evaluate(() => { void window.ng.getComponent(document.querySelector('runloom-studio')).openDefinition('editable'); });
  await page.getByRole('dialog').getByRole('button', {name: 'Keep editing', exact: true}).click();
  await pathIs('/workflows/definitions/assigned');
  await page.evaluate(() => { void window.ng.getComponent(document.querySelector('runloom-studio')).openDefinition('editable'); });
  await page.getByRole('dialog').getByRole('button', {name: 'Discard changes', exact: true}).click();
  await pathIs('/workflows/definitions/editable');
  await page.waitForFunction(() => document.querySelector('input[aria-label="Workflow name"]')?.value === 'editable');

  // A move changes membership, not the editor URL.
  assert.equal((await fetch(api + '/api/v1/studio/definitions/editable/project', {method: 'POST', body: JSON.stringify({project_id: full})})).status, 200);
  await page.reload(); await signIn();
  await page.getByLabel('Workflow name', {exact: true}).waitFor();
  await pathIs('/workflows/definitions/editable');
  await page.getByRole('button', {name: 'Back to workflows', exact: true}).click();
  await pathIs(projectPath);
  await page.getByRole('button', {name: 'Details for editable', exact: true}).waitFor();
  await page.getByRole('button', {name: 'Back to projects', exact: true}).click();
  await pathIs('/workflows/projects');
  await page.getByRole('heading', {name: 'Projects', exact: true}).waitFor();
  await highlighted('Projects');

  for (const path of ['/workflows/projects/missing', '/workflows/definitions/missing']) {
    await page.goto(base + path); await signIn();
    await page.getByRole('heading', {name: 'Unable to open this page'}).waitFor();
    await pathIs(path);
    assert.equal(await page.getByRole('button', {name: 'New workflow', exact: true}).count(), 0);
  }
  assert.deepEqual(errors, []);
  console.log('PASS project/editor deep links, highlights, reload, parameter changes, Back/Forward guards, moved workflows and missing IDs.');
} finally {
  await browser.close();
  await fetch(api + '/__test__/stop', {method: 'POST'}).catch(() => {});
}
