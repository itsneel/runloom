import assert from 'node:assert/strict';
import test from 'node:test';
import {loadTypeScript} from './helpers/load-typescript.mjs';

const {WorkflowExamples} = loadTypeScript(new URL('../src/app/features/builder/state/workflow-examples.ts', import.meta.url));
const json = value => JSON.parse(JSON.stringify(value));

function fixture(overrides = {}) {
  const calls = [], tokens = new Map(), saved = [], notices = [];
  const context = {
    destroyed: false, generation: 0, projectId: 'sample-project', busy: false,
    canCreate: () => true, confirm: async () => true, reload: async () => {},
    accept: (...args) => calls.push(['accept', ...json(args)]),
    notify: (...args) => notices.push(args), handleError() {}, errorText: error => error.message, markForCheck() {},
    ...overrides,
  };
  const api = {
    editLock: async (id, method, token) => { calls.push(['lock', id, method, token]); },
    getDefinition: async () => { throw {status: 404}; },
    setEditToken: (id, token) => token ? tokens.set(id, token) : tokens.delete(id),
    save: async (definition, _message, project) => {
      assert.ok(tokens.has(definition.id), 'sample writes must carry an edit lease');
      assert.equal(project, 'sample-project');
      saved.push(json(definition)); calls.push(['save', definition.id]);
      return json(definition);
    },
    publish: async id => {
      assert.ok(tokens.has(id)); calls.push(['publish', id]);
      return {...saved.find(item => item.id === id), version: 3};
    },
  };
  return {context, api, calls, saved, notices, tokens, controller: new WorkflowExamples(api, context)};
}

test('guided sample publishes only the child, pins the returned version and saves the parent draft in the selected project', async () => {
  const f = fixture();
  await f.controller.createParallel();
  assert.equal(f.saved.length, 2);
  const [child, parent] = f.saved;
  assert.equal(child.kind, 'child');
  assert.equal(child.steps[0].type, 'wait');
  assert.equal(child.steps[0].seconds, 5);
  assert.equal(parent.kind, 'main');
  assert.equal(parent.version, 0);
  assert.deepEqual(f.calls.filter(c => c[0] === 'publish'), [['publish', child.id]]);
  assert.equal(parent.steps[0].type, 'parallel');
  assert.equal(parent.steps[0].branches.length, 2);
  for (const branch of parent.steps[0].branches) {
    assert.equal(branch.steps[0].type, 'subworkflow');
    assert.equal(branch.steps[0].definition_id, child.id);
    assert.equal(branch.steps[0].version, 3);
  }
  assert.equal(parent.steps.at(-1).type, 'end');
  const accepted = f.calls.find(c => c[0] === 'accept');
  assert.deepEqual(accepted[2], {customer: 'Avery', reviewer: 'Jordan'});
  assert.equal(accepted[3], 'sample-project');
  assert.equal(f.tokens.size, 0);
  assert.equal(f.calls.filter(c => c[0] === 'lock' && c[2] === 'DELETE').length, 2);
  assert.equal(f.context.busy, false);
  assert.match(f.notices.at(-1)[0], /parent is still a draft/);
});

test('cancel, missing permissions and a changed project during confirmation create nothing', async () => {
  const canceled = fixture({confirm: async () => false});
  const denied = fixture({canCreate: () => false});
  const changed = fixture();
  changed.context.confirm = async () => { changed.context.projectId = 'other-project'; return true; };
  for (const f of [canceled, denied, changed]) {
    await f.controller.createParallel();
    assert.equal(f.calls.length, 0);
    assert.equal(f.context.busy, false);
  }
});

test('existing IDs and publish failures preserve existing workflows and release leases', async () => {
  const collision = fixture();
  collision.api.getDefinition = async () => ({id: 'existing'});
  await collision.controller.createParallel();
  assert.equal(collision.saved.length, 0);
  assert.match(collision.notices.at(-1)[0], /already exists/);
  assert.equal(collision.tokens.size, 0);
  const publishedOnly = fixture();
  publishedOnly.api.getDefinition = async (_id, version) => {
    if (version === 0) throw {status: 404};
    return {id: 'existing-published', version: 1};
  };
  await publishedOnly.controller.createParallel();
  assert.equal(publishedOnly.saved.length, 0);
  assert.match(publishedOnly.notices.at(-1)[0], /already exists/);
  assert.equal(publishedOnly.tokens.size, 0);
  const failed = fixture();
  failed.api.publish = async () => { throw new Error('Publishing unavailable'); };
  await failed.controller.createParallel();
  assert.equal(failed.saved.length, 1);
  assert.match(failed.notices.at(-1)[0], /Publishing unavailable.*already saved were kept/);
  assert.ok(!failed.calls.some(c => c[0] === 'accept'));
  assert.equal(failed.tokens.size, 0);
  assert.equal(failed.context.busy, false);
});

test('destroying the workspace during save stops publication and parent creation', async () => {
  const f = fixture();
  const save = f.api.save;
  f.api.save = async (...args) => { const value = await save(...args); f.context.destroyed = true; return value; };
  await f.controller.createParallel();
  assert.equal(f.saved.length, 1);
  assert.ok(!f.calls.some(c => ['publish', 'accept'].includes(c[0])));
  assert.equal(f.tokens.size, 0);
  assert.equal(f.context.busy, false);
});

test('repeat sample creation uses new IDs without republishing old workflows', async () => {
  const f = fixture();
  await f.controller.createParallel();
  await f.controller.createParallel();
  assert.equal(new Set(f.saved.map(item => item.id)).size, 4);
});
