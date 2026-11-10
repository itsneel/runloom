// Real Go vault/routes and isolated file storage; synthetic authentication only.
// Start TestPromptBrowserAPI with RUNLOOM_PROMPT_BROWSER=1 RUNLOOM_SECRETS_BROWSER=1 RUNLOOM_DASHBOARD_BROWSER=1.
import assert from 'node:assert/strict';
const backend = process.env.RUNLOOM_PROMPT_API || 'http://127.0.0.1:17290';
const fixture = await (await fetch(backend + '/fixture')).json();
const { chromium } = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({ headless: true, ...(process.env.RUNLOOM_CHROME ? { executablePath: process.env.RUNLOOM_CHROME } : {}) });
const page = await browser.newPage({ viewport: { width: 1600, height: 1050 } });
page.setDefaultTimeout(15000);
const errors = [];
page.on('pageerror', error => errors.push(error.message));
let failSave = true;
let token = 'admin';
const endpoint = `/api/v1/studio/orgs/${fixture.org}/teams/${fixture.team}/secrets`;
await page.route('**/api/**', async route => {
  const request = route.request(), url = new URL(request.url());
  if (url.pathname.endsWith('/auth/config')) return route.fulfill({ status: 503, json: {} });
  if (url.pathname.endsWith('/health')) return route.fulfill({ json: { temporal: true, database: true } });
  if (request.method() === 'PUT' && url.pathname.includes('/secrets/')) {
    assert.deepEqual(Object.keys(request.postDataJSON()), ['value']);
    if (failSave) {
      failSave = false;
      // Deliberately echo a synthetic credential to prove the UI does not display error bodies.
      return route.fulfill({ status: 503, json: { error: 'synthetic-do-not-echo' } });
    }
  }
  const response = await route.fetch({ url: backend + url.pathname + url.search, headers: { ...request.headers(), authorization: 'Bearer ' + token } });
  return route.fulfill({ response });
});
const metadata = async (path = endpoint, credential = token) => {
  const response = await fetch(backend + path, { headers: { authorization: 'Bearer ' + credential } });
  assert.equal(response.status, 200);
  return response.json();
};
try {
  await page.goto(process.env.RUNLOOM_UI_URL || 'http://localhost:4420');
  await page.waitForFunction(() => window.ng?.getComponent(document.querySelector('app-root'))?.auth.ready());
  await page.evaluate(fixture => {
    const shell = window.ng.getComponent(document.querySelector('app-root')), auth = shell.auth;
    shell.error = ''; auth.bearer = async () => 'synthetic';
    auth.profile.set({ user: { id: 'admin', email: 'admin@example.test' }, organizations: [{ id: fixture.org, name: 'Test organization', role: 'owner', teams: [{ id: fixture.team, name: 'Team', role: 'admin' }, { id: fixture.other_team, name: 'Other team', role: 'admin' }] }] });
    auth.organizationId.set(fixture.org); auth.teamId.set(fixture.team); auth.signedIn.set(true);
  }, fixture);
  await page.getByRole('button', { name: 'Management', exact: true }).click();
  await page.getByRole('button', { name: 'Workflow secrets', exact: true }).click();
  const vault = page.locator('runloom-workflow-secrets');
  const name = vault.getByLabel('Secret name', { exact: true });
  const value = vault.getByLabel('Secret value', { exact: true });
  const save = vault.getByRole('button', { name: 'Save / replace secret', exact: true });
  assert.equal(await vault.getByLabel('Secret origin', { exact: true }).count(), 0);
  await name.fill('INVALID NAME'); await value.fill('synthetic-do-not-echo');
  assert.equal(await save.isDisabled(), true);
  await name.fill('billing-api'); await save.click();
  await vault.getByText(/Secret storage is not configured/).waitFor();
  assert.ok(!(await vault.innerText()).includes('synthetic-do-not-echo'));
  assert.equal(await value.inputValue(), '');
  assert.equal((await metadata()).items.length, 0);
  await value.fill('synthetic-first-value'); await save.click();
  await vault.getByText('Secret saved. Its value cannot be retrieved.', { exact: true }).waitFor();
  let listed = await metadata();
  assert.equal(listed.configured, true);
  assert.deepEqual(Object.keys(listed.items[0]).sort(), ['name', 'updated_at']);
  assert.equal(listed.items[0].name, 'billing-api');
  assert.ok(!JSON.stringify(listed).includes('synthetic-first-value'));
  assert.equal(await value.inputValue(), '');
  assert.ok(!(await page.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage }))).includes('synthetic-first-value'));
  const other = endpoint.replace(`/teams/${fixture.team}/`, `/teams/${fixture.other_team}/`);
  assert.equal((await metadata(other)).items.length, 0);
  await name.fill('billing-api'); await value.fill('synthetic-replacement'); await save.click();
  await page.getByRole('dialog').getByTestId('dialog-confirm').click();
  await page.waitForFunction(() => document.querySelector('[aria-label="Secret name"]').value === '');
  assert.equal((await metadata()).items.length, 1);
  const viewerWrite = await fetch(backend + endpoint + '/denied', { method: 'PUT', headers: { authorization: 'Bearer viewer', 'Content-Type': 'application/json' }, body: JSON.stringify({ value: 'synthetic' }) });
  assert.equal(viewerWrite.status, 403);
  assert.equal((await metadata(endpoint, 'viewer')).items.length, 1);
  await vault.getByRole('button', { name: 'Delete secret billing-api', exact: true }).click();
  await page.getByRole('dialog').getByTestId('dialog-confirm').click();
  await vault.getByText('No workflow secrets in this team.', { exact: true }).waitFor();
  assert.equal((await metadata()).items.length, 0);
  assert.deepEqual(errors, []);
  console.log('PASS real Go vault browser save/replace/delete, value-only contract, failure recovery, redaction and team permissions');
} finally {
  await browser.close();
  await fetch(backend + '/done', { method: 'POST' });
}
