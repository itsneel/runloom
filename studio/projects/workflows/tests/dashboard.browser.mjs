// Real federated UI; synthetic workflow APIs. No persisted workflows or runs.
import assert from 'node:assert/strict';
const { chromium } = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({headless: true, ...(process.env.RUNLOOM_CHROME ? {executablePath: process.env.RUNLOOM_CHROME} : {})});
const base = process.env.RUNLOOM_UI_URL || 'http://localhost:4200';
assert(['localhost', '127.0.0.1'].includes(new URL(base).hostname), 'Local browser verification only');
const page = await browser.newPage({viewport: {width: 1440, height: 1000}});
page.setDefaultTimeout(15000);
const errors = [];
page.on('pageerror', error => errors.push(error.message));
let detailReads = 0, lockReads = 0, releases = 0, failList = false, failDetails = false;
const summaries = [
  {id: 'billing', name: 'Billing approval', project_id: 'default', latest_version: 3, published_kind: 'main', has_draft: true},
  {id: 'onboarding', name: 'Customer onboarding', project_id: 'default', latest_version: 0, has_draft: true},
  {id: 'child', name: 'Send confirmation', project_id: 'second', latest_version: 1, published_kind: 'child', has_draft: false},
];
const definitions = Object.fromEntries(summaries.map(item => [item.id, {
  schema_version: '1.0', id: item.id, name: item.name, kind: item.published_kind || 'main', version: 0,
  description: '', trigger: {type: 'manual'}, input_schema: {type: 'object'}, steps: [], output: null,
}]));
try {
  if (process.env.RUNLOOM_DASHBOARD_LIVE_LOGIN === '1') {
    await page.goto(base);
    await page.locator('#username').fill('admin@runloom.test');
    await page.locator('#password').fill('RunloomLocal!123');
    await page.locator('#kc-login').click();
    await page.getByRole('heading', {name: 'Workflows', exact: true}).waitFor();
    assert.equal(await page.locator('runloom-workflow-graph').count(), 0, 'Real login must land on the dashboard');
  }
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname, method = route.request().method();
    const reply = json => route.fulfill({json});
    if (path === '/api/v1/auth/config' && process.env.RUNLOOM_DASHBOARD_LIVE_LOGIN !== '1') return route.fulfill({status: 503, json: {}});
    if (path === '/api/v1/studio/projects') return reply([{id: 'default', name: 'General'}, {id: 'second', name: 'Messaging'}, {id: 'empty', name: 'Empty project'}]);
    if (path === '/api/v1/studio/definitions') return failList ? route.fulfill({status: 503, json: {error: 'Unavailable'}}) : reply(summaries);
    if (path.endsWith('/edit-lock')) {
      lockReads++;
      if (method === 'DELETE') releases++;
      return reply({locked: method === 'POST', owner: 'tester', owner_name: 'Tester', ttl_seconds: 60, writing: false});
    }
    if (path.endsWith('/validate')) return reply({valid: true, definition: definitions.billing});
    const match = path.match(/^\/api\/v1\/studio\/definitions\/([^/]+)$/);
    if (match) { detailReads++; return failDetails ? route.fulfill({status: 503, json: {error: 'Unavailable'}}) : reply(definitions[match[1]]); }
    if (path.endsWith('/health')) return reply({temporal: true, database: true});
    return process.env.RUNLOOM_DASHBOARD_LIVE_LOGIN === '1' ? route.continue() : reply([]);
  });
  async function enter() {
    await page.goto(base + '/workflows');
    if (process.env.RUNLOOM_DASHBOARD_LIVE_LOGIN !== '1') {
      await page.waitForFunction(() => window.ng?.getComponent(document.querySelector('app-root'))?.auth.ready());
      await page.evaluate(() => {
        const auth = window.ng.getComponent(document.querySelector('app-root')).auth;
        auth.bearer = async () => 'synthetic-token';
        auth.profile.set({user: {id: 'tester', email: 'test@example.test'}, organizations: [{id: 'org', name: 'Test org', role: 'owner', teams: [{id: 'team', name: 'Test team', role: 'admin'}]}]});
        auth.organizationId.set('org'); auth.teamId.set('team'); auth.signedIn.set(true);
      });
    }
    await page.getByRole('button', {name: 'Details for Billing approval', exact: true}).waitFor();
  }
  const dashboard = page.locator('runloom-workflow-dashboard');
  const details = name => page.getByRole('button', {name: `Details for ${name}`, exact: true});
  await enter();
  assert.equal(detailReads, 0, 'Landing must not fetch a definition');
  assert.equal(lockReads, 0, 'Landing must not request edit locks');
  assert.equal(await page.locator('runloom-workflow-graph').count(), 0);
  assert.equal(await dashboard.locator('tbody tr').count(), 2);
  await dashboard.getByText('Billing approval', {exact: true}).click();
  assert.equal(detailReads, 0, 'Only Details opens an existing workflow');
  await page.getByLabel('Search workflows', {exact: true}).fill('onboard');
  await details('Billing approval').waitFor({state: 'detached'});
  assert.equal(await dashboard.locator('tbody tr').count(), 1);
  await page.getByLabel('Filter workflows by status').selectOption('published');
  await page.getByRole('heading', {name: 'No matching workflows'}).waitFor();
  await page.getByRole('button', {name: 'Clear filters'}).click();
  await page.getByLabel('Browse project', {exact: true}).selectOption('second');
  await details('Send confirmation').waitFor();
  assert.equal(detailReads, 0, 'Project changes must not open a workflow');
  await page.getByLabel('Browse project', {exact: true}).selectOption('empty');
  await page.getByRole('heading', {name: 'Your project starts here'}).waitFor();
  await page.getByLabel('Browse project', {exact: true}).selectOption('default');
  await details('Billing approval').waitFor();
  failDetails = true;
  await details('Billing approval').click();
  await page.waitForFunction(() => document.querySelector('runloom-notice'));
  assert.equal(await page.locator('runloom-workflow-graph').count(), 0, 'Failed details stays on dashboard');
  failDetails = false;
  await details('Billing approval').click();
  await page.getByLabel('Workflow name', {exact: true}).waitFor();
  assert.equal(await page.getByLabel('Workflow name', {exact: true}).inputValue(), 'Billing approval');
  assert.equal(await page.getByLabel('Workflow name', {exact: true}).isDisabled(), true);
  await page.getByRole('button', {name: 'Edit workflow', exact: true}).click();
  await page.getByLabel('Workflow name', {exact: true}).fill('Unsaved edit');
  await page.getByRole('button', {name: 'Back to workflows', exact: true}).click();
  await page.getByRole('dialog').getByRole('button', {name: 'Keep editing', exact: true}).click();
  assert.equal(await page.getByLabel('Workflow name', {exact: true}).inputValue(), 'Unsaved edit');
  await page.getByRole('button', {name: 'Back to workflows', exact: true}).click();
  await page.getByRole('dialog').getByRole('button', {name: 'Discard changes', exact: true}).click();
  await details('Billing approval').waitFor();
  assert(releases > 0, 'Returning to dashboard must release editing');
  failList = true;
  await page.getByRole('button', {name: 'Refresh', exact: true}).click();
  await page.getByRole('heading', {name: 'Could not load workflows'}).waitFor();
  failList = false;
  await page.getByRole('button', {name: 'Try again', exact: true}).click();
  await details('Billing approval').waitFor();
  await page.getByRole('button', {name: 'New workflow', exact: true}).click();
  await page.getByLabel('Workflow name', {exact: true}).waitFor();
  assert.equal(await page.getByLabel('Workflow name', {exact: true}).isEnabled(), true);
  await page.getByRole('button', {name: 'Back to workflows', exact: true}).click();
  const readsBeforeRefresh = detailReads;
  await enter();
  assert.equal(detailReads, readsBeforeRefresh, 'Refreshing returns to dashboard without loading a definition');
  for (const theme of ['light', 'dark']) {
    await page.evaluate(theme => document.documentElement.setAttribute('data-theme', theme), theme);
    if (process.env.RUNLOOM_SCREENSHOT_DIR) await page.screenshot({path: `${process.env.RUNLOOM_SCREENSHOT_DIR}/workflows-dashboard-${theme}.png`, animations: 'disabled'});
  }
  await page.setViewportSize({width: 390, height: 844});
  await page.getByRole('button', {name: 'Toggle workspace sidebar', exact: true}).waitFor();
  if (process.env.RUNLOOM_SCREENSHOT_DIR) await page.screenshot({path: `${process.env.RUNLOOM_SCREENSHOT_DIR}/workflows-dashboard-mobile.png`, animations: 'disabled'});
  const mobileLayout = await dashboard.boundingBox();
  assert(mobileLayout.x >= 0 && mobileLayout.x + mobileLayout.width <= 390, 'Dashboard must fit the mobile viewport');
  assert(await details('Billing approval').evaluate(el => el.getBoundingClientRect().right <= innerWidth), 'Details must remain visible on mobile');
  assert.deepEqual(errors, []);
  console.log('PASS workflow dashboard: explicit Details, project/search/status filters, empty/error recovery, edit/discard guards, refresh, theme and mobile layout. Workflow APIs mocked; no persisted changes.');
} finally { await browser.close(); }
