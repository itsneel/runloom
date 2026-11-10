import test from 'node:test';
import assert from 'node:assert/strict';
import { loadTypeScript } from './helpers/load-typescript.mjs';
const { organizationOptions } = loadTypeScript(
  new URL('../src/app/layout/shell/organization-options.ts', import.meta.url),
);
const organizations = [
  { id: 'runloom', name: 'Runloom' },
  { id: 'child', name: 'Child', parent_id: 'runloom' },
  { id: 'sibling', name: 'Sibling', parent_id: 'runloom' },
  { id: 'other', name: 'Other parent' },
  { id: 'other-child', name: 'Other child', parent_id: 'other' },
];
test('the organization switcher never mixes independently administered families', () => {
  for (const selected of ['runloom', 'child', 'sibling']) {
    assert.equal(
      JSON.stringify(organizationOptions(organizations, selected).map((o) => o.id)),
      JSON.stringify(['runloom', 'child', 'sibling']),
    );
  }
  assert.equal(
    JSON.stringify(organizationOptions(organizations, 'other').map((o) => o.id)),
    JSON.stringify(['other', 'other-child']),
  );
});
test('metadata-only parents are not inserted and an invalid selection returns no choices', () => {
  const childMemberships = organizations.filter((o) => o.id !== 'runloom');
  assert.equal(
    JSON.stringify(organizationOptions(childMemberships, 'child').map((o) => o.id)),
    JSON.stringify(['child', 'sibling']),
  );
  assert.equal(organizationOptions(organizations, 'missing').length, 0);
  assert.equal(organizationOptions(organizations, '').length, 0);
});
