// Run against the independent development apps; all API responses are synthetic.
import assert from 'node:assert/strict';
const { chromium } = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({ headless: true, ...(process.env.RUNLOOM_CHROME ? { executablePath: process.env.RUNLOOM_CHROME } : {}) });
const page = await browser.newPage();
const errors = [];
page.on('pageerror', error => errors.push(error.message));
let items = ['asg', 'billing-' + 'a'.repeat(56)].map(name => ({ name, updated_at: '2026-10-08T09:00:00Z' }));
await page.route('**/api/**', route => {
  const path = new URL(route.request().url()).pathname;
  if (path.endsWith('/auth/config')) return route.fulfill({ status: 503, json: {} });
  if (path.endsWith('/secrets')) return route.fulfill({ json: { configured: true, items } });
  if (path.includes('/secrets/') && route.request().method() === 'DELETE') {
    items = items.filter(item => item.name !== decodeURIComponent(path.split('/').at(-1)));
    return route.fulfill({ json: {} });
  }
  return route.fulfill({ json: [] });
});
try {
  await page.goto(new URL('/management/secrets', process.env.RUNLOOM_UI_URL || 'http://localhost:44200').href);
  await page.waitForFunction(() => window.ng?.getComponent(document.querySelector('app-root'))?.auth.ready());
  await page.evaluate(() => {
    const { auth } = window.ng.getComponent(document.querySelector('app-root'));
    auth.bearer = async () => 'synthetic';
    auth.profile.set({ user: { id: 'admin', email: 'admin@example.test' }, organizations: [{ id: 'org', name: 'Test org', role: 'owner', teams: [{ id: 'team', name: 'General', role: 'admin' }] }] });
    auth.organizationId.set('org'); auth.teamId.set('team'); auth.signedIn.set(true);
  });
  const vault = page.locator('runloom-workflow-secrets');
  await vault.getByRole('heading', { name: 'Saved secrets 2' }).waitFor();
  for (const theme of ['light', 'dark']) {
    await page.evaluate(theme => document.documentElement.setAttribute('data-theme', theme), theme);
    for (const width of [1440, 768, 390]) {
      await page.setViewportSize({ width, height: 1000 });
      const rows = vault.locator('.secret-row');
      assert.equal(await rows.count(), 2);
      for (const row of await rows.all()) {
        const name = await row.locator('.secret-name').boundingBox();
        const button = await row.getByRole('button').boundingBox();
        assert(name && button);
        assert(name.x + name.width + 10 <= button.x, 'Name and action need a visible gap');
        assert(button.height >= 44, 'Delete must have a touch-sized target');
        assert(button.x + button.width <= width, 'Delete must stay within the viewport');
      }
      assert(await vault.evaluate(el => el.scrollWidth <= el.clientWidth), 'Vault must not overflow');
      if (process.env.RUNLOOM_SCREENSHOT_DIR) {
        await vault.screenshot({ path: `${process.env.RUNLOOM_SCREENSHOT_DIR}/secrets-${theme}-${width}.png` });
      }
    }
  }
  await vault.getByRole('button', { name: 'Delete secret asg', exact: true }).click();
  await page.getByRole('dialog').getByTestId('dialog-cancel').click();
  assert.equal(items.length, 2);
  await vault.getByRole('button', { name: 'Delete secret asg', exact: true }).click();
  await page.getByRole('dialog').getByTestId('dialog-confirm').click();
  await vault.getByRole('heading', { name: 'Saved secrets 1' }).waitFor();
  await vault.locator('.secret-row').getByRole('button').click();
  await page.getByRole('dialog').getByTestId('dialog-confirm').click();
  await vault.getByText('No workflow secrets in this team.', { exact: true }).waitFor();
  await vault.getByRole('heading', { name: 'Saved secrets 0' }).waitFor();
  assert.deepEqual(errors, []);
  console.log('PASS secret rows, long names, light/dark, desktop/tablet/mobile, delete confirmation and empty state');
} finally {
  await browser.close();
}
