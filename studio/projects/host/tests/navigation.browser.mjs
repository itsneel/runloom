// Run against independently served development applications. Synthetic APIs only;
// no real identity, workflow, secret, schedule or database is changed.
import assert from 'node:assert/strict';
const { chromium } = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({ headless: true, ...(process.env.RUNLOOM_CHROME ? { executablePath: process.env.RUNLOOM_CHROME } : {}) });
const origin = process.env.RUNLOOM_UI_URL || 'http://localhost:4200';
try {
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  page.setDefaultTimeout(15000);
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/v1/auth/config') return route.fulfill({ status: 503, json: {} });
    if (path === '/api/v1/health') return route.fulfill({ json: { temporal: true, database: true } });
    if (path.endsWith('/secrets')) return route.fulfill({ json: { configured: true, items: [] } });
    if (path.includes('llm-analytics') || path.includes('budget')) return route.fulfill({ status: 503, json: { error: 'Synthetic report unavailable' } });
    if (path.endsWith('/edit-lock')) return route.fulfill({ json: { locked: false, writing: false, ttl_seconds: 60 } });
    if (path.endsWith('/projects')) return route.fulfill({ json: [{ id: 'default', name: 'General' }] });
    return route.fulfill({ json: [] });
  });
  async function signIn() {
    await page.waitForFunction(() => window.ng?.getComponent(document.querySelector('app-root'))?.auth.ready());
    await page.evaluate(() => {
      const shell = window.ng.getComponent(document.querySelector('app-root'));
      shell.error = '';
      shell.auth.bearer = async () => 'synthetic';
      shell.auth.profile.set({ user: { id: 'test', email: 'test@example.test' }, organizations: [{ id: 'org', name: 'Test org', role: 'owner', teams: [{ id: 'team', name: 'General', role: 'admin' }] }] });
      shell.auth.organizationId.set('org'); shell.auth.teamId.set('team'); shell.auth.signedIn.set(true);
    });
  }
  async function assertPath(path) { await page.waitForURL(url => url.pathname === path); }
  async function assertSection(section) {
    await page.waitForFunction(section => {
      const element = document.querySelector('runloom-management');
      return element && window.ng?.getComponent(element)?.managementSection === section;
    }, section);
  }
  await page.goto(origin + '/management/prompts');
  await signIn();
  await assertSection('prompts');
  await assertPath('/management/prompts');
  for (const [label, section, path] of [
    ['Teams & members', 'teams', 'teams'], ['Organization', 'organization', 'organization'],
    ['Service accounts', 'accounts', 'service-accounts'], ['Workflow secrets', 'secrets', 'secrets'],
    ['Cost analytics', 'analytics', 'analytics'], ['LLM budgets', 'budgets', 'budgets'],
    ['Schedules', 'schedules', 'schedules'], ['Prompt library', 'prompts', 'prompts'], ['Run history', 'runs', 'runs'],
  ]) {
    await page.getByRole('button', { name: label, exact: true }).click();
    await assertPath('/management/' + path);
    await assertSection(section);
    await page.reload();
    await signIn();
    await assertSection(section);
    await assertPath('/management/' + path);
  }
  await page.getByRole('button', { name: 'Teams & members', exact: true }).click();
  await assertSection('teams');
  await page.getByRole('button', { name: 'Workflow secrets', exact: true }).click();
  await assertSection('secrets');
  await page.getByLabel('Secret name', { exact: true }).fill('synthetic-name');
  await page.getByLabel('Secret value', { exact: true }).fill('synthetic-value');
  await page.evaluate(() => history.back());
  await page.getByRole('dialog').getByRole('button', { name: 'Keep editing', exact: true }).click();
  await assertPath('/management/secrets');
  assert.equal(await page.getByLabel('Secret value', { exact: true }).inputValue(), 'synthetic-value');
  await page.evaluate(() => history.back());
  await page.getByRole('dialog').getByRole('button', { name: 'Discard changes', exact: true }).click();
  await assertSection('teams');
  await assertPath('/management/teams');
  await page.evaluate(() => history.forward());
  await assertSection('secrets');
  await assertPath('/management/secrets');
  await page.getByRole('button', { name: 'Workflows', exact: true }).click();
  await assertPath('/workflows');
  await page.locator('runloom-studio').waitFor();
  await page.reload(); await signIn();
  await page.locator('runloom-studio').waitFor();
  await assertPath('/workflows');
  const management = page.getByRole('heading', { name: 'Management', exact: true });
  for (const [heading, labels] of [
    ['Workflow operations', ['Schedules', 'Prompt library', 'Run history']],
    ['Costs', ['Cost analytics', 'LLM budgets']],
    ['Organization', ['Teams & members', 'Organization']],
    ['Secrets & access', ['Service accounts', 'Workflow secrets', 'Database connections']],
  ]) {
    const group = page.getByRole('group', { name: heading, exact: true });
    assert.equal(await group.getByRole('heading', { name: heading, exact: true }).isVisible(), true);
    for (const label of labels) assert.equal(await group.getByRole('button', { name: label, exact: true }).count(), 1);
  }
  assert.equal(await management.isVisible(), true);
  assert.equal(await page.getByRole('button', { name: 'Management', exact: true }).count(), 0);
  assert.equal(await page.getByRole('button', { name: 'Schedules', exact: true }).isVisible(), true);
  assert.equal(await page.locator('#management-links [aria-current="page"]').count(), 0);
  await management.click();
  assert.equal(await page.getByRole('button', { name: 'Schedules', exact: true }).isVisible(), true);
  await assertPath('/workflows');
  await page.getByRole('button', { name: 'Schedules', exact: true }).click();
  await assertPath('/management/schedules');
  await assertSection('schedules');
  await management.click();
  await page.reload(); await signIn();
  await assertSection('schedules');
  assert.equal(await management.isVisible(), true);
  assert.equal(await page.getByRole('button', { name: 'Schedules', exact: true }).isVisible(), true);
  assert.deepEqual(errors, []);
  console.log('PASS management URLs, direct links, refresh, workflow base, Back/Forward, unsaved-change cancellation and non-collapsible management navigation');
} finally { await browser.close(); }
