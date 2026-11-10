// Real federated Angular UI and Go CRUD/publish API. The fixture substitutes
// identity verification and Temporal; engine sample tests verify real semantics.
import assert from 'node:assert/strict';
const {chromium} = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.RUNLOOM_UI_URL || 'http://127.0.0.1:4300';
const api = process.env.RUNLOOM_SAMPLE_API_URL || 'http://127.0.0.1:17290';
for (const url of [base, api]) assert(['127.0.0.1', 'localhost'].includes(new URL(url).hostname));
const fixture = await (await fetch(api + '/fixture')).json();
const browser = await chromium.launch({headless: true, ...(process.env.RUNLOOM_CHROME ? {executablePath: process.env.RUNLOOM_CHROME} : {})});
const page = await browser.newPage({viewport: {width: 1500, height: 1050}});
page.setDefaultTimeout(20000);
const errors = [], writes = [];
page.on('pageerror', error => errors.push(error.message));
try {
  await page.route('**/api/**', route => {
    const request = route.request(), url = new URL(request.url());
    if (url.pathname === '/api/v1/auth/config') return route.fulfill({status: 503, json: {}});
    if (url.pathname === '/api/v1/health') return route.fulfill({json: {status: 'ok', database: true, temporal: true}});
    if (request.method() === 'POST') writes.push(url.pathname);
    return route.continue({headers: {...request.headers(), authorization: 'Bearer admin', 'x-runloom-team': fixture.team}});
  });
  await page.goto(base + '/workflows');
  await page.waitForFunction(() => window.ng?.getComponent(document.querySelector('app-root'))?.auth.ready());
  await page.evaluate(fixture => {
    const auth = window.ng.getComponent(document.querySelector('app-root')).auth;
    auth.bearer = async () => 'admin';
    auth.profile.set({user: {id: 'admin', email: 'admin@example.test'}, organizations: [{id: fixture.org, name: 'Organization', role: 'owner', teams: [{id: fixture.team, name: 'General', role: 'admin'}]}]});
    auth.organizationId.set(fixture.org); auth.teamId.set(fixture.team); auth.signedIn.set(true);
  }, fixture);
  await page.getByLabel('Guided example', {exact: true}).selectOption('parallel-subworkflows');
  await page.getByRole('button', {name: 'Try a guided example', exact: true}).click();
  await page.getByRole('button', {name: 'Cancel', exact: true}).click();
  assert.equal(writes.filter(path => path.endsWith('/definitions')).length, 0);
  await page.getByRole('button', {name: 'Try a guided example', exact: true}).click();
  await page.getByRole('button', {name: 'Create example', exact: true}).click();
  await page.getByRole('button', {name: 'Test workflow', exact: true}).waitFor();
  assert.match(new URL(page.url()).pathname, /^\/workflows\/definitions\/parallel-parent-/);
  assert.equal(writes.filter(path => path.endsWith('/definitions')).length, 2);
  assert.equal(writes.filter(path => path.endsWith('/publish')).length, 1);
  assert.ok(!writes.some(path => /\/runs(?:\/|$)/.test(path)), 'preparing an example must not execute it');
  await page.getByRole('button', {name: 'Edit Greet customer', exact: true}).waitFor();
  await page.getByRole('button', {name: 'Edit Greet reviewer', exact: true}).waitFor();
  await page.getByRole('button', {name: 'Test workflow', exact: true}).click();
  assert.deepEqual(JSON.parse(await page.getByLabel('Test input', {exact: true}).inputValue()), {customer: 'Avery', reviewer: 'Jordan'});
  await page.reload();
  await page.waitForFunction(() => window.ng?.getComponent(document.querySelector('app-root'))?.auth.ready());
  await page.evaluate(fixture => {
    const auth = window.ng.getComponent(document.querySelector('app-root')).auth;
    auth.bearer = async () => 'admin';
    auth.profile.set({user: {id: 'admin', email: 'admin@example.test'}, organizations: [{id: fixture.org, name: 'Organization', role: 'owner', teams: [{id: fixture.team, name: 'General', role: 'admin'}]}]});
    auth.organizationId.set(fixture.org); auth.teamId.set(fixture.team); auth.signedIn.set(true);
  }, fixture);
  await page.getByRole('button', {name: 'Test workflow', exact: true}).click();
  assert.deepEqual(JSON.parse(await page.getByLabel('Test input', {exact: true}).inputValue()), {customer: 'Avery', reviewer: 'Jordan'});
  assert.deepEqual(errors, []);
  console.log('PASS guided parallel sample: confirmation/cancel, Go draft and publish APIs, unique child/parent IDs, editor route, graph, prefilled input and reload; no workflow executed by preparation.');
} finally {
  await browser.close();
  await fetch(api + '/done');
}
