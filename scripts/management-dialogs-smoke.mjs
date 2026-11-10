// Development-server regression; synthetic state only, no real credentials or API writes.
import assert from 'node:assert/strict';
const { chromium } = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({ headless: true, ...(process.env.RUNLOOM_CHROME ? { executablePath: process.env.RUNLOOM_CHROME } : {}) });
const page = await browser.newPage();
page.setDefaultTimeout(10000);
const errors = [], nativeDialogs = [];
page.on('pageerror', error => errors.push(error.message));
page.on('dialog', async dialog => { nativeDialogs.push(dialog.type()); await dialog.dismiss(); });
await page.route('**/api/**', route => {
  const path = new URL(route.request().url()).pathname;
  if (path === '/api/v1/auth/config') return route.fulfill({ status: 503, json: {} });
  if (path === '/api/v1/studio/projects') return route.fulfill({ json: [{ id: 'default', name: 'General' }] });
  if (path.endsWith('/secrets')) return route.fulfill({ json: { configured: true, items: [] } });
  return route.fulfill({ json: [] });
});
const modal = page.locator('runloom-dialogs').getByRole('dialog');
const secret = page.getByRole('textbox', { name: 'Service account client secret', exact: true });
async function decision(accept) {
  await modal.getByTestId(accept ? 'dialog-confirm' : 'dialog-cancel').click();
  await modal.waitFor({ state: 'hidden' });
}
async function unloadBlocked() {
  return page.evaluate(() => {
    const event = new Event('beforeunload', { cancelable: true });
    window.dispatchEvent(event);
    return event.defaultPrevented;
  });
}
async function revealSecret() {
  await page.locator('runloom-service-accounts').waitFor();
  await page.evaluate(() => {
    const component = window.ng.getComponent(document.querySelector('runloom-service-accounts'));
    component.credentials = { client_id: 'synthetic-client', client_secret: 'synthetic-one-time-value' };
    component.cd.markForCheck();
  });
  await secret.waitFor();
}
try {
  await page.goto(process.env.RUNLOOM_UI_URL || 'http://localhost:4200');
  await page.waitForFunction(() => window.ng?.getComponent(document.querySelector('app-root'))?.auth.ready());
  await page.evaluate(() => {
    const auth = window.ng.getComponent(document.querySelector('app-root')).auth;
    auth.bearer = async () => 'synthetic-token';
    auth.profile.set({ user: { id: 'test', email: 'test@example.test' }, organizations: [
      { id: 'org', name: 'Org', role: 'owner', teams: [{ id: 'team', name: 'Team', role: 'admin' }, { id: 'second', name: 'Second', role: 'admin' }] },
      { id: 'other', name: 'Other', role: 'owner', teams: [{ id: 'third', name: 'Third', role: 'admin' }] },
    ] });
    auth.organizationId.set('org'); auth.teamId.set('team'); auth.signedIn.set(true);
    auth.logout = async () => {
      if (window.testEditDuringLogout) {
        const secrets = window.ng.getComponent(document.querySelector('runloom-workflow-secrets'));
        secrets.name = 'newer-edit-during-sign-out';
        secrets.cd.markForCheck();
      }
      const event = new Event('beforeunload', { cancelable: true });
      window.dispatchEvent(event);
      window.testLogoutBlocked = event.defaultPrevented;
      throw new Error('Simulated logout failure');
    };
  });
  await page.getByRole('button', { name: 'Management', exact: true }).click();
  await page.getByRole('button', { name: 'Workflow secrets', exact: true }).click();
  const secretName = page.getByLabel('Secret name', { exact: true });
  await secretName.fill('unsaved-credential');
  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await decision(true);
  await page.getByText('Simulated logout failure', { exact: true }).waitFor();
  assert.equal(await page.evaluate(() => window.testLogoutBlocked), false, 'Confirmed sign-out must not trigger a second native warning');
  assert.equal(await secretName.inputValue(), 'unsaved-credential');
  assert.equal(await unloadBlocked(), true, 'Failed sign-out must re-arm the browser warning');
  await page.evaluate(() => { window.testEditDuringLogout = true; });
  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await decision(true);
  await page.getByText('Simulated logout failure', { exact: true }).waitFor();
  assert.equal(await page.evaluate(() => window.testLogoutBlocked), true, 'Edits made after confirmation must still be protected');
  assert.equal(await secretName.inputValue(), 'newer-edit-during-sign-out');
  await page.evaluate(() => { window.testEditDuringLogout = false; });
  await secretName.fill('');
  await page.getByRole('button', { name: 'Service accounts', exact: true }).click();
  await revealSecret();
  assert.equal(await unloadBlocked(), true);
  await page.getByRole('button', { name: 'Switch organization or team', exact: true }).click();
  for (const action of [
    () => page.getByRole('button', { name: 'Workflow secrets', exact: true }).click(),
    () => page.getByRole('button', { name: 'Teams & members', exact: true }).click(),
    () => page.getByRole('button', { name: 'Workflows', exact: true }).click(),
    () => page.getByRole('combobox', { name: 'Team', exact: true }).selectOption('second'),
    () => page.getByRole('combobox', { name: 'Organization', exact: true }).selectOption('other'),
    () => page.getByRole('button', { name: 'Sign out', exact: true }).click(),
  ]) {
    await action();
    await decision(false);
    assert.equal(await secret.inputValue(), 'synthetic-one-time-value');
    assert.equal(await page.getByRole('combobox', { name: 'Team', exact: true }).inputValue(), 'team');
    assert.equal(await page.getByRole('combobox', { name: 'Organization', exact: true }).inputValue(), 'org');
  }
  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await decision(true);
  await page.getByText('Simulated logout failure', { exact: true }).waitFor();
  assert.equal(await page.evaluate(() => window.testLogoutBlocked), false);
  assert.equal(await unloadBlocked(), true);
  assert.equal(await secret.inputValue(), 'synthetic-one-time-value');
  await page.getByRole('combobox', { name: 'Team', exact: true }).selectOption('second');
  await decision(true);
  await secret.waitFor({ state: 'hidden' });
  assert.equal(await unloadBlocked(), false);
  await page.getByRole('button', { name: 'Workflow secrets', exact: true }).click();
  await secretName.fill('another-unsaved-secret');
  await page.getByRole('button', { name: 'Service accounts', exact: true }).click();
  await decision(false);
  assert.equal(await secretName.inputValue(), 'another-unsaved-secret');
  await page.getByRole('button', { name: 'Service accounts', exact: true }).click();
  await decision(true);
  assert.equal(await unloadBlocked(), false, 'Confirmed section change destroys only the discarded draft');
  await revealSecret();
  await page.evaluate(() => {
    window.ng.getComponent(document.querySelector('app-root')).auth.logout = async () => { location.assign('/signed-out'); };
  });
  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await modal.getByRole('heading', { name: 'Leave one-time credentials?' }).waitFor();
  await modal.getByTestId('dialog-confirm').click();
  await page.waitForURL('**/signed-out');
  assert.deepEqual(nativeDialogs, []);
  assert.deepEqual(errors, []);
  console.log('PASS management navigation protects one-time credentials; confirmed logout has no native popup; failed logout re-arms warnings');
} finally { await browser.close(); }
