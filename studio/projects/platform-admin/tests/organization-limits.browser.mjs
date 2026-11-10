// Local deployed integration. Reuses Runloom and restores its original policy.
import assert from 'node:assert/strict';
const { chromium } = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.RUNLOOM_UI_URL || 'http://localhost:4200';
assert.ok(['localhost', '127.0.0.1'].includes(new URL(base).hostname));
const browser = await chromium.launch({
  headless: true,
  ...(process.env.RUNLOOM_CHROME ? { executablePath: process.env.RUNLOOM_CHROME } : {}),
});
let restore;
try {
  const errors = [];
  async function login(email, password, path) {
    const context = await browser.newContext({ viewport: { width: 1600, height: 1100 } }),
      page = await context.newPage();
    page.on('pageerror', (e) => errors.push(e.name));
    await page.goto(base + path);
    await page.locator('#username').fill(email);
    await page.locator('#password').fill(password);
    const ready = page.waitForResponse(
      (r) => new URL(r.url()).pathname === '/api/v1/studio/me' && r.ok(),
    );
    await page.locator('#kc-login').click();
    const response = await ready;
    return {
      page,
      headers: { authorization: (await response.request().allHeaders()).authorization },
    };
  }
  const admin = await login(
    'platform-admin@runloom.test',
    'RunloomPlatformAdmin!123',
    '/platform/organizations',
  );
  await admin.page.getByLabel('Maximum child organizations', { exact: true }).waitFor();
  const list = await admin.page.request.get(base + '/api/v1/platform/organizations?limit=100', {
    headers: admin.headers,
  });
  assert.equal(list.status(), 200);
  const { items } = await list.json();
  const target = items.find((o) => !o.parent_id && o.name === 'Runloom');
  assert.ok(
    target,
    'The local Runloom organization is required; no organizations are created here',
  );
  const policyPath = base + '/api/v1/platform/organizations/' + target.id + '/child-limit';
  restore = async () => {
    const response =
      target.child_limit.override === null
        ? await admin.page.request.delete(policyPath, { headers: admin.headers })
        : await admin.page.request.put(policyPath, {
            headers: admin.headers,
            data: { max_children: target.child_limit.override },
          });
    assert.equal(response.status(), 200, 'Restore the original Runloom policy');
  };
  const row = admin.page
    .locator('.organizations-table tbody tr')
    .filter({ has: admin.page.locator('td.identifier', { hasText: target.id }) });
  async function save(mode, limit) {
    await row
      .getByRole('button', { name: 'Edit child limit for ' + target.name, exact: true })
      .click();
    await row.getByLabel('Child limit source', { exact: true }).selectOption(mode);
    if (mode === 'custom')
      await row.getByLabel('Organization child limit', { exact: true }).fill(String(limit));
    const saved = admin.page.waitForResponse(
      (r) =>
        r.url() === policyPath && r.request().method() === (mode === 'custom' ? 'PUT' : 'DELETE'),
    );
    await row.getByRole('button', { name: 'Save child limit', exact: true }).click();
    const result = await saved;
    assert.equal(result.status(), 200);
    const policy = await result.json();
    await row
      .getByRole('button', { name: 'Edit child limit for ' + target.name, exact: true })
      .waitFor();
    return policy;
  }
  assert.equal((await save('custom', 0)).override, 0);
  await admin.page.reload();
  await row.getByText('Organization override', { exact: true }).waitFor();
  assert.match(await row.locator('strong').innerText(), /\/ 0 children/);
  const owner = await login('admin@runloom.test', 'RunloomLocal!123', '/management/organization');
  await owner.page.evaluate((id) => localStorage.setItem('runloom-org', id), target.id);
  await owner.page.reload();
  const hierarchy = owner.page.getByRole('region', { name: 'Organization hierarchy', exact: true });
  await hierarchy.getByText(target.child_count + ' / 0 children', { exact: true }).waitFor();
  const denied = await owner.page.request.post(base + '/api/v1/studio/orgs', {
    headers: owner.headers,
    data: { name: 'Blocked quota test', parent_id: target.id },
  });
  assert.equal(denied.status(), 400);
  assert.equal(
    (
      await owner.page.request.put(policyPath, {
        headers: owner.headers,
        data: { max_children: 3 },
      })
    ).status(),
    403,
  );
  const custom = await save('custom', 3);
  assert.equal(custom.max_children, 3);
  await row.getByText(target.child_count + ' / 3 children', { exact: true }).waitFor();
  const other = items.find((o) => !o.parent_id && o.id !== target.id);
  if (other) {
    const unchanged = await admin.page.request.get(
      base + '/api/v1/platform/organizations/' + other.id + '/child-limit',
      { headers: admin.headers },
    );
    assert.deepEqual(
      await unchanged.json(),
      other.child_limit,
      'An override must not change another parent',
    );
  }
  await admin.page.screenshot({ path: '/tmp/runloom-per-organization-limits.png', fullPage: true });
  const inherited = await save('default');
  assert.equal(inherited.override, null);
  assert.equal(inherited.max_children, inherited.default_max_children);
  const staff = await login(
    'platform-staff@runloom.test',
    'RunloomPlatformStaff!123',
    '/platform/organizations',
  );
  await staff.page.getByRole('heading', { name: 'Organizations', exact: true }).waitFor();
  assert.equal(await staff.page.getByRole('button', { name: /Edit child limit/ }).count(), 0);
  assert.equal(
    (
      await staff.page.request.put(policyPath, {
        headers: staff.headers,
        data: { max_children: 3 },
      })
    ).status(),
    403,
  );
  assert.equal(
    (await staff.page.request.delete(policyPath, { headers: staff.headers })).status(),
    403,
  );
  await admin.page.setViewportSize({ width: 390, height: 1000 });
  assert.ok(
    await admin.page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
    'Narrow tables scroll within their container',
  );
  assert.deepEqual(errors, []);
  console.log(
    'PASS live per-organization child limits: zero, persistence, Studio enforcement, isolated override, reset to default and staff/org-admin denials. Original policy restored.',
  );
} catch (error) {
  console.error(
    'Organization limits browser check failed: ' +
      error.name +
      ': ' +
      error.message.split('Call log:')[0],
  );
  process.exitCode = 1;
} finally {
  try {
    if (restore) await restore();
  } catch {
    console.error('Demo organization policy restore failed; retry from Platform console.');
    process.exitCode = 1;
  }
  await browser.close();
}
