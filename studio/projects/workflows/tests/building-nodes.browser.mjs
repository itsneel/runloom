// Real federated Angular components with synthetic APIs. No external side effects.
import assert from 'node:assert/strict';
const {chromium} = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.RUNLOOM_UI_URL || 'http://127.0.0.1:4300';
assert(['localhost', '127.0.0.1'].includes(new URL(base).hostname));
const browser = await chromium.launch({headless: true, ...(process.env.RUNLOOM_CHROME ? {executablePath: process.env.RUNLOOM_CHROME} : {})});
const page = await browser.newPage({viewport: {width: 1600, height: 1100}});
page.setDefaultTimeout(20000);
const errors = [];
page.on('pageerror', error => errors.push(error.message));
let catalogFails = true;
const definition = {
  schema_version: '1.0', id: 'building', name: 'Building nodes', kind: 'main', version: 0,
  trigger: {type: 'manual'}, input_schema: {type: 'object'}, output: null,
  steps: [
    {id: 'map', name: 'Map fields', type: 'transform', data: {operation: 'map', source: '${input}', fields: [{name: 'name', value: '${item.name}'}]}},
    {id: 'each', name: 'Each item', type: 'foreach', foreach: {items: '${input.items}', output: '${loop.item}', concurrency: 2}, branches: [{id: 'body', steps: [{id: 'pause-item', type: 'wait', seconds: 1}]}]},
    {id: 'ai', name: 'AI task', type: 'llm', prompt: 'Classify this message', model: 'test-model'},
    {id: 'send', name: 'Send message', type: 'integration', integration: {operation: '', secret: '', parameters: {}}},
  ],
};
try {
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname;
    const reply = json => route.fulfill({json});
    if (path.endsWith('/auth/config')) return route.fulfill({status: 503, json: {}});
    if (path.endsWith('/projects')) return reply([{id: 'default', name: 'General'}]);
    if (path === '/api/v1/studio/definitions') return reply([{id: 'building', name: 'Building nodes', project_id: 'default', latest_version: 0, has_draft: true}]);
    if (path.endsWith('/edit-lock')) return reply({locked: route.request().method() === 'POST', owner: 'tester', owner_name: 'Tester', ttl_seconds: 60});
    if (path.endsWith('/definitions/building')) return reply(definition);
    if (path.endsWith('/validate')) return reply({valid: true, definition});
    if (path.endsWith('/health')) return reply({temporal: true, database: true});
    if (path.endsWith('/catalog')) return catalogFails ? reply({}) : reply({integrations: [{
      id: 'slack-send', provider: 'Slack', name: 'Send message', description: 'Send text', mutates: true,
      documentation: 'https://docs.slack.dev/reference/methods/chat.postMessage/',
      parameters: [{name: 'channel', label: 'Channel', type: 'string', required: true}, {name: 'text', label: 'Message', type: 'string', required: true}],
    }]});
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
  await page.getByRole('button', {name: 'Details for Building nodes', exact: true}).click();
  const select = name => page.getByRole('button', {name: `Edit ${name}`, exact: true}).dispatchEvent('click');
  await select('Map fields');
  assert(await page.getByLabel('Mapping name 1', {exact: true}).isDisabled());
  await page.getByRole('button', {name: 'Edit workflow', exact: true}).click();
  await page.getByLabel('Mapping name 1', {exact: true}).fill('customer');
  await page.getByRole('button', {name: 'Add field', exact: true}).click();
  await page.getByLabel('Mapping name 2', {exact: true}).waitFor();
  assert.equal(await page.getByLabel('Mapping name 2', {exact: true}).count(), 1);
  await select('Each item');
  await page.getByLabel('For Each concurrency').fill('3');
  await page.getByRole('button', {name: 'Add node to loop body'}).click();
  assert(await page.getByText('Transform', {exact: true}).count() > 0);
  await select('AI task');
  await page.getByLabel('LLM task', {exact: true}).selectOption('classify');
  await page.getByLabel('Classification labels').fill('normal\nurgent');
  await select('Send message');
  await page.getByText('Could not load integrations.', {exact: false}).waitFor();
  catalogFails = false;
  await page.locator('runloom-integration-editor').getByRole('button', {name: 'Retry'}).click();
  await page.getByLabel('Integration operation').selectOption('slack-send');
  await page.getByLabel('Integration secret').fill('slack-access-token');
  await page.getByLabel('Channel', {exact: true}).fill('C123');
  await page.getByLabel('Message', {exact: true}).fill('${input.message}');
  await page.getByText('This operation changes an external system', {exact: false}).waitFor();
  for (const theme of ['light', 'dark']) {
    await page.evaluate(theme => document.documentElement.setAttribute('data-theme', theme), theme);
    if (process.env.RUNLOOM_SCREENSHOT_DIR) await page.screenshot({path: `${process.env.RUNLOOM_SCREENSHOT_DIR}/building-${theme}.png`});
  }
  const draft = await page.evaluate(() => window.ng.getComponent(document.querySelector('runloom-studio')).definition);
  assert.equal(draft.steps[0].data.fields[0].name, 'customer');
  assert.equal(draft.steps[1].foreach.concurrency, 3);
  assert.deepEqual(draft.steps[2].llm_task.labels, ['normal', 'urgent']);
  assert.equal(draft.steps[3].integration.parameters.text, '${input.message}');
  assert.deepEqual(errors, []);
  console.log('PASS building controls: read-only, mapping, loop body, typed LLM, catalog error/retry and integration configuration. APIs mocked.');
} finally { await browser.close(); }
