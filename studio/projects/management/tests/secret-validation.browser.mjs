// Local deployed Studio only. Creates/deletes one synthetic secret; tokens stay in memory.
import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
const base = process.env.RUNLOOM_UI_URL || 'http://localhost:4200';
assert.ok(['localhost', '127.0.0.1'].includes(new URL(base).hostname));
const { chromium } = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({headless: true, ...(process.env.RUNLOOM_CHROME ? {executablePath: process.env.RUNLOOM_CHROME} : {})});
const secret = 'validation-' + randomUUID();
let cleanup;
try {
  const page = await browser.newPage();
  page.setDefaultTimeout(15000);
  await page.goto(new URL('/management/secrets', base).href);
  await page.locator('#username').fill('admin@runloom.test');
  await page.locator('#password').fill('RunloomLocal!123');
  const profile = page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/studio/me' && r.ok());
  await page.locator('#kc-login').click();
  const response = await profile, me = await response.json();
  const token = (await response.request().allHeaders()).authorization;
  const org = me.organizations.find(o => o.name === 'Runloom'), team = org.teams.find(t => t.name === 'General');
  const endpoint = `/api/v1/studio/orgs/${org.id}/teams/${team.id}/secrets/${secret}`;
  cleanup = () => fetch(base + endpoint, {method: 'DELETE', headers: {Authorization: token}});
  const vault = page.locator('runloom-workflow-secrets');
  const name = vault.getByLabel('Secret name', {exact: true});
  const value = vault.getByLabel('Secret value', {exact: true});
  const save = vault.getByRole('button', {name: 'Save / replace secret', exact: true});
  let saves = 0;
  page.on('request', request => { if (request.method() === 'PUT' && request.url().includes('/secrets/')) saves++; });
  for (const theme of ['light', 'dark']) {
    await page.evaluate(theme => document.documentElement.setAttribute('data-theme', theme), theme);
    for (const width of [1440, 390]) {
      await page.setViewportSize({width, height: 1000});
      for (const invalid of ['api key', ' api-key', 'api-key ']) {
        await name.fill(invalid);
        await value.fill(' keep exact spaces ');
        await vault.locator('#secret-name-error').waitFor();
        assert.match(await vault.locator('#secret-name-error').innerText(), /Spaces are not allowed/);
        assert.equal(await name.getAttribute('aria-invalid'), 'true');
        assert.ok((await name.getAttribute('aria-describedby')).includes('secret-name-error'));
        assert.ok(await save.isDisabled());
        assert.ok(await vault.evaluate(el => el.scrollWidth <= el.clientWidth), 'No horizontal overflow');
        const colors = await name.evaluate(el => ({border: getComputedStyle(el).borderTopColor, error: getComputedStyle(document.getElementById('secret-name-error')).color}));
        assert.equal(colors.border, colors.error, 'Invalid field and error text use the same error color');
      }
      await name.fill(secret);
      await vault.locator('#secret-name-error').waitFor({state: 'detached'});
      assert.equal(await name.getAttribute('aria-invalid'), 'false');
      assert.ok(await save.isEnabled());
    }
  }
  assert.equal(saves, 0, 'Invalid names never reach the API');
  const saved = page.waitForResponse(r => r.request().method() === 'PUT' && new URL(r.url()).pathname === endpoint);
  await save.click();
  const result = await saved;
  assert.equal(result.status(), 200);
  assert.equal(result.request().postDataJSON().value, ' keep exact spaces ', 'Values are never trimmed');
  await vault.getByRole('button', {name: 'Delete secret ' + secret, exact: true}).waitFor();
  assert.equal(await vault.locator('.field-error').count(), 0, 'Successful save resets validation state');
  assert.equal(await value.inputValue(), '');
  await vault.getByRole('button', {name: 'Delete secret ' + secret, exact: true}).click();
  const deleted = page.waitForResponse(r => r.request().method() === 'DELETE' && new URL(r.url()).pathname === endpoint);
  await page.getByRole('dialog').getByTestId('dialog-confirm').click();
  assert.equal((await deleted).status(), 200);
  cleanup = undefined;
  console.log('PASS: inline whitespace errors, accessible red fields, recovery, light/dark desktop/mobile, real secret save/delete and exact value preservation.');
} finally {
  if (cleanup) await cleanup().catch(() => {});
  await browser.close();
}
