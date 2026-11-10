import { spawn } from 'node:child_process';
import { createServer } from 'node:net';
import { existsSync } from 'node:fs';
import { dirname, delimiter, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { setTimeout as delay } from 'node:timers/promises';
import { randomUUID } from 'node:crypto';
import { command } from './local-k8s-command.mjs';
import { withPrivateManifest } from './local-k8s-services.mjs';
import { localAccountTable } from './local-account-table.mjs';

const root = fileURLToPath(new URL('../', import.meta.url));

export function bootstrapJob(plan, image, name, pullPolicy = 'IfNotPresent') {
  return {
    apiVersion: 'batch/v1', kind: 'Job', metadata: { name, namespace: plan.namespace },
    spec: { backoffLimit: 2, activeDeadlineSeconds: 150, ttlSecondsAfterFinished: 3600,
      template: { spec: { restartPolicy: 'Never', automountServiceAccountToken: false,
        securityContext: { runAsNonRoot: true, runAsUser: 65532, fsGroup: 65532, seccompProfile: { type: 'RuntimeDefault' } },
        volumes: [
          {name: 'provisioner', secret: {secretName: `${plan.fullname}-provisioner`, defaultMode: 0o440}},
          {name: 'configuration', configMap: {name: `${plan.fullname}-config`, items: [{key: 'auth.yaml', path: 'config.yaml'}]}},
          {name: 'database', secret: {secretName: plan.databaseSecretName || `${plan.fullname}-auth-database`, defaultMode: 0o440, items: [{key: plan.databaseSecretKey || 'url', path: 'url'}]}},
        ],
        containers: [{ name: 'local-admin', image, imagePullPolicy: pullPolicy, command: ['/runloom-local-admin'], args: ['--json'],
          securityContext: { allowPrivilegeEscalation: false, readOnlyRootFilesystem: true, capabilities: { drop: ['ALL'] } },
          resources: { requests: { cpu: '50m', memory: '64Mi' }, limits: { cpu: '1', memory: '256Mi' } },
          volumeMounts: [{name: 'provisioner', mountPath: '/var/run/runloom-provisioner', readOnly: true}, {name: 'configuration', mountPath: '/etc/runloom', readOnly: true}, {name: 'database', mountPath: '/var/run/runloom-database', readOnly: true}],
          env: [
            { name: 'RUNLOOM_ENV', value: 'local' }, { name: 'RUNLOOM_LOCAL_BOOTSTRAP', value: '1' },
            { name: 'RUNLOOM_LOCAL_K8S_SERVICE_PREFIX', value: plan.fullname },
            { name: 'RUNLOOM_LOCAL_KEYCLOAK_URL', value: `http://${plan.fullname}-keycloak:8080/auth` },
            { name: 'RUNLOOM_CONFIG_FILE', value: '/etc/runloom/config.yaml' },
          ],
        }],
      } },
    },
  };
}

export async function ensureLocalAdmin(c, plan, run = command) {
  const image = plan.localAdminImage;
  if (!image?.repository || !image?.tag || !['Always', 'IfNotPresent', 'Never'].includes(image.pullPolicy)) {
    throw new Error('Dedicated administrator setup image is not configured. Run make k8s-upgrade to build/load it and set images.localAdmin.');
  }
  const kube = ['--context', c.context, '--namespace', c.namespace];
  // A Helm upgrade may still have an old ready replica while Keycloak rolls.
  // Wait for the new replicas before issuing identity-management requests.
  for (const role of ['keycloak', 'auth']) run(c.kubectl, [...kube, 'rollout', 'status', `deployment/${plan.fullname}-${role}`, `--timeout=${c.timeout || '5m'}`]);
  const name = `${plan.fullname.slice(0,42)}-local-admin-${randomUUID().slice(0, 8)}`;
  withPrivateManifest(bootstrapJob(plan, `${image.repository}:${image.tag}`, name, image.pullPolicy), path => run(c.kubectl, [...kube, 'create', '-f', path], true));
  run(c.kubectl, [...kube, 'wait', '--for=condition=complete', `job/${name}`, '--timeout=180s']);
  const account = JSON.parse(run(c.kubectl, [...kube, 'logs', `job/${name}`], true));
  if (account.email !== 'admin@runloom.test' || account.organization !== 'Runloom' || account.team !== 'General') throw new Error('Unexpected local administrator bootstrap result.');
  validateMember(account.member);
  validatePlatformAccounts(account);
  return account;
}

function validatePlatformAccounts(account) {
  for (const role of ['platform_admin', 'platform_staff']) {
    const entry = account[role];
    if (entry?.email !== `${role.replace('_', '-')}@runloom.test` || typeof entry.default_password !== 'string' ||
      typeof entry.created !== 'boolean' || !['', 'platform_admin', 'platform_staff'].includes(entry.role)) {
      throw new Error('Local platform account setup result is missing or invalid. Rebuild the admin setup image with make k8s-upgrade.');
    }
  }
}

function validateMember(member) {
  if (member?.email !== 'member@runloom.test' || typeof member.default_password !== 'string' || typeof member.created !== 'boolean' ||
    member.default_org_role !== 'member' || member.default_team_role !== 'editor') {
    throw new Error('Local team-member setup result is missing or invalid. Rebuild the admin setup image with make k8s-upgrade.');
  }
}

export function connectionInfo(plan, options, account) {
  if (account.email !== 'admin@runloom.test' || typeof account.default_password !== 'string' || typeof account.created !== 'boolean') throw new Error('Invalid local admin setup result.');
  validateMember(account.member);
  validatePlatformAccounts(account);
  return `Website: ${options.url}\nCluster: ${plan.context} | Namespace: ${plan.namespace}\n${localAccountTable(account, plan.origin)}\nUse Log in / Open workflow studio on the website.\nKeep this terminal open. Ctrl+C stops only this website preview and Studio forwarding.`;
}

export function websiteOptions(origin, env = process.env) {
  const port = Number(env.K8S_WEBSITE_PORT || 4300);
  if (!Number.isInteger(port) || port < 1024 || port > 65535) throw new Error('K8S_WEBSITE_PORT must be between 1024 and 65535.');
  const studioPort = Number(new URL(origin).port || 80);
  if (port === studioPort) throw new Error('Website and Studio need different ports. Set K8S_WEBSITE_PORT.');
  return { port, studioPort, url: `http://127.0.0.1:${port}`, browser: env.K8S_OPEN_BROWSER !== '0' };
}

export function browserCommand(url, platform = process.platform) {
  if (platform === 'darwin') return ['open', [url]];
  if (platform === 'win32') return ['rundll32', ['url.dll,FileProtocolHandler', url]];
  return ['xdg-open', [url]];
}

export async function assertFreePort(port) {
  await new Promise((resolve, reject) => {
    const server = createServer();
    server.once('error', () => reject(new Error(`Local port ${port} is occupied or unavailable. Stop the existing forward/preview or select another website port; no existing process was stopped.`)));
    server.listen(port, '127.0.0.1', () => server.close(resolve));
  });
}

export async function waitForURL(url, check, signal, timeout = 60000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    signal.throwIfAborted();
    try {
      const response = await fetch(url, { signal: AbortSignal.any([signal, AbortSignal.timeout(2000)]) });
      if (response.ok && await check(response)) return;
    } catch { /* Startup connection failures are retried until the deadline. */ }
    await delay(300, undefined, { signal });
  }
  throw new Error(`Timed out waiting for ${url}. Check make k8s-status and pod readiness.`);
}

