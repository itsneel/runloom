import assert from 'node:assert/strict';
import test from 'node:test';
import { loadTypeScript } from './helpers/load-typescript.mjs';

const feature = '../src/app/features/builder/';
const load = (name) => loadTypeScript(new URL(feature + name, import.meta.url));
const { WorkflowEditor } = load('state/workflow-editor.ts');
const { WorkflowEditLock } = load('state/workflow-edit-lock.ts');
const { WorkflowTestRun } = load('state/workflow-test-run.ts');
const { emptyDefinition } = load('model/workflow-defaults.ts');
const json = (value) => JSON.parse(JSON.stringify(value));
const deferred = () => {
  let resolve, reject;
  const promise = new Promise((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
};
function context(overrides = {}) {
  const notices = [];
  return {
    definition: emptyDefinition(),
    canEditWorkflow: true,
    canEdit: () => true,
    saving: false,
    projectBusy: false,
    documentGeneration: 0,
    destroyed: false,
    isNewWorkflow: true,
    builderOpen: true,
    definitions: [],
    publishedChildren: [],
    webhookUrl: '',
    changes: 0,
    notices,
    markChanged() {
      this.changes++;
    },
    markForCheck() {},
    notify: (...args) => notices.push(args),
    handleError: (error) => notices.push([error.message, true]),
    errorText: (error) => error.message,
    confirm: async () => true,
    confirmDiscardChanges: async () => true,
    canSaveEditors: () => true,
    replaceDefinition(value) {
      this.definition = value;
      this.documentGeneration++;
    },
    openDefinition: async () => {},
    ...overrides,
  };
}

test('editor changes the authoritative draft, preserves selection and duplicate IDs', () => {
  const ctx = context(),
    editor = new WorkflowEditor(ctx);
  editor.openNodePicker();
  editor.addStep('parallel');
  const parallel = editor.selectedStep;
  assert.equal(ctx.definition.steps[0], parallel);
  editor.addParallelBranch(parallel);
  assert.equal(parallel.branches.length, 3);
  editor.openNodePicker(0, parallel.branches[2].steps);
  editor.addStep('activity');
  const original = editor.selectedStep;
  editor.duplicateStep(0, parallel.branches[2].steps);
  assert.notEqual(editor.selectedStep.id, original.id);
  assert.equal(editor.selectedSteps, parallel.branches[2].steps);
  editor.updateCanvasLayout({ [original.id]: { x: 100, y: 80 } });
  assert.equal(ctx.definition.layout.positions[original.id].x, 100);
  assert(ctx.changes >= 5);
});

test('graph guards reject cycles, End in parallel paths and nested AI decisions', () => {
  const ctx = context(),
    editor = new WorkflowEditor(ctx);
  editor.openNodePicker();
  editor.addStep('parallel');
  const parallel = ctx.definition.steps[0];
  editor.moveGraphNode({
    from: { steps: ctx.definition.steps, index: 0 },
    to: { steps: parallel.branches[0].steps, index: 0 },
  });
  assert.equal(ctx.definition.steps[0], parallel);
  assert.match(ctx.notices.at(-1)[0], /own branch/);
  editor.openNodePicker(parallel.branches[0].steps.length, parallel.branches[0].steps);
  editor.addStep('end');
  assert.match(ctx.notices.at(-1)[0], /Place End last/);
  editor.openNodePicker();
  editor.addStep('decision');
  const decision = editor.selectedStep;
  editor.openNodePicker(0, decision.branches[0].steps);
  editor.addStep('decision');
  assert.match(ctx.notices.at(-1)[0], /cannot be nested/);
  editor.openNodePicker();
  editor.addStep('end');
  editor.openNodePicker();
  editor.addStep('activity');
  assert.match(ctx.notices.at(-1)[0], /No nodes can follow End/);
});

test('invalid node/schema drafts survive until corrected or the node is removed', () => {
  const ctx = context(),
    editor = new WorkflowEditor(ctx);
  editor.openNodePicker();
  editor.addStep('http');
  const step = editor.selectedStep,
    original = json(step.http);
  editor.setHTTPData('{');
  assert.equal(editor.httpDataText(step), '{');
  assert.deepEqual(json(step.http), original);
  editor.setSchema(step, '[]');
  assert.equal(editor.schemaText(step), '[]');
  editor.setSchema(step, '{"type":"object"}');
  assert.equal(editor.schemaDrafts.size, 0);
  editor.setHTTPData('{"url":"https://example.test","method":"GET"}');
  assert.equal(editor.stepDataDrafts.size, 0);
  editor.setHTTPData('{');
  editor.removeStep(0);
  assert.equal(editor.stepDataDrafts.size, 0);
  assert.equal(editor.selectedStep, undefined);
});

test('a branch removal confirmation cannot delete a changed document', async () => {
  const wait = deferred(),
    ctx = context({ confirm: () => wait.promise }),
    editor = new WorkflowEditor(ctx);
  editor.openNodePicker();
  editor.addStep('parallel');
  const step = editor.selectedStep;
  editor.addParallelBranch(step);
  const removing = editor.removeBranch(step, 0);
  ctx.documentGeneration++;
  wait.resolve(true);
  await removing;
  assert.equal(step.branches.length, 3);
});

test('lease acquisition fences navigation and releases the abandoned token', async () => {
  const wait = deferred(),
    calls = [],
    ctx = context();
  const api = {
    setEditToken() {},
    editLock: async (...args) => {
      calls.push(args);
      return { locked: true, ttl_seconds: 60 };
    },
    getDefinition: () => wait.promise,
  };
  const lease = new WorkflowEditLock(api, ctx);
  const opening = lease.editWorkflow();
  await new Promise((resolve) => setImmediate(resolve));
  ctx.documentGeneration++;
  wait.resolve(emptyDefinition());
  await opening;
  assert.equal(lease.ownsEdit, false);
  assert.equal(lease.acquiringEdit, false);
  assert(calls.some(([, method]) => method === 'DELETE'));
});

test('new draft collisions and lease renewal failures cannot enable saving', async () => {
  const ctx = context(),
    tokens = [];
  const api = {
    setEditToken: (...args) => tokens.push(args),
    editLock: async () => ({ locked: true, ttl_seconds: 600 }),
    getDefinition: async () => ctx.definition,
  };
  const lease = new WorkflowEditLock(api, ctx);
  assert.equal(await lease.ensureNewDraftLock(), false);
  assert.match(ctx.notices.at(-1)[0], /already exists/);
  assert.equal(tokens.length, 0);
  api.getDefinition = async () => {
    throw { status: 404 };
  };
  assert.equal(await lease.ensureNewDraftLock(), true);
  assert(lease.editUntil <= Date.now() + 60000, 'lease is conservatively capped');
  api.editLock = async () => {
    throw new Error('offline');
  };
  await lease.refreshEditLock();
  assert.equal(lease.canEditWorkflow, false);
  assert.equal(lease.editingInterrupted, true);
  assert.match(ctx.notices.at(-1)[0], /changes are kept/);
  await lease.releaseEditing();
});

test('testing uses a detached unpublished draft and ignores stale start responses', async () => {
  const wait = deferred(),
    ctx = context({ isNewWorkflow: false });
  ctx.definition.version = 8;
  let submitted;
  const run = new WorkflowTestRun(
    {
      testDraft: (definition, input) => {
        submitted = { definition, input };
        return wait.promise;
      },
    },
    ctx,
  );
  run.runInput = '{"name":"Avery"}';
  const starting = run.startRun();
  assert.equal(submitted.definition.version, 0);
  assert.equal(ctx.definition.version, 8);
  assert.notEqual(submitted.definition, ctx.definition);
  assert.equal(submitted.input.name, 'Avery');
  ctx.documentGeneration++;
  wait.resolve({ workflow_id: 'old' });
  await starting;
  assert.equal(run.run, undefined);
  assert.equal(run.startingTest, false);
});

test('run polling cannot overwrite a newer run; budget resume has no override payload', async () => {
  const wait = deferred(),
    ctx = context(),
    calls = [];
  const api = {
    runStatus: () => wait.promise,
    resume: async (...args) => calls.push(args),
  };
  const run = new WorkflowTestRun(api, ctx);
  run.run = { workflow_id: 'old' };
  const polling = run.pollRun();
  run.run = { workflow_id: 'new', state: { paused_step: { step_id: 'llm', budget: {} } } };
  wait.resolve({ workflow_id: 'old', temporal_status: 'completed' });
  await polling;
  assert.equal(run.run.workflow_id, 'new');
  run.resumePayload = '{"bypass":true}';
  run.pollRun = async () => {};
  await run.resume();
  assert.deepEqual(json(calls), [['new', 'llm', {}]]);
});

test('handler action paths accept inserted nodes and remain separate from the main sequence', () => {
  const ctx = context();
  ctx.definition.signal_handlers = [
    { id: 'on-change', signal_name: 'customer.updated', steps: [{ id: 'return', type: 'end' }] },
  ];
  const editor = new WorkflowEditor(ctx),
    path = ctx.definition.signal_handlers[0].steps;
  editor.openNodePicker(0, path);
  editor.addStep('wait');
  assert.equal(path.length, 2);
  assert.equal(path[0].type, 'wait');
  assert.equal(editor.selectedSteps, path);
  assert(!ctx.definition.steps.includes(path[0]));
});

test('handler event retries retain their id after a delivery failure and new successful sends get a new id', async () => {
  const calls = [],
    ctx = context();
  let fail = true;
  const run = new WorkflowTestRun(
    {
      async signal(...args) {
        calls.push(args);
        if (fail) throw new Error('Delivery interrupted');
      },
    },
    ctx,
  );
  run.pollRun = async () => {};
  run.run = { workflow_id: 'handler-run', temporal_status: 'running' };
  run.handlerPayloads['customer.updated'] = '{"value":1}';
  await run.sendHandlerSignal('customer.updated');
  assert.equal(calls.length, 1);
  fail = false;
  await run.sendHandlerSignal('customer.updated');
  assert.equal(calls[0][3], calls[1][3]);
  await run.sendHandlerSignal('customer.updated');
  assert.notEqual(calls[1][3], calls[2][3]);
  run.handlerPayloads['customer.updated'] = '{';
  await run.sendHandlerSignal('customer.updated');
  assert.equal(calls.length, 3);
  assert.equal(run.handlerSending.size, 0);
});
