// Local-only check: rerun setup without resetting passwords, then compare identities
// and catalog records. No credentials, provider responses or user content are logged.
import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {existsSync} from 'node:fs';
import {createHash} from 'node:crypto';
import {fileURLToPath} from 'node:url';
import {plan} from './local-services.mjs';
import {configuration, execute} from './local-k8s.mjs';
import {ensureLocalAdmin} from './local-k8s-open.mjs';

const root = fileURLToPath(new URL('../', import.meta.url));
const kind = process.env.RUNLOOM_ADMIN_TEST_KIND === '1';
const config = kind ? configuration() : undefined;
const installed = kind ? execute('open', config) : undefined;
const base = installed?.origin || `http://localhost:${plan(process.env.SERVICES_DATABASE).ports.UI_PORT}`;
assert(['localhost', '127.0.0.1'].includes(new URL(base).hostname), 'Local tests only');
const {chromium} = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || new URL('../Website/node_modules/playwright/index.mjs', import.meta.url).href);
const chrome = process.env.RUNLOOM_CHROME || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
const browser = await chromium.launch({headless: true, ...(existsSync(chrome) ? {executablePath: chrome} : {})});
let phase = 'initial browser login';
try {
  const snapshot = async () => {
    const result = [];
    for (const [email, password, expectedRole] of [
      ['admin@runloom.test', process.env.RUNLOOM_TEST_PASSWORD || 'RunloomLocal!123'],
      ['member@runloom.test', process.env.RUNLOOM_TEST_MEMBER_PASSWORD || 'RunloomMember!123'],
      ['platform-admin@runloom.test', process.env.RUNLOOM_TEST_PLATFORM_ADMIN_PASSWORD || 'RunloomPlatformAdmin!123', 'platform_admin'],
      ['platform-staff@runloom.test', process.env.RUNLOOM_TEST_PLATFORM_STAFF_PASSWORD || 'RunloomPlatformStaff!123', 'platform_staff'],
    ]) {
      phase = `browser login for ${email}`;
      const context = await browser.newContext();
      try {
        const page = await context.newPage();
        await page.goto(base + (expectedRole === 'platform_staff' ? '/' : '/workflows'));
        await page.locator('#username').fill(email);
        await page.locator('#password').fill(password);
        const profile = page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/studio/me' && r.ok());
        await page.locator('#kc-login').click();
        const response = await profile, me = await response.json();
        assert.equal(me.user.email, email);
        assert.equal(typeof me.user.id, 'string');
        assert(me.user.id.length > 0, 'Persistent user identity is required');
        const authorization = (await response.request().allHeaders()).authorization;
        const platformResponse = await page.request.get(base + '/api/v1/platform/me', {headers: {Authorization: authorization}});
        assert(platformResponse.ok(), 'Platform identity unavailable after setup');
        const platform = await platformResponse.json();
        if (expectedRole) {
          phase = `platform landing for ${email}`;
          assert.equal(platform.role, expectedRole);
          assert.deepEqual(me.organizations, [], 'Platform defaults must not inherit tenant memberships');
          await page.waitForURL(url => url.pathname === '/platform/health');
          await page.getByRole('heading', {name: 'Service health', exact: true}).waitFor();
          await page.getByRole('link', {name: 'Back to Studio'}).click();
          const navigation = page.locator('.primary-navigation');
          const consoleButton = navigation.getByRole('button', {name: 'Platform console', exact: true});
          await consoleButton.waitFor();
          phase = `sidebar alignment for ${email}`;
          const styles = await navigation.evaluate(nav => [...nav.querySelectorAll('button')].map(button => {
            const style = getComputedStyle(button);
            return [style.display, style.alignItems, style.padding, style.gap, style.minHeight, style.borderWidth];
          }));
          assert.deepEqual(styles.at(-1), styles[0], 'Platform entry must use standard sidebar row alignment and spacing');
          phase = `collapsed sidebar for ${email}`;
          await page.getByRole('button', {name: 'Collapse sidebar'}).click();
          await consoleButton.locator('.nav-text').waitFor({state: 'hidden'});
          await consoleButton.hover();
          await page.getByRole('tooltip', {name: 'Platform console', exact: true}).waitFor();
          await page.getByRole('button', {name: 'Expand sidebar'}).click();
          await consoleButton.click();
          await page.getByRole('heading', {name: 'Service health', exact: true}).waitFor();
          phase = `platform authorization for ${email}`;
          for (const path of ['health', 'organizations']) {
            assert.equal((await page.request.get(base + '/api/v1/platform/' + path, {headers: {Authorization: authorization}})).status(), 200);
          }
          for (const path of ['staff', 'audit']) {
            assert.equal((await page.request.get(base + '/api/v1/platform/' + path, {headers: {Authorization: authorization}})).status(), expectedRole === 'platform_admin' ? 200 : 403);
          }
          if (expectedRole === 'platform_admin') {
            await page.getByRole('link', {name: 'Staff & access'}).click();
            await page.getByRole('cell', {name: 'platform-admin@runloom.test', exact: true}).waitFor();
            assert.equal(await page.getByRole('cell', {name: 'admin@runloom.test', exact: true}).count(), 0, 'Organization administrator must not be listed as platform staff');
          }
          const tenantRead = await page.request.get(base + '/api/v1/studio/definitions', {headers: {Authorization: authorization, 'X-Runloom-Team': result[0].team}});
          assert.equal(tenantRead.status(), 403, 'Platform account must not bypass tenant authorization');
          result.push({user: me.user.id, platformRole: platform.role, organizations: me.organizations});
          continue;
        }
        const orgs = me.organizations.filter(o => o.name === 'Runloom');
        assert.equal(orgs.length, 1, 'Exactly one default organization');
        const teams = orgs[0].teams.filter(t => t.name === 'General');
        assert.equal(teams.length, 1, 'Exactly one default team');
        const headers = {Authorization: (await response.request().allHeaders()).authorization, 'X-Runloom-Team': teams[0].id};
        assert.equal(platform.role, '', 'Tenant setup accounts must not have platform access');
        for (const path of ['health', 'organizations', 'staff', 'audit']) {
          assert.equal((await page.request.get(base + '/api/v1/platform/' + path, {headers})).status(), 403);
        }
        const catalog = await page.request.get(base + '/api/v1/studio/definitions', {headers});
        assert(catalog.ok(), 'Workflow catalog unavailable after login');
        const data = await catalog.json();
        // IDs/versions protect existing drafts without printing their contents.
        const items = Array.isArray(data) ? data : data.items;
        assert(Array.isArray(items), 'Unexpected workflow catalog response');
        const records = items.map(d => ({id: d.id, version: d.version})).sort((a, b) => a.id.localeCompare(b.id));
        result.push({user: me.user.id, platformRole: platform.role, org: orgs[0].id, orgRole: orgs[0].role,
          team: teams[0].id, teamRole: teams[0].role,
          catalog: createHash('sha256').update(JSON.stringify(records)).digest('hex')});
        await page.locator('runloom-studio').waitFor();
      } finally { await context.close(); }
    }
    assert.equal(result[0].org, result[1].org);
    assert.equal(result[0].team, result[1].team);
    assert.equal(result[0].orgRole, 'owner');
    assert.equal(result[0].teamRole, 'admin');
    return result;
  };
  const before = await snapshot();
  phase = 'idempotent administrator setup';
  if (kind) {
    const result = await ensureLocalAdmin(config, installed);
    assert.equal(result.created, false);
    assert.equal(result.member.created, false);
    assert.equal(result.platform_admin.created, false);
    assert.equal(result.platform_staff.created, false);
    assert.equal(result.reset, false);
  } else {
    const result = spawnSync(process.execPath, ['scripts/local-services.mjs', 'admin'], {
      cwd: root, env: process.env, encoding: 'utf8', timeout: 600000, maxBuffer: 16 * 1024 * 1024,
    });
    assert.equal(result.status, 0, 'Repeated setup failed; credential-bearing output withheld');
  }
  phase = 'repeat browser login and preservation';
  const after = await snapshot();
  assert(JSON.stringify(after) === JSON.stringify(before), 'Setup changed existing identities, memberships or workflow catalog');
  console.log('PASS: all four local account browser logins before and after repeated setup; platform staff restrictions, tenant isolation, identities, roles and workflow catalog preserved.');
} catch (error) {
  console.error(`Admin setup verification failed during ${phase}: ${error.name}${error.code === 'ERR_ASSERTION' ? ' (' + error.operator + ')' : ''}`);
  process.exitCode = 1;
} finally { await browser.close(); }
