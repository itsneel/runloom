import { spawn } from 'node:child_process';

// Own a temporary random loopback port, independently of Studio/browser forwarding.
export async function withKeycloakForward(c, fullname, action, spawnChild = spawn) {
  const child = spawnChild(c.kubectl, ['--context', c.context, '--namespace', c.namespace,
    'port-forward', '--address', '127.0.0.1', `service/${fullname}-keycloak`, ':8080'], { stdio: ['ignore', 'pipe', 'pipe'] });
  const stop = () => { if (child.exitCode === null) child.kill(); };
  process.once('SIGINT', stop);
  process.once('SIGTERM', stop);
  try {
    const port = await new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error('Local Keycloak port-forward timed out.')), 30000);
      let output = '';
      child.stdout.on('data', data => {
        output = (output + data.toString()).slice(-2048);
        const match = output.match(/Forwarding from 127\.0\.0\.1:(\d+) ->/);
        if (match) { clearTimeout(timer); resolve(Number(match[1])); }
      });
      child.stderr.resume();
      child.once('error', () => { clearTimeout(timer); reject(new Error('Unable to start local Keycloak port-forward.')); });
      child.once('exit', () => { clearTimeout(timer); reject(new Error('Local Keycloak port-forward stopped.')); });
    });
    return await action(`http://127.0.0.1:${port}/auth`);
  } finally {
    stop();
    process.removeListener('SIGINT', stop);
    process.removeListener('SIGTERM', stop);
  }
}
