// Subprocess CLI regression fixture: never connect to or mutate a real cluster.
import childProcess from 'node:child_process';
import { syncBuiltinESMExports } from 'node:module';

childProcess.spawnSync = (tool, args) => {
  let stdout;
  if (tool === 'docker' && args[0] === 'context') stdout = 'unix:///var/run/docker.sock';
  else if (tool === 'docker' && args[0] === 'info') stdout = 'test';
  else if (tool === 'kind' && args[0] === 'get') stdout = 'runloom-local';
  else if (tool === 'kubectl' && args.includes('view')) stdout = JSON.stringify({ clusters: [{ cluster: { server: 'https://127.0.0.1:6443' } }] });
  else if (tool === 'helm' && args[0] === 'list') stdout = JSON.stringify([{ name: 'runloom', chart: 'runloom-0.1.0' }]);
  else if (tool === 'helm' && args[0] === 'get') stdout = JSON.stringify({ development: { enabled: true }, auth: { publicURL: 'http://localhost:4200' }, database: { driver: 'mongodb' } });
  else throw new Error('Unexpected subprocess in isolated CLI test');
  return { status: 0, stdout, stderr: '' };
};
childProcess.spawn = () => { throw new Error('Isolated CLI test must stop before launching services'); };
syncBuiltinESMExports();
