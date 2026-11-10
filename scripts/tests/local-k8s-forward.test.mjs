import test from 'node:test';
import assert from 'node:assert/strict';
import { EventEmitter } from 'node:events';
import { PassThrough } from 'node:stream';
import { withKeycloakForward } from '../local-k8s-forward.mjs';

test('temporary forwarding binds loopback on a random port and cleans up on success/failure', async () => {
  for (const fail of [false, true]) {
    let stopped = false;
    const spawn = (tool, args) => {
      assert.equal(tool, 'kubectl');
      assert.ok(args.includes('kind-runloom-local'));
      assert.ok(args.includes(':8080'));
      assert.ok(args.includes('127.0.0.1'));
      const child = new EventEmitter();
      Object.assign(child, { stdout: new PassThrough(), stderr: new PassThrough(), exitCode: null, kill: () => { stopped = true; } });
      setImmediate(() => child.stdout.write('Forwarding from 127.0.0.1:34567 -> 8080\n'));
      return child;
    };
    const result = withKeycloakForward({ kubectl: 'kubectl', context: 'kind-runloom-local', namespace: 'runloom' }, 'runloom', async url => {
      assert.equal(url, 'http://127.0.0.1:34567/auth');
      if (fail) throw new Error('simulated');
    }, spawn);
    if (fail) await assert.rejects(result, /simulated/); else await result;
    assert.equal(stopped, true);
  }
});
