// Real federated UI with synthetic identities/API responses. No accounts are changed.
import assert from 'node:assert/strict';
const {chromium} = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({headless: true, ...(process.env.RUNLOOM_CHROME ? {executablePath: process.env.RUNLOOM_CHROME} : {})});
const origin = process.env.RUNLOOM_UI_URL || 'http://localhost:4200';
try {
  for (const [orgRole, teamRole] of [['member', 'editor'], ['member', 'viewer'], ['member', 'admin'], ['owner', 'admin']]) {
    const page = await browser.newPage({viewport: {width: 1440, height: 1000}});
    page.setDefaultTimeout(15000);
    const errors = [], requests = [];
    page.on('pageerror', error => errors.push(error.message));
    const members = [
      {email: 'owner@example.test', user_id: 'owner', role: 'admin', inherited: true},
      {email: 'peer@example.test', user_id: 'peer', role: 'editor'},
    ];
    await page.route('**/api/**', route => {
      const path = new URL(route.request().url()).pathname;
      requests.push(path);
      if (path === '/api/v1/auth/config') return route.fulfill({status: 503, json: {}});
      if (path.endsWith('/teams/team/members')) return route.fulfill({json: members});
      if (path.endsWith('/orgs/org/members')) return route.fulfill({json: []});
      return route.fulfill({json: []});
    });
    await page.goto(origin + '/management/teams');
    await page.waitForFunction(() => window.ng?.getComponent(document.querySelector('app-root'))?.auth.ready());
    await page.evaluate(({orgRole, teamRole}) => {
      const shell = window.ng.getComponent(document.querySelector('app-root'));
      shell.error = '';
      shell.auth.bearer = async () => 'synthetic';
      shell.auth.profile.set({user: {id: 'me', email: 'me@example.test'}, organizations: [
        {id: 'org', name: 'Runloom', role: orgRole, teams: [{id: 'team', name: 'General', role: teamRole}]},
      ]});
      shell.auth.organizationId.set('org'); shell.auth.teamId.set('team'); shell.auth.signedIn.set(true);
    }, {orgRole, teamRole});
    const management = page.locator('runloom-management');
    const rows = management.locator('.member-row');
    await management.getByText('peer@example.test', {exact: true}).waitFor();
    assert.equal(await rows.count(), 2);
    assert.equal(await management.getByLabel('Change team role', {exact: true}).count(), teamRole === 'admin' ? 1 : 0);
    assert.equal(await management.getByRole('button', {name: 'Remove', exact: true}).count(), teamRole === 'admin' ? 1 : 0);
    assert.equal(await management.getByRole('button', {name: 'Add to team', exact: true}).count(), teamRole === 'admin' ? 1 : 0);
    assert.equal(await rows.filter({hasText: 'owner@example.test'}).locator('select, button').count(), 0);
    await management.getByLabel('Search team members', {exact: true}).fill('PEER');
    assert.equal(await rows.count(), 1);
    await management.getByLabel('Search team members', {exact: true}).fill('');
    if (teamRole === 'editor') {
      await page.screenshot({path: '/tmp/runloom-member-roster-desktop.png'});
      await page.setViewportSize({width: 390, height: 844});
      assert(await rows.evaluateAll(items => items.every(row => row.scrollWidth <= row.clientWidth + 1)));
      await page.screenshot({path: '/tmp/runloom-member-roster-mobile.png'});
      await page.setViewportSize({width: 1440, height: 1000});
    }
    await page.getByRole('button', {name: 'Organization', exact: true}).click();
    await page.waitForURL('**/management/organization');
    await management.getByRole('heading', {name: 'Organization', exact: true}).waitFor();
    assert.equal(await management.getByRole('button', {name: 'Create organization', exact: true}).count(), orgRole === 'owner' ? 1 : 0);
    if (orgRole === 'member') assert(!requests.includes('/api/v1/studio/orgs/org/members'));
    assert.deepEqual(errors, []);
    await page.close();
  }
  console.log('PASS member roster/search, inherited access, admin-only controls, member-blocked organization creation and mobile layout');
} finally {
  await browser.close();
}
