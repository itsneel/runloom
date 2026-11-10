// Local-only orchestration. No credentials, tokens, or provider responses are logged.
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { homedir } from 'node:os';
import { existsSync, readFileSync } from 'node:fs';
import { ensureLocalWorkflowKey } from './local-workflow-key.mjs';
import { ensureLocalConsumer } from './local-consumer-auth.mjs';

// Local Compose owns these JSON-compatible YAML documents. Validate the effective
// mounted settings, never environment fallbacks or credential contents.
export function mountedSettings(config, serviceName) {
  const service = config.services[serviceName];
  if (service.environment?.RUNLOOM_CONFIG_FILE !== '/etc/runloom/config.yaml') throw new Error('Local services must use mounted configuration.');
  const files = (service.configs || []).filter(file => file.target === '/etc/runloom/config.yaml' || /^\/etc\/runloom\/config\.d\/[^/]+\.yaml$/.test(file.target));
  if (!files.some(file => file.target === '/etc/runloom/config.yaml')) throw new Error('Missing base configuration mount.');
  const values = {};
  for (const file of files.sort((a, b) => (a.target.endsWith('/config.yaml') ? '' : a.target).localeCompare(b.target.endsWith('/config.yaml') ? '' : b.target))) {
    try { Object.assign(values, JSON.parse(readFileSync(config.configs[file.source].file, 'utf8'))); }
    catch { throw new Error('Local mounted configuration must be a readable JSON-compatible YAML file.'); }
  }
  return values;
}

export function validateLocalConfig(config, expectedProject = 'runloom-local') {
  if (!['runloom-local', 'runloom-fresh'].includes(expectedProject) || config.name !== expectedProject) throw new Error('Local setup must use its dedicated local project.');
  for (const service of Object.values(config.services || {})) {
    if (service.network_mode === 'host' || service.external_links?.length) throw new Error('Host networking and external links are not allowed for local setup.');
    for (const port of service.ports || []) {
      if (!['127.0.0.1', '::1'].includes(port.host_ip)) throw new Error('Every published local port must be bound to loopback.');
    }
  }
  if (Object.values(config.networks || {}).some(network => network.external)) throw new Error('Local setup cannot use external networks.');
  const api = mountedSettings(config, 'api');
  const seedSettings = mountedSettings(config, 'local-admin');
  for (const address of [config.services.keycloak.environment.KC_HOSTNAME, api.RUNLOOM_OIDC_ISSUER]) {
    const url = new URL(address);
    if (url.protocol !== 'http:' || !['localhost', '127.0.0.1', '[::1]'].includes(url.hostname) || url.username || url.password) throw new Error('Identity URLs must use the local browser origin.');
  }
  const seed = config.services['local-admin'].environment;
  for (const key of ['RUNLOOM_DATABASE_DRIVER', 'RUNLOOM_DATABASE_URL_FILE', 'RUNLOOM_DATABASE_NAME']) {
    if (!api[key] || seedSettings[key] !== api[key]) throw new Error('Bootstrap and API database settings must match.');
  }
  const databaseMount = name => config.services[name].secrets?.find(secret => ['/run/secrets/database-url', 'database-url'].includes(secret.target || secret.source))?.source;
  if (!databaseMount('api') || databaseMount('api') !== databaseMount('local-admin')) throw new Error('Bootstrap and API must mount the same database credential.');
  if (seed.RUNLOOM_ENV !== 'local' || seed.RUNLOOM_LOCAL_BOOTSTRAP !== '1') throw new Error('Local bootstrap opt-in is missing.');
}

export function validateDockerEndpoint(endpoint) {
  if (!endpoint?.startsWith('unix://') && !endpoint?.startsWith('npipe://')) throw new Error('Local setup refuses remote Docker daemons. Use a local Docker Desktop/Engine context.');
}

// A new env file must not silently select another existing installation's data.
export function assertLocalDatabaseContinuity(config, containers) {
  const planned = mountedSettings(config, 'api');
  for (const container of containers) {
    const mounts = (container.Mounts || []).filter(m => m.Destination === '/etc/runloom/config.yaml' || /^\/etc\/runloom\/config\.d\/[^/]+\.yaml$/.test(m.Destination));
    if (!mounts.length) continue;
    const active = {};
    for (const mount of mounts.sort((a,b) => (a.Destination.endsWith('/config.yaml') ? '' : a.Destination).localeCompare(b.Destination.endsWith('/config.yaml') ? '' : b.Destination))) {
      try { Object.assign(active, JSON.parse(readFileSync(mount.Source, 'utf8'))); }
      catch { throw new Error('Cannot verify the existing mounted database configuration; no cutover performed.'); }
    }
    for (const key of ['RUNLOOM_DATABASE_DRIVER', 'RUNLOOM_DATABASE_NAME']) {
      if (active[key] && planned[key] !== active[key]) throw new Error('Existing application and selected config use different databases. Choose the migration source explicitly before changing the local stack.');
    }
  }
}

