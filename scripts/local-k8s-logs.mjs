import { spawn } from 'node:child_process';
import { command } from './local-k8s-command.mjs';

// One native kubectl follower per pod also includes databases without Helm labels.
// Discovery is a snapshot: rerun after a rollout or container restart.
export async function followLogs(plan, dependencies = {}) {
  const run = dependencies.run || command;
  const start = dependencies.spawn || spawn;
  const signals = dependencies.signals || process;
  const log = dependencies.log || console.log;
  const warn = dependencies.warn || console.error;
  const scope = ['--context', plan.context, '--namespace', plan.namespace];
  const pods = run(plan.kubectl, [...scope, 'get', 'pods', '-o', 'name'], true).trim().split(/\s+/).filter(Boolean);
  if (!pods.length) {
    log(`No pods in ${plan.context}/${plan.namespace}.`);
    return;
  }
  log(`Following ${pods.length} pods in ${plan.context}/${plan.namespace}, including all containers. Ctrl+C stops tailing only. Rerun after rollouts/restarts.`);
  const children = new Set();
  let stopping = false;
  let failed = false;
  const stop = () => {
    stopping = true;
    for (const child of children) child.kill('SIGTERM');
  };
  signals.on('SIGINT', stop);
  signals.on('SIGTERM', stop);
  try {
    await Promise.all(pods.map(pod => new Promise(resolve => {
      let child;
      const failure = () => {
        if (!stopping) {
          failed = true;
          warn(`Log follower failed for ${pod}; other followers continue. Check pod status and rerun make k8s-logs.`);
        }
      };
      try {
        // The parent owns Ctrl+C so kubectl exits cannot race its signal handler.
        child = start(plan.kubectl, [...scope, 'logs', pod, '--follow', '--all-containers=true', '--prefix=true', '--timestamps=true', '--tail=100'], {detached: true, stdio: ['ignore', 'inherit', 'inherit']});
      } catch {
        failure();
        resolve();
        return;
      }
      children.add(child);
      child.once('error', failure);
      child.once('close', code => {
        children.delete(child);
        if (code !== 0) failure();
        resolve();
      });
    })));
  } finally {
    signals.removeListener('SIGINT', stop);
    signals.removeListener('SIGTERM', stop);
  }
  if (failed) throw new Error('One or more pod log followers failed.');
}
