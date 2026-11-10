// Fresh local-only deployments. Existing monolith data and volumes are never touched.
import { spawnSync } from 'node:child_process';
import { createHash, randomBytes } from 'node:crypto';
import { chmod, mkdir, readFile, writeFile, lstat, rename } from 'node:fs/promises';
import { homedir } from 'node:os';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { validateDockerEndpoint } from './local-stack.mjs';
import { ensureLocalConsumer } from './local-consumer-auth.mjs';
import { localAccountTable } from './local-account-table.mjs';

const root = fileURLToPath(new URL('../', import.meta.url));
export function plan(driver = 'mongodb') {
  if (!['mongodb', 'postgres'].includes(driver)) throw new Error('SERVICES_DATABASE must be mongodb or postgres.');
  const pg = driver === 'postgres';
  return { driver, project: `runloom-services-${driver}`, ports: { DOCS_PORT: pg ? 35400 : 34400, UI_PORT: pg ? 35200 : 34200, API_PORT: pg ? 39080 : 38080, MANAGEMENT_PORT: pg ? 39083 : 38083, KEYCLOAK_PORT: pg ? 39081 : 38081, TEMPORAL_PORT: pg ? 39233 : 37233, TEMPORAL_UI_PORT: pg ? 39234 : 38233, POSTGRES_PORT: pg ? 36432 : 35432, MONGODB_PORT: pg ? 37018 : 37017, MAILPIT_PORT: pg ? 39025 : 38025 } };
}

