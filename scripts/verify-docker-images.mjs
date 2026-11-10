// Local image checks only. Creates disposable containers/network and optional
// local Trivy reports; never mounts the Docker socket into a container.
import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {mkdtemp, readFile, unlink} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {randomUUID} from 'node:crypto';

const tag = process.env.RUNLOOM_IMAGE_TEST_TAG || 'local';
assert(/^[a-zA-Z0-9_.-]+$/.test(tag), 'Invalid image tag');
const scan = process.argv.includes('--scan');
const trivy = 'aquasec/trivy@sha256:2416d1f74045b57274df1a340eee6b510cacd73c66a6ccb8da092b117eb7d222';
const apps = ['auth', 'workflows', 'worker', 'management', 'assistant', 'gateway', 'database-setup', 'local-admin', 'ui', 'workflows-ui', 'management-ui', 'platform-ui'];
const runtime = ['--read-only', '--cap-drop=ALL', '--security-opt=no-new-privileges', '--tmpfs', '/tmp:rw,nosuid,nodev,noexec,size=64m'];
const suffix = randomUUID().slice(0, 8), network = `runloom-image-test-${suffix}`;
const containers = [];
let networkCreated = false;
let reports;

function docker(args, options = {}) {
  const result = spawnSync('docker', args, {encoding: 'utf8', timeout: 180000, maxBuffer: 16 * 1024 * 1024, ...options});
  if (result.status !== 0) throw new Error(`Docker ${args[0]} failed: ${result.stderr || result.error || result.stdout}`);
  return result.stdout.trim();
}
const image = app => `runloom-${app}:${tag}`;
async function ready(url) {
  for (let i = 0; i < 60; i++) {
    try { const response = await fetch(url, {signal: AbortSignal.timeout(1000)}); if (response.ok) return response; } catch {}
    await new Promise(resolve => setTimeout(resolve, 250));
  }
  throw new Error(`Image did not become healthy: ${url}`);
}

