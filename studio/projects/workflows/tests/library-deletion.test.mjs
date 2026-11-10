import assert from 'node:assert/strict';
import test from 'node:test';
import {loadTypeScript} from './helpers/load-typescript.mjs';

const {LibraryDeletion} = loadTypeScript(new URL('../src/app/features/builder/state/library-deletion.ts', import.meta.url));
function fixture() {
  const calls = [];
  const context = {
    canDelete: true, destroyed: false, generation: 1, busy: false,
    projects: [{id: 'default', name: 'General'}, {id: 'empty', name: 'Empty'}, {id: 'full', name: 'Full'}],
    workflows: [{id: 'draft', name: 'Draft', has_draft: true, latest_version: null, project_id: 'full'}, {id: 'published', has_draft: true, latest_version: 1}],
    confirm: async () => true, reload: async () => calls.push('reload'),
    projectDeleted: id => { calls.push(`selected:${id}`); context.generation++; },
    notify: message => calls.push(`notice:${message}`), handleError: error => calls.push(`error:${error.message}`), markForCheck() {},
  };
  const api = {deleteProject: async id => calls.push(`project:${id}`), deleteDraftWorkflow: async id => calls.push(`workflow:${id}`)};
  return {context, api, calls, action: new LibraryDeletion(api, context)};
}
test('only empty non-default projects and never-published saved drafts are deletable', async () => {
  const {action, calls, context} = fixture();
  for (const id of ['default', 'full', 'missing']) await action.project(id);
  await action.workflow('published');
  await action.workflow('missing');
  assert.deepEqual(calls, []);
  context.workflows.push({id: 'history', has_draft: true, latest_version: 0, published_versions: [1]});
  await action.workflow('history');
  assert.deepEqual(calls, []);
  await action.project('empty');
  assert.deepEqual(calls, ['project:empty', 'selected:empty', 'reload', 'notice:Project “Empty” deleted.']);
  calls.length = 0;
  await action.workflow('draft');
  assert.deepEqual(calls, ['workflow:draft', 'reload', 'notice:Draft workflow “Draft” deleted.']);
});
test('cancellation and read-only access never send deletion requests', async () => {
  const {action, context, calls} = fixture();
  context.confirm = async () => false;
  await action.workflow('draft');
  assert.equal(context.busy, false);
  context.confirm = async () => true;
  context.canDelete = false;
  await action.project('empty');
  assert.deepEqual(calls, []);
});
test('duplicate clicks are fenced and scope changes invalidate pending confirmations', async () => {
  const {action, context, calls} = fixture();
  let resolve;
  context.confirm = () => new Promise(done => { resolve = done; });
  const pending = action.workflow('draft');
  assert.equal(context.busy, true);
  await action.workflow('draft');
  context.generation++;
  resolve(true);
  await pending;
  assert.deepEqual(calls, []);
  assert.equal(context.busy, false);
});
test('server conflicts retain the list and report the actual error', async () => {
  const {action, api, context, calls} = fixture();
  api.deleteDraftWorkflow = async () => { throw new Error('Workflow was published'); };
  await action.workflow('draft');
  assert.deepEqual(calls, ['error:Workflow was published']);
  assert.equal(context.workflows[0].id, 'draft');
  assert.equal(context.busy, false);
});
