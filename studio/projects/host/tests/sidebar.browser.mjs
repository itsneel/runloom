// Real federated applications with synthetic APIs; no local application data is changed.
import assert from 'node:assert/strict';
const {chromium} = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({headless: true, ...(process.env.RUNLOOM_CHROME ? {executablePath: process.env.RUNLOOM_CHROME} : {})});
const origin = process.env.RUNLOOM_UI_URL || 'http://localhost:4200';
assert(['localhost', '127.0.0.1'].includes(new URL(origin).hostname));
try {
  const page = await browser.newPage({viewport: {width: 1440, height: 1100}, colorScheme: process.env.RUNLOOM_COLOR_SCHEME || 'light'});
  page.setDefaultTimeout(15000);
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  let failProjects = false;
  const projects = [{id: 'default', name: 'General'}, {id: 'second', name: 'Messaging'}];
  const workflows = [{id: 'sample', name: 'Sample workflow', project_id: 'second', latest_version: 0, has_draft: true}];
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname, method = route.request().method();
    if (path === '/api/v1/auth/config') return route.fulfill({status: 503, json: {}});
    if (path === '/api/v1/health') return route.fulfill({json: {temporal: true, database: true}});
    if (path.endsWith('/projects')) {
      if (failProjects) return route.fulfill({status: 503, json: {error: 'Synthetic projects unavailable'}});
      if (method === 'POST') {
        const project = {id: 'created', name: route.request().postDataJSON().name};
        projects.push(project);
        return route.fulfill({json: project});
      }
      return route.fulfill({json: projects});
    }
    if (path.endsWith('/rename')) {
      const project = projects.find(project => path.includes('/' + project.id + '/'));
      project.name = route.request().postDataJSON().name;
      return route.fulfill({json: project});
    }
    if (path.endsWith('/definitions')) return route.fulfill({json: workflows});
    if (path.endsWith('/secrets')) return route.fulfill({json: {configured: true, items: []}});
    if (path.endsWith('/edit-lock')) return route.fulfill({json: {locked: false, writing: false, ttl_seconds: 60}});
    return route.fulfill({json: []});
  });
  async function signIn(role = 'owner') {
    await page.waitForFunction(() => window.ng?.getComponent(document.querySelector('app-root'))?.auth.ready());
    await page.evaluate(role => {
      const shell = window.ng.getComponent(document.querySelector('app-root'));
      shell.error = '';
      shell.auth.bearer = async () => 'synthetic';
      shell.auth.profile.set({user: {id: 'test', email: 'test@example.test'}, organizations: [{id: 'org', name: 'Test org', role, teams: [{id: 'team', name: 'General', role: role === 'owner' ? 'admin' : 'viewer'}]}]});
      shell.auth.organizationId.set('org'); shell.auth.teamId.set('team'); shell.auth.signedIn.set(true);
    }, role);
  }
  const sidebar = page.locator('#workspace-sidebar');
  const management = sidebar.getByRole('heading', {name: 'Management', exact: true});
  const nav = sidebar.locator('.sidebar-navigation');
  const footer = sidebar.locator('.sidebar-footer');
  const navClick = name => sidebar.getByRole('button', {name, exact: true}).click();
  const y = async element => (await element.boundingBox()).y;
  await page.goto(origin + '/workflows');
  await signIn();
  await page.getByRole('heading', {name: 'Workflows', exact: true}).waitFor();
  const baseline = await y(management), bottom = await y(footer);
  for (const [name, path, heading] of [
    ['Projects', '/workflows/projects', 'Projects'],
    ['Teams & members', '/management/teams', 'Teams & members'],
    ['Workflows', '/workflows', 'Workflows'],
    ['Projects', '/workflows/projects', 'Projects'],
  ]) {
    await navClick(name);
    await page.waitForURL(url => url.pathname === path).catch(error => {
      throw new Error(`${name}: expected ${path}, got ${page.url()}`, {cause: error});
    });
    if (name === 'Teams & members') {
      await page.waitForFunction(() => !!window.ng.getComponent(document.querySelector('app-root')).bridge.management());
    } else await page.getByRole('heading', {name: heading, exact: true}).waitFor();
    assert(Math.abs(await y(management) - baseline) < 1, `${name}: Management moved`);
    assert(Math.abs(await y(footer) - bottom) < 1, `${name}: footer moved`);
  }
  await page.getByLabel('Search projects', {exact: true}).fill('mess');
  assert.equal(await page.getByRole('button', {name: 'Open project General', exact: true}).count(), 0);
  await page.getByRole('button', {name: 'Open project Messaging', exact: true}).click();
  await page.getByRole('button', {name: 'Details for Sample workflow', exact: true}).waitFor();
  assert.equal(await sidebar.locator('[aria-label="Project folders"]').count(), 0);
  await page.getByRole('button', {name: 'Back to projects', exact: true}).click();
  await page.getByRole('button', {name: 'New project', exact: true}).click();
  await page.getByRole('dialog').getByLabel('Project name').fill('Finance');
  await page.getByTestId('dialog-confirm').click();
  await page.getByRole('button', {name: 'Rename project Finance', exact: true}).click();
  await page.getByRole('dialog').getByLabel('Project name').fill('Accounting');
  await page.getByTestId('dialog-confirm').click();
  await page.getByRole('button', {name: 'Open project Accounting', exact: true}).waitFor();
  await page.reload(); await signIn();
  await page.getByRole('heading', {name: 'Projects', exact: true}).waitFor();
  assert.equal(new URL(page.url()).pathname, '/workflows/projects');
  await page.screenshot({path: '/tmp/runloom-stable-sidebar-projects.png', fullPage: true});

  await navClick('Workflows');
  await page.getByRole('button', {name: 'New workflow', exact: true}).click();
  await page.getByLabel('Workflow name', {exact: true}).fill('Keep this draft');
  await navClick('Projects');
  await page.getByRole('dialog').getByRole('button', {name: 'Keep editing', exact: true}).click();
  assert.equal(new URL(page.url()).pathname, '/workflows');
  assert.equal(await page.getByLabel('Workflow name', {exact: true}).inputValue(), 'Keep this draft');
  await navClick('Projects');
  await page.getByRole('dialog').getByRole('button', {name: 'Discard changes', exact: true}).click();
  await page.getByRole('heading', {name: 'Projects', exact: true}).waitFor();
  failProjects = true;
  await page.getByRole('button', {name: 'Refresh', exact: true}).click();
  await page.getByRole('heading', {name: 'Could not load projects', exact: true}).waitFor();
  failProjects = false;
  await page.getByRole('button', {name: 'Try again', exact: true}).click();
  await page.getByRole('button', {name: 'Open project Messaging', exact: true}).waitFor();
  const switcher = page.locator('.content-header').getByRole('button', {name: 'Switch organization or team'});
  assert.equal(await sidebar.getByRole('button', {name: 'Switch organization or team'}).count(), 0);
  await switcher.click();
  await page.getByRole('combobox', {name: 'Organization', exact: true}).waitFor();
  assert(Math.abs(await y(management) - baseline) < 1, 'Workspace switcher must overlay, not shift navigation');
  await page.keyboard.press('Escape');
  await navClick('Collapse sidebar');
  await sidebar.getByRole('button', {name: 'Projects', exact: true}).hover();
  await page.getByRole('tooltip').getByText('Projects', {exact: true}).waitFor();
  assert.equal(Math.round((await sidebar.boundingBox()).width), 64);
  await switcher.click();
  await page.getByRole('combobox', {name: 'Team', exact: true}).waitFor();
  assert.equal(Math.round((await sidebar.boundingBox()).width), 64);
  await page.keyboard.press('Escape');
  assert(await switcher.evaluate(el => el === document.activeElement));
  await navClick('Expand sidebar');
  await page.waitForFunction(() => Math.round(document.querySelector('#workspace-sidebar').getBoundingClientRect().width) === 268);

  await page.setViewportSize({width: 1280, height: 640});
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  await nav.evaluate(element => { element.scrollTop = 120; });
  const scrollTop = await nav.evaluate(element => element.scrollTop);
  assert(scrollTop > 0);
  const shortFooter = await y(footer), shortManagement = await y(management);
  // Router navigation without scrolling an offscreen button into view.
  await page.evaluate(() => window.ng.getComponent(document.querySelector('app-root')).openManagement('teams'));
  await page.waitForURL(url => url.pathname === '/management/teams');
  assert.equal(await nav.evaluate(element => element.scrollTop), scrollTop);
  assert(Math.abs(await y(footer) - shortFooter) < 1, `Footer moved from ${shortFooter} to ${await y(footer)}`);
  assert(Math.abs(await y(management) - shortManagement) < 1);
  assert((await footer.boundingBox()).y + (await footer.boundingBox()).height <= 640);

  await page.setViewportSize({width: 390, height: 720});
  await page.getByRole('button', {name: 'Toggle workspace sidebar', exact: true}).click();
  await nav.evaluate(element => { element.scrollTop = 0; });
  await navClick('Projects');
  await page.getByRole('heading', {name: 'Projects', exact: true}).waitFor();
  await page.getByRole('button', {name: 'Toggle workspace sidebar', exact: true}).click();
  await sidebar.getByRole('button', {name: 'Sign out', exact: true}).waitFor();
  assert.equal(await page.getByRole('button', {name: 'Lumi', exact: true}).isVisible(), false, 'Assistant must not cover the account footer');
  assert((await footer.boundingBox()).y + (await footer.boundingBox()).height <= 720);
  await page.screenshot({path: '/tmp/runloom-stable-sidebar-mobile.png', fullPage: true});
  await page.keyboard.press('Escape');
  await page.getByRole('button', {name: 'Lumi', exact: true}).waitFor({state: 'visible'});
  await page.reload(); await signIn('member');
  await page.getByRole('heading', {name: 'Projects', exact: true}).waitFor();
  assert.equal(await page.getByRole('button', {name: 'New project', exact: true}).isDisabled(), true);
  assert.equal(await page.getByRole('button', {name: 'Rename project General', exact: true}).count(), 0);
  assert.deepEqual(errors, []);
  console.log('PASS stable sidebar positions, project browsing/create/rename/retry, direct link, dirty guards, collapsed tooltip, scroll preservation, mobile footer and viewer permissions');
} finally { await browser.close(); }
