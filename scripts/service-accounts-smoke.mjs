// UI regression with synthetic credentials only; no real identity/database writes.
import assert from 'node:assert/strict';
const { chromium } = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({ headless: true, ...(process.env.RUNLOOM_CHROME ? { executablePath: process.env.RUNLOOM_CHROME } : {}) });
const page = await browser.newPage();
const errors = [];
page.on('pageerror', error => errors.push(error.message));
const accounts = [];
const firstSecret = 'synthetic-create-secret-not-a-real-credential';
const rotatedSecret = 'synthetic-rotation-secret-not-a-real-credential';
let createBody, updateBody;
await page.route('**/api/**', async route => {
  const path = new URL(route.request().url()).pathname;
  const method = route.request().method();
  if (path === '/api/v1/auth/config') return route.fulfill({ status: 503, json: {} });
  if (path === '/api/v1/health') return route.fulfill({ json: { temporal: true, database: true } });
  if (path === '/api/v1/studio/projects') return route.fulfill({ json: [{ id: 'default', name: 'General' }] });
  if (path === '/api/v1/studio/definitions') return route.fulfill({ json: [{ id: 'main', name: 'Published main', latest_version: 3, published_kind: 'main' }, { id: 'child', name: 'Child', latest_version: 1, published_kind: 'child' }] });
  if (path.startsWith('/api/v1/studio/definitions/')) return route.fulfill({ json: { schema_version: '1.0', id: 'main', name: 'Published main', kind: 'main', version: 3, trigger: { type: 'manual' }, steps: [], input_schema: { type: 'object' } } });
  if (path.endsWith('/service-accounts')) {
    if (method === 'GET') return route.fulfill({ json: accounts });
    createBody = route.request().postDataJSON();
    const account = { ...createBody, client_id: 'synthetic-client-id', status: 'active' };
    accounts.push(account);
    return route.fulfill({ status: 201, json: { account, client_id: account.client_id, client_secret: firstSecret } });
  }
  if (path.endsWith('/service-accounts/synthetic-client-id') && method === 'PUT') {
    updateBody = route.request().postDataJSON();
    Object.assign(accounts[0], updateBody);
    return route.fulfill({ json: { updated: true } });
  }
  if (path.endsWith('/rotate')) return route.fulfill({ json: { client_id: accounts[0].client_id, client_secret: rotatedSecret } });
  if (path.endsWith('/revoke')) { accounts[0].status = 'revoked'; return route.fulfill({ json: { revoked: true, provider_disabled: true } }); }
  return route.fulfill({ json: [] });
});
async function signIn() {
  await page.waitForFunction(() => window.ng?.getComponent(document.querySelector('app-root'))?.auth.ready());
  await page.evaluate(() => {
    const shell = window.ng.getComponent(document.querySelector('app-root'));
    shell.auth.bearer = async () => 'synthetic-user-token';
    shell.auth.profile.set({ user: { id: 'admin', email: 'admin@example.test' }, organizations: [{ id: 'org', name: 'Org', role: 'owner', teams: [{ id: 'team', name: 'Team', role: 'admin' }] }] });
    shell.auth.organizationId.set('org'); shell.auth.teamId.set('team'); shell.auth.signedIn.set(true);
  });
  await page.getByRole('heading', { name: 'Service accounts', exact: true }).waitFor();
}
try {
  await page.goto(new URL('/management/service-accounts', process.env.RUNLOOM_UI_URL || 'http://localhost:4200').href);
  await signIn();
  await page.getByRole('textbox', { name: 'Service account name', exact: true }).fill('Billing');
  const cancelScope = page.getByRole('checkbox', { name: 'runs:cancel', exact: true });
  assert.equal(await cancelScope.isEnabled(), true);
  await cancelScope.check();
  await cancelScope.uncheck();
  await page.getByRole('button', { name: 'Add workflow grant', exact: true }).click();
  const picker = page.getByLabel('Published main workflow', { exact: true });
  assert.equal(await picker.locator('option[value="child"]').count(), 0);
  await picker.selectOption('main');
  await page.getByRole('button', { name: 'Create credentials', exact: true }).click();
  const secret = page.getByRole('textbox', { name: 'Service account client secret', exact: true });
  await secret.waitFor();
  assert.equal(await secret.inputValue(), firstSecret);
  assert.deepEqual(createBody.grants, [{ definition_id: 'main', versions: [3], input_fields: [], output_fields: [] }]);
  assert(!createBody.scopes.includes('runs:cancel'));
  assert.equal(await page.getByRole('button', { name: 'Rotate secret', exact: true }).isDisabled(), true);
  const persisted = await page.evaluate(() => JSON.stringify({ local: { ...localStorage }, session: { ...sessionStorage } }));
  assert(!persisted.includes(firstSecret));
  await page.getByRole('button', { name: 'I saved the secret — hide it', exact: true }).click();
  await secret.waitFor({ state: 'detached' });
  assert.equal(await secret.count(), 0);
  await page.reload(); await signIn();
  assert.equal(await secret.count(), 0, 'Reload must not recover a secret');
  await page.getByRole('button', { name: 'Edit permissions', exact: true }).click();
  await page.getByText('Scopes cannot be changed after creation.', { exact: false }).waitFor();
  for (const scope of ['workflows:read', 'runs:start', 'runs:read', 'runs:cancel']) {
    const checkbox = page.getByRole('checkbox', { name: scope, exact: true });
    assert.equal(await checkbox.isDisabled(), true);
    assert.equal(await checkbox.isChecked(), createBody.scopes.includes(scope));
  }
  await page.getByRole('textbox', { name: 'Service account name', exact: true }).fill('Billing updated');
  await page.getByRole('button', { name: 'Save permissions', exact: true }).click();
  await page.getByRole('heading', { name: 'Billing updated', exact: true }).waitFor();
  assert.deepEqual(updateBody.scopes, createBody.scopes);
  assert.equal(await cancelScope.isEnabled(), true, 'Creation form should allow selecting scopes again');
  await page.getByRole('button', { name: 'Rotate secret', exact: true }).click();
  await page.getByRole('dialog').getByTestId('dialog-cancel').click();
  await page.getByRole('dialog').waitFor({ state: 'hidden' });
  assert.equal(await secret.count(), 0, 'Canceled rotation must not reveal a new secret');
  await page.getByRole('button', { name: 'Rotate secret', exact: true }).click();
  await page.getByRole('dialog').getByTestId('dialog-confirm').click();
  await secret.waitFor(); assert.equal(await secret.inputValue(), rotatedSecret);
  await page.getByRole('button', { name: 'I saved the secret — hide it', exact: true }).click();
  await page.getByRole('button', { name: 'Revoke account', exact: true }).click();
  await page.getByRole('dialog').getByTestId('dialog-confirm').click();
  await page.getByText(/Status: revoked/).waitFor();
  assert.equal(await page.getByRole('button', { name: 'Rotate secret', exact: true }).isDisabled(), true);
  assert.deepEqual(errors, []);
  console.log('PASS service-account grants, secret reveal-once, no browser persistence, rotation and revocation');
} catch (error) { console.error('Browser runtime errors:', errors); throw error; }
finally { await browser.close(); }
