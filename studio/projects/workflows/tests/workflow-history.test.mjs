import assert from 'node:assert/strict';
import test from 'node:test';
import {loadTypeScript} from './helpers/load-typescript.mjs';
const {WorkflowHistory, isTextEditingTarget} = loadTypeScript(new URL('../src/app/features/builder/state/workflow-history.ts', import.meta.url));

test('undo/redo preserves baseline, branches history and bounds memory', () => {
  const h = new WorkflowHistory('initial', 2);
  h.record('add'); h.record('move');
  assert.equal(h.undo(), 'add');
  assert.equal(h.redo(), 'move');
  assert.equal(h.undo(), 'add');
  h.record('delete');
  assert.equal(h.canRedo, false);
  h.record('rename');
  assert.equal(h.undo(), 'delete');
  assert.equal(h.undo(), 'add');
  assert.equal(h.undo(), undefined);
  h.reset('other workflow');
  assert.equal(h.canUndo, false);
  assert.equal(h.canRedo, false);
});

test('typing groups retain the pre-typing snapshot; separate actions and undo end grouping', () => {
  const h = new WorkflowHistory('before'), field = {};
  h.record('a', field, 1000); h.record('ab', field, 1100);
  assert.equal(h.undo(), 'before');
  assert.equal(h.redo(), 'ab');
  h.record('abc', field, 1200);
  assert.equal(h.undo(), 'ab');
  h.record('node');
  assert.equal(h.undo(), 'ab');
});

test('text controls and embedded editors retain native undo', () => {
  for (const tagName of ['INPUT', 'TEXTAREA', 'SELECT']) assert(isTextEditingTarget({tagName}));
  assert(isTextEditingTarget({isContentEditable: true}));
  assert(isTextEditingTarget({tagName: 'DIV', closest: () => ({})}));
  assert.equal(isTextEditingTarget({tagName: 'BUTTON'}), false);
});

test('autosave debounces, fences navigation, waits for blockers and stops after failure', async t => {
  t.mock.timers.enable({apis: ['setTimeout']});
  const {WorkflowAutosave} = loadTypeScript(new URL('../src/app/features/builder/state/workflow-autosave.ts', import.meta.url));
  let writes = 0;
  const ctx = {eligible: true, dirty: true, busy: false, blocked: false, generation: 1,
    save: async () => { writes++; ctx.dirty = false; return true; }, markForCheck() {}};
  const auto = new WorkflowAutosave(ctx);
  auto.changed(); t.mock.timers.tick(900); auto.changed(); t.mock.timers.tick(900);
  assert.equal(writes, 0);
  ctx.blocked = true; t.mock.timers.tick(100);
  assert.equal(writes, 0);
  ctx.blocked = false; t.mock.timers.tick(1000); await new Promise(resolve => setImmediate(resolve));
  assert.equal(writes, 1);
  ctx.dirty = true; auto.changed(); ctx.generation++; t.mock.timers.tick(1000);
  assert.equal(writes, 1);
  ctx.save = async () => { writes++; return false; };
  auto.changed(); t.mock.timers.tick(1000); await new Promise(resolve => setImmediate(resolve));
  assert.equal(auto.failed, true);
  t.mock.timers.tick(5000); assert.equal(writes, 2);
  await auto.retry(); assert.equal(writes, 3);
  auto.changed(); ctx.eligible = false; t.mock.timers.tick(1000); assert.equal(writes, 3);
  ctx.eligible = true; auto.changed(); auto.dispose(); t.mock.timers.tick(1000); assert.equal(writes, 3);
});

test('autosave serializes writers and follows successful writes with newer edits', async t => {
  t.mock.timers.enable({apis: ['setTimeout']});
  const {WorkflowAutosave} = loadTypeScript(new URL('../src/app/features/builder/state/workflow-autosave.ts', import.meta.url));
  let writes = 0;
  const ctx = {eligible: true, dirty: true, busy: true, blocked: false, generation: 1,
    save: async () => { writes++; return true; }, markForCheck() {}};
  const auto = new WorkflowAutosave(ctx);
  auto.changed(); t.mock.timers.tick(1000); assert.equal(writes, 0);
  ctx.busy = false; t.mock.timers.tick(1000); await new Promise(resolve => setImmediate(resolve)); assert.equal(writes, 1);
  ctx.dirty = false; t.mock.timers.tick(1000); assert.equal(writes, 1);
  auto.dispose();
});
