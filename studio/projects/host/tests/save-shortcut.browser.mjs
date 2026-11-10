import assert from 'node:assert/strict';

const {chromium} = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.RUNLOOM_UI_URL || 'http://127.0.0.1:4300';
assert(['localhost', '127.0.0.1'].includes(new URL(base).hostname));
const browser = await chromium.launch({
  headless: true,
  ...(process.env.RUNLOOM_CHROME ? {executablePath: process.env.RUNLOOM_CHROME} : {}),
});
try {
  const page = await browser.newPage();
  const downloads = [], mutations = [];
  page.on('download', item => downloads.push(item.suggestedFilename()));
  await page.route('**/api/**', route => {
    if (route.request().method() !== 'GET') mutations.push(route.request().method());
    return route.fulfill({status: 503, json: {}});
  });
  await page.goto(base);
  await page.getByRole('heading', {name: 'Your workspace awaits.'}).waitFor();
  await page.evaluate(() => {
    window.saveEvents = [];
    // A focused control that stops propagation must not bypass global suppression.
    const input = document.createElement('textarea');
    input.setAttribute('aria-label', 'Shortcut fixture');
    input.addEventListener('keydown', event => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 's') {
        window.saveEvents.push(event.defaultPrevented);
        event.stopPropagation();
      }
    });
    document.body.append(input);
  });
  await page.getByLabel('Shortcut fixture').focus();
  await page.keyboard.press('Meta+s');
  await page.keyboard.press('Control+s');
  assert.deepEqual(await page.evaluate(() => window.saveEvents), [true, true]);
  assert.deepEqual(downloads, []);
  assert.deepEqual(mutations, []);
  console.log('PASS: signed-out Studio disables Cmd/Ctrl+S even inside a control that stops bubbling.');
} finally {
  await browser.close();
}
