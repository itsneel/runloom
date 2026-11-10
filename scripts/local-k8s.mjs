// Local kind only. No shell interpolation, credential output, or implicit data deletion.
import { randomUUID } from 'node:crypto';
import { pathToFileURL } from 'node:url';
import { resolve } from 'node:path';
import { mkdtempSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { validateDockerEndpoint } from './local-stack.mjs';
import { command } from './local-k8s-command.mjs';
import { localServiceValues, provisionLocalDatabases } from './local-k8s-services.mjs';
export { command } from './local-k8s-command.mjs';
import { localAccountTable } from './local-account-table.mjs';

// Helm 4 removed `list --all`; explicit status filters work in Helm 3 and 4.
const releaseStatuses = ['--deployed', '--failed', '--pending', '--uninstalled', '--superseded', '--uninstalling'];
const components = [
  ['assistant', 'runloom-assistant', 'server/assistant'],
  ['auth', 'runloom-auth', 'server/auth'],
  ['localAdmin', 'runloom-local-admin', 'server/auth', 'Dockerfile.local-admin'],
  ['workflows', 'runloom-workflows', 'server/workflows'],
  ['worker', 'runloom-worker', 'server/workflows', 'Dockerfile.worker'],
  ['management', 'runloom-management', 'server/management'],
  ['gateway', 'runloom-gateway', 'server/gateway'],
  ['ui', 'runloom-ui', 'studio/projects/host'],
  ['workflows-ui', 'runloom-workflows-ui', 'studio/projects/workflows'],
  ['management-ui', 'runloom-management-ui', 'studio/projects/management'],
  ['platform-ui', 'runloom-platform-ui', 'studio/projects/platform-admin'],
  ['docs', 'runloom-docs', 'docs-site'],
];

export function configuration(env = process.env) {
  if (env.RUNLOOM_ENV && env.RUNLOOM_ENV !== 'local') throw new Error('Local Kubernetes requires RUNLOOM_ENV=local or unset.');
  const c = {
    cluster: env.K8S_CLUSTER || 'runloom-local',
    namespace: env.K8S_NAMESPACE || 'runloom',
    release: env.K8S_RELEASE || 'runloom',
    timeout: env.K8S_TIMEOUT || '10m',
    database: env.K8S_DATABASE || '',
    port: env.K8S_PORT || '',
    docsPort: env.K8S_DOCS_PORT || '',
    docker: env.DOCKER || 'docker', kind: env.KIND || 'kind',
    kubectl: env.KUBECTL || 'kubectl', helm: env.HELM || 'helm',
    dockerHost: env.DOCKER_HOST, dockerContext: env.DOCKER_CONTEXT,
    serviceValues: env.K8S_SERVICE_VALUES,
  };
  for (const key of ['cluster', 'namespace', 'release']) {
    if (!/^[a-z][a-z0-9-]{0,46}[a-z0-9]$|^[a-z]$/.test(c[key])) throw new Error(`K8S_${key.toUpperCase()} must be a lowercase DNS label of at most 48 characters.`);
  }
  if (c.database && !['mongodb', 'postgres'].includes(c.database)) throw new Error('K8S_DATABASE must be mongodb or postgres.');
  if (c.port && (!/^\d+$/.test(c.port) || Number(c.port) < 1024 || Number(c.port) > 65535)) throw new Error('K8S_PORT must be between 1024 and 65535.');
  if (c.docsPort && (!/^\d+$/.test(c.docsPort) || Number(c.docsPort) < 1024 || Number(c.docsPort) > 65535)) throw new Error('K8S_DOCS_PORT must be between 1024 and 65535.');
  if (!/^[1-9]\d*(s|m)$/.test(c.timeout)) throw new Error('K8S_TIMEOUT must be a positive duration such as 10m or 600s.');
  return { ...c, context: `kind-${c.cluster}` };
}

export function localOrigin(value) {
  const url = new URL(value);
  if (url.protocol !== 'http:' || !['localhost', '127.0.0.1'].includes(url.hostname) || url.username || url.password || url.pathname !== '/' || url.search || url.hash) {
    throw new Error('Local Kubernetes requires an HTTP localhost/127.0.0.1 browser origin.');
  }
  return url;
}

export function execute(action, c = configuration(), run = command, log = console.log, dependencies = {}) {
  if (!['cluster', 'up', 'down', 'install', 'upgrade', 'run', 'open', 'docs', 'status', 'logs', 'test'].includes(action)) throw new Error('Expected cluster, up, down, install, upgrade, run, open, docs, status, logs, or test.');
  const endpoint = c.dockerContext || !c.dockerHost
    ? run(c.docker, ['context', 'inspect', '--format', '{{.Endpoints.docker.Host}}'], true)
    : c.dockerHost;
  validateDockerEndpoint(endpoint);
  run(c.docker, ['info', '--format', '{{.ServerVersion}}'], true);
  const clusters = run(c.kind, ['get', 'clusters'], true).split(/\s+/);
  if (action === 'down') {
    if (!clusters.includes(c.cluster)) {
      log(`Local kind cluster ${c.cluster} is already absent.`);
      return;
    }
    log(`Deleting local kind cluster ${c.cluster}: all workloads, Kubernetes Secrets and cluster-local persistent data will be permanently removed.`);
    run(c.kind, ['delete', 'cluster', '--name', c.cluster]);
    log(`Deleted ${c.cluster}. Docker images, host credential backups and other clusters are preserved.`);
    return;
  }
  if (!clusters.includes(c.cluster)) {
    if (!['cluster', 'up'].includes(action)) throw new Error(`Cluster ${c.cluster} does not exist. Run make k8s-cluster first.`);
    run(c.kind, ['create', 'cluster', '--name', c.cluster, '--wait', c.timeout]);
  }
  const kube = ['--context', c.context];
  const ns = [...kube, '--namespace', c.namespace];
  const helm = ['--kube-context', c.context, '--namespace', c.namespace];
  // A matching context name alone is not proof it points at a local cluster.
  const kubeconfig = JSON.parse(run(c.kubectl, [...kube, 'config', 'view', '--minify', '-o', 'json'], true));
  const server = new URL(kubeconfig.clusters?.[0]?.cluster?.server);
  if (server.protocol !== 'https:' || !['127.0.0.1', 'localhost', '[::1]'].includes(server.hostname)) {
    throw new Error('Refusing a non-loopback Kubernetes API endpoint, even with a kind context name.');
  }
  if (action === 'cluster') {
    run(c.kubectl, [...kube, 'wait', '--for=condition=Ready', 'nodes', '--all', `--timeout=${c.timeout}`]);
    log(`Local cluster ready: ${c.context}`);
    return;
  }
  if (action === 'logs') return {kubectl: c.kubectl, context: c.context, namespace: c.namespace};
  if (action === 'status') {
    run(c.helm, ['list', ...helm, ...releaseStatuses]);
    run(c.kubectl, [...ns, 'get', 'pods,services,pvc']);
    return;
  }
  const releases = JSON.parse(run(c.helm, ['list', ...helm, ...releaseStatuses, '--filter', `^${c.release}$`, '--output', 'json'], true));
  const release = releases.find(item => item.name === c.release);
  if (release && !release.chart?.startsWith('runloom-')) throw new Error('Refusing to change a release that is not the Runloom chart.');
  if (action === 'install' && release) throw new Error('Release already exists. Use make k8s-upgrade or make k8s-up.');
  if (['upgrade', 'run', 'open', 'docs', 'test'].includes(action) && !release) throw new Error('Release does not exist. Run make k8s-up first.');
  // Read effective values privately so upgrades retain optional secrets/settings.
  const values = release ? JSON.parse(run(c.helm, ['get', 'values', c.release, ...helm, '--all', '--output', 'json'], true)) : {};
  if (release && (values.development?.enabled !== true || values.ingress?.enabled)) throw new Error('These commands only manage bundled, non-ingress development releases.');
  const existingOrigin = values.auth?.publicURL;
  if (existingOrigin) localOrigin(existingOrigin);
  // Imported Keycloak clients persist their redirect URLs in the identity DB.
  // Changing only KC_HOSTNAME on upgrade would leave those URLs stale.
  if (existingOrigin && c.port && Number(localOrigin(existingOrigin).port || 80) !== Number(c.port)) {
    throw new Error('K8S_PORT differs from the installed login origin. Use a separate K8S_RELEASE; changing an existing origin needs coordinated Keycloak configuration.');
  }
  const origin = existingOrigin || `http://localhost:${c.port ? Number(c.port) : 4200}`;
  const url = localOrigin(origin);
  const prefix = values.fullnameOverride || (c.release.includes(values.nameOverride || 'runloom') ? c.release : `${c.release}-${values.nameOverride || 'runloom'}`);
  const fullname = prefix.slice(0, 48).replace(/-$/, '');
  const docsOrigin = c.docsPort ? `http://localhost:${Number(c.docsPort)}` : values.docs?.publicURL || `http://localhost:${url.port === '4400' ? 4401 : 4400}`;
  const docsURL = new URL(docsOrigin);
  const docsLocal = values.docs?.enabled !== false && docsURL.protocol === 'http:' && ['localhost', '127.0.0.1'].includes(docsURL.hostname);
  const docsForwardArgs = docsLocal ? [...ns, 'port-forward', '--address', '127.0.0.1', `service/${fullname}-docs`, `${docsURL.port || '80'}:80`] : undefined;
  if (action === 'docs') {
    if (!docsForwardArgs) throw new Error(`Documentation is externally hosted. Open ${docsOrigin}.`);
    log(`Documentation: ${docsOrigin} — keep this terminal open.`);
    run(c.kubectl, docsForwardArgs);
    return;
  }
  if (action === 'open') return {
    docsOrigin, docsForwardArgs,
    localAdminImage: values.images?.localAdmin,
    origin: url.origin, kubectl: c.kubectl,
    cluster: c.cluster, context: c.context, namespace: c.namespace, fullname,
    docker: c.docker, kind: c.kind, databaseSecretKey: values.splitServices?.databases?.auth?.secretKey || 'url', databaseSecretName: values.splitServices?.databases?.auth?.existingSecret || 'runloom-auth-database',
    forwardArgs: [...ns, 'port-forward', '--address', '127.0.0.1', `service/${fullname}-ui`, `${url.port || '80'}:80`],
  };
  if (action === 'run') {
    log(`Studio: ${url.origin} — keep this terminal open; Ctrl+C stops forwarding, not the cluster.`);
    run(c.kubectl, [...ns, 'port-forward', '--address', '127.0.0.1', `service/${fullname}-ui`, `${url.port || '80'}:80`]);
    return;
  }
  if (action === 'test') {
    run(c.helm, ['test', c.release, ...helm, '--logs', '--timeout', c.timeout]);
    return;
  }
  if (release && values.splitServices?.enabled !== true) throw new Error('This release uses the removed monolith. Provision independent databases and use a fresh K8S_RELEASE; automatic migration is not supported.');
  const database = c.database || values.database?.driver || 'mongodb';
  if (release && database !== values.database?.driver) throw new Error('Refusing to switch an existing database/PVC. Use a separate K8S_RELEASE for a fresh database.');
  const tag = `dev-${Date.now()}-${randomUUID().slice(0, 8)}`;
  const managed = !c.serviceValues && (!release || values.localSetup?.managed === true);
  const buildComponents = managed ? [...components, ['database-setup', 'runloom-database-setup', 'tools/database-setup']] : components;
  const settings = ['--set', 'development.enabled=true', '--set', 'ingress.enabled=false',
    '--set-string', `docs.publicURL=${docsOrigin}`,
    ...(process.env.K8S_OBSERVABILITY === '1' ? ['--set', 'observability.enabled=true'] : []),
    '--set', 'splitServices.enabled=true', '--set-string', `database.driver=${database}`, '--set-string', `auth.publicURL=${url.origin}`,
    ...(c.serviceValues ? ['-f', c.serviceValues, '--set', 'localSetup.managed=false'] : [])];
  for (const [key, repository] of buildComponents) {
    const imagePath = key === 'docs' ? 'docs.image' : `images.${key}`;
    settings.push('--set-string', `${imagePath}.repository=${repository}`, '--set-string', `${imagePath}.tag=${tag}`, '--set', `${imagePath}.pullPolicy=IfNotPresent`);
  }
  // Preserve installed settings privately, dropping only obsolete monolith defaults.
  // Resetting chart defaults lets newly introduced per-service settings take effect.
  let temporary;
  try {
    const preserved = [];
    temporary = mkdtempSync(resolve(tmpdir(), 'runloom-helm-values-'));
    if (release) {
      const upgraded = structuredClone(values);
      for (const key of ['name', 'existingSecret', 'secretKey']) delete upgraded.database[key];
      if (upgraded.images) delete upgraded.images.backend;
      delete upgraded.development.mongodbImage;
      const path = resolve(temporary, 'values.json');
      writeFileSync(path, JSON.stringify(upgraded), {mode: 0o600});
      preserved.push('-f', path);
    }
    if (managed) {
      const path = resolve(temporary, 'local-services.json');
      writeFileSync(path, JSON.stringify(localServiceValues(fullname)), {mode: 0o600});
      preserved.push('-f', path);
    }
    // Validate the effective upgrade values before builds; never print rendered Secrets.
    run(c.helm, ['lint', '--strict', 'charts/runloom', ...preserved, ...settings]);
    for (const [, repository, context, dockerfile] of buildComponents) run(c.docker, ['build', '--tag', `${repository}:${tag}`, ...(dockerfile ? ['--file', `${context}/${dockerfile}`] : []), context]);
    run(c.kind, ['load', 'docker-image', ...buildComponents.map(([, repository]) => `${repository}:${tag}`), '--name', c.cluster]);
    if (managed) (dependencies.databases || provisionLocalDatabases)(c, {fullname, database, tag}, run);
    run(c.helm, [release ? 'upgrade' : 'install', c.release, 'charts/runloom', ...helm,
      ...(release ? ['--reset-values'] : ['--create-namespace']), ...preserved, ...settings, '--wait', '--timeout', c.timeout]);
  } finally {
    if (temporary) rmSync(temporary, {recursive: true, force: true});
  }
  return { fullname, origin: url.origin, managed, localAdminImage: {repository: 'runloom-local-admin', tag, pullPolicy: 'IfNotPresent'} };
}

export async function executeAction(action, c = configuration(), dependencies = {}) {
  const run = dependencies.run || command;
  const log = dependencies.log || console.log;
  const plan = execute(action, c, run, log, dependencies);
  if (action === 'logs') {
    const follow = dependencies.logs || (await import('./local-k8s-logs.mjs')).followLogs;
    await follow(plan);
  }
  if (['up', 'install', 'upgrade'].includes(action)) {
    const provision = dependencies.provision || (await import('./local-k8s-consumer.mjs')).provisionLocalConsumer;
    await provision(c, plan);
    if (plan.managed) {
      const admin = dependencies.admin || (await import('./local-k8s-open.mjs')).ensureLocalAdmin;
      const accounts = await admin(c, {...plan, namespace: c.namespace, context: c.context, databaseSecretName: `${plan.fullname}-auth-database`, databaseSecretKey: 'url'}, run);
      log(localAccountTable(accounts, plan.origin));
    }
    run(c.helm, ['test', c.release, '--kube-context', c.context, '--namespace', c.namespace, '--logs', '--timeout', c.timeout]);
    log(`Runloom ready in ${c.context}/${c.namespace}. Run make k8s-run with the same K8S_CLUSTER/K8S_NAMESPACE/K8S_RELEASE settings; open ${plan.origin}.`);
  }
  return plan;
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  try {
    const plan = await executeAction(process.argv[2]);
    if (process.argv[2] === 'open') await (await import('./local-k8s-open.mjs')).openWebsite(plan);
  }
  catch (error) { console.error(error.message); process.exitCode = 1; }
}