export async function executeLocalStack(action, env = process.env, dependencies = {}) {
  const fresh = action === 'fresh-up' || action === 'fresh-down';
  if (fresh) action = action.slice(6);
  if (!['up', 'admin', 'reset', 'down', 'config'].includes(action)) throw new Error('Expected up, admin, reset, down, or config.');
  if (env.RUNLOOM_ENV && env.RUNLOOM_ENV !== 'local') throw new Error('Refusing local setup outside RUNLOOM_ENV=local.');
  const root = fileURLToPath(new URL('../', import.meta.url));
  const docker = env.DOCKER || 'docker';
  const composeEnv = { ...env };
  for (const [key, value] of Object.entries(composeEnv)) if (key.startsWith('RUNLOOM_') && value === '') delete composeEnv[key];
  let composeInput;
  let vaultGroup;
  const run = (args, capture = false) => {
    if (dependencies.run) return dependencies.run(args, capture, { ...composeEnv }, composeInput);
    const result = spawnSync(docker, args, { cwd: root, env: composeEnv, encoding: 'utf8', input: composeInput,
      stdio: capture ? 'pipe' : composeInput ? ['pipe', 'inherit', 'inherit'] : 'inherit' });
    if (result.error || result.status !== 0) throw new Error('Local Docker operation failed. No account credentials were printed.');
    return result.stdout?.trim();
  };
  // DOCKER_CONTEXT takes priority over DOCKER_HOST in Docker's CLI.
  const endpoint = env.DOCKER_CONTEXT || !env.DOCKER_HOST
    ? run(['context', 'inspect', '--format', '{{.Endpoints.docker.Host}}'], true)
    : env.DOCKER_HOST;
  validateDockerEndpoint(endpoint);
  const project = fresh ? 'runloom-fresh' : 'runloom-local';
  const args = ['compose', '--project-directory', root, '--env-file', resolve(root, fresh ? 'deploy/fresh.env' : env.RUNLOOM_LOCAL_ENV_FILE || 'deploy/local.env'), '-p', project, '-f', 'compose/compose.yaml', '-f', 'compose/compose.local.yaml'];
  const config = JSON.parse(run([...args, '--profile', 'local-setup', 'config', '--format', 'json'], true));
  validateLocalConfig(config, project);
  if (action === 'config') { console.log('Local configuration validated; no services or accounts changed.'); return; }
  if (action === 'down') { run([...args, '--profile', 'local-setup', 'down']); return; }
  let provisionerPath;
  if (action === 'up') {
    // Respect file paths supplied through the selected Compose env file as well as the shell.
    const interpolation = run([...args, 'config', '--environment'], true);
    provisionerPath = env.RUNLOOM_PROVISIONER_SECRET_PATH || interpolation.split('\n').find(line => line.startsWith('RUNLOOM_PROVISIONER_SECRET_PATH='))?.slice('RUNLOOM_PROVISIONER_SECRET_PATH='.length);
    const apiIDs = run([...args, 'ps', '--all', '--quiet', 'api'], true).trim().split(/\s+/).filter(Boolean);
    if (apiIDs.some(id => !/^[a-f0-9]{12,64}$/.test(id))) throw new Error('Cannot identify existing local API safely.');
    if (apiIDs.length) assertLocalDatabaseContinuity(config, JSON.parse(run(['inspect', ...apiIDs], true)));
    if (!provisionerPath && apiIDs.length) {
      const mounts = JSON.parse(run(['inspect', ...apiIDs], true)).flatMap(container => (container.Mounts || []).filter(mount => mount.Destination === '/run/secrets/runloom-provisioner'));
      if (mounts.some(mount => mount.Type !== 'bind') || new Set(mounts.map(mount => mount.Source)).size > 1) throw new Error('Supply the original provisioner credential file explicitly.');
      provisionerPath = mounts[0]?.Source;
    }
    let supplied = env.RUNLOOM_SECRETS_KEY_PATH || interpolation.split('\n').find(line => line.startsWith('RUNLOOM_SECRETS_KEY_PATH='))?.slice('RUNLOOM_SECRETS_KEY_PATH='.length);
    if (!supplied) {
      // Preserve a previously supplied mount even when the next shell forgets
      // its override. Never silently switch an existing vault to the default key.
      const ids = run([...args, 'ps', '--all', '--quiet', 'api', 'worker'], true).trim().split(/\s+/).filter(Boolean);
      if (ids.some(id => !/^[a-f0-9]{12,64}$/.test(id))) throw new Error('Cannot identify existing local containers safely.');
      if (ids.length) {
        const containers = JSON.parse(run(['inspect', ...ids], true));
        const mounts = containers.flatMap(container => (container.Mounts || []).filter(mount => mount.Destination === '/run/secrets/workflow-key'));
        if (mounts.some(mount => mount.Type !== 'bind')) throw new Error('Existing vault key is not a host file; supply its original key file explicitly.');
        const paths = [...new Set(mounts.map(mount => mount.Source))];
        if (paths.length > 1) throw new Error('Existing API and worker use different key files; resolve this before startup.');
        supplied = paths[0];
      }
    }
    const key = await (dependencies.ensureKey || ensureLocalWorkflowKey)({ ...env, RUNLOOM_SECRETS_KEY_PATH: supplied });
    composeEnv.RUNLOOM_SECRETS_KEY_PATH = key.path;
    composeEnv.RUNLOOM_SECRETS_KEY_GID = key.gid;
    vaultGroup = key.gid;
    args.push('-f', 'compose/compose.secrets.yaml');
    validateLocalConfig(JSON.parse(run([...args, '--profile', 'local-setup', 'config', '--format', 'json'], true)), project);
    console.log(`Local workflow encryption key ${key.created ? 'created' : 'reused'}; value not printed. Back it up separately from the database.`);
  }
  run([...args, 'build', 'local-admin']);
  run([...args, 'up', '-d', '--wait', '--wait-timeout', '180', 'identity-db', 'mailpit', 'keycloak', 'postgres', 'mongodb']);
  // Synchronous one-shot barrier: startup stops here if either identity or membership setup fails.
  run([...args, 'run', '--rm', '--no-deps', 'local-admin', ...(action === 'reset' ? ['--reset-password'] : [])]);
  if (action === 'up') {
    const port = config.services.keycloak.ports?.find(port => Number(port.target) === 8080 && port.host_ip === '127.0.0.1');
    if (!port || !/^\d+$/.test(String(port.published))) throw new Error('Local service-account setup requires a loopback Keycloak port.');
    const provisioner = await (dependencies.ensureConsumer || ensureLocalConsumer)({
      baseURL: `http://127.0.0.1:${port.published}/auth`,
      secretFile: provisionerPath ? resolve(root, provisionerPath) : resolve(homedir(), '.config/runloom/local/compose', `${project}-provisioner-secret`),
      managed: !provisionerPath,
    });
    composeEnv.RUNLOOM_PROVISIONER_SECRET_PATH = provisioner.path;
    composeEnv.RUNLOOM_PROVISIONER_SECRET_GID = provisioner.gid;
    args.push('-f', 'compose/compose.consumer.yaml');
    // Compose concatenates group_add from overlays, including duplicate GIDs.
    // Use an ephemeral stdin override so shared host groups remain valid, while
    // preserving both groups when external credential files have different owners.
    const groups = [...new Set([vaultGroup, provisioner.gid].filter(Boolean).map(String))];
    if (groups.some(group => !/^\d+$/.test(group))) throw new Error('Invalid credential file group.');
    composeInput = `services:\n  api:\n    group_add: !override ${JSON.stringify(groups)}\n`;
    args.push('-f', '-');
    validateLocalConfig(JSON.parse(run([...args, '--profile', 'local-setup', 'config', '--format', 'json'], true)), project);
    console.log(`Local service-account provisioner ${provisioner.created ? 'created' : 'reused'} and verified; credential not printed.`);
    run([...args, 'up', '--build', '-d', '--wait', '--wait-timeout', '180']);
  }
  console.log('Local Studio: ' + config.services.keycloak.environment.KC_HOSTNAME.replace(/\/auth\/?$/, '/') + ' (default credentials: docs/local-development.md).');
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  try { await executeLocalStack(process.argv[2]); }
  catch (error) { console.error(error.message); process.exitCode = 1; }
}
