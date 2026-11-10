// Development-server-only browser regression tests. All backend calls are mocked;
// Angular debug APIs inject a synthetic session without changing production auth.
import assert from 'node:assert/strict';
const { chromium } = await import(process.env.RUNLOOM_PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.RUNLOOM_UI_URL || 'http://localhost:4200';
const browser = await chromium.launch({
  headless: true,
  ...(process.env.RUNLOOM_CHROME ? { executablePath: process.env.RUNLOOM_CHROME } : {}),
});
// Use an explicit context: browser.newPage() owns its context and page.close()
// force-closes that context even when a beforeunload dialog is canceled.
const context = await browser.newContext();
const page = await context.newPage();
page.setDefaultTimeout(10000);
const errors = [];
page.on('pageerror', (error) => errors.push(error.message));
page.on('dialog', dialog => {
  assert.equal(dialog.type(), 'beforeunload', 'In-app decisions must never use native browser dialogs');
});
const original = {
  schema_version: '1.0', id: 'example', name: 'Saved workflow', description: '',
  version: 0, trigger: { type: 'manual' }, input_schema: { type: 'object' },
  steps: [{ id: 'echo', name: 'Echo', type: 'activity', activity: 'runloom.echo', with: {},
    timeout_seconds: 30, retry: { maximum_attempts: 1 } }], output: null,
};
let saved = structuredClone(original);
const projects = [{ id: 'default', name: 'General' }];
const assignments = new Map();
let failMove = false;
let failSave = false;
let holdSave;
let saveStarted;
let releaseSave;
let holdLoad;
let loadStarted;
let releaseLoad;
let holdParentStatus;
let parentStatusStarted;
const childTests = [];
let productionStarts = 0;
await page.route('**/api/**', async (route) => {
  const url = new URL(route.request().url());
  const path = url.pathname;
  if (path === '/api/v1/auth/config') return route.fulfill({ status: 503, json: {} });
  if (path === '/api/v1/health') return route.fulfill({ json: { temporal: true, database: true } });
  if (path.endsWith('/edit-lock')) return route.fulfill({ json: { locked: route.request().method() !== 'DELETE', owner: 'tester', owner_name: 'Tester', ttl_seconds: 90, writing: false } });
  if (path.startsWith('/api/v1/studio/definitions/') && path.endsWith('/test')) {
    childTests.push({ path, body: route.request().postDataJSON(), headers: route.request().headers() });
    return route.fulfill({ status: 201, json: { workflow_id: 'mock-child-test', is_test: true } });
  }
  if (path === '/api/v1/studio/runs' && route.request().method() === 'POST') productionStarts++;
  if (path === '/api/v1/studio/runs' && route.request().method() === 'GET' && childTests.length) return route.fulfill({ json: [{
    workflow_id: 'mock-child-test', definition_id: saved.id, trigger_type: 'child_test',
    temporal_status: 'completed', created_at: '2026-09-29T12:00:00Z',
  }] });
  if (path === '/api/v1/studio/runs/mock-child-test') return route.fulfill({ json: {
    workflow_id: 'mock-child-test', temporal_status: 'completed',
    state: { status: 'completed', is_test: true, output: { message: 'Hello, Avery!' } },
  } });
  if (path === '/api/v1/studio/runs/mock-child-test/history') return route.fulfill({ json: {
    run: { input: { name: 'Avery' }, trigger_type: 'child_test' }, events: [],
  } });
  if (path === '/api/v1/studio/runs/mock-parent' || path === '/api/v1/studio/runs/mock-child') {
    const parent = path.endsWith('mock-parent');
    if (parent && holdParentStatus) { parentStatusStarted?.(); await holdParentStatus; }
    return route.fulfill({ json: { workflow_id: parent ? 'mock-parent' : 'mock-child', temporal_status: 'completed', state: {
      status: 'completed', ...(parent ? { child_workflows: { call: 'mock-child' } } : { parent_workflow_id: 'mock-parent' }),
    } } });
  }
  if (path.startsWith('/api/v1/studio/runs/mock-') && path.endsWith('/history')) return route.fulfill({ json: { run: { input: {} }, events: [] } });
  if (path === '/api/v1/studio/projects') {
    if (route.request().method() === 'GET') return route.fulfill({ json: projects });
    const project = { id: `project-${projects.length}`, name: route.request().postDataJSON().name };
    projects.push(project);
    return route.fulfill({ status: 201, json: project });
  }
  if (path.startsWith('/api/v1/studio/projects/') && path.endsWith('/rename')) {
    const project = projects.find((p) => p.id === path.split('/')[5]);
    project.name = route.request().postDataJSON().name;
    return route.fulfill({ json: project });
  }
  if (path.endsWith('/project')) {
    if (failMove) return route.fulfill({ status: 503, json: { detail: 'Simulated move failure' } });
    const id = path.split('/')[5];
    const projectId = route.request().postDataJSON().project_id;
    assignments.set(id, projectId);
    return route.fulfill({ json: { definition_id: id, project_id: projectId } });
  }
  if (path === '/api/v1/studio/definitions' && route.request().method() === 'POST') {
    const body = route.request().postDataJSON();
    saveStarted?.();
    if (holdSave) await holdSave;
    if (failSave) return route.fulfill({ status: 500, json: { detail: 'Simulated save failure' } });
    saved = body;
    assignments.set(body.id, route.request().headers()['x-runloom-project'] || assignments.get(body.id) || 'default');
    return route.fulfill({ json: saved });
  }
  if (path === '/api/v1/studio/definitions') return route.fulfill({ json: [
    { id: saved.id, name: saved.name, project_id: assignments.get(saved.id) || 'default', version: saved.version, versions: [1], latest_version: 1, published_kind: 'main' },
    { id: 'sample-child', name: 'Child sample', project_id: 'default', latest_version: 1, published_kind: 'child' },
  ] });
  if (path.endsWith('/validate')) return route.fulfill({ json: {
    valid: true, definition: route.request().postDataJSON(),
  } });
  if (path.endsWith('/publish')) return route.fulfill({ json: { ...saved, version: 1 } });
  if (path.endsWith('/history')) return route.fulfill({ json: {
    commits: [], versions: [],
  } });
  if (path.endsWith('/restore')) return route.fulfill({ json: original });
  if (path.startsWith('/api/v1/studio/definitions/')) {
    if (path.split('/')[5] !== saved.id) return route.fulfill({ status: 404, json: { detail: 'Not found' } });
    loadStarted?.();
    if (holdLoad) await holdLoad;
    return route.fulfill({ json: { ...saved, version: Number(url.searchParams.get('version') || 0) } });
  }
  return route.fulfill({ json: [] });
});
const studio = (expression) => page.evaluate((expression) => {
  const app = window.ng.getComponent(document.querySelector('runloom-studio'));
  // Expressions are fixed test code, never user input.
  return new Function('app', `return (${expression})`)(app);
}, expression);
async function dirty(expected) {
  await page.waitForFunction((expected) =>
    window.ng.getComponent(document.querySelector('runloom-studio')).hasUnsavedChanges === expected,
  expected);
}
async function confirm(action, accept, type = 'confirm') {
  if (type === 'confirm') {
    const actionPromise = action();
    const dialog = page.locator('runloom-dialogs').getByRole('dialog');
    await dialog.waitFor();
    assert.match(await dialog.innerText(), /unsaved|unapplied/i);
    await dialog.getByTestId(accept ? 'dialog-confirm' : 'dialog-cancel').click();
    await dialog.waitFor({ state: 'hidden' });
    await actionPromise;
    return;
  }
  const dialogPromise = page.waitForEvent('dialog');
  const actionPromise = action();
  const dialog = await dialogPromise;
  assert.equal(dialog.type(), type);
  if (type === 'confirm') assert.match(dialog.message(), /unsaved|unapplied/i);
  await (accept ? dialog.accept() : dialog.dismiss());
  await actionPromise;
}
async function reset() {
  await page.locator('runloom-studio').waitFor();
  await page.waitForFunction(() => !window.ng.getComponent(document.querySelector('runloom-studio')).definitionsLoading);
  if (await studio('app.hasUnsavedChanges')) {
    await confirm(() => studio(`app.openDefinition(${JSON.stringify(saved.id)})`), true);
  } else await studio(`app.openDefinition(${JSON.stringify(saved.id)})`);
  await studio('app.edit.editWorkflow()');
  await dirty(false);
}
const name = page.getByRole('textbox', { name: 'Workflow name', exact: true });
try {
  await page.goto(base);
  await page.waitForFunction(() => window.ng?.getComponent(document.querySelector('app-root'))?.auth.ready());
  await page.evaluate(() => {
    const shell = window.ng.getComponent(document.querySelector('app-root'));
    const auth = shell.auth;
    auth.bearer = async () => 'synthetic-browser-test-token';
    auth.logout = async () => { window.location.assign('/signed-out'); };
    auth.profile.set({ user: { id: 'tester', email: 'test@example.test', name: 'Tester' }, organizations: [
      { id: 'org-1', name: 'First org', role: 'owner', teams: [
        { id: 'team-1', name: 'First team', role: 'admin' },
        { id: 'team-2', name: 'Second team', role: 'admin' },
      ] },
      { id: 'org-2', name: 'Second org', role: 'owner', teams: [
        { id: 'team-3', name: 'Third team', role: 'admin' },
      ] },
    ] });
    auth.organizationId.set('org-1');
    auth.teamId.set('team-1');
    auth.signedIn.set(true);
  });
  await page.getByRole('button', {name: 'Details for Saved workflow', exact: true}).click();
  await name.waitFor();
  await page.waitForFunction(() => document.querySelector('[aria-label="Workflow name"]').value === 'Saved workflow');
  await studio('app.edit.editWorkflow()');
  await dirty(false);
  // Modal accessibility, validation, safe dismissal and lifecycle isolation.
  await page.getByRole('button', { name: 'Projects', exact: true }).click();
  const newProject = page.getByRole('button', { name: 'New project', exact: true });
  const modal = page.locator('runloom-dialogs').getByRole('dialog');
  await newProject.click();
  const projectInput = modal.getByLabel('Project name');
  await projectInput.waitFor();
  assert.equal(await projectInput.evaluate(el => el === document.activeElement), true);
  await modal.getByTestId('dialog-confirm').click();
  await modal.getByRole('alert').waitFor();
  await projectInput.fill('x'.repeat(81));
  await modal.getByTestId('dialog-confirm').click();
  await modal.getByRole('alert').waitFor();
  await modal.getByTestId('dialog-confirm').focus();
  await page.keyboard.press('Tab');
  assert.equal(await modal.evaluate(el => el.contains(document.activeElement)), true);
  await page.keyboard.press('Escape');
  await modal.waitFor({ state: 'hidden' });
  assert.equal(await newProject.evaluate(el => el === document.activeElement), true);
  assert.equal(projects.length, 1, 'Canceled or invalid names must not create projects');
  assert.equal(await studio(`(async () => {
    const pending = app.dialogs.confirm('First decision');
    const duplicate = await app.dialogs.confirm('Must not queue');
    app.dialogs.cancel();
    return duplicate === false && await pending === false;
  })()`), true);
  console.log('PASS Angular modal focus, focus restoration, Escape, validation and duplicate cancellation');
  await page.getByRole('button', { name: 'Workflows', exact: true }).click();
  await page.getByRole('button', { name: 'Details for Saved workflow', exact: true }).click();
  await page.waitForFunction(() => {
    const app = window.ng.getComponent(document.querySelector('runloom-studio'));
    return app.builderOpen && !app.openingDefinitionId && app.definition.name === 'Saved workflow';
  });
  await studio('app.edit.editWorkflow()');
  await name.fill('Edited workflow');
  await dirty(true);
  await page.getByText('Unsaved changes', { exact: true }).waitFor();
  await confirm(() => page.getByRole('button', { name: 'Workflows', exact: true }).click(), false);
  assert.equal(await name.inputValue(), 'Edited workflow');
  await confirm(() => studio('app.loadExample()'), false);
  await confirm(() => studio("app.openDefinition('example')"), false);
  await confirm(() => studio('app.versions.openPublishedVersion(1)'), false);
  await confirm(() => studio("app.versions.restoreCommit({sequence:1,commit_id:'one',message:'Original'})"), false);
  await confirm(() => page.getByRole('button', { name: 'Teams & members', exact: true }).click(), false);
  await page.getByRole('button', { name: 'Switch organization or team', exact: true }).click();
  await confirm(() => page.getByRole('combobox', { name: 'Team', exact: true }).selectOption('team-2'), false);
  assert.equal(await page.getByRole('combobox', { name: 'Team', exact: true }).inputValue(), 'team-1');
  await confirm(() => page.getByRole('combobox', { name: 'Organization', exact: true }).selectOption('org-2'), false);
  assert.equal(await page.getByRole('combobox', { name: 'Organization', exact: true }).inputValue(), 'org-1');
  console.log('PASS cancellation preserves edits for workflow, version, restore, management, team and organization switches');

  await confirm(() => page.evaluate(() => { window.location.reload(); }), false, 'beforeunload');
  await name.click();
  await confirm(() => page.evaluate(() => { window.location.assign('about:blank'); }), false, 'beforeunload');
  await name.click();
  await confirm(() => page.close({ runBeforeUnload: true }), false, 'beforeunload');
  assert.equal(page.isClosed(), false);
  await confirm(() => page.getByRole('button', { name: 'Sign out', exact: true }).click(), false);
  await dirty(true);
  await page.evaluate(() => {
    window.ng.getComponent(document.querySelector('app-root')).auth.logout = async () => {
      throw new Error('Simulated sign-out failure');
    };
  });
  await confirm(() => page.getByRole('button', { name: 'Sign out', exact: true }).click(), true);
  await page.getByText('Simulated sign-out failure', { exact: true }).waitFor();
  await confirm(() => page.evaluate(() => { window.location.reload(); }), false, 'beforeunload');
  await page.evaluate(() => {
    window.ng.getComponent(document.querySelector('app-root')).auth.logout = async () => {
      window.location.assign('/signed-out');
    };
  });
  console.log('PASS native reload, external navigation, tab-close and sign-out warnings');
  // Failed sign-out already released the lease. Reopen the latest saved draft,
  // then make a new edit rather than silently reacquiring stale unsaved content.
  await confirm(() => studio('app.edit.editWorkflow()'), true);
  await name.fill('Edited workflow');

  await studio('app.validate()');
  await dirty(true);
  failSave = true;
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await page.getByText('Simulated save failure', { exact: true }).waitFor();
  await dirty(true);
  failSave = false;
  await studio('app.versions.save()');
  await dirty(false);
  await name.fill('Temporary edit');
  await name.fill('Edited workflow');
  await dirty(false);
  console.log('PASS validate and failed saves stay dirty; successful saves and reverting edits clear the warning');

  await page.locator('.editor-header').getByRole('button', { name: 'JSON', exact: true }).click();
  await page.getByRole('textbox', { name: 'Workflow JSON', exact: true }).fill('{');
  await dirty(true);
  await confirm(() => page.getByRole('button', { name: 'Cancel', exact: true }).click(), false);
  await page.getByRole('button', { name: 'Apply to builder', exact: true }).click();
  assert.equal(await studio('app.definition.name'), 'Edited workflow');
  await dirty(true);
  await confirm(() => page.getByRole('button', { name: 'Close JSON editor' }).click(), true);
  await dirty(false);
  await page.locator('.editor-header').getByRole('button', { name: 'JSON', exact: true }).click();
  const json = JSON.parse(await page.getByRole('textbox', { name: 'Workflow JSON', exact: true }).inputValue());
  json.name = 'JSON edit';
  await page.getByRole('textbox', { name: 'Workflow JSON', exact: true }).fill(JSON.stringify(json));
  await page.getByRole('button', { name: 'Apply to builder', exact: true }).click();
  await dirty(true);
  await studio('app.versions.save()');
  await dirty(false);
  const nodeData = page.locator('label').filter({ has: page.locator('span', { hasText: /^Input data$/ }) }).locator('textarea');
  await nodeData.fill('{');
  await dirty(true);
  assert.equal(await studio('app.versions.save()'), false);
  await nodeData.fill('{}');
  await dirty(false);
  console.log('PASS unapplied and invalid JSON is protected; applying JSON still requires saving');

  await name.fill('First edit');
  holdSave = new Promise((resolve) => { releaseSave = resolve; });
  const started = new Promise((resolve) => { saveStarted = resolve; });
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await started;
  await name.fill('Newer edit during save');
  releaseSave();
  await page.getByText('Draft committed. Newer edits are still unsaved.', { exact: true }).waitFor();
  assert.equal(await name.inputValue(), 'Newer edit during save');
  await dirty(true);
  holdSave = undefined;
  saveStarted = undefined;
  await studio('app.versions.save()');
  await dirty(false);

  holdLoad = new Promise((resolve) => { releaseLoad = resolve; });
  const loading = new Promise((resolve) => { loadStarted = resolve; });
  const opening = studio("app.openDefinition('example')");
  void opening.catch(() => undefined);
  await loading;
  await page.locator('[aria-label="Workflow name"]:disabled').waitFor();
  assert(await name.isDisabled(), 'Reopening releases the old editing session before loading');
  releaseLoad();
  await opening;
  assert.equal(await name.inputValue(), saved.name);
  await dirty(false);
  holdLoad = undefined;
  loadStarted = undefined;
  await studio('app.edit.editWorkflow()');
  console.log('PASS in-flight saves preserve newer edits and reopening is read-only until acquired');

  async function prompt(action, value) {
    const actionPromise = action();
    const dialog = page.locator('runloom-dialogs').getByRole('dialog');
    await dialog.getByLabel('Project name').fill(value);
    await dialog.getByTestId('dialog-confirm').click();
    await dialog.waitFor({ state: 'hidden' });
    await actionPromise;
  }
  await reset();
  await page.keyboard.press('Escape');
  await page.getByRole('button', { name: 'Projects', exact: true }).click();
  await prompt(() => page.getByRole('button', { name: 'New project', exact: true }).click(), 'Finance');
  const finance = page.getByRole('button', { name: 'Open project Finance', exact: true });
  await finance.waitFor();
  await page.getByRole('button', { name: 'Workflows', exact: true }).click();
  await reset();
  await name.fill('Unsaved before changing project');
  await confirm(() => page.getByRole('button', {name: 'Projects', exact: true}).click(), false);
  assert.equal(await name.inputValue(), 'Unsaved before changing project');
  await confirm(() => page.getByRole('button', {name: 'Projects', exact: true}).click(), true);
  await finance.click();
  await page.getByRole('heading', { name: 'Your project starts here', exact: true }).waitFor();
  const firstId = await studio('app.definition.id');
  await page.getByRole('button', { name: 'New workflow', exact: true }).click();
  assert.notEqual(await studio('app.definition.id'), firstId, 'New workflows need unique IDs across folders');
  await name.fill('Invoices');
  await studio("app.editor.addStep('activity')");
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await dirty(false);
  assert.equal(assignments.get(saved.id), projects[1].id, 'Save must send the selected project');
  assert(await studio("app.definitions.some(item => item.name === 'Invoices')"));
  await page.getByRole('button', {name: 'Back to workflows', exact: true}).click();
  await prompt(() => page.getByRole('button', { name: 'Rename project', exact: true }).click(), 'Operations');
  assert.equal(await page.getByLabel('Browse project', {exact: true}).locator('option:checked').textContent(), 'Operations');
  await page.getByRole('button', {name: 'Details for Invoices', exact: true}).click();
  await page.waitForFunction(() => {
    const app = window.ng.getComponent(document.querySelector('runloom-studio'));
    return app.builderOpen && !app.openingDefinitionId && app.definition.name === 'Invoices';
  });
  await studio('app.edit.editWorkflow()');
  const workflowProject = page.getByRole('combobox', { name: 'Workflow project', exact: true });
  failMove = true;
  await workflowProject.selectOption('default');
  await page.getByText('Simulated move failure', { exact: true }).waitFor();
  assert.equal(await workflowProject.inputValue(), projects[1].id);
  failMove = false;
  await workflowProject.selectOption('default');
  await page.getByText('Workflow moved to “General”.', { exact: true }).waitFor();
  await dirty(false);
  assert.equal(assignments.get(saved.id), 'default');
  await name.fill('Unsaved invoice edits');
  await workflowProject.selectOption(projects[1].id);
  await page.getByText('Workflow moved to “Operations”. Your workflow edits are still unsaved.', { exact: true }).waitFor();
  await dirty(true);
  assert.equal(await name.inputValue(), 'Unsaved invoice edits');
  await confirm(() => page.getByRole('button', {name: 'Workflows', exact: true}).click(), true);
  await page.getByLabel('Browse project', {exact: true}).selectOption(projects[1].id);
  await page.getByLabel('Search workflows', {exact: true}).fill('not-a-workflow');
  await page.getByRole('heading', { name: 'No matching workflows', exact: true }).waitFor();
  await page.getByLabel('Search workflows', {exact: true}).fill('');
  console.log('PASS project creation, rename, scoped creation, unique IDs, move errors, search and unsaved-change protection');

  await reset();
  // Management scheduling is covered by schedules-dashboard-smoke.mjs.
  await studio("app.editor.addStep('subworkflow')");
  assert.equal(await page.getByRole('combobox', { name: 'Subworkflow', exact: true }).locator(`option[value="${saved.id}"]`).count(), 0);
  await page.getByRole('combobox', { name: 'Subworkflow', exact: true }).selectOption('sample-child');
  await page.getByRole('spinbutton', { name: 'Subworkflow version' }).fill('1');
  await page.getByRole('textbox', { name: 'Subworkflow input' }).fill('{"value":"${input.value}"}');
  await dirty(true);
  assert.equal(await studio('app.editor.selectedStep.definition_id'), 'sample-child');
  assert.deepEqual(await studio('app.editor.selectedStep.with'), { value: '${input.value}' });
  await reset();
  console.log('PASS pinned subworkflow controls');

  const beforeChildTest = structuredClone(saved);
  await page.locator('.trigger-node').click();
  await page.getByRole('combobox', { name: 'Workflow kind' }).selectOption('child');
  assert.equal(await studio('app.definition.kind'), 'child');
  await dirty(true);
  await page.getByRole('button', { name: 'Test child workflow', exact: true }).click();
  assert.equal(await page.getByRole('button', { name: 'Start new test', exact: true }).isDisabled(), false);
  await page.getByText('No publishing needed', { exact: true }).waitFor();
  assert.equal(childTests.length, 0);
  await page.getByRole('textbox', { name: 'Test input', exact: true }).fill('{"name":"Avery"}');
  await page.getByRole('button', { name: 'Start new test', exact: true }).click();
  await page.getByText('Test execution — real activities and side effects.', { exact: true }).waitFor();
  await page.getByText('Run finished', { exact: true }).waitFor();
  await page.getByRole('button', { name: 'View all runs in Management', exact: true }).waitFor();
  assert.equal(childTests.length, 1);
  assert.equal(childTests[0].path, `/api/v1/studio/definitions/${saved.id}/test`);
  assert.deepEqual(childTests[0].body.input, { name: 'Avery' });
  assert.equal(childTests[0].body.definition.version, 0);
  assert.equal(childTests[0].body.definition.kind, 'child');
  assert.deepEqual(saved, beforeChildTest, 'Tests must not save or publish unsaved changes');
  await dirty(true);
  assert.equal(childTests[0].headers['x-runloom-team'], 'team-1');
  assert.equal(productionStarts, 0, 'Child tests must not use the production start endpoint');
  assert.equal(await studio('app.definition.kind'), 'child');
  await page.getByRole('button', { name: 'Close test panel', exact: true }).click();
  saved = beforeChildTest;
  await reset();
  console.log('PASS unpublished child tests, unsaved snapshot, test labels and separate endpoint');

  for (const type of ['manual', 'webhook']) {
    await studio(`(app.definition.kind = 'main', app.definition.trigger = ${JSON.stringify(type === 'manual' ? { type } : { type, method: 'POST', path: 'draft-webhook' })}, app.markChanged())`);
    await page.getByRole('button', { name: 'Test workflow', exact: true }).click();
    await page.getByRole('button', { name: 'Start new test', exact: true }).click();
    await page.getByText('Run finished', { exact: true }).waitFor();
    assert.equal(childTests.at(-1).body.definition.version, 0);
    assert.equal(childTests.at(-1).body.definition.trigger.type, type);
    assert.equal(productionStarts, 0);
    assert.deepEqual(saved, beforeChildTest);
    await dirty(true);
    await page.getByRole('button', { name: 'Close test panel', exact: true }).click();
    await reset();
  }
  console.log('PASS unpublished main/manual and webhook snapshots without save or publish');

  await studio("app.testRun.openRelatedRun('mock-parent')");
  await page.getByRole('button', { name: '↳ Open child: call' }).click();
  await page.getByRole('button', { name: '↰ Open parent run' }).waitFor();
  assert.equal(await studio('app.testRun.run.workflow_id'), 'mock-child');
  await page.getByRole('button', { name: '↰ Open parent run' }).click();
  await page.getByRole('button', { name: '↳ Open child: call' }).waitFor();
  let releaseParent;
  holdParentStatus = new Promise((resolve) => { releaseParent = resolve; });
  const parentStarted = new Promise((resolve) => { parentStatusStarted = resolve; });
  const oldPoll = studio('app.testRun.pollRun()');
  await parentStarted;
  await page.getByRole('button', { name: '↳ Open child: call' }).click();
  await page.getByRole('button', { name: '↰ Open parent run' }).waitFor();
  releaseParent();
  await oldPoll;
  holdParentStatus = undefined;
  assert.equal(await studio('app.testRun.run.workflow_id'), 'mock-child', 'A late parent response must not overwrite the selected child');
  await studio('app.testRun.runOpen = false');
  console.log('PASS parent/child navigation and protection against stale run responses');

  await reset();
  await studio('app.versions.publish()');
  await dirty(false);
  await name.fill('Unsaved published edit');
  await dirty(true);
  if (!(await page.getByRole('combobox', { name: 'Team', exact: true }).isVisible())) {
    await page.getByRole('button', { name: 'Switch organization or team', exact: true }).click();
  }
  await confirm(() => page.getByRole('combobox', { name: 'Team', exact: true }).selectOption('team-2'), true);
  await dirty(false);
  assert.equal(await page.getByRole('combobox', { name: 'Team', exact: true }).inputValue(), 'team-2');
  await page.getByRole('button', { name: 'Teams & members', exact: true }).click();
  await page.locator('runloom-studio').waitFor({ state: 'detached' });
  await page.getByRole('button', { name: 'Workflows', exact: true }).click();
  await page.getByLabel('Browse project', {exact: true}).selectOption(assignments.get(saved.id) || 'default');
  await page.getByRole('button', {name: `Details for ${saved.name}`, exact: true}).click();
  await name.waitFor();
  await dirty(false);
  assert.equal(await page.evaluate(() => {
    const event = new Event('beforeunload', { cancelable: true });
    window.dispatchEvent(event);
    return event.defaultPrevented;
  }), false);
  await studio('app.edit.editWorkflow()');
  await name.fill('Last edit before sign-out');
  const signOutDialogs = [];
  const signOutDialog = async (dialog) => {
    signOutDialogs.push(dialog.type());
    await dialog.accept();
  };
  page.on('dialog', signOutDialog);
  await confirm(() => page.getByRole('button', { name: 'Sign out', exact: true }).click(), true);
  await page.waitForURL('**/signed-out');
  page.off('dialog', signOutDialog);
  assert.deepEqual(signOutDialogs, [], 'Confirmed sign-out should not show native dialogs');
  const unexpected = [];
  const unexpectedDialog = async (dialog) => { unexpected.push(dialog.type()); await dialog.dismiss(); };
  page.on('dialog', unexpectedDialog);
  await page.goto('about:blank');
  assert.deepEqual(unexpected, []);
  assert.deepEqual(errors, []);
  console.log('PASS publishing clears dirty state, confirmed switches discard, clean navigation has no warning');
  console.log('PASS canceled or failed sign-out preserves edits; confirmed sign-out prompts only once');
  console.log('All unsaved-changes browser checks passed.');
} finally {
  releaseSave?.();
  releaseLoad?.();
  await browser.close();
}
