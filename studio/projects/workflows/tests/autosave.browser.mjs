// Real federated Angular UI, synthetic session and mocked APIs; no live workflow effects.
import assert from 'node:assert/strict';
const {chromium} = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.RUNLOOM_UI_URL || 'http://127.0.0.1:4300';
assert(['localhost', '127.0.0.1'].includes(new URL(base).hostname));
const browser = await chromium.launch({headless: true});
const page = await browser.newPage({viewport: {width: 1600, height: 1000}});
page.setDefaultTimeout(20000);
let definition = {schema_version: '1.0', id: 'autosave', name: 'Autosave example', kind: 'main', version: 0, trigger: {type: 'manual'}, input_schema: {type: 'object'}, steps: [{id: 'wait', type: 'wait', seconds: 1}]};
let failSave = false, writes = 0, publishes = 0;
const errors = [];
page.on('pageerror', error => errors.push(error.message));
try {
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname;
    const reply = json => route.fulfill({json});
    if (path.endsWith('/auth/config')) return route.fulfill({status: 503, json: {}});
    if (path.endsWith('/projects')) return reply([{id: 'default', name: 'General'}]);
    if (path === '/api/v1/studio/definitions') {
      if (route.request().method() === 'POST') {
        writes++;
        if (failSave) return route.fulfill({status: 503, json: {detail: 'Synthetic offline error'}});
        definition = route.request().postDataJSON();
        return reply(definition);
      }
      return reply([{id: definition.id, name: definition.name, project_id: 'default', latest_version: 0, has_draft: true}]);
    }
    if (path.endsWith('/edit-lock')) return reply({locked: route.request().method() !== 'GET', owner: 'tester', ttl_seconds: 60});
    if (path.endsWith('/definitions/autosave')) return reply(definition);
    if (path.endsWith('/validate')) return reply({valid: true, definition: route.request().postDataJSON()});
    if (path.endsWith('/publish')) { publishes++; return reply(definition); }
    if (path.endsWith('/health')) return reply({temporal: true, database: true});
    return reply([]);
  });
  await page.goto(base + '/workflows');
  await page.waitForFunction(() => window.ng?.getComponent(document.querySelector('app-root'))?.auth.ready());
  await page.evaluate(() => {
    const auth = window.ng.getComponent(document.querySelector('app-root')).auth;
    auth.bearer = async () => 'synthetic-token';
    auth.profile.set({user: {id: 'tester', email: 'test@example.test'}, organizations: [{id: 'org', name: 'Test', role: 'owner', teams: [{id: 'team', name: 'Team', role: 'admin'}]}]});
    auth.organizationId.set('org'); auth.teamId.set('team'); auth.signedIn.set(true);
  });
  await page.getByRole('button', {name: 'Details for Autosave example', exact: true}).click();
  const name = page.getByLabel('Workflow name', {exact: true});
  assert(await name.isDisabled());
  await page.getByRole('button', {name: 'Edit workflow', exact: true}).click();
  await name.fill('Automatically saved');
  await page.waitForFunction(() => document.querySelector('.unsaved-indicator')?.textContent.trim() === 'Saved');
  assert.equal(definition.name, 'Automatically saved'); assert.equal(writes, 1);
  await page.getByRole('button', {name: 'Undo', exact: true}).click();
  await page.waitForFunction(() => document.querySelector('input[aria-label="Workflow name"]')?.value === 'Autosave example');
  await page.keyboard.press('Control+Shift+z');
  await page.waitForFunction(() => document.querySelector('input[aria-label="Workflow name"]')?.value === 'Automatically saved');
  await name.fill('Native typing');
  const historyCount = await page.evaluate(() => window.ng.getComponent(document.querySelector('runloom-studio')).history.index);
  await page.keyboard.press('Control+z');
  assert.equal(await page.evaluate(() => window.ng.getComponent(document.querySelector('runloom-studio')).history.index), historyCount, 'native text undo is not intercepted by graph history');
  failSave = true;
  await name.fill('Keep after failed save');
  await page.getByRole('button', {name: 'Retry save', exact: true}).waitFor();
  assert.equal(await name.inputValue(), 'Keep after failed save');
  failSave = false;
  await page.getByRole('button', {name: 'Retry save', exact: true}).click();
  await page.waitForFunction(() => document.querySelector('.unsaved-indicator')?.textContent.trim() === 'Saved');
  assert.equal(definition.name, 'Keep after failed save');
  const guide = page.getByRole('button', {name: 'Keyboard shortcuts', exact: true});
  await guide.click();
  const dialog = page.getByRole('dialog', {name: 'Keyboard shortcuts'});
  assert(await dialog.isVisible());
  if (process.env.RUNLOOM_SCREENSHOT_DIR) await page.screenshot({path: `${process.env.RUNLOOM_SCREENSHOT_DIR}/shortcuts-desktop.png`});
  await page.keyboard.press('Control+z'); assert.equal(await name.inputValue(), 'Keep after failed save');
  await page.keyboard.press('Escape'); assert(await guide.evaluate(el => el === document.activeElement));
  await page.setViewportSize({width: 390, height: 844});
  await guide.click();
  assert(await dialog.isVisible());
  assert(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth));
  if (process.env.RUNLOOM_SCREENSHOT_DIR) await page.screenshot({path: `${process.env.RUNLOOM_SCREENSHOT_DIR}/shortcuts-mobile.png`});
  await page.keyboard.press('Escape');
  assert.equal(publishes, 0); assert.deepEqual(errors, []);
  console.log('PASS autosave, undo/redo, native text shortcuts, failed-save retry, read-only state, dialog focus/Escape and mobile guide. APIs mocked.');
} finally { await browser.close(); }
