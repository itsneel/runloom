// Local-only provisioning, shared by startup and the optional repair command.
import { homedir } from 'node:os';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { command } from './local-k8s-command.mjs';
import { ensureLocalConsumer } from './local-consumer-auth.mjs';
import { withKeycloakForward } from './local-k8s-forward.mjs';

export function provisioningSettings(values, plan) {
  const expected = {
    clientID: 'runloom-provisioner', existingSecret: `${plan.fullname}-provisioner`,
    secretKey: 'client-secret', adminURL: `http://${plan.fullname}-keycloak:8080/auth`,
  };
  const current = values.consumer?.provisioning || {};
  for (const [key, value] of Object.entries(expected)) {
    if (current[key] && current[key] !== value) throw new Error('Custom provisioning settings found; use the operator setup instead of overriding them.');
  }
  return expected;
}

export async function provisionLocalConsumer(c, plan, dependencies = {}) {
  const run = dependencies.run || command;
  const helm = ['--kube-context', c.context, '--namespace', c.namespace];
  const kube = ['--context', c.context, '--namespace', c.namespace];
  const values = JSON.parse(run(c.helm, ['get', 'values', c.release, ...helm, '--all', '--output', 'json'], true));
  const settings = provisioningSettings(values, plan);
  const stored = run(c.kubectl, [...kube, 'get', 'secret', settings.existingSecret, '--ignore-not-found', '-o', 'json'], true);
  const secretFile = join(homedir(), '.config', 'runloom', 'local', c.cluster, c.namespace, `${c.release}-provisioner-secret`);
  await (dependencies.forward || withKeycloakForward)(c, plan.fullname, baseURL => (dependencies.ensureConsumer || ensureLocalConsumer)({
    baseURL, secretFile, organizations: true,
    storedSecret: stored ? Buffer.from(JSON.parse(stored).data?.[settings.secretKey] || '', 'base64').toString('utf8').trim() : undefined,
  }));
  if (!stored) run(c.kubectl, [...kube, 'create', 'secret', 'generic', settings.existingSecret, `--from-file=${settings.secretKey}=${secretFile}`], true);
  const current = values.consumer?.provisioning || {};
  if (current.enabled !== true || Object.entries(settings).some(([key, value]) => current[key] !== value)) {
    run(c.helm, ['upgrade', c.release, 'charts/runloom', ...helm, '--reuse-values',
      '--set', 'consumer.provisioning.enabled=true',
      ...Object.entries(settings).flatMap(([key, value]) => ['--set-string', `consumer.provisioning.${key}=${value}`]),
      '--wait', '--timeout', c.timeout]);
  }
  console.log('Local service-account provisioning verified; existing credentials preserved.');
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  try {
    // Import the CLI only here, never from the module dependency graph (TLA cycle).
    const { configuration, execute } = await import('./local-k8s.mjs');
    const c = configuration();
    await provisionLocalConsumer(c, execute('open', c, command, () => {}));
  } catch (error) { console.error(error.message); process.exitCode = 1; }
}
