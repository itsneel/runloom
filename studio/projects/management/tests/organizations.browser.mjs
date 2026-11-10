// Opt-in local integration check with isolated identities and organization cleanup.
import assert from 'node:assert/strict';
import { organizationIdentity } from './fixtures/organization-identity.mjs';
const { chromium } = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.RUNLOOM_UI_URL || 'http://localhost:4200';
assert.ok(['localhost', '127.0.0.1'].includes(new URL(base).hostname));
const browser = await chromium.launch({
  headless: true,
  ...(process.env.RUNLOOM_CHROME ? { executablePath: process.env.RUNLOOM_CHROME } : {}),
});
let identity;
try {
  identity = await organizationIdentity(base);
  const errors = [];
  async function login(email, password, path, selectedOrg) {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1100 } });
    const page = await context.newPage();
    page.setDefaultTimeout(30000);
    page.on('pageerror', (e) => errors.push(e.name));
    await page.goto(base + path);
    await page.locator('#username').fill(email);
    await page.locator('#password').fill(password);
    const profile = page.waitForResponse(
      (r) => new URL(r.url()).pathname === '/api/v1/studio/me' && r.ok(),
    );
    await page.locator('#kc-login').click();
    const response = await profile;
    const data = await response.json();
    const headers = { authorization: (await response.request().allHeaders()).authorization };
    if (selectedOrg) {
      await page.evaluate((id) => localStorage.setItem('runloom-org', id), selectedOrg);
      await page.reload();
    }
    return {
      page,
      headers,
      profile: data,
    };
  }
  const { page, headers, profile } = await login(
    identity.owner.email,
    identity.owner.password,
    '/management/organization',
  );
  assert.equal(profile.organizations.length, 0, 'Fresh test identity has no existing memberships');
  const rootName = identity.prefix;
  const initial = await page.request.post(base + '/api/v1/studio/orgs', {
    headers,
    data: { name: rootName },
  });
  assert.equal(initial.status(), 201);
  const { id: root } = await initial.json();
  await page.reload();
  await page.goto(base + '/management/organization');
  const component = page.locator('runloom-organizations');
  async function create(name, parent = '') {
    await component.getByLabel('New organization name', { exact: true }).fill(name);
    await component.getByLabel('Parent organization', { exact: true }).selectOption(parent);
    const response = page.waitForResponse(
      (r) => new URL(r.url()).pathname === '/api/v1/studio/orgs' && r.request().method() === 'POST',
    );
    await component.getByRole('button', { name: 'Create organization', exact: true }).click();
    const result = await response;
    assert.equal(result.status(), 201);
    const { id } = await result.json();
    await component.locator('.identity-panel h2').filter({ hasText: name }).waitFor();
    await page.waitForFunction(
      (id) =>
        document.querySelector('.org-row.current strong')?.textContent !== undefined &&
        document.querySelector('runloom-organizations code')?.textContent === id,
      id,
    );
    await component.getByLabel('Parent organization', { exact: true }).waitFor();
    return id;
  }
  const child = await create(rootName + ' · Child one', root);
  const sibling = await create(rootName + ' · Child two', root);
  assert.equal(await component.locator('.org-row').count(), 3);
  const values = await component
    .getByLabel('Parent organization', { exact: true })
    .locator('option')
    .evaluateAll((options) => options.map((o) => o.value));
  assert.ok(values.includes(root));
  assert.deepEqual(values, [root], 'Only the selected family root; no second independent family');
  const deniedRoot = await page.request.post(base + '/api/v1/studio/orgs', {
    headers,
    data: { name: 'Rejected second root' },
  });
  assert.equal(deniedRoot.status(), 400);
  assert.ok(!values.includes(child) && !values.includes(sibling));
  for (const parent of [root, child]) {
    const result = await page.request.post(base + '/api/v1/studio/orgs', {
      headers,
      data: { name: 'Rejected overflow', parent_id: parent },
    });
    assert.equal(result.status(), 400);
  }
  await component
    .getByLabel('Organization display name', { exact: true })
    .fill(rootName + ' · Renamed child');
  const renamed = page.waitForResponse(
    (r) =>
      new URL(r.url()).pathname === '/api/v1/studio/orgs/' + sibling &&
      r.request().method() === 'PATCH',
  );
  await component.getByRole('button', { name: 'Save name', exact: true }).click();
  assert.equal((await renamed).status(), 200);
  await component
    .getByText('Organization renamed. Its ID and Temporal namespace are unchanged.', {
      exact: true,
    })
    .waitFor();
  assert.equal(await component.locator('code').innerText(), sibling);
  assert.equal(
    (
      await page.request.patch(base + '/api/v1/studio/orgs/' + sibling, {
        headers,
        data: { name: 'Invalid', id: 'changed' },
      })
    ).status(),
    400,
  );
  assert.equal(
    (
      await page.request.put(base + '/api/v1/platform/organization-settings', {
        headers,
        data: { max_children: 10 },
      })
    ).status(),
    403,
  );
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 1100 });
    assert.ok(
      await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
      'No page overflow',
    );
    await page.screenshot({ path: '/tmp/runloom-organizations-' + width + '.png', fullPage: true });
  }
  assert.equal(
    (
      await page.request.post(base + '/api/v1/studio/orgs/' + child + '/members', {
        headers,
        data: { email: identity.member.email, role: 'member' },
      })
    ).status(),
    200,
  );
  const member = await login(
    identity.member.email,
    identity.member.password,
    '/management/organization',
    child,
  );
  await member.page
    .getByRole('button', { name: 'Switch organization or team', exact: true })
    .click();
  assert.equal(
    await member.page.getByRole('combobox', { name: 'Organization', exact: true }).inputValue(),
    child,
  );
  const memberTree = member.page.locator('runloom-organizations');
  await memberTree
    .locator('.identity-panel h2')
    .filter({ hasText: rootName + ' · Child one' })
    .waitFor();
  await memberTree
    .getByRole('button', { name: 'Membership required', exact: true })
    .first()
    .waitFor();
  assert.equal(
    await memberTree.getByRole('button', { name: 'Membership required', exact: true }).count(),
    2,
  );
  for (const button of await memberTree
    .getByRole('button', { name: 'Membership required', exact: true })
    .all())
    assert.equal(await button.isDisabled(), true);
  assert.equal(await memberTree.getByLabel('New organization name', { exact: true }).count(), 0);
  assert.equal(
    (
      await member.page.request.get(base + '/api/v1/studio/orgs/' + root + '/members', {
        headers: member.headers,
      })
    ).status(),
    403,
  );
  assert.equal(
    (
      await member.page.request.get(base + '/api/v1/studio/orgs/' + sibling + '/members', {
        headers: member.headers,
      })
    ).status(),
    403,
  );
  const admin = await login(
    'platform-admin@runloom.test',
    'RunloomPlatformAdmin!123',
    '/platform/organizations',
  );
  const settings = admin.page.getByLabel('Maximum child organizations', { exact: true });
  await settings.waitFor();
  const previous = await settings.inputValue();
  try {
    await settings.fill('10');
    const saved = admin.page.waitForResponse(
      (r) =>
        new URL(r.url()).pathname === '/api/v1/platform/organization-settings' &&
        r.request().method() === 'PUT',
    );
    await admin.page.getByRole('button', { name: 'Save organization limit', exact: true }).click();
    assert.equal((await saved).status(), 200);
    assert.equal(
      (
        await (
          await admin.page.request.get(base + '/api/v1/platform/organization-settings', {
            headers: admin.headers,
          })
        ).json()
      ).max_children,
      10,
    );
    await admin.page.screenshot({ path: '/tmp/runloom-organization-policy.png', fullPage: true });
  } finally {
    assert.equal(
      (
        await admin.page.request.put(base + '/api/v1/platform/organization-settings', {
          headers: admin.headers,
          data: { max_children: Number(previous) },
        })
      ).status(),
      200,
    );
  }
  const staff = await login(
    'platform-staff@runloom.test',
    'RunloomPlatformStaff!123',
    '/platform/organizations',
  );
  await staff.page.getByRole('heading', { name: 'Organizations', exact: true }).waitFor();
  assert.equal(
    await staff.page.getByLabel('Maximum child organizations', { exact: true }).count(),
    0,
  );
  assert.equal(
    (
      await staff.page.request.put(base + '/api/v1/platform/organization-settings', {
        headers: staff.headers,
        data: { max_children: 10 },
      })
    ).status(),
    403,
  );
  assert.deepEqual(errors, []);
  console.log(
    'PASS isolated organization hierarchy, single-family memberships, child limits, stable-ID rename, responsive tree, platform policy and restricted staff.',
  );
} catch (error) {
  console.error(
    'Organization browser check failed: ' + error.name + ': ' + error.message.split('Call log:')[0],
  );
  process.exitCode = 1;
} finally {
  await browser.close();
  if (identity) await identity.cleanup();
}
