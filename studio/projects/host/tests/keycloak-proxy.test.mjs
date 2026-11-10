import test from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';

test(
  'Compose proxy preserves identity framing and CSP while Studio stays frame-denied',
  { skip: !process.env.RUNLOOM_TEST_PROXY_IMAGE },
  async () => {
    const docker = (...args) =>
      execFileSync('docker', args, { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim();
    assert.match(
      docker('context', 'inspect', '--format', '{{.Endpoints.docker.Host}}'),
      /^(unix|npipe):\/\//,
      'Local Docker only',
    );
    const suffix = randomUUID(),
      network = 'runloom-headers-' + suffix,
      identity = network + '-id',
      proxy = network + '-ui';
    const directory = await mkdtemp(join(tmpdir(), 'runloom-headers-'));
    const dockerfile = await readFile(new URL('../Dockerfile', import.meta.url), 'utf8');
    const nginx = [...dockerfile.matchAll(/^FROM (\S+)/gm)].at(-1)[1];
    await writeFile(
      join(directory, 'identity.conf'),
      `server {
    listen 8080;
    location /auth/admin/ {
      add_header X-Frame-Options SAMEORIGIN always;
      add_header Content-Security-Policy "frame-src 'self'; frame-ancestors 'self'; object-src 'none';" always;
      return 200 'console';
    }
    location /auth/realms/ {
      add_header Content-Security-Policy "frame-src 'self'; object-src 'none';" always;
      return 200 'cookie check';
    }
  }`,
    );
    const containers = [];
    let created = false;
    try {
      docker('network', 'create', network);
      created = true;
      docker(
        'run',
        '-d',
        '--name',
        identity,
        '--network',
        network,
        '--network-alias',
        'keycloak',
        '--mount',
        `type=bind,source=${join(directory, 'identity.conf')},target=/etc/nginx/conf.d/default.conf,readonly`,
        nginx,
      );
      containers.push(identity);
      docker(
        'run',
        '-d',
        '--name',
        proxy,
        '--network',
        network,
        '--read-only',
        '--tmpfs',
        '/tmp:rw,noexec,nosuid,size=16m',
        '--publish',
        '127.0.0.1::8080',
        process.env.RUNLOOM_TEST_PROXY_IMAGE,
      );
      containers.push(proxy);
      const port = docker('port', proxy, '8080/tcp').split(':').at(-1),
        origin = 'http://127.0.0.1:' + port;
      let ready = false;
      for (let attempt = 0; attempt < 60; attempt++) {
        try {
          if ((await fetch(origin + '/healthz', { signal: AbortSignal.timeout(1000) })).ok) {
            ready = true;
            break;
          }
        } catch {}
        await delay(100);
      }
      assert.ok(ready, 'Isolated proxy did not start');
      const root = await fetch(origin + '/');
      assert.equal(root.headers.get('x-frame-options'), 'DENY');
      const admin = await fetch(origin + '/auth/admin/master/console/');
      assert.equal(admin.status, 200);
      assert.equal(admin.headers.get('x-frame-options'), 'SAMEORIGIN');
      assert.match(admin.headers.get('content-security-policy'), /frame-ancestors 'self'/);
      const iframe = await fetch(
        origin + '/auth/realms/master/protocol/openid-connect/3p-cookies/step1.html',
      );
      assert.equal(iframe.status, 200);
      assert.equal(iframe.headers.get('x-frame-options'), null);
      assert.equal(
        iframe.headers.get('content-security-policy'),
        "frame-src 'self'; object-src 'none';",
      );
      assert.equal(iframe.headers.get('x-content-type-options'), 'nosniff');
      assert.equal(iframe.headers.get('referrer-policy'), 'no-referrer');
    } finally {
      for (const name of containers.reverse()) docker('rm', '-f', name);
      if (created) docker('network', 'rm', network);
      await rm(directory, { recursive: true, force: true });
    }
  },
);
