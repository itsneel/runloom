// Local-only browser/API regression. Retains synthetic team/definition/run records.
// Authentication tokens stay in memory and are never printed.
import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { existsSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const base = (process.env.RUNLOOM_UI_URL || 'http://localhost:34200').replace(/\/$/, '');
assert(['localhost', '127.0.0.1'].includes(new URL(base).hostname), 'Local verification only');
const modulePath = process.env.RUNLOOM_PLAYWRIGHT_MODULE || fileURLToPath(new URL('../Website/node_modules/playwright/index.mjs', import.meta.url));
const { chromium } = await import(modulePath);
const chrome = process.env.RUNLOOM_CHROME || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
const browser = await chromium.launch({ headless: true, ...(existsSync(chrome) ? { executablePath: chrome } : {}) });
let phase = 'login';
try {
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  await page.goto(base);
  await page.locator('#username').fill('admin@runloom.test');
  await page.locator('#password').fill('RunloomLocal!123');
  const pendingProfile = page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/studio/me' && r.ok());
  await page.locator('#kc-login').click();
  const profile = await pendingProfile;
  const org = (await profile.json()).organizations.find(o => o.name === 'Runloom');
  assert(org, 'Default local organization missing');
  const token = (await profile.request().allHeaders()).authorization;
  let team = org.teams[0].id;
  const request = async (path, body, selectedTeam = team) => {
    const response = await fetch(base + path, {
      method: body === undefined ? 'GET' : 'POST',
      headers: { Authorization: token, 'X-Runloom-Team': selectedTeam, 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: AbortSignal.timeout(30000),
    });
    assert(response.ok, `${path}: HTTP ${response.status}`);
    return response.json();
  };
  phase = 'synthetic run';
  const id = 'tag-check-' + randomUUID();
  team = (await request(`/api/v1/studio/orgs/${org.id}/teams`, { name: id })).id;
  await request('/api/v1/studio/definitions', {
    schema_version: '1.0', id, name: 'Run tag dashboard check', version: 0,
    trigger: { type: 'manual' }, input_schema: { type: 'object' },
    steps: [{ id: 'echo', type: 'activity', activity: 'runloom.echo', with: { value: 'synthetic' } }],
    output: '${steps.echo.output}',
  });
  await request(`/api/v1/studio/definitions/${id}/publish`, {});
  const started = await request('/api/v1/studio/runs', { definition_id: id, input: {} });
  const runId = started.workflow_id;
  const history = await request(`/api/v1/studio/runs/${runId}/history`);
  assert.equal(history.run.metadata.org_id, org.id);
  assert.equal(history.run.metadata.team_id, team);
  assert.equal(history.run.metadata.is_test, false);
  phase = 'server tag search';
  for (const query of [
    'search=' + encodeURIComponent(id.toUpperCase()),
    'tag_key=source&tag_value=manual',
    'tag_key=is_test&tag_value=false',
    'tag_key=definition_version&tag_value=1',
  ]) assert.equal((await request('/api/v1/studio/runs?' + query))[0].workflow_id, runId);
  assert.equal((await request('/api/v1/studio/runs?tag_key=source&tag_value=consumer')).length, 0);
  assert.equal((await request('/api/v1/studio/runs?search=' + id, undefined, org.teams[0].id)).length, 0, 'Tag search crossed team scope');
  phase = 'dashboard rendering and interactions';
  await page.evaluate(({ org, team }) => {
    localStorage.setItem('runloom-org', org);
    localStorage.setItem('runloom-team', team);
  }, { org: org.id, team });
  await page.goto(base + '/management/runs');
  const dashboard = page.locator('runloom-run-history');
  await dashboard.locator('tbody tr').waitFor();
  assert.equal(await dashboard.locator('tbody .run-tag').count(), 4);
  phase = 'clicking source tag';
  const tagged = page.waitForResponse(r => r.url().includes('tag_key=source') && r.ok());
  await dashboard.locator('tbody .run-tag').filter({ hasText: 'manual' }).click();
  await tagged;
  assert.equal(await dashboard.getByLabel('Exact tag value', { exact: true }).inputValue(), 'manual');
  phase = 'opening run details';
  await dashboard.getByRole('button', { name: id, exact: true }).click();
  const details = dashboard.getByRole('region', { name: 'Run details' });
  await details.getByRole('button', { name: 'Namespace', exact: false }).waitFor();
  assert.equal(await details.locator('.run-tag').count(), 10);
  phase = 'no-match search';
  await dashboard.getByLabel('Search runs', { exact: true }).fill('nonexistent-' + id);
  await dashboard.getByText('No runs match these filters.', { exact: true }).waitFor();
  phase = 'reset filters';
  await dashboard.getByRole('button', { name: 'Clear filters', exact: true }).click();
  await dashboard.locator('tbody tr').waitFor();
  assert.equal(await dashboard.getByLabel('Exact tag value', { exact: true }).inputValue(), '');
  await page.setViewportSize({ width: 390, height: 844 });
  assert.equal(await dashboard.locator('tbody .run-tag').count(), 4);
  if (process.env.RUNLOOM_TAG_SCREENSHOT) await page.screenshot({ path: process.env.RUNLOOM_TAG_SCREENSHOT, fullPage: true });
  console.log('PASS: run tag API filters, team isolation, dashboard chips, exact filtering, run details, no-match/reset and narrow layout.');
} catch (error) {
  console.error(`Run tag verification failed during ${phase}: ${error.name}${error.name === 'AssertionError' ? ': ' + error.message : ''}`);
  process.exitCode = 1;
} finally {
  await browser.close();
}
