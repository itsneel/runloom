import assert from 'node:assert/strict';
import test from 'node:test';
import {readFileSync, readdirSync} from 'node:fs';
import {resolve} from 'node:path';
import {fileURLToPath} from 'node:url';

const root = fileURLToPath(new URL('../../', import.meta.url));
const services = ['server/auth', 'server/workflows', 'server/management', 'server/assistant', 'server/gateway', 'tools/database-setup'];
const apps = ['studio/projects/host', 'studio/projects/workflows', 'studio/projects/management', 'studio/projects/platform-admin'];
const read = file => readFileSync(resolve(root, file), 'utf8');

for (const directory of [...services, ...apps, 'docs-site']) {
  for (const filename of readdirSync(resolve(root, directory)).filter(name => name.startsWith('Dockerfile'))) {
    test(`${directory}/${filename}: pinned multi-stage build and least-privilege runtime`, () => {
      const text = read(`${directory}/${filename}`);
      const stages = [...text.matchAll(/^FROM (\S+)(?: AS (\w+))?$/gm)];
      assert.equal(stages.length, 2);
      for (const [, image] of stages) assert.match(image, /@sha256:[a-f0-9]{64}$/);
      assert.doesNotMatch(text, /^COPY(?: --\S+)* \. \.$/m, 'do not copy the whole context');
      assert.match(text, /^USER (65532:65532|101:101)$/m);
      assert.match(text, /^ENTRYPOINT \[/m, 'use exec-form entrypoints');
      assert.match(text, /^COPY --from=build /m);
      const runtime = text.slice(text.lastIndexOf('\nFROM '));
      assert.doesNotMatch(runtime, /\b(npm ci|go build|apk add|apt-get|node_modules|\/src)\b/);
      assert.match(text, /org\.opencontainers\.image\.licenses="MIT"/);
      if (services.includes(directory)) {
        assert.doesNotMatch(text, /runloom-healthcheck|cmd\/healthcheck/);
        assert.match(text, /GOTOOLCHAIN=local GOPROXY=off/);
        assert.match(text, /RUN --network=none/);
        assert.match(text, /-trimpath -buildvcs=false -ldflags="-s -w"/);
        assert.match(runtime, /COPY --from=build --chmod=0555/);
        assert.match(runtime, /distroless\/static-debian12:nonroot/);
      } else {
        assert.match(text, /USER node\n/);
        assert.match(text, /npm ci --ignore-scripts --no-audit --no-fund/);
        assert.match(text, /RUN --network=none npm run build -- --configuration production --source-map=false/);
        assert.match(runtime, /nginx:[^@\s]+-alpine-slim@/);
        assert.match(read(`${directory}/nginx-main.conf`), /server_tokens off;/);
      }
    });
  }
  test(`${directory}: context is allowlisted and credential files stay excluded`, () => {
    const ignore = read(`${directory}/.dockerignore`);
    const rules = ignore.split('\n').filter(line => line && !line.startsWith('#'));
    assert.equal(rules[0], '**');
    for (const pattern of ['**/.env', '**/.env.*', '**/.npmrc', '**/.netrc', '**/*.key', '**/*.pem', '**/*.p12', '**/*.pfx', '**/.git']) {
      assert(rules.includes(pattern), `missing ${pattern}`);
      assert(rules.indexOf(pattern) > rules.findLastIndex(line => line.startsWith('!')), 'secret exclusions must follow all allow rules');
    }
    assert(rules.includes('!LICENSE'));
  });
}

test('bootstrap-only image contains only its one executable', () => {
  const text = read('server/auth/Dockerfile.local-admin');
  assert.equal((text.match(/go build /g) || []).length, 1);
  assert.equal((text.match(/COPY --from=build/g) || []).length, 1);
  assert.doesNotMatch(text, /EXPOSE|\/out\/runloom-auth|\/out\/runloom-healthcheck/);
});

test('workflow API and worker are independent runtime images with their own code runner', () => {
  for (const [file, own, absent] of [['Dockerfile', 'workflows', 'worker'], ['Dockerfile.worker', 'worker', 'workflows']]) {
    const text = read('server/workflows/' + file);
    for (const binary of [own, 'code']) assert(text.includes(`/out/runloom-${binary} /runloom-${binary}`));
    assert(!text.includes(`/out/runloom-${absent}`));
    assert(text.includes(`ENTRYPOINT ["/runloom-${own}"]`));
  }
  assert.doesNotMatch(read('server/auth/Dockerfile'), /runloom-local-admin|cmd\/local-admin/);
});

test('test sources are excluded from each independent Docker context', () => {
  for (const directory of services) {
    const ignore = read(`${directory}/.dockerignore`);
    for (const pattern of ['**/*_test.go', '**/tests/**', '**/testdata/**']) assert(ignore.includes(pattern));
  }
  for (const directory of apps) {
    const ignore = read(`${directory}/.dockerignore`);
    for (const pattern of ['**/*.spec.ts', '**/*.test.ts', '**/tests/**']) assert(ignore.includes(pattern));
    assert(ignore.lastIndexOf('!tests/boundaries.mjs') > ignore.indexOf('**/tests/**'));
  }
});
