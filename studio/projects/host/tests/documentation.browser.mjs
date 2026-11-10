// Read-only verification of the deployed host and independent docs site.
import assert from 'node:assert/strict';
const { chromium } = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const origin = process.env.RUNLOOM_UI_URL || 'http://localhost:4200';
const docs = process.env.RUNLOOM_DOCS_URL || 'http://localhost:4400';
assert(['localhost', '127.0.0.1'].includes(new URL(origin).hostname));
const browser = await chromium.launch({headless: true, ...(process.env.RUNLOOM_CHROME ? {executablePath: process.env.RUNLOOM_CHROME} : {})});
try {
  const context = await browser.newContext({viewport: {width: 1440, height: 1000}});
  const page = await context.newPage();
  page.setDefaultTimeout(30000);
  await page.goto(origin + '/workflows');
  await page.locator('#username').fill(process.env.RUNLOOM_TEST_EMAIL || 'admin@runloom.test');
  await page.locator('#password').fill(process.env.RUNLOOM_TEST_PASSWORD || 'RunloomLocal!123');
  await page.locator('#kc-login').click();
  const button = page.getByRole('button', {name: 'Documentation', exact: true});
  await button.waitFor();
  const original = page.url();
  for (const collapsed of [false, true]) {
    if (collapsed) await page.getByRole('button', {name: 'Collapse sidebar', exact: true}).click();
    assert.equal(await page.locator('.sidebar-navigation button[aria-label="Documentation"]').count(), 0);
    assert.equal(await page.locator('.sidebar-footer .documentation-icon').count(), 2);
    const position = await button.evaluate(element => {
      const appearance = element.nextElementSibling;
      return {next: appearance?.tagName, docsBottom: element.getBoundingClientRect().bottom, appearanceTop: appearance?.getBoundingClientRect().top};
    });
    assert.equal(position.next, 'RUNLOOM-THEME-PICKER');
    assert.ok(position.docsBottom <= position.appearanceTop);
    for (const [label, path] of [['Documentation', '/'], ['Consumer API documentation', '/api/consumer']]) {
      const opened = context.waitForEvent('page');
      await page.getByRole('button', {name: label, exact: true}).click();
      const documentation = await opened;
      await documentation.waitForURL(url => url.origin === docs && url.pathname === path);
      await documentation.getByRole('heading', {level: 1}).waitFor();
      if (path === '/api/consumer') await documentation.getByRole('heading', {name: 'Consumer API', level: 1, exact: true}).waitFor();
      assert.equal(await documentation.evaluate(() => window.opener), null);
      assert.equal(page.url(), original, 'the Studio page must not navigate');
      await documentation.close();
    }
  }
  console.log('PASS: Documentation and Consumer API docs sit above Appearance in the footer and open protected new tabs in expanded/collapsed layouts; Studio remains unchanged.');
} finally { await browser.close(); }
