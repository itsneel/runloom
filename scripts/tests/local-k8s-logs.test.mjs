import test from 'node:test';
import assert from 'node:assert/strict';
import { EventEmitter } from 'node:events';
import { followLogs } from '../local-k8s-logs.mjs';

const plan = {kubectl: 'custom-kubectl', context: 'kind-runloom-local', namespace: 'runloom'};
function harness(pods = 'pod/runloom-auth-1\npod/runloom-database-0\npod/completed-job') {
  const calls = [];
  const children = [];
  const signals = new EventEmitter();
  const warnings = [];
  const dependencies = {
    run: (tool, args, capture) => {calls.push({tool, args, capture}); return pods;},
    spawn: (tool, args, options) => {
      calls.push({tool, args, options});
      const child = new EventEmitter();
      child.kill = signal => {child.killedWith = signal; queueMicrotask(() => child.emit('close', null));};
      children.push(child);
      return child;
    },
    signals, log: () => {}, warn: message => warnings.push(message),
  };
  return {calls, children, signals, warnings, dependencies};
}

test('tails every discovered pod including database and completed jobs, with source prefixes', async () => {
  const h = harness();
  const running = followLogs(plan, h.dependencies);
  assert.equal(h.children.length, 3);
  assert.deepEqual(h.calls[0], {tool: plan.kubectl, args: ['--context', plan.context, '--namespace', plan.namespace, 'get', 'pods', '-o', 'name'], capture: true});
  assert.deepEqual(h.calls.slice(1).map(call => call.args[5]), ['pod/runloom-auth-1', 'pod/runloom-database-0', 'pod/completed-job']);
  for (const call of h.calls.slice(1)) {
    assert.equal(call.tool, plan.kubectl);
    assert.equal(call.options.detached, true);
    assert.deepEqual(call.args.slice(0, 4), ['--context', plan.context, '--namespace', plan.namespace]);
    for (const flag of ['--follow', '--all-containers=true', '--prefix=true', '--timestamps=true', '--tail=100']) assert.ok(call.args.includes(flag));
  }
  h.children[2].emit('close', 0);
  h.signals.emit('SIGINT');
  await running;
  assert.deepEqual(h.children.map(c => c.killedWith), ['SIGTERM', 'SIGTERM', undefined]);
  assert.equal(h.signals.listenerCount('SIGINT'), 0);
  assert.equal(h.signals.listenerCount('SIGTERM'), 0);
});

test('empty namespace starts no followers', async () => {
  const h = harness('');
  await followLogs(plan, h.dependencies);
  assert.equal(h.children.length, 0);
  assert.equal(h.signals.listenerCount('SIGINT'), 0);
});

test('follower failures remain visible without killing other streams', async () => {
  const h = harness();
  const running = followLogs(plan, h.dependencies);
  h.children[0].emit('close', 1);
  assert.equal(h.warnings.length, 1);
  assert.equal(h.children[1].killedWith, undefined);
  h.signals.emit('SIGTERM');
  await assert.rejects(running, /log followers failed/);
  assert.equal(h.signals.listenerCount('SIGTERM'), 0);
});

test('spawn failures and pod listing failures reject', async () => {
  const h = harness('pod/missing');
  h.dependencies.spawn = () => {throw new Error('missing binary');};
  await assert.rejects(followLogs(plan, h.dependencies), /log followers failed/);
  assert.equal(h.signals.listenerCount('SIGINT'), 0);
  h.dependencies.run = () => {throw new Error('access denied');};
  await assert.rejects(followLogs(plan, h.dependencies), /access denied/);
});
