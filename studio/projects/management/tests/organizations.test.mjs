import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { runInNewContext } from 'node:vm';
import ts from 'typescript';
const source = readFileSync(
  new URL('../src/app/features/organizations/state/organization-workspace.ts', import.meta.url),
  'utf8',
);
const compiled = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText;
const exports = {};
runInNewContext(compiled, { exports });
function fixture({ allowed = true, role = 'owner', request } = {}) {
  const calls = [],
    org = { id: 'root', name: 'Root', role };
  const state = new exports.OrganizationWorkspace({
    organization: () => org,
    request: async (path, body, method) => {
      calls.push([path, body, method]);
      if (request) return request(path, body, method);
      if (path.startsWith('/api/v1/studio/orgs/creation'))
        return {
          can_create: allowed,
          can_create_root: false,
          max_children: 2,
          parents: role === 'member' ? [] : [org],
        };
      if (path.endsWith('/hierarchy')) return { root: org, children: [], max_children: 2 };
      return { id: 'new' };
    },
    refreshProfile: async () => {},
    selectOrganization: async (id) => calls.push(['select', id]),
    confirm: async () => true,
    markForCheck: () => {},
  });
  return { state, calls, org };
}
test('members cannot create through a direct controller call or switch to metadata-only relatives', async () => {
  const { state, calls } = fixture({ allowed: false, role: 'member' });
  await state.load();
  calls.length = 0;
  state.name = 'Forbidden';
  await state.create();
  await state.switchTo({ id: 'sibling', name: 'Sibling' });
  assert.deepEqual(calls, []);
});
test('creation uses explicit parent IDs and releases its busy guard before switching', async () => {
  const { state, calls } = fixture();
  await state.load();
  state.name = ' Child ';
  state.parent = 'root';
  await state.create();
  const created = calls.find((call) => call[0] === '/api/v1/studio/orgs');
  assert.equal(JSON.stringify(created[1]), JSON.stringify({ name: 'Child', parent_id: 'root' }));
  assert.ok(calls.some((call) => call[0] === 'select' && call[1] === 'new'));
  assert.equal(state.dirty, false);
});
test('creation options always carry the currently selected organization context', async () => {
  const { state, calls } = fixture();
  await state.load();
  assert.ok(calls.some((call) => call[0] === '/api/v1/studio/orgs/creation?organization_id=root'));
  assert.ok(!calls.some((call) => call[0] === '/api/v1/studio/orgs/creation'));
});
test('renaming sends only a name against the permanent ID and respects read-only roles', async () => {
  const { state, calls } = fixture();
  await state.load();
  state.rename = 'Renamed';
  await state.saveName();
  const write = calls.find((call) => call[2] === 'PATCH');
  assert.equal(write[0], '/api/v1/studio/orgs/root');
  assert.equal(JSON.stringify(write[1]), '{"name":"Renamed"}');
  const member = fixture({ role: 'member' });
  await member.state.load();
  member.calls.length = 0;
  await member.state.saveName();
  assert.deepEqual(member.calls, []);
});
test('busy writes block exits and late loads cannot overwrite another organization', async () => {
  let finish;
  const pending = new Promise((resolve) => (finish = resolve));
  const { state } = fixture({ request: () => pending });
  const loading = state.load();
  state.destroy();
  finish({ can_create: true, parents: [] });
  await loading;
  assert.equal(state.options, undefined);
  state.busy = true;
  assert.equal(await state.confirmDiscard(), false);
});

test('existing members default to their parent and cannot submit an independent or unrelated org', async () => {
  const { state, calls } = fixture();
  await state.load();
  assert.equal(state.parent, 'root');
  state.name = 'Second parent';
  state.parent = '';
  await state.create();
  assert.match(state.error, /only create a child/);
  state.parent = 'foreign';
  await state.create();
  assert.match(state.error, /Select a parent/);
  assert.ok(!calls.some((call) => call[0] === '/api/v1/studio/orgs'));
});
