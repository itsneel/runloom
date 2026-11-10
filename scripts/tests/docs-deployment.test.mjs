import test from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { readFileSync } from 'node:fs';

const render = (...args) => execFileSync('helm', ['template', 'check', 'charts/runloom', ...args], {encoding: 'utf8'});
test('both database selections include independent static docs with no database or token mounts', () => {
  for (const driver of ['mongodb', 'postgres']) {
    const output = render('--set', `database.driver=${driver}`, '--show-only', 'templates/docs.yaml');
    assert.match(output, /name: check-runloom-docs/);
    assert.match(output, /readOnlyRootFilesystem: true/);
    assert.match(output, /automountServiceAccountToken: false/);
    assert.match(output, /studio\.json/);
    assert.doesNotMatch(output, /secretName|database-url|service-token/);
  }
});
test('docs have their own ingress hostname and can instead be externally hosted', () => {
  const output = render('--set', 'docs.publicURL=https://docs.example.com', '--set', 'docs.ingress.enabled=true', '--set', 'docs.ingress.className=nginx', '--show-only', 'templates/docs.yaml');
  assert.match(output, /host: "docs.example.com"/);
  assert.match(output, /ingressClassName: "nginx"/);
  assert.match(output, /path: \/\n/);
  const external = render('--set', 'docs.enabled=false', '--set', 'docs.publicURL=https://docs.example.com', '--show-only', 'templates/docs.yaml');
  assert.match(external, /documentation\.json/);
  assert.doesNotMatch(external, /kind: Deployment|kind: Service\n|kind: Ingress/);
});
test('chart rejects docs subpaths, embedded credentials and the Studio origin', () => {
  for (const setting of ['docs.publicURL=https://example.com/docs', 'docs.publicURL=javascript:alert(1)', 'docs.publicURL=https://user:pass@example.com', 'docs.publicURL=http://localhost:4200']) {
    assert.notEqual(spawnSync('helm', ['template', 'check', 'charts/runloom', '--set', setting], {encoding: 'utf8'}).status, 0);
  }
});
test('Compose mounts public destination files and binds the independent docs port to loopback', () => {
  const output = JSON.parse(execFileSync('docker', ['compose', '--project-directory', '.', '-f', 'compose/compose.yaml', 'config', '--format', 'json'], {encoding: 'utf8'}));
  const docs = output.services.docs;
  assert.equal(docs.ports[0].host_ip, '127.0.0.1');
  assert.equal(docs.ports[0].target, 8080);
  assert.equal(docs.read_only, true);
  assert.equal(docs.configs[0].target, '/usr/share/nginx/html/studio.json');
  assert(output.services.ui.configs.some(item => item.target === '/usr/share/nginx/html/documentation.json'));
  assert.doesNotMatch(readFileSync('studio/projects/host/nginx.conf', 'utf8'), /location \/docs\//);
});
