// Shared runner must not import either CLI entry point (including dynamically).
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('../', import.meta.url));

export function command(tool, args, capture = false, spawn = spawnSync) {
  const result = spawn(tool, args, {
    cwd: root, encoding: 'utf8', stdio: capture ? 'pipe' : 'inherit',
    env: { ...process.env, KIND_EXPERIMENTAL_PROVIDER: 'docker' },
  });
  if (result.error?.code === 'ENOENT') throw new Error(`Missing ${tool}; see the local Kubernetes prerequisites in docs/deployment.md.`);
  if (result.error || result.status !== 0) {
    // Only extract known diagnostic phrases; captured stdout may contain Secrets.
    const stderr = result.stderr || '';
    const unknownFlag = stderr.match(/unknown flag: (--[a-z][a-z0-9-]*)/i)?.[1];
    const reason = unknownFlag ? `Unsupported flag ${unknownFlag}; check tool-version compatibility.`
      : /Kubernetes cluster unreachable|connection refused/i.test(stderr) ? 'The local Kubernetes cluster is unreachable; check Docker and the kind cluster.'
      : /forbidden|unauthorized/i.test(stderr) ? 'Access was denied; check Kubernetes credentials and permissions.'
      : /timed out|deadline exceeded/i.test(stderr) ? 'The operation timed out; check cluster readiness.'
      : 'Check the local tools/cluster; captured configuration is not printed.';
    throw new Error(`${tool} ${args[0]} failed (exit ${result.status ?? 'unknown'}). ${reason}`);
  }
  return result.stdout?.trim() || '';
}