try {
  for (const app of apps) {
    const config = JSON.parse(docker(['image', 'inspect', image(app)]))[0].Config;
    assert.equal(config.User, app.endsWith('ui') ? '101:101' : '65532:65532');
    assert(config.Entrypoint?.length, `${app}: missing exec entrypoint`);
    assert(!config.Env?.some(value => /^(?:.*TOKEN|.*PASSWORD|.*SECRET|.*API_KEY)=.+/.test(value)), `${app}: credential environment baked into image`);
  }
  docker(['network', 'create', network]); networkCreated = true;
  for (const app of ['workflows-ui', 'management-ui', 'platform-ui', 'ui']) {
    const name = `runloom-image-${app}-${suffix}`;
    const args = ['run', '-d', '--name', name, ...runtime, '--network', network, '--network-alias', app,
      '-p', '127.0.0.1::8080', image(app)];
    docker(args); containers.push(name);
    const address = docker(['port', name, '8080/tcp']);
    const base = 'http://' + address;
    const health = await ready(base + '/healthz');
    assert.equal(health.headers.get('server'), 'nginx', 'Nginx must not advertise its version');
    assert((await fetch(base + '/')).ok);
    if (app === 'ui') {
      assert((await fetch(base + '/workflows/definitions/test-link')).ok, 'SPA deep link fallback');
      for (const remote of ['workflows', 'management', 'platform']) {
        const response = await fetch(base + `/remotes/${remote}/remoteEntry.js`);
        assert(response.ok && (await response.text()).includes('export'), `Federation entry ${remote}`);
      }
    } else assert.equal((await fetch(base + '/missing-executable.js')).status, 404);
    // Prove the runtime is unprivileged, immutable and has no development tools.
    docker(['exec', name, 'sh', '-ec',
      'test "$(id -u)" = 101; test ! -w /etc/nginx/nginx.conf; test ! -w /usr/share/nginx/html/index.html; ! command -v node; ! command -v npm; ! command -v gcc; test -z "$(find /usr/share/nginx/html -name \'*.map\' -print -quit)"']);
    console.log(`PASS ${app}: non-root, read-only health/static serving and no source maps/build tools`);
  }
  for (const app of ['auth', 'workflows', 'worker', 'management', 'assistant', 'gateway']) {
    // A broken config must be ignored by probe mode, but rejected by normal startup.
    const probe = ['run', '--rm', ...runtime, '--network', network,
      '-e', 'RUNLOOM_CONFIG_FILE=/missing-probe-test-config', image(app)];
    docker([...probe, 'healthcheck', 'http://ui:8080/healthz']);
    for (const [args, code] of [
      [['healthcheck'], 2], [['healthcheck', 'http://ui:8080/healthz', 'extra'], 2],
      [['healthcheck', '://invalid'], 1], [['healthcheck', 'http://127.0.0.1:1/readyz'], 1],
      [['healthcheck', 'http://workflows-ui:8080/missing-executable.js'], 1],
    ]) {
      const result = spawnSync('docker', [...probe, ...args], {encoding: 'utf8', timeout: 15000});
      assert.equal(result.status, code, `${app}: wrong probe exit code for ${args.join(' ')}`);
      assert.equal(result.stdout, '', `${app}: probe must not print payloads`);
      assert.equal(result.stderr, '', `${app}: probe must not initialize or log application configuration`);
    }
    const startup = spawnSync('docker', probe, {encoding: 'utf8', timeout: 15000});
    assert.equal(startup.status, 1, `${app}: normal startup must reject missing configuration`);
    assert(startup.stderr.length > 0, `${app}: normal startup failure must be reported`);
    const removedProbe = spawnSync('docker', ['run', '--rm', ...runtime, '--network', 'none', '--entrypoint', '/runloom-healthcheck', image(app)], {encoding: 'utf8'});
    assert(removedProbe.status !== 0 && /no such file|not found/.test(removedProbe.stderr), `${app}: standalone probe must be absent`);
    const shell = spawnSync('docker', ['run', '--rm', ...runtime, '--network', 'none', '--entrypoint', '/bin/sh', image(app), '-c', 'exit 0'], {encoding: 'utf8'});
    assert(shell.status !== 0 && /no such file|not found/.test(shell.stderr), `${app}: unexpected shell or unrelated runtime failure`);
    console.log(`PASS ${app}: healthcheck subcommand bypasses configuration, correct failure codes, normal startup guard, no standalone probe or shell`);
  }
  const removedAdmin = spawnSync('docker', ['run', '--rm', ...runtime, '--network', 'none', '--entrypoint', '/runloom-local-admin', image('auth')], {encoding: 'utf8'});
  assert(removedAdmin.status !== 0 && /no such file|not found/.test(removedAdmin.stderr), 'Auth must not contain the admin setup executable');
  for (const app of ['workflows', 'worker']) {
    const code = JSON.parse(docker(['run', '--rm', '-i', ...runtime, '--network', 'none', '--entrypoint', '/runloom-code', image(app)],
      {input: JSON.stringify({code: {runtime: 'quickjs-ng-0.11.0-v1', source: 'return {ok: true};'}, input: {}})}));
    assert.deepEqual(code.output, {ok: true});
    assert(!code.error);
    console.log(`PASS ${app} JavaScript sandbox executes in the restricted image`);
    const other = app === 'workflows' ? 'worker' : 'workflows';
    const absent = spawnSync('docker', ['run', '--rm', ...runtime, '--network', 'none', '--entrypoint', '/runloom-' + other, image(app)], {encoding:'utf8'});
    assert(absent.status !== 0 && /no such file|not found/.test(absent.stderr), `${app}: contains the other runtime executable`);
  }
  if (scan) {
    let blockingFindings = 0;
    reports = await mkdtemp(join(tmpdir(), 'runloom-image-scan-'));
    console.log(`Local scan reports: ${reports}`);
    for (const app of apps) {
      const archive = join(reports, `${app}.tar`);
      try {
        docker(['image', 'save', '-o', archive, image(app)]);
        docker(['run', '--rm', '--read-only', '--cap-drop=ALL', '--security-opt=no-new-privileges',
          '--tmpfs', '/tmp:rw,nosuid,nodev,size=2g', '--mount', `type=bind,src=${reports},dst=/scan`,
          trivy, 'image', '--quiet', '--cache-dir', '/scan/cache', '--scanners', 'vuln,secret', '--disable-telemetry',
          '--input', `/scan/${app}.tar`, '--format', 'json', '--output', `/scan/${app}.json`],
        {timeout: 600000});
        const report = JSON.parse(await readFile(join(reports, `${app}.json`), 'utf8'));
        const vulnerabilities = report.Results?.flatMap(result => result.Vulnerabilities || []) || [];
        const secrets = report.Results?.flatMap(result => result.Secrets || []) || [];
        blockingFindings += secrets.length + vulnerabilities.filter(item => ['CRITICAL', 'HIGH'].includes(item.Severity)).length;
        console.log(JSON.stringify({image: image(app), vulnerabilities: Object.fromEntries(
          ['CRITICAL', 'HIGH', 'MEDIUM', 'LOW', 'UNKNOWN'].map(level => [level, vulnerabilities.filter(item => item.Severity === level).length])),
        secretFindings: secrets.length}));
      } finally { await unlink(archive).catch(() => {}); }
    }
    assert.equal(blockingFindings, 0, `High/critical vulnerabilities or secret findings require review; see ${reports}`);
  }
} finally {
  for (const name of containers.reverse()) docker(['rm', '--force', name]);
  if (networkCreated) docker(['network', 'rm', network]);
}
