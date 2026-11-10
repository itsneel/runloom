import test from 'node:test';
import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
const cwd = fileURLToPath(new URL('../../', import.meta.url));
function render(chart = 'runloom', args = [], success = true) {
  const result = spawnSync('helm', ['template', chart, `charts/${chart}`, ...args], {cwd, encoding: 'utf8'});
  assert.equal(result.status === 0, success, 'Chart validation mismatch; potentially sensitive rendered output withheld');
  return result.stdout;
}
test('controller and app ingress default off; the app never owns a controller', () => {
  assert.equal(render('runloom-ingress').trim(), '');
  const app = render();
  assert(!/^kind: Ingress$/m.test(app));
  assert(!/^kind: (IngressClass|ClusterRole|ClusterRoleBinding)$/m.test(app));
  assert(app.includes('proxy_set_header X-Forwarded-Proto http;'));
  assert(app.includes('proxy_set_header Host localhost:4200;'));
  assert(app.includes('proxy_set_header X-Forwarded-Port 4200;'));
});
test('optional pinned F5 controller has its own non-default class and no snippets', () => {
  const text = render('runloom-ingress', ['--set', 'nginx.enabled=true']);
  assert(text.includes('image: nginx/nginx-ingress:5.7.0'));
  assert(text.includes('kind: IngressClass'));
  assert(text.includes('name: runloom-nginx'));
  assert(text.includes('-enable-custom-resources=false'));
  assert(text.includes('-enable-snippets=false'));
  assert(text.includes('-enable-telemetry-reporting=false'));
  assert(!text.includes('is-default-class: "true"'));
  assert(!text.includes('PersistentVolumeClaim'));
});
test('both databases support bundled or external controller classes and TLS', () => {
  for (const driver of ['mongodb', 'postgres']) {
    for (const mode of ['local', 'external']) {
      const text = render('runloom', ['-f', `charts/runloom/values-ingress-${mode}.yaml`, '--set', `database.driver=${driver}`]);
      assert(text.includes('host: "studio.runloom.dev"'));
      assert(text.includes(`ingressClassName: "${mode === 'local' ? 'runloom-nginx' : 'nginx'}"`));
      assert(text.includes('secretName: runloom-local-tls'));
      assert(text.includes('proxy_set_header X-Forwarded-Proto https;'));
      assert(text.includes('proxy_set_header Host studio.runloom.dev;'));
      assert(text.includes('proxy_set_header X-Forwarded-Port 443;'));
      assert(!text.includes('proxy_set_header X-Forwarded-Proto $scheme'));
      assert(!text.includes('$http_x_forwarded_proto'));
      assert(!/^kind: IngressClass$/m.test(text));
    }
  }
});
test('externally managed routes create no ingress but retain the independent public origin', () => {
  const text = render('runloom', ['-f', 'charts/runloom/values-routing-external.yaml']);
  assert(!/^kind: Ingress$/m.test(text));
  assert(text.includes('https://studio.runloom.dev/auth/realms/runloom'));
  assert(text.includes('proxy_set_header X-Forwarded-Proto https;'));
});
test('other ingress classes and controller-specific annotations remain configurable', () => {
  const text = render('runloom', ['-f', 'charts/runloom/values-ingress-external.yaml', '--set', 'ingress.className=traefik', '--set', 'ingress.annotations.example=preserved']);
  assert(text.includes('ingressClassName: "traefik"'));
  assert(text.includes('example: preserved'));
});
test('invalid public origins and mismatched ingress auth/TLS configuration fail closed', () => {
  for (const setting of ['ingress.className=', 'auth.publicURL=https://different.example', 'auth.publicURL=http://studio.runloom.dev', 'ingress.tls[0].hosts[0]=different.example']) {
    render('runloom', ['-f', 'charts/runloom/values-ingress-local.yaml', '--set', setting], false);
  }
  for (const url of ['https://user:password@studio.runloom.dev', 'https://studio.runloom.dev?x=y', 'https://studio.runloom.dev;injected', 'https://studio.runloom.dev/path', 'https://studio.runloom.dev\ninvalid']) {
    render('runloom', ['--set-string', `auth.publicURL=${url}`], false);
  }
});
