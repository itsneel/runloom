import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { loadTypeScript } from './helpers/load-typescript.mjs';

const signal = (initial) => {
  let value = initial;
  const read = () => value;
  read.set = (next) => {
    value = next;
  };
  return read;
};
const load = () =>
  loadTypeScript(
    new URL('../src/app/features/platform/state/platform-console.ts', import.meta.url),
    { modules: { '@angular/core': { signal } } },
  ).PlatformConsoleState;
test('latest page owns the state and an obsolete response cannot restore data', async () => {
  const State = load();
  const pending = [];
  const state = new State({
    list: (...args) => new Promise((resolve) => pending.push({ args, resolve })),
  });
  const old = state.load('organizations');
  const current = state.load('staff');
  pending[1].resolve({ items: [{ id: 'staff' }], total: 1 });
  await current;
  pending[0].resolve({ items: [{ id: 'old-org' }], total: 1 });
  await old;
  assert.equal(state.staff()[0].id, 'staff');
  assert.equal(state.organizations().length, 0);
});
test('access failure clears displayed data and reports the actual error', async () => {
  const State = load();
  const state = new State({
    list: async () => {
      throw new Error('permission denied');
    },
  });
  state.organizations.set([{ id: 'previous' }]);
  state.health.set({ items: [{}] });
  await state.load('organizations');
  assert.equal(state.organizations().length, 0);
  assert.equal(state.health(), undefined);
  assert.equal(state.error(), 'permission denied');
  assert.equal(state.busy(), false);
});
test('destroy/revocation invalidates in-flight results', async () => {
  const State = load();
  let resolve;
  const state = new State({ health: () => new Promise((r) => (resolve = r)) });
  const pending = state.load('health');
  state.clear();
  resolve({ items: [{ service: 'auth' }] });
  await pending;
  assert.equal(state.health(), undefined);
  assert.equal(state.busy(), false);
});
test('independent remote uses separate templates and has no tenant-content affordances', () => {
  const template = readFileSync(
    new URL('../src/app/features/platform/platform-console.component.html', import.meta.url),
    'utf8',
  );
  const navigation = readFileSync(
    new URL(
      '../src/app/features/platform/components/platform-navigation/platform-navigation.component.html',
      import.meta.url,
    ),
    'utf8',
  );
  assert.match(navigation, /Platform console/);
  assert.match(navigation, /access.can\(tab.permission\)/);
  assert.doesNotMatch(template, /window\.confirm|window\.alert|secret value|impersonat/i);
  const entry = readFileSync(new URL('../src/app/federation/entry.ts', import.meta.url), 'utf8');
  assert.match(entry, /contractVersion = 1/);
});
