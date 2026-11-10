// Deployed, read-only UI regression: actual wheel scrolling and responsive navigation.
import assert from 'node:assert/strict';
const { chromium } = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.RUNLOOM_UI_URL || 'http://localhost:4200';
assert.ok(['localhost', '127.0.0.1'].includes(new URL(base).hostname));
const browser = await chromium.launch({
  headless: true,
  ...(process.env.RUNLOOM_CHROME ? { executablePath: process.env.RUNLOOM_CHROME } : {}),
});
let phase = 'desktop navigation';
try {
  const errors = [];
  async function login(email, password) {
    const context = await browser.newContext({ viewport: { width: 1440, height: 720 } }),
      page = await context.newPage();
    page.setDefaultTimeout(30000);
    page.on('pageerror', (error) => errors.push(error.name));
    await page.goto(base + '/platform/organizations');
    await page.locator('#username').fill(email);
    await page.locator('#password').fill(password);
    await page.locator('#kc-login').click();
    await page.getByRole('heading', { level: 1, name: 'Organizations', exact: true }).waitFor();
    await page.getByRole('button', { name: 'Refresh', exact: true }).waitFor();
    return page;
  }
  const page = await login('platform-admin@runloom.test', 'RunloomPlatformAdmin!123');
  const sidebar = page.getByRole('complementary', { name: 'Platform sidebar' });
  const navigation = page.getByRole('navigation', { name: 'Platform navigation' });
  assert.deepEqual(await navigation.getByRole('link').allTextContents(), [
    'System health',
    'Organizations',
    'Staff & access',
    'Platform audit',
  ]);
  assert.equal(
    await page.locator('runloom-theme-picker').count(),
    1,
    'Mount only the active application theme picker',
  );
  const content = page.getByRole('region', { name: 'Platform page content', exact: true });
  const box = await content.boundingBox(),
    before = await sidebar.boundingBox();
  assert.ok(box.height > 200 && box.height < 720);
  assert.ok(
    await content.evaluate((el) => el.scrollHeight > el.clientHeight),
    'Organization content must extend beyond the viewport for this regression',
  );
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.wheel(0, 1000);
  await page.waitForFunction(() => document.querySelector('.console-scroll').scrollTop > 100);
  assert.deepEqual(
    await sidebar.boundingBox(),
    before,
    'Sidebar must stay fixed while content scrolls',
  );
  await page.locator('.pagination').scrollIntoViewIfNeeded();
  assert.ok(
    await page.locator('.pagination').evaluate((el) => {
      const footer = el.getBoundingClientRect(),
        viewport = el.closest('.console-scroll').getBoundingClientRect();
      return footer.bottom <= viewport.bottom + 1;
    }),
    'Bottom pagination must be reachable',
  );
  await navigation.getByRole('link', { name: 'System health', exact: true }).click();
  await page.waitForURL('**/platform/health');
  await page.getByRole('heading', { level: 1, name: 'System health', exact: true }).waitFor();
  assert.equal(await content.evaluate((el) => el.scrollTop), 0);
  await navigation.getByRole('link', { name: 'Organizations', exact: true }).click();
  await page.waitForURL('**/platform/organizations');
  await page.reload();
  await navigation.getByRole('link', { name: 'Organizations', exact: true }).waitFor();
  assert.equal(
    await navigation
      .getByRole('link', { name: 'Organizations', exact: true })
      .getAttribute('aria-current'),
    'page',
  );
  await sidebar.getByRole('button', { name: /^Appearance:/ }).click();
  await page
    .getByRole('group', { name: 'Color theme' })
    .getByRole('button', { name: /^Dark/ })
    .click();
  assert.equal(await page.locator('html').getAttribute('data-theme'), 'dark');
  await page.screenshot({ path: '/tmp/runloom-platform-sidebar-dark.png', fullPage: true });
  await sidebar.getByRole('button', { name: 'Collapse platform navigation', exact: true }).click();
  await sidebar.getByRole('button', { name: 'Expand platform navigation', exact: true }).waitFor();
  assert.equal(Math.round((await sidebar.boundingBox()).width), 72);
  assert.ok(await navigation.getByRole('link', { name: 'Organizations', exact: true }).isVisible());
  const tooltip = page.getByRole('tooltip');
  for (const label of ['System health', 'Organizations', 'Staff & access', 'Platform audit']) {
    phase = 'collapsed tooltip ' + label;
    await navigation.getByRole('link', { name: label, exact: true }).hover();
    await tooltip.filter({ hasText: label }).waitFor();
  }
  await sidebar.getByRole('button', { name: 'Sign out', exact: true }).hover();
  await tooltip.filter({ hasText: 'Sign out' }).waitFor();
  await sidebar.getByRole('button', { name: /^Appearance:/ }).hover();
  phase = 'appearance tooltip';
  await tooltip.filter({ hasText: /Appearance:/ }).waitFor();
  await navigation.getByRole('link', { name: 'Organizations', exact: true }).focus();
  await tooltip.filter({ hasText: 'Organizations' }).waitFor();
  await page.screenshot({ path: '/tmp/runloom-platform-collapsed-tooltip.png', fullPage: true });
  await page.keyboard.press('Escape');
  await tooltip.waitFor({ state: 'hidden' });
  await page.reload();
  await sidebar.getByRole('button', { name: 'Expand platform navigation', exact: true }).click();
  await navigation.getByRole('link', { name: 'Organizations', exact: true }).hover();
  assert.equal(await tooltip.count(), 0, 'Expanded labels do not need tooltips');
  await sidebar.getByRole('button', { name: /^Appearance:/ }).click();
  await page
    .getByRole('group', { name: 'Color theme' })
    .getByRole('button', { name: /^Light/ })
    .click();
  await page.screenshot({ path: '/tmp/runloom-platform-sidebar-light.png', fullPage: true });
  await page.setViewportSize({ width: 390, height: 780 });
  const open = page.getByRole('button', { name: 'Open platform navigation', exact: true });
  await open.click();
  const close = page.getByRole('button', { name: 'Close platform navigation', exact: true });
  await close.waitFor();
  assert.equal(await page.locator('.platform-main').getAttribute('inert'), '');
  await close.focus();
  await page.keyboard.press('Shift+Tab');
  assert.equal(
    await sidebar
      .getByRole('button', { name: 'Sign out', exact: true })
      .evaluate((el) => el === document.activeElement),
    true,
    'Keep keyboard focus in the mobile sidebar',
  );
  await page.keyboard.press('Escape');
  await open.waitFor();
  phase = 'mobile focus restoration';
  await page.waitForFunction(
    () => document.activeElement?.getAttribute('aria-label') === 'Open platform navigation',
  );
  await open.click();
  await navigation.getByRole('link', { name: 'System health', exact: true }).click();
  await page.waitForURL('**/platform/health');
  await open.waitFor();
  assert.equal(await page.locator('.platform-main').getAttribute('inert'), null);
  await open.click();
  await navigation.getByRole('link', { name: 'Organizations', exact: true }).click();
  await page.waitForURL('**/platform/organizations');
  assert.ok(
    await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
    'No horizontal page overflow on mobile',
  );
  const mobileBox = await content.boundingBox();
  phase = 'mobile wheel scrolling';
  await page.mouse.move(200, mobileBox.y + 200);
  await page.mouse.wheel(0, 1200);
  await page.waitForFunction(() => document.querySelector('.console-scroll').scrollTop > 100);
  await page.locator('.pagination').scrollIntoViewIfNeeded();
  await page.screenshot({ path: '/tmp/runloom-platform-sidebar-mobile.png', fullPage: true });
  const staff = await login('platform-staff@runloom.test', 'RunloomPlatformStaff!123');
  phase = 'restricted staff navigation';
  const staffNav = staff.getByRole('navigation', { name: 'Platform navigation' });
  assert.deepEqual(await staffNav.getByRole('link').allTextContents(), [
    'System health',
    'Organizations',
  ]);
  assert.equal(await staff.getByLabel('Maximum child organizations', { exact: true }).count(), 0);
  assert.deepEqual(errors, []);
  console.log(
    'PASS live platform navigation: wheel scrolling, reachable pagination, fixed sidebar, deep links, collapse persistence, light/dark themes, mobile focus/Escape and staff permissions.',
  );
} catch (error) {
  console.error(
    'Platform navigation check failed during ' +
      phase +
      ': ' +
      error.name +
      ': ' +
      error.message.split('Call log:')[0],
  );
  process.exitCode = 1;
} finally {
  await browser.close();
}
