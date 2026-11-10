import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const children = [
  ['host', ...process.argv.slice(2)],
  ['workflows', '--port', process.env.RUNLOOM_WORKFLOWS_PORT || '4201'],
  ['management', '--port', process.env.RUNLOOM_MANAGEMENT_PORT || '4202'],
].map(([project,...args]) => {
  const cwd = fileURLToPath(new URL(`../studio/projects/${project}/`, import.meta.url));
  return spawn(process.execPath,[`${cwd}node_modules/@angular/cli/bin/ng.js`,'serve',project,...args],{cwd,stdio:'inherit',env:process.env});
});
let stopping = false;
function stop(code) { if (stopping) return; stopping = true; for (const child of children) child.kill('SIGTERM'); process.exitCode = code; }
for (const child of children) {
  child.on('error', () => stop(1));
  child.on('exit', code => { if (!stopping) stop(code || 0); });
}
process.on('SIGINT', () => stop(0));
process.on('SIGTERM', () => stop(0));
