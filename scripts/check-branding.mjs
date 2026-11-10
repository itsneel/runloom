// Keep product identifiers aligned across independently built components.
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { readFileSync, existsSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('../', import.meta.url));
const read = path => readFileSync(new URL('../' + path, import.meta.url), 'utf8');
const json = path => JSON.parse(read(path));
const files = execFileSync('git', ['ls-files', '--cached', '--others', '--exclude-standard', '-z'], { cwd: root, encoding: 'utf8' }).split('\0').filter(Boolean);
const allowed = new Set(['docs/rebranding.md', 'scripts/check-branding.mjs']);
const previous = /jsonflow|json flow|temporal[- ]json[- ]workflows|JSONWorkflow|workflow-studio|\bJF\b/i;
for (const path of new Set(files)) {
  if (allowed.has(path) || path.includes('/vendor/') || /\/docs\/swagger-ui/.test(path) || !existsSync(new URL('../' + path, import.meta.url))) continue;
  const body = read(path);
  if (body.includes('\0')) continue;
  assert(!previous.test(path) && !previous.test(body), 'Stale product name in ' + path);
}
assert.equal(json('Website/package.json').name, 'runloom-website');
for (const folder of ['Website', 'docs-site', 'studio/projects/host', 'studio/projects/workflows', 'studio/projects/management', 'studio/projects/platform-admin', 'studio/libraries/platform']) {
  const name = json(folder + '/package.json').name;
  assert.equal(json(folder + '/package-lock.json').name, name);
  assert.equal(json(folder + '/package-lock.json').packages[''].name, name);
  if (folder === 'Website' || folder === 'docs-site' || folder.startsWith('studio/projects/')) {
    const projects = json(folder + '/angular.json').projects;
    const project = folder === 'Website' || folder === 'docs-site' ? name : folder.split('/').at(-1);
    assert.deepEqual(Object.keys(projects), [project]);
  }
}
for (const service of ['auth','workflows','management','assistant']) assert(read(`server/${service}/go.mod`).startsWith(`module github.com/example/runloom/${service}\n`));
assert.match(read('charts/runloom/Chart.yaml'), /^name: runloom$/m);
assert.match(read('compose/compose.yaml'), /^name: runloom$/m);
const realm = json('deploy/keycloak/runloom-realm.json');
assert.equal(realm.realm, 'runloom');
assert(realm.clients.some(client => client.clientId === 'runloom'));
assert.equal(read('deploy/keycloak/runloom-realm.json'), read('charts/runloom/files/runloom-realm.json'));
for (const folder of ['server','server/auth','server/workflows','server/management','server/gateway','tools/database-setup','sdk/go-auth', 'Website', 'docs-site', 'charts/runloom', 'studio/projects/host', 'studio/projects/workflows', 'studio/projects/management', 'studio/projects/platform-admin', 'studio/libraries/platform']) assert.equal(read(folder + '/LICENSE'), read('LICENSE'));
console.log('PASS Runloom naming, packages, chart, identity realm and license consistency');
