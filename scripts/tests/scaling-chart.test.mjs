import test from 'node:test';
import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
const root = fileURLToPath(new URL('../../', import.meta.url));
const roles = ['api', 'auth', 'workflows', 'management', 'worker', 'ui', 'workflows-ui', 'management-ui', 'platform-ui'];
function render(args = [], success = true) {
  const result = spawnSync('helm', ['template', 'runloom', 'charts/runloom', ...args], {cwd: root, encoding: 'utf8'});
  assert.equal(result.status === 0, success, 'Unexpected chart validation result; rendered Secrets omitted');
  return result.stdout.split(/\n---\n/);
}
const find = (docs, kind, role) => docs.find(d => d.includes(`kind: ${kind}\n`) && d.includes(`  name: runloom-${role}\n`));
test('local defaults keep fixed replicas with tokenless per-workload accounts and internal services', () => {
  const docs = render();
  assert.ok(!docs.some(d => d.includes('kind: HorizontalPodAutoscaler') || d.includes('kind: PodDisruptionBudget') || d.includes('kind: Ingress')));
  for (const role of roles) {
    assert.ok(find(docs, 'Deployment', role).includes('replicas: 1'));
    assert.ok(find(docs, 'Deployment', role).includes(`serviceAccountName: runloom-${role}`));
    assert.ok(find(docs, 'ServiceAccount', role).includes('automountServiceAccountToken: false'));
    assert.ok(find(docs, 'Service', role).includes('type: ClusterIP'));
  }
  assert.ok(find(docs, 'Service', 'worker').includes('port: 8081'));
});
test('production creates nine independent HPAs and disruption budgets without Helm replica contention', () => {
  const docs = render(['-f', 'charts/runloom/values-production.yaml']);
  for (const role of roles) {
    const deploy = find(docs, 'Deployment', role), hpa = find(docs, 'HorizontalPodAutoscaler', role), pdb = find(docs, 'PodDisruptionBudget', role);
    assert.ok(!deploy.includes('  replicas:'), 'HPA owns the replica count');
    assert.ok(deploy.includes('rollingUpdate: {maxSurge: 1, maxUnavailable: 0}'));
    assert.ok(deploy.includes('topologySpreadConstraints:'));
    assert.ok(hpa.includes('apiVersion: autoscaling/v2'));
    assert.ok(hpa.includes(`name: runloom-${role}`));
    assert.ok(hpa.includes('minReplicas: 2'));
    assert.ok(hpa.includes('averageUtilization: 70'));
    assert.ok(hpa.includes('stabilizationWindowSeconds: 300'));
    assert.ok(pdb.includes('apiVersion: policy/v1'));
    assert.ok(pdb.includes('maxUnavailable: 1'));
    assert.ok(pdb.includes(`app.kubernetes.io/component: ${role}`));
  }
  const ingress = docs.find(d => d.includes('kind: Ingress'));
  for (const text of ['ingressClassName: "nginx"', 'host: "runloom.example.com"', 'secretName: runloom-tls', 'name: runloom-ui', 'pathType: Prefix']) assert.ok(ingress.includes(text));
  assert.ok(!ingress.includes('name: runloom-auth'));
});
test('metrics, replicas, service annotations and external accounts can be overridden independently', () => {
  const docs = render(['--set', 'autoscaling.workloads.auth.enabled=true', '--set', 'autoscaling.workloads.auth.targetCPUUtilizationPercentage=0', '--set', 'autoscaling.workloads.auth.targetMemoryUtilizationPercentage=80', '--set', 'autoscaling.workloads.auth.minReplicas=3', '--set', 'autoscaling.workloads.auth.maxReplicas=5', '--set', 'serviceAccount.create=false', '--set', 'serviceAccount.name=external-account', '--set', 'service.uiType=LoadBalancer', '--set-string', 'service.uiAnnotations.example=enabled', '--set', 'podDisruptionBudgets.workloads.auth.enabled=true', '--set-string', 'podDisruptionBudgets.workloads.auth.maxUnavailable=25%']);
  const hpa = find(docs, 'HorizontalPodAutoscaler', 'auth');
  assert.ok(hpa.includes('name: memory') && !hpa.includes('name: cpu'));
  assert.ok(hpa.includes('minReplicas: 3') && hpa.includes('maxReplicas: 5'));
  assert.ok(find(docs, 'Deployment', 'management').includes('replicas: 1'));
  assert.ok(find(docs, 'Deployment', 'auth').includes('serviceAccountName: external-account'));
  assert.ok(!docs.some(d => d.includes('kind: ServiceAccount')));
  assert.ok(find(docs, 'Service', 'ui').includes('type: LoadBalancer'));
  assert.ok(find(docs, 'Service', 'ui').includes('example: enabled'));
  assert.ok(!find(docs, 'Service', 'auth').includes('example: enabled'));
  assert.ok(find(docs, 'PodDisruptionBudget', 'auth').includes('maxUnavailable: "25%"'));
});
test('invalid scaling, ingress and topology settings fail before deployment', () => {
  for (const values of [
    ['autoscaling.workloads.typo.enabled=true'],
    ['autoscaling.workloads.api.enabled=true', 'autoscaling.workloads.api.minReplicas=7'],
    ['autoscaling.workloads.api.enabled=true', 'autoscaling.defaults.targetCPUUtilizationPercentage=0'],
    ['autoscaling.workloads.api.enabled=true', 'api.resources.requests.cpu=0'],
    ['autoscaling.workloads.auth.enabled=true', 'splitServices.auth.resources.requests.cpu=0m'],
    ['autoscaling.workloads.api.enabled=true', 'autoscaling.defaults.minReplicas=0'],
    ['autoscaling.defaults.behavior.scaleDown.stabilizationWindowSeconds=3601'],
    ['podDisruptionBudgets.workloads.api.maxUnavailable=101%'],
    ['podDisruptionBudgets.workloads.api.maxUnavailable=-1'],
    ['service.uiType=ExternalName'],
    ['ingress.enabled=true', 'ingress.host=https://bad.example'],
    ['ingress.tls[0].secretName=tls'],
    ['topologySpreadConstraints[0].maxSkew=0', 'topologySpreadConstraints[0].topologyKey=zone', 'topologySpreadConstraints[0].whenUnsatisfiable=Wrong'],
  ]) render(values.flatMap(v => ['--set', v]), false);
});
