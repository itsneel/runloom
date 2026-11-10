import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { runInNewContext } from 'node:vm';
import ts from 'typescript';

// Exercise the actual component methods with HTTP/DI doubles, without TestBed.
const source = readFileSync(new URL('../src/app/features/connections/connections.component.ts', import.meta.url), 'utf8');
const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, experimentalDecorators: true } }).outputText;
function fixture({ saveError, testError, enabled = true, pauseTest } = {}) {
  const calls = [];
  const http = {
    put: async (url, body) => { calls.push(['put', url, { ...body }]); if (saveError) throw saveError; return {}; },
    post: async (url) => { calls.push(['post', url]); if (pauseTest) await pauseTest; if (testError) throw testError; return {}; },
    get: async () => ({ configured: true, items: [] }),
  };
  const values = { AuthService: { teamAdmin: () => true, canEdit: () => true }, HttpClient: http, ChangeDetectorRef: { markForCheck() {} } };
  const core = { Component: () => value => value, HostListener: () => () => {}, Input: () => () => {}, inject: token => values[token], ChangeDetectorRef: 'ChangeDetectorRef' };
  const module = { exports: {} };
  runInNewContext(compiled, { exports: module.exports, module, require: name => {
    if (name === '@angular/core') return core;
    if (name === '@angular/common/http') return { HttpClient: 'HttpClient' };
    if (name === '@runloom/studio-platform') return { AuthService: 'AuthService', injectDialogs: () => ({}) };
    if (name === 'rxjs') return { firstValueFrom: value => value };
    return {};
  } });
  const c = new module.exports.Connections();
  c.orgId = 'org'; c.teamId = 'team'; c.configured = true; c.formOpen = true;
  c.draft = { id:'sample', name:'Sample', host:'db.test', database:'sample_db', username:'sample_user', password:'synthetic-secret', enabled };
  return { c, calls };
}
test('saving automatically tests the saved endpoint and only then reports success', async () => {
  const { c, calls } = fixture(); await c.save();
  assert.equal(calls.map(c => c[0]).join(','), 'put,post');
  assert.equal(calls[1][1], '/api/v1/studio/orgs/org/teams/team/connections/sample/test');
  assert.match(c.message, /saved and connection test passed/);
  assert.equal(c.failed, false); assert.equal(c.formOpen, false); assert.equal(c.busy, false);
  assert.equal(c.draft.password, '');
});
test('failed auto-test distinguishes saved settings and keeps a password-free editor open', async () => {
  const { c } = fixture({ testError: { error: { detail:'Authentication database is blank, so Runloom used admin.' } } });
  await c.save();
  assert.match(c.message, /Connection saved, but its test failed/); assert.match(c.message, /Authentication database is blank/);
  assert.equal(c.failed,true); assert.equal(c.formOpen,true); assert.equal(c.editing,true); assert.equal(c.dirty,false);
  assert.equal(c.draft.password,''); assert.equal(c.testing,false);
});
test('save failure never tests and displays the API validation detail', async () => {
  const { c, calls } = fixture({ saveError:{error:{detail:'TLS is required for this host.'}} }); await c.save();
  assert.equal(calls.length,1); assert.match(c.message,/TLS is required/); assert.equal(c.formOpen,true);
});
test('disabled connections save without a test and explain the skipped test', async () => {
  const { c, calls } = fixture({enabled:false}); await c.save();
  assert.equal(calls.length,1); assert.match(c.message,/saved as disabled/);
});
test('save remains busy during automatic testing and rejects a duplicate save', async () => {
  let finish; const pauseTest = new Promise(resolve => finish=resolve);
  const { c, calls } = fixture({pauseTest}); const saved = c.save(); await new Promise(setImmediate);
  assert.equal(c.testing,true); assert.equal(c.busy,true); await c.save(); assert.equal(calls.length,2);
  finish(); await saved; assert.equal(c.busy,false);
});
test('old auto-test results cannot overwrite a newly selected team', async () => {
  let finish; const pauseTest = new Promise(resolve => finish=resolve);
  const { c } = fixture({pauseTest}); const saved=c.save(); await new Promise(setImmediate);
  c.teamId='another-team'; c.ngOnChanges(); finish(); await saved;
  assert.equal(c.message,''); assert.equal(c.testing,false); assert.equal(c.formOpen,false);
});
test('manual test shows server diagnostics and a safe fallback for transport failures', async () => {
  for (const [error, expected] of [[{error:{detail:'MongoDB permission denied.'}},/MongoDB permission denied/],[{},/Could not reach/]]) {
    const { c }=fixture({testError:error}); await c.test(c.draft); assert.match(c.message,expected); assert.equal(c.failed,true);
  }
});