export async function openWebsite(plan) {
  const options = websiteOptions(plan.origin);
  const website = resolve(root, 'Website');
  const cli = resolve(website, 'node_modules/@angular/cli/bin/ng.js');
  if (!existsSync(cli)) throw new Error('Website dependencies are missing. Run make setup-website first, then make k8s-open.');
  await assertFreePort(options.studioPort);
  await assertFreePort(options.port);
  if (plan.docsForwardArgs) {
    const docsPort = Number(new URL(plan.docsOrigin).port || 80);
    if ([options.studioPort, options.port].includes(docsPort)) throw new Error('Documentation must use a separate port from Studio and the website. Update docs.publicURL in Helm values.');
    await assertFreePort(docsPort);
  }
  const controller = new AbortController();
  const children = new Set();
  const childEnv = { ...process.env, PATH: `${dirname(process.execPath)}${delimiter}${process.env.PATH || ''}` };
  let stopping = false;
  let failure;
  let finish;
  const stopped = new Promise(resolve => { finish = resolve; });
  const stop = (error) => {
    if (stopping) return;
    stopping = true;
    failure = error;
    controller.abort();
    for (const child of children) child.kill('SIGTERM');
    finish();
  };
  const onSignal = () => stop();
  for (const signal of ['SIGINT', 'SIGTERM']) process.on(signal, onSignal);
  const start = (tool, args, cwd, env = childEnv, persistent = false, input) => {
    const child = spawn(tool, args, { cwd, env, stdio: input ? ['pipe', 'inherit', 'inherit'] : 'inherit' });
    if (input) {
      child.stdin.on('error', () => {}); // Process failure is reported by completion.
      child.stdin.end(input);
    }
    children.add(child);
    const completion = new Promise((resolve, reject) => {
      child.once('error', error => {
        children.delete(child);
        const problem = new Error(`Could not start ${tool}: ${error.message}`);
        if (persistent) stop(problem);
        reject(problem);
      });
      child.once('exit', (code) => {
        children.delete(child);
        if (persistent && !stopping) stop(new Error(`${tool} stopped unexpectedly (exit ${code}).`));
        if (code === 0 || stopping) resolve();
        else reject(new Error(`${tool} failed (exit ${code}).`));
      });
    });
    // Persistent children report failures through stop(), not unhandled rejections.
    if (persistent) completion.catch(() => {});
    return completion;
  };
  try {
    console.log('Connecting Studio and building the independent product website…');
    start(plan.kubectl, plan.forwardArgs, root, childEnv, true);
    if (plan.docsForwardArgs) {
      start(plan.kubectl, plan.docsForwardArgs, root, childEnv, true);
      const docsHealth = new URL('/healthz', plan.docsOrigin);
      docsHealth.hostname = '127.0.0.1';
      await waitForURL(docsHealth, async response => (await response.text()).trim() === 'ok', controller.signal);
    }
    if (plan.docsOrigin) console.log(`Documentation: ${plan.docsOrigin} (also linked in Studio's left menu)`);
    const health = new URL('/api/v1/health', plan.origin);
    health.hostname = '127.0.0.1';
    await waitForURL(health, async response => {
      const body = await response.json();
      return body.status === 'ok' && body.database === true && body.temporal === true;
    }, controller.signal);
    const keycloak = new URL('/auth/realms/runloom/.well-known/openid-configuration', health);
    await waitForURL(keycloak, async response => typeof (await response.json()).issuer === 'string', controller.signal);
    console.log('Ensuring the local organization admin, team member, platform admin and platform staff; existing passwords and platform access are preserved…');
    const account = await ensureLocalAdmin(plan, plan);
    await start(process.execPath, [cli, 'build'], website);
    controller.signal.throwIfAborted();
    start(process.execPath, ['scripts/preview.mjs', '--port', String(options.port)], website,
      { ...childEnv, RUNLOOM_WEBSITE_STUDIO_URL: plan.origin }, true);
    await waitForURL(options.url, async response => (await response.text()).includes(`name="runloom-studio-url" content="${plan.origin}/"`), controller.signal);
    console.log(connectionInfo(plan, options, account));
    if (options.browser) {
      const [tool, args] = browserCommand(options.url);
      try { await start(tool, args, root); }
      catch { console.warn(`Could not open a browser automatically. Open ${options.url} manually.`); }
    }
    await stopped;
    if (failure) throw failure;
  } catch (error) {
    if (failure) throw failure;
    if (!stopping) throw error;
  } finally {
    stopping = true;
    controller.abort();
    for (const signal of ['SIGINT', 'SIGTERM']) process.removeListener(signal, onSignal);
    await Promise.all([...children].map(child => new Promise(resolve => {
      const timer = setTimeout(() => child.kill('SIGKILL'), 3000);
      child.once('exit', () => { clearTimeout(timer); resolve(); });
      child.kill('SIGTERM');
    })));
  }
}
