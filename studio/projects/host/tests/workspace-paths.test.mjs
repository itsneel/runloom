import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

const source = readFileSync(new URL('../src/app/routing/workspace-paths.ts', import.meta.url), 'utf8');
const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext } }).outputText;
const { managementPaths, managementUrl } = await import('data:text/javascript;base64,' + Buffer.from(compiled).toString('base64'));

test('every management section has a unique canonical URL beneath its own base', () => {
  assert.deepEqual(Object.keys(managementPaths).sort(), ['accounts', 'analytics', 'budgets', 'connections', 'organization', 'prompts', 'runs', 'schedules', 'secrets', 'teams']);
  const paths = Object.keys(managementPaths).map(managementUrl);
  assert.equal(new Set(paths).size, 10);
  for (const path of paths) assert.match(path, /^\/management\/[a-z-]+$/);
  assert.equal(managementUrl('accounts'), '/management/service-accounts');
});

test('projects have a direct, guarded URL separate from the workflow dashboard', () => {
  const routes = readFileSync(new URL('../src/app/routing/app.routes.ts', import.meta.url), 'utf8');
  assert.match(routes, /path: 'workflows\/projects', component: WorkspacePage,\s*data: \{ application: 'workflows', view: 'projects' \}, canDeactivate: \[confirmWorkspaceExit\]/);
});

test('project and workflow deep links are guarded when path parameters change', () => {
  const routes = readFileSync(new URL('../src/app/routing/app.routes.ts', import.meta.url), 'utf8');
  for (const path of ['workflows/projects/:projectId', 'workflows/definitions/:workflowId']) {
    const entry = routes.slice(routes.indexOf(`path: '${path}'`)).split('\n  },')[0];
    assert.match(entry, /canDeactivate: \[confirmWorkspaceExit\]/);
    assert.match(entry, /runGuardsAndResolvers: 'always'/);
  }
});
