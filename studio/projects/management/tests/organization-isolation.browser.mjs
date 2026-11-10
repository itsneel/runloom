// Read-only regression: the local account belongs to exactly one organization family.
import assert from 'node:assert/strict';
const { chromium } = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.RUNLOOM_UI_URL || 'http://localhost:4200';
assert.ok(['localhost', '127.0.0.1'].includes(new URL(base).hostname));
const browser = await chromium.launch({
  headless: true,
  ...(process.env.RUNLOOM_CHROME ? { executablePath: process.env.RUNLOOM_CHROME } : {}),
});
try {
  const page = await browser.newPage({ viewport: { width: 1440, height: 1050 } });
  const errors = [];
  page.on('pageerror', (error) => errors.push(error.name));
  await page.goto(base + '/management/organization');
  await page.locator('#username').fill('admin@runloom.test');
  await page.locator('#password').fill('RunloomLocal!123');
  const ready = page.waitForResponse(
    (r) => new URL(r.url()).pathname === '/api/v1/studio/me' && r.ok(),
  );
  await page.locator('#kc-login').click();
  const response = await ready,
    profile = await response.json();
  const headers = { authorization: (await response.request().allHeaders()).authorization };
  const runloom = profile.organizations.find((o) => o.name === 'Runloom');
  assert.ok(runloom);
  assert.ok(
    profile.organizations.every((o) => (o.parent_id || o.id) === runloom.id),
    'Local admin must belong only to the Runloom family',
  );
  for (const selected of profile.organizations) {
    await page.evaluate((id) => localStorage.setItem('runloom-org', id), selected.id);
    await page.reload();
    const component = page.locator('runloom-organizations');
    await component.locator('code').filter({ hasText: selected.id }).waitFor();
    const familyID = selected.parent_id || selected.id;
    const expected = profile.organizations
      .filter((o) => o.id === familyID || o.parent_id === familyID)
      .map((o) => o.id)
      .sort();
    await page.getByRole('button', { name: 'Switch organization or team', exact: true }).click();
    const selector = page.getByRole('combobox', { name: 'Organization', exact: true });
    await selector.locator(`option[value="${selected.id}"]`).waitFor({ state: 'attached' });
    const actual = await selector.locator('option').evaluateAll((options) =>
      options
        .map((o) => o.value)
        .filter(Boolean)
        .sort(),
    );
    assert.deepEqual(actual, expected, 'Workspace switcher must contain only the selected family');
    const parents = component.getByLabel('Parent organization', { exact: true });
    await parents.waitFor();
    await parents.locator(`option[value="${familyID}"]`).waitFor({ state: 'attached' });
    assert.deepEqual(
      await parents
        .locator('option')
        .evaluateAll((options) => options.map((o) => o.value).filter(Boolean)),
      [familyID],
    );
    const options = await page.request.get(
      base + '/api/v1/studio/orgs/creation?organization_id=' + selected.id,
      { headers },
    );
    assert.equal(options.status(), 200);
    assert.deepEqual(
      (await options.json()).parents.map((o) => o.id),
      [familyID],
    );
    if (selected.id === runloom.id) {
      assert.equal(await component.locator('.org-row strong').count(), expected.length);
      await page.getByRole('button', { name: 'Switch organization or team', exact: true }).click();
      await page.screenshot({ path: '/tmp/runloom-organization-isolation.png', fullPage: true });
    }
  }
  const unscoped = await page.request.get(base + '/api/v1/studio/orgs/creation', { headers });
  assert.deepEqual((await unscoped.json()).parents, []);
  assert.deepEqual(errors, []);
  console.log(
    'PASS local admin has one family; organization switcher and creation options stay within it. No records created.',
  );
} catch (error) {
  console.error(
    'Organization isolation browser check failed: ' +
      error.name +
      ': ' +
      error.message.split('Call log:')[0],
  );
  process.exitCode = 1;
} finally {
  await browser.close();
}
