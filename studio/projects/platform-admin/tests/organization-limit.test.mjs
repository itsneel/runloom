import test from 'node:test';
import assert from 'node:assert/strict';
import { loadTypeScript } from './helpers/load-typescript.mjs';
const { OrganizationLimitEditor } = loadTypeScript(
  new URL('../src/app/features/platform/state/organization-limit-editor.ts', import.meta.url),
);
const organization = {
  id: 'root',
  name: 'Root',
  child_count: 1,
  child_limit: { max_children: 2, default_max_children: 2, override: null, maximum: 10 },
};
function fixture(save, allowed = true) {
  const calls = [],
    notices = [];
  const state = new OrganizationLimitEditor({
    canEdit: () => allowed,
    save: async (...args) => {
      calls.push(args);
      return save
        ? save(...args)
        : { ...organization.child_limit, max_children: args[1] ?? 2, override: args[1] ?? null };
    },
    saved: () => notices.push('saved'),
  });
  state.load(organization);
  return { state, calls, notices };
}
test('zero is an explicit organization override; default mode resets rather than copies the default', async () => {
  const { state, calls, notices } = fixture();
  state.edit();
  state.mode = 'custom';
  state.limit = 0;
  await state.save();
  assert.equal(calls[0][0], 'root');
  assert.equal(calls[0][1], 0);
  assert.equal(state.organization.child_limit.override, 0);
  state.edit();
  state.mode = 'default';
  await state.save();
  assert.equal(calls[1][1], undefined);
  assert.equal(state.organization.child_limit.override, null);
  assert.equal(notices.length, 2);
});
test('staff cannot edit and invalid values do not send a write', async () => {
  const staff = fixture(undefined, false);
  staff.state.edit();
  assert.equal(staff.state.editing, false);
  staff.state.editing = true;
  await staff.state.save();
  assert.equal(staff.calls.length, 0);
  for (const limit of [-1, 11, 1.5, null]) {
    const { state, calls } = fixture();
    state.edit();
    state.mode = 'custom';
    state.limit = limit;
    await state.save();
    assert.equal(calls.length, 0);
    assert.match(state.error, /whole number/);
  }
});
test('duplicate saves are blocked and obsolete results cannot update a new row', async () => {
  let finish;
  const pending = new Promise((resolve) => (finish = resolve));
  const { state, calls, notices } = fixture(() => pending);
  state.edit();
  const saving = state.save();
  await state.save();
  assert.equal(calls.length, 1);
  state.load({ ...organization, id: 'other' });
  finish(organization.child_limit);
  await saving;
  assert.equal(state.organization.id, 'other');
  assert.equal(notices.length, 0);
});
test('failed writes keep the editor and limit available for retry', async () => {
  const { state } = fixture(async () => {
    throw new Error('permission denied');
  });
  state.edit();
  state.mode = 'custom';
  state.limit = 4;
  await state.save();
  assert.equal(state.error, 'permission denied');
  assert.equal(state.limit, 4);
  assert.equal(state.busy, false);
  assert.equal(state.editing, true);
  state.cancel();
  assert.equal(state.editing, false);
  assert.equal(state.error, '');
});
