// Read-only browser test for a local Studio team with at least one stored run.
import assert from 'node:assert/strict';
const { chromium } = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({
  headless: true,
  ...(process.env.RUNLOOM_CHROME ? { executablePath: process.env.RUNLOOM_CHROME } : {}),
});
const origin = process.env.RUNLOOM_UI_URL || 'http://localhost:4200';
try {
  const page = await browser.newPage();
  await page.goto(origin + '/management/runs');
  await page.locator('#username').fill(process.env.RUNLOOM_USERNAME);
  await page.locator('#password').fill(process.env.RUNLOOM_PASSWORD);
  await page.locator('#kc-login').click();
  if (process.env.RUNLOOM_ORG_ID) {
    await page.getByRole('button', { name: 'Switch organization or team' }).click();
    await page
      .getByRole('combobox', { name: 'Organization', exact: true })
      .selectOption(process.env.RUNLOOM_ORG_ID);
    const team = page.getByRole('combobox', { name: 'Team', exact: true });
    if (process.env.RUNLOOM_TEAM_ID && (await team.count()))
      await team.selectOption(process.env.RUNLOOM_TEAM_ID);
    await page.keyboard.press('Escape');
  }
  await page.goto(origin + '/management/runs');
  const button = page.locator('runloom-run-history table .link').first();
  const dialog = page.getByRole('dialog', { name: 'Run details' });
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 800 });
    await button.scrollIntoViewIfNeeded();
    await button.focus();
    const before = await button.boundingBox();
    // Hold the first response to verify the overlay also owns the loading state.
    let release;
    const gate = new Promise((resolve) => (release = resolve));
    await page.route(
      '**/history?trace=true',
      async (route) => {
        await gate;
        await route.continue();
      },
      { times: 1 },
    );
    try {
      await page.keyboard.press('Enter');
      await dialog.getByText('Loading run details…').waitFor();
      assert(await dialog.evaluate((el) => el.matches(':modal')));
    } finally {
      release();
    }
    await dialog.getByRole('heading', { name: 'Execution timeline' }).waitFor();
    const box = await dialog.boundingBox();
    assert(box.x >= 0 && box.y >= 0 && box.x + box.width <= width && box.y + box.height <= 801);
    const close = dialog.getByRole('button', { name: 'Close details' });
    const header = await close.boundingBox();
    await dialog.locator('.dialog-content').evaluate((el) => (el.scrollTop = el.scrollHeight));
    assert.equal((await close.boundingBox()).y, header.y);
    if (width === 390) await close.click();
    else await page.keyboard.press('Escape');
    await dialog.waitFor({ state: 'hidden' });
    assert(await button.evaluate((el) => el === document.activeElement));
    assert.deepEqual(await button.boundingBox(), before, 'Run list position must be preserved');
  }
  console.log(
    'PASS run details overlay, loading state, fixed close control, focus and scroll restoration on desktop and mobile',
  );
} finally {
  await browser.close();
}
