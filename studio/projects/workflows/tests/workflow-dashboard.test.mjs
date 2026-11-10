import assert from 'node:assert/strict';
import test from 'node:test';
import { filterWorkflows } from '../src/app/features/dashboard/workflow-dashboard.model.ts';

const items = [
  {id: 'billing', name: 'Billing', latest_version: 2, has_draft: true},
  {id: 'parent', name: 'Approval', latest_version: 1, has_draft: false},
  {id: 'onboarding', name: 'Welcome', latest_version: 0, has_draft: true},
];

test('dashboard sorts by name without mutating summaries', () => {
  assert.deepEqual(filterWorkflows(items, '', 'all').map(item => item.id), ['parent', 'billing', 'onboarding']);
  assert.equal(items[0].id, 'billing');
});
test('search matches names and IDs, ignores case and outer whitespace', () => {
  assert.deepEqual(filterWorkflows(items, ' APPROVAL ', 'all').map(item => item.id), ['parent']);
  assert.deepEqual(filterWorkflows(items, 'onboard', 'all').map(item => item.id), ['onboarding']);
  assert.deepEqual(filterWorkflows(items, 'missing', 'all'), []);
});
test('saved drafts and published versions can coexist', () => {
  assert.deepEqual(filterWorkflows(items, '', 'published').map(item => item.id), ['parent', 'billing']);
  assert.deepEqual(filterWorkflows(items, '', 'draft').map(item => item.id), ['billing', 'onboarding']);
  assert.deepEqual(filterWorkflows(items, 'welcome', 'published'), []);
});
