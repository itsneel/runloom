import test from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { existsSync } from 'node:fs';
import { resolve } from 'node:path';
import { mountedSettings } from '../local-stack.mjs';

const root = fileURLToPath(new URL('../../', import.meta.url));
test('Compose entry points retain independent build contexts, mounts and secret paths', () => {
  for (const file of ['compose/compose.yaml', 'compose/compose.server.yaml']) {
    const result = spawnSync('docker', ['compose', '--project-directory', root, '-f', file, 'config', '--format', 'json'], {
      cwd: root, encoding: 'utf8', env: { ...process.env,
        RUNLOOM_DATABASE_SECRET_PATH: './deploy/local-postgres-url',
        RUNLOOM_LLM_API_KEY_PATH: './deploy/empty-llm-key',
      },
    });
    assert.equal(result.status, 0, 'Compose rendering failed; configuration omitted');
    const config = JSON.parse(result.stdout);
    assert.equal(config.name, 'runloom');
    for (const [service, directory] of Object.entries({api: 'server/workflows', worker: 'server/workflows', ui: 'studio/projects/host', 'workflows-ui': 'studio/projects/workflows', 'management-ui': 'studio/projects/management'})) {
      assert.equal(config.services[service].build.context, resolve(root, directory));
      assert.ok(existsSync(resolve(config.services[service].build.context, config.services[service].build.dockerfile || 'Dockerfile')));
    }
    for (const mount of config.services.keycloak.volumes.filter(mount => mount.type === 'bind')) {
      assert.ok(existsSync(mount.source), 'Keycloak bind source must exist');
    }
    assert.equal(config.secrets['database-url'].file, resolve(root, 'deploy/local-postgres-url'));
    assert.equal(config.secrets['llm-api-key'].file, resolve(root, 'deploy/empty-llm-key'));
    assert.equal(config.volumes['postgres-data'].name, 'runloom_postgres-data');
  }
});

test('Docker Make targets explicitly select the relocated stack and root project directory', () => {
  for (const target of ['docker-up', 'docker-up-mongo', 'docker-down', 'docker-logs', 'docker-config', 'docker-up-go', 'docker-up-go-mongo', 'docker-down-go', 'docker-logs-go', 'docker-up-secrets', 'docker-config-secrets', 'docker-up-consumer', 'docker-up-consumer-mongo', 'docker-config-consumer']) {
    const result = spawnSync('make', ['-n', target, 'DOCKER=docker'], { cwd: root, encoding: 'utf8' });
    assert.equal(result.status, 0);
    if(result.stdout.includes('scripts/local-services.mjs')) continue;
    const commands = result.stdout.split('\n').filter(line => line.includes('docker compose'));
    assert.ok(commands.length, target);
    for (const command of commands) {
      assert.ok(command.includes(`--project-directory "${root.replace(/\/$/, '')}"`), target);
      assert.match(command, /-f compose\/compose(?:\.server)?\.yaml/);
    }
  }
});

test('Compose accepts a key file and grants it to API and worker only', () => {
  // Configuration rendering does not read or create this synthetic file.
  const path = '/private/example/workflow-key';
  const result = spawnSync('docker', ['compose', '--project-directory', root, '-f', 'compose/compose.yaml', '-f', 'compose/compose.secrets.yaml', 'config', '--format', 'json'], {
    cwd: root, encoding: 'utf8', env: { ...process.env, RUNLOOM_SECRETS_KEY_PATH: path, RUNLOOM_SECRETS_KEY_GID: '1234' },
  });
  assert.ok(result.status === 0, 'Compose rendering failed; configuration omitted');
  const config = JSON.parse(result.stdout);
  assert.equal(config.secrets['workflow-key'].file, path);
  for (const [name, service] of Object.entries(config.services)) {
    const backend = ['api', 'worker'].includes(name);
    assert.equal((service.secrets || []).some(secret => secret.source === 'workflow-key'), backend);
    assert.equal(backend ? mountedSettings(config, name).RUNLOOM_SECRETS_KEY_FILE : undefined, backend ? '/run/secrets/workflow-key' : undefined);
    assert.equal((service.group_add || []).includes('1234'), backend);
  }
});
test('Compose fails closed when no key file path is provided', () => {
  const result = spawnSync('docker', ['compose', '--project-directory', root, '-f', 'compose/compose.yaml', '-f', 'compose/compose.secrets.yaml', 'config', '--quiet'], {
    cwd: root, encoding: 'utf8', env: { ...process.env, RUNLOOM_SECRETS_KEY_PATH: '' },
  });
  assert.ok(result.status !== 0, 'missing key file accepted');
});

test('local vault and consumer overlays coexist; only API receives the provisioner credential and group', () => {
  for (const environment of ['local', 'fresh']) {
    const result = spawnSync('docker', ['compose', '--project-directory', root, '--env-file', `deploy/${environment}.env`, '-f', 'compose/compose.yaml', '-f', 'compose/compose.local.yaml', '-f', 'compose/compose.secrets.yaml', '-f', 'compose/compose.consumer.yaml', '-f', '-', 'config', '--format', 'json'], {
      cwd: root, encoding: 'utf8', input: 'services:\n  api:\n    group_add: !override ["1234", "2345"]\n', env: { ...process.env,
        RUNLOOM_SECRETS_KEY_PATH: '/private/example/key', RUNLOOM_SECRETS_KEY_GID: '1234',
        RUNLOOM_PROVISIONER_SECRET_PATH: '/private/example/provisioner', RUNLOOM_PROVISIONER_SECRET_GID: '2345',
      },
    });
    assert.equal(result.status, 0, 'Compose rendering failed; configuration omitted');
    const config = JSON.parse(result.stdout);
    assert.equal(config.secrets['runloom-provisioner'].file, '/private/example/provisioner');
    for (const [name, service] of Object.entries(config.services)) {
      assert.equal((service.secrets || []).some(secret => secret.source === 'runloom-provisioner'), name === 'api');
      assert.equal((service.group_add || []).includes('2345'), name === 'api');
    }
    assert.ok(config.services.api.group_add.includes('1234'));
    assert.equal(mountedSettings(config, 'api').RUNLOOM_KEYCLOAK_PROVISIONER_SECRET_FILE, '/run/secrets/runloom-provisioner');
  }
});

test('automatic setup can deduplicate shared secret groups through its stdin overlay', () => {
  const result = spawnSync('docker', ['compose', '--project-directory', root, '-f', 'compose/compose.yaml', '-f', 'compose/compose.secrets.yaml', '-f', 'compose/compose.consumer.yaml', '-f', '-', 'config', '--format', 'json'], {
    cwd: root, encoding: 'utf8', input: 'services:\n  api:\n    group_add: !override ["1234"]\n',
    env: {...process.env, RUNLOOM_SECRETS_KEY_PATH: '/private/test-key', RUNLOOM_PROVISIONER_SECRET_PATH: '/private/test-provider', RUNLOOM_SECRETS_KEY_GID: '1234', RUNLOOM_PROVISIONER_SECRET_GID: '1234'},
  });
  assert.equal(result.status, 0, 'Compose group normalization failed; configuration omitted');
  assert.deepEqual(JSON.parse(result.stdout).services.api.group_add, ['1234']);
});
