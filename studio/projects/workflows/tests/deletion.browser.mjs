// Real Angular UI and an opt-in Go API fixture with disposable file storage.
// Start TestDeletionBrowserAPI with RUNLOOM_DELETION_BROWSER=1 first.
import assert from 'node:assert/strict';
const {chromium} = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.RUNLOOM_UI_URL || 'http://127.0.0.1:4300';
const api = 'http://127.0.0.1:17395';
assert(['localhost', '127.0.0.1'].includes(new URL(base).hostname));
const browser = await chromium.launch({headless: true, ...(process.env.RUNLOOM_CHROME ? {executablePath: process.env.RUNLOOM_CHROME} : {})});
const page = await browser.newPage({viewport: {width: 1500, height: 1000}});
page.setDefaultTimeout(20000);
const errors = [], downloads = [];
page.on('pageerror', error => errors.push(error.message));
page.on('download', item => downloads.push(item.suggestedFilename()));
let saves = 0;
try {
  await page.route('**/api/**', async route => {
    const url = new URL(route.request().url());
    if (url.pathname.endsWith('/auth/config')) return route.fulfill({status: 503, json: {}});
    if (url.pathname.endsWith('/health')) return route.fulfill({json: {temporal: false, database: true}});
    if (url.pathname === '/api/v1/studio/definitions' && route.request().method() === 'POST') saves++;
    // Production's authenticated gateway supplies a consistent verified actor.
    // This anonymous loopback fixture must do the same for lease ownership.
    const response = await route.fetch({
      url: api + url.pathname + url.search,
      headers: {...route.request().headers(), 'x-runloom-actor': 'browser-tester'},
    });
    return route.fulfill({response});
  });
  await page.goto(base + '/workflows');
  await page.waitForFunction(() => window.ng?.getComponent(document.querySelector('app-root'))?.auth.ready());
  await page.evaluate(() => {
    const auth = window.ng.getComponent(document.querySelector('app-root')).auth;
    auth.bearer = async () => 'synthetic-token';
    auth.profile.set({user: {id: 'tester', email: 'test@example.test'}, organizations: [{id: 'org', name: 'Fixture', role: 'owner', teams: [{id: 'team', name: 'Team', role: 'admin'}]}]});
    auth.organizationId.set('org'); auth.teamId.set('team'); auth.signedIn.set(true);
    window.saveKeys = [];
    window.addEventListener('keydown', event => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 's') window.saveKeys.push(event.defaultPrevented);
    });
  });
  const deleteDraft = page.getByRole('button', {name: 'Delete workflow draft', exact: true});
  await deleteDraft.waitFor();
  assert.equal(await page.getByRole('button', {name: 'Delete workflow published', exact: true}).count(), 0);
  await page.keyboard.press('Control+s');
  assert.equal(saves, 0);
  await deleteDraft.click();
  await page.getByRole('dialog').getByRole('button', {name: 'Cancel', exact: true}).click();
  assert.equal((await fetch(api + '/api/v1/studio/definitions/draft?version=0')).status, 200);
  await deleteDraft.click();
  await page.getByRole('dialog').getByRole('button', {name: 'Delete workflow', exact: true}).click();
  await deleteDraft.waitFor({state: 'detached'});
  assert.equal((await fetch(api + '/api/v1/studio/definitions/draft?version=0')).status, 404);
  // Stale UI eligibility must not bypass the backend publication check.
  assert.equal((await fetch(api + '/api/v1/studio/definitions/race/publish', {method: 'POST', body: '{}'})).status, 200);
  await page.getByRole('button', {name: 'Delete workflow race', exact: true}).click();
  await page.getByRole('dialog').getByRole('button', {name: 'Delete workflow', exact: true}).click();
  await page.getByText('only workflows that have never been published can be deleted', {exact: false}).first().waitFor();
  assert.equal((await fetch(api + '/api/v1/studio/definitions/race?version=1')).status, 200);
  await page.getByRole('button', {name: 'Projects', exact: true}).click();
  await page.getByRole('heading', {name: 'Projects', exact: true}).waitFor();
  const projectTable = page.getByRole('table');
  await projectTable.waitFor();
  assert.equal(await projectTable.getByRole('row').count(), 4);
  await page.getByRole('searchbox', {name: 'Search projects'}).fill('Full fixture');
  assert.equal(await projectTable.getByRole('row').count(), 2);
  await page.getByRole('searchbox', {name: 'Search projects'}).fill('no-matching-project');
  await page.getByRole('heading', {name: 'No matching projects'}).waitFor();
  await page.getByRole('button', {name: 'Clear search'}).click();
  await projectTable.waitFor();
  if (process.env.RUNLOOM_PROJECTS_SCREENSHOT) await page.screenshot({path: process.env.RUNLOOM_PROJECTS_SCREENSHOT});
  await page.setViewportSize({width: 390, height: 844});
  assert(await page.getByRole('region', {name: 'Projects table'}).evaluate(element =>
    element.scrollWidth > element.clientWidth && element.getBoundingClientRect().right <= window.innerWidth));
  await page.setViewportSize({width: 1500, height: 1000});
  assert(await page.getByRole('button', {name: 'Delete project Full fixture', exact: true}).isDisabled());
  assert.equal(await page.getByRole('button', {name: 'Delete project General', exact: true}).count(), 0);
  await page.getByRole('button', {name: 'Delete project Empty fixture', exact: true}).click();
  await page.getByRole('dialog').getByRole('button', {name: 'Delete project', exact: true}).click();
  await page.getByRole('button', {name: 'Delete project Empty fixture', exact: true}).waitFor({state: 'detached'});
  const projects = await (await fetch(api + '/api/v1/studio/projects')).json();
  assert(!projects.some(item => item.name === 'Empty fixture'));
  await page.getByRole('button', {name: 'Workflows', exact: true}).click();
  await page.getByRole('button', {name: 'Details for editable', exact: true}).click();
  await page.keyboard.press('Meta+s');
  assert.equal(saves, 0, 'read-only workflow does not save');
  await page.getByRole('button', {name: 'Edit workflow', exact: true}).click();
  await page.getByLabel('Workflow name', {exact: true}).fill('Saved with keyboard');
  await page.keyboard.press('Control+s');
  try {
    await page.getByText('Draft committed to workflow history.', {exact: false}).first().waitFor();
  } catch (error) {
    console.error('Synthetic fixture save diagnostics:', {saves, notices: await page.locator('runloom-notice').allTextContents(), keys: await page.evaluate(() => window.saveKeys)});
    throw error;
  }
  assert.equal(saves, 1);
  const draft = await (await fetch(api + '/api/v1/studio/definitions/editable?version=0')).json();
  assert.equal(draft.name, 'Saved with keyboard');
  assert.equal((await fetch(api + '/api/v1/studio/definitions/editable?version=1')).status, 404, 'shortcut must not publish');
  assert.deepEqual(await page.evaluate(() => window.saveKeys), [true, true, true]);
  assert.deepEqual(errors, []);
  assert.deepEqual(downloads, []);
  console.log('PASS real Go + browser: draft/project deletion, cancel, published/stale safeguards, and Ctrl/Cmd+S without HTML download.');
} finally {
  await browser.close();
  await fetch(api + '/__test__/stop', {method: 'POST'}).catch(() => {});
}
