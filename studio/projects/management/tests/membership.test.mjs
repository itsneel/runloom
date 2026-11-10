import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { runInNewContext } from 'node:vm';
import ts from 'typescript';

const source = readFileSync(new URL('../src/app/layout/management-shell/management-shell.component.ts', import.meta.url), 'utf8');
const compiled = ts.transpileModule(source, {compilerOptions: {
  module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, experimentalDecorators: true,
}}).outputText;
function fixture({organizations = [{id: 'org', role: 'member'}], team = {id: 'team', role: 'editor'}, request} = {}) {
  const calls = [];
  const auth = {
    profile: () => ({organizations}), organizations: () => organizations,
    organizationId: () => 'org', teamId: () => team?.id || '', team: () => team,
    orgAdmin: () => ['owner', 'admin'].includes(organizations[0]?.role),
    teamAdmin: () => team?.role === 'admin',
    request: async (path, body) => { calls.push({path, body}); return request ? request(path, body) : []; },
  };
  const exports = {};
  runInNewContext(compiled, {exports, require: name => {
    if (name === '@angular/core') return {
      Component: () => value => value, Input: () => () => {}, ViewChild: () => () => {},
      ChangeDetectorRef: 'detector',
      inject: token => token === 'auth' ? auth : token === 'detector' ? {markForCheck() {}} : {},
    };
    if (name === '@runloom/studio-platform') return {AuthService: 'auth', injectDialogs: () => ({})};
    return {};
  }});
  return {component: new exports.Management(), auth, calls};
}

test('editors and viewers load and search their own team roster without querying the organization directory', async () => {
  for (const role of ['editor', 'viewer']) {
    const members = [{email: 'owner@example.test', role: 'admin', inherited: true}, {email: 'peer@example.test', role: 'editor'}];
    const {component, calls} = fixture({team: {id: 'team', role}, request: () => members});
    await component.loadMembers();
    assert.equal(calls.length, 1);
    assert.equal(calls[0].path, '/api/v1/studio/orgs/org/teams/team/members');
    assert.deepEqual(component.teamMembers, members);
    component.memberSearch = 'PEER';
    assert.equal(component.shownTeamMembers.length, 1);
    assert.equal(component.shownTeamMembers[0].email, 'peer@example.test');
  }
});

test('no team selected means no team-roster request', async () => {
  const {component, calls} = fixture({team: null});
  await component.loadMembers();
  assert.equal(calls.length, 0);
});

test('a slow roster response cannot overwrite a newly selected team', async () => {
  let finish;
  const pending = new Promise(resolve => { finish = resolve; });
  const {component, auth} = fixture({request: path => path.includes('/team/') ? pending : [{email: 'new@example.test'}]});
  const first = component.loadMembers();
  auth.teamId = () => 'next';
  await component.loadMembers();
  finish([{email: 'stale@example.test'}]);
  await first;
  assert.equal(component.teamMembers[0].email, 'new@example.test');
});