async function main() {
  const action = process.argv[2] || 'up';
  if (!['up', 'config', 'down', 'logs', 'admin', 'reset'].includes(action)) throw new Error('Expected up, config, down, logs, admin or reset.');
  const { driver, project, ports } = plan(process.env.SERVICES_DATABASE || 'mongodb');
  const telemetry = process.env.SERVICES_OBSERVABILITY === '1';
  const directory = resolve(homedir(), '.config/runloom/local/services', project);
  const env = { ...process.env, ...Object.fromEntries(Object.entries(ports).map(([k,v])=>[k,String(v)])), RUNLOOM_PUBLIC_URL: `http://localhost:${ports.UI_PORT}`, RUNLOOM_SERVICES_CONFIG_DIR: directory };
  for (const [key,value] of Object.entries(env)) if (key.startsWith('RUNLOOM_') && value === '') delete env[key];
  env.AUTH_IMAGE = 'runloom-auth:local';
  env.LOCAL_ADMIN_IMAGE = 'runloom-local-admin:local';
  env.WORKFLOWS_IMAGE = 'runloom-workflows:local';
  env.WORKER_IMAGE = 'runloom-worker:local';
  env.MANAGEMENT_IMAGE = 'runloom-management:local';
  env.GATEWAY_IMAGE = 'runloom-gateway:local';
  env.DATABASE_SETUP_IMAGE = 'runloom-database-setup:local';
  env.RUNLOOM_DOCUMENTATION_CONFIG_PATH = resolve(directory, 'documentation.json');
  env.RUNLOOM_DOCS_STUDIO_CONFIG_PATH = resolve(directory, 'docs-studio.json');
  const docker = env.DOCKER || 'docker';
  const run = (args, capture = false) => {
    const result = spawnSync(docker, args, { cwd: root, env, encoding: 'utf8', stdio: capture ? 'pipe' : 'inherit' });
    if (result.error || result.status !== 0) throw new Error('Local service operation failed; credential-bearing output was not printed.');
    return result.stdout?.trim() || '';
  };
  validateDockerEndpoint(env.DOCKER_CONTEXT || !env.DOCKER_HOST ? run(['context', 'inspect', '--format', '{{.Endpoints.docker.Host}}'], true) : env.DOCKER_HOST);
  await mkdir(directory, { recursive: true, mode: 0o700 });
  await chmod(directory, 0o700);
  const gid = String((await lstat(directory)).gid);
  env.RUNLOOM_SERVICE_GID = env.RUNLOOM_SECRETS_KEY_GID = env.RUNLOOM_PROVISIONER_SECRET_GID = gid;
  env.RUNLOOM_SECRETS_KEY_PATH = resolve(directory, 'workflow-key');
  env.RUNLOOM_PROVISIONER_SECRET_PATH = resolve(directory, 'provisioner-secret');
  const write = async (name, value) => {
    const path = resolve(directory,name), tmp = path + '.tmp';
    await writeFile(tmp, value, { mode: 0o640 }); await chmod(tmp,0o640); await rename(tmp,path);
  };
  const json = (name,value) => write(name,JSON.stringify(value,null,2)+'\n');
  // Public browser destinations, not credentials; nginx's unprivileged UID must read them.
  for (const [name, value] of [['documentation.json', {url: `http://localhost:${ports.DOCS_PORT}`}], ['docs-studio.json', {studioUrl: env.RUNLOOM_PUBLIC_URL}]]) {
    await json(name, value);
    await chmod(resolve(directory, name), 0o644);
  }
  const secret = async (name, bytes = 32, encoding = 'base64url') => {
    const path = resolve(directory,name);
    try { const bytesValue=randomBytes(bytes); await writeFile(path,encoding ? bytesValue.toString(encoding) : bytesValue,{mode:0o640,flag:'wx'}); }
    catch(error) { if(error.code!=='EEXIST') throw error; }
    const stat = await lstat(path);
    if(!stat.isFile() || stat.isSymbolicLink() || stat.uid!==process.getuid?.()) throw new Error('Credentials must be regular files owned by the local user.');
    const value = await readFile(path);
    if(encoding ? value.length<32 : value.length!==bytes) throw new Error('Existing credential is invalid; it was not replaced.');
    await chmod(path,0o640); return encoding ? value.toString('utf8').trim() : value;
  };
  const args = ['compose','--project-directory',root,'--env-file','/dev/null','-p',project,...['compose','compose.local','compose.secrets','compose.consumer','compose.services'].flatMap(name=>['-f',`compose/${name}.yaml`])];
  if (telemetry) { args.push('-f','compose/compose.observability.yaml'); env.PROMETHEUS_PORT = driver === 'postgres' ? '39091' : '39090'; }
  if(action==='down') { run([...args,'--profile','local-setup','down']); console.log('Stopped selected stack; data and credentials preserved.'); return; }
  if(action==='logs') { run([...args,'logs','--follow']); return; }
  const password = await secret('database-root-password');
  await secret('mongodb-replica-key',512,'base64');
  await secret('workflow-key',32,null);
  const adminURL = driver==='postgres' ? `postgres://runloom_root:${password}@postgres:5432/postgres?sslmode=disable` : `mongodb://runloom_root:${password}@mongodb:27017/admin?replicaSet=runloom`;
  await write('database-admin-url',adminURL);
  await json('database-setup.json',{driver,adminURLFile:'/run/secrets/database-admin-url',directory:'/run/secrets',assistant:true});
  const trust = {}, discovery = { RUNLOOM_AUTH_URL:'http://auth:8080',RUNLOOM_WORKFLOWS_URL:'http://workflows:8080',RUNLOOM_MANAGEMENT_URL:'http://management:8080' };
  const readJSON = async name => JSON.parse(await readFile(resolve(root,'compose/config',name+'.yaml'),'utf8'));
  const common = { ...await readJSON('base'), ...await readJSON('api'), ...discovery, RUNLOOM_DATABASE_DRIVER:driver, RUNLOOM_OIDC_ISSUER:`${env.RUNLOOM_PUBLIC_URL}/auth/realms/runloom` };
  if (telemetry) common.RUNLOOM_TELEMETRY_ENDPOINT = 'http://127.0.0.1:4318';
  for(const role of ['auth','workflows','management','worker']) {
    trust[role] = createHash('sha256').update(await secret(role+'-token')).digest('hex');
    const owner=role==='worker'?'workflows':role, name='runloom_'+owner;
    const cfg = { ...common, RUNLOOM_DATABASE_NAME:name,RUNLOOM_SERVICE_TOKEN_FILE:'/run/secrets/service-token',RUNLOOM_SERVICE_TRUST_FILE:'/etc/runloom/trust.json' };
    if(role==='worker') Object.assign(cfg,await readJSON('worker'));
    if(role==='workflows'||role==='worker') Object.assign(cfg,{...await readJSON('database-connections'),RUNLOOM_SECRETS_KEY_FILE:'/run/secrets/workflow-key',TEMPORAL_NAMESPACE_PREFIX:project,TEMPORAL_RETENTION_DAYS:'30'});
    if(role==='auth') Object.assign(cfg,await readJSON('consumer'),{RUNLOOM_PLATFORM_ALLOW_PASSWORD_ONLY:'true',RUNLOOM_ASSISTANT_URL:'http://assistant:8080',RUNLOOM_WORKER_URL:'http://worker:8081',RUNLOOM_GATEWAY_URL:'http://api:8080'});
    await json(role+'.yaml',cfg);
    if(role!=='worker') {
      const pwd = await secret(role+'-database-password');
      await write(role+'-database-url',driver==='postgres' ? `postgres://${name}:${pwd}@postgres:5432/${name}?sslmode=disable` : `mongodb://${name}:${pwd}@mongodb:27017/${name}?replicaSet=runloom&authSource=${name}`);
    }
  }
  await json('trust.json',trust);
  const assistantPassword = await secret('assistant-database-password');
  await write('assistant-database-url',driver==='postgres' ? `postgres://runloom_assistant:${assistantPassword}@postgres:5432/runloom_assistant?sslmode=disable` : `mongodb://runloom_assistant:${assistantPassword}@mongodb:27017/runloom_assistant?replicaSet=runloom&authSource=runloom_assistant`);
  await json('assistant.yaml',{...common,...await readJSON('assistant'),RUNLOOM_DATABASE_NAME:'runloom_assistant',RUNLOOM_TELEMETRY_ENDPOINT:telemetry?'http://127.0.0.1:4318':''});
  await json('gateway.yaml',{RUNLOOM_API_ADDRESS:'0.0.0.0:8080',...discovery,RUNLOOM_ASSISTANT_URL:'http://assistant:8080',...(telemetry?{RUNLOOM_TELEMETRY_ENDPOINT:'http://127.0.0.1:4318'}:{})});
  run([...args,'--profile','local-setup','config','--quiet'],true);
  if(action==='config') { console.log(`Validated fresh ${driver} configuration for ${project}. No containers started.`); return; }
  run([...args,'build','api','auth','local-admin','workflows','worker','management','assistant','database-setup','ui','workflows-ui','management-ui','platform-ui','docs']);
  run([...args,'up','-d','--wait','--wait-timeout','240','identity-db','mailpit','keycloak',driver,'temporal']);
  run([...args,'run','--rm','--no-deps','database-setup']);
  await ensureLocalConsumer({baseURL:`http://localhost:${ports.KEYCLOAK_PORT}/auth`,secretFile:env.RUNLOOM_PROVISIONER_SECRET_PATH,organizations:true});
  const accounts = JSON.parse(run([...args,'run','--rm','--no-deps','local-admin','--json',...(action==='reset'?['--reset-password']:[])],true));
  console.log(localAccountTable(accounts, env.RUNLOOM_PUBLIC_URL));
  if(action==='admin'||action==='reset') return;
  run([...args,'up','-d','--wait','--wait-timeout','180','auth','workflows','management','assistant','worker','api','workflows-ui','management-ui','platform-ui','ui','docs']);
  if (telemetry) {
    run([...args,'up','-d','--wait','--wait-timeout','180','telemetry-api','telemetry-auth','telemetry-workflows','telemetry-management','telemetry-worker','telemetry-assistant','prometheus']);
    console.log(`Prometheus API (local only): http://localhost:${env.PROMETHEUS_PORT}/api/v1/query`);
  }
  console.log(`Runloom (${driver}) ready. Studio: ${env.RUNLOOM_PUBLIC_URL}/ | API: http://localhost:${ports.API_PORT}/api/v1/health | Temporal: http://localhost:${ports.TEMPORAL_UI_PORT}/`);
  console.log(`Documentation: http://localhost:${ports.DOCS_PORT}/`);
}
if(process.argv[1] && resolve(process.argv[1])===fileURLToPath(import.meta.url)) main().catch(error=>{console.error(error.message);process.exitCode=1;});
