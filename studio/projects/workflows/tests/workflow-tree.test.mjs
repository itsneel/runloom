import assert from 'node:assert/strict';
import test from 'node:test';
import { allSteps, availableID, containsPath, decisionPath, duplicateTree, parallelPath } from '../src/app/features/builder/model/workflow-tree.ts';

const fixture = () => [{ id: 'fork', type: 'parallel', branches: [
  { id: 'a', steps: [{ id: 'decision', type: 'condition', then: [{ id: 'inner', type: 'activity' }], else: [] }] },
  { id: 'b', steps: [{ id: 'next', type: 'activity', with: { result: '${steps.inner.output}', external: '${steps.other.output}' } }] },
] }];
test('traversal includes nested decisions and parallel paths', () => {
  const root = fixture();
  assert.deepEqual(allSteps(root).map(node => node.id), ['fork', 'decision', 'inner', 'next']);
  assert(containsPath(root[0], root[0].branches[0].steps[0].then));
  assert(!containsPath(root[0], root));
  assert(parallelPath(root, root[0].branches[0].steps[0].then));
  assert(!parallelPath(root, root));
});
test('copying a fork regenerates every nested ID and internal references', () => {
  const root = fixture(), original = structuredClone(root);
  const copy = duplicateTree(root[0], root);
  assert.deepEqual(root, original);
  const ids = allSteps([...root, copy]).map(step => step.id);
  assert.equal(new Set(ids).size, ids.length);
  assert.equal(copy.branches[1].steps[0].with.result, '${steps.inner-2.output}');
  assert.equal(copy.branches[1].steps[0].with.external, '${steps.other.output}');
});
test('long IDs terminate and remain valid when allocating repeated copies', () => {
  const ids = new Set(), candidate = 'a'.repeat(64);
  for (let i = 0; i < 500; i++) assert.match(availableID(candidate, ids), /^[a-z][a-z0-9-]{0,63}$/);
  assert.equal(ids.size, 500);
});
test('new composite nodes avoid collisions with previously moved descendants', () => {
  const existing = [{id: 'decision-yes', type: 'activity'}];
  const created = duplicateTree({id: 'decision', name: 'Choose route', type: 'condition', then: [{id: 'decision-yes', type: 'activity'}], else: []}, existing, false);
  assert.equal(created.name, 'Choose route');
  assert.equal(created.then[0].id, 'decision-yes-2');
});

test('AI action paths participate in copying, traversal and End restrictions', () => {
  const root=[{id:'route',type:'decision',decision:{mode:'agent'},branches:[{id:'lookup',description:'Look up',steps:[{id:'lookup-node',type:'activity',with:{round:'${decision.round}'}}]}]}];
  assert(parallelPath(root,root[0].branches[0].steps));
  assert(decisionPath(root,root[0].branches[0].steps));
  assert(!decisionPath(root,root));
  const parallel=fixture();
  assert(!decisionPath(parallel,parallel[0].branches[0].steps));
  const copy=duplicateTree(root[0],root);
  assert.equal(copy.branches[0].id,'lookup');
  assert.equal(copy.branches[0].description,'Look up');
  assert.notEqual(copy.branches[0].steps[0].id,'lookup-node');
  assert.equal(copy.branches[0].steps[0].with.round,'${decision.round}');
});
