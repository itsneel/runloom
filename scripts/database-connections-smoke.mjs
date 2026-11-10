// Disposable local fixtures only. Never touches the application's containers/data.
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { randomBytes } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { setTimeout } from 'node:timers/promises';
import { createServer } from 'node:net';
import { validateDockerEndpoint } from './local-stack.mjs';

const exec = promisify(execFile);
const root = fileURLToPath(new URL('../', import.meta.url));
const docker = process.env.DOCKER || 'docker';
const command = async (file, args, env = process.env) => (await exec(file, args, {cwd:root,env,timeout:180000,maxBuffer:2*1024*1024})).stdout.trim();
const endpoint = process.env.DOCKER_HOST || await command(docker,['context','inspect','--format','{{.Endpoints.docker.Host}}']);
validateDockerEndpoint(endpoint);
async function availablePort(){const server=createServer();await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));const port=server.address().port;await new Promise(resolve=>server.close(resolve));return port;}
let current = '';
let fixturePassword = '';
async function cleanup() { if(current){const name=current;current='';await command(docker,['rm','-f',name]);console.log('Removed disposable fixture '+name);} }
process.once('SIGINT',()=>{void cleanup().finally(()=>process.exit(130));});
process.once('SIGTERM',()=>{void cleanup().finally(()=>process.exit(143));});
try {
 for(const provider of ['postgres','mongodb','mysql']){
  const password=randomBytes(24).toString('hex'); fixturePassword=password;
  const name='runloom-connector-test-'+provider+'-'+randomBytes(4).toString('hex');
  const port=provider==='postgres'?5432:provider==='mongodb'?await availablePort():3306;
  const image=provider==='postgres'?'postgres:17-alpine':provider==='mongodb'?'mongo:8':'mysql:8.4';
  const vars=provider==='postgres'?{POSTGRES_USER:'connector',POSTGRES_PASSWORD:password,POSTGRES_DB:'connector_test'}:provider==='mongodb'?{}:{MYSQL_ROOT_PASSWORD:password,MYSQL_USER:'connector',MYSQL_PASSWORD:password,MYSQL_DATABASE:'connector_test'};
  const mount=provider==='postgres'?'/var/lib/postgresql/data':provider==='mongodb'?'/data/db':'/var/lib/mysql';
  await command(docker,['run','-d','--name',name,'--label','runloom.fixture=database-connections','--memory','1g','--cpus','1','--tmpfs',mount+':rw,size=512m','-p','127.0.0.1:'+(provider==='mongodb'?port:'')+':'+port,...Object.keys(vars).flatMap(k=>['-e',k]),image,...(provider==='mongodb'?['--wiredTigerCacheSizeGB','0.25','--port',String(port),'--replSet','connector']:[])],{...process.env,...vars});
  current=name;
  const address=await command(docker,['port',name,port+'/tcp']);const hostPort=address.split(':').at(-1);
  let ready=false;
  for(let attempt=0;attempt<60;attempt++){
   try{
    const args=provider==='postgres'?['pg_isready','-U','connector','-d','connector_test']:provider==='mongodb'?['mongosh','--port',String(port),'--quiet','--eval','db.adminCommand({ping:1}).ok']:['mysqladmin','ping','--silent'];
    await command(docker,['exec',name,...args]);ready=true;break;
   }catch{await setTimeout(1000);}
  }
  if(!ready)throw Error('Fixture startup timed out');
  if(provider==='mongodb'){
   await command(docker,['exec',name,'mongosh','--port',String(port),'--quiet','--eval',`rs.initiate({_id:'connector',members:[{_id:0,host:'127.0.0.1:${port}'}]}); while(!db.hello().isWritablePrimary){sleep(100)}; db.getSiblingDB('admin').createUser({user:'connector',pwd:'${password}',roles:['root']});`]);
  }
  // The initialized server can switch listeners immediately after readiness.
  await setTimeout(1500);
  const result=await exec('go',['test','./internal/dbnode','-run','^TestLiveDatabaseConnections$','-count=1','-v'],{cwd:root+'server/workflows',env:{...process.env,RUNLOOM_CONNECTOR_TEST_PROVIDER:provider,RUNLOOM_CONNECTOR_TEST_PORT:hostPort,RUNLOOM_CONNECTOR_TEST_PASSWORD:password},timeout:120000,maxBuffer:2*1024*1024});
  console.log(provider+': '+result.stdout.replaceAll(password,'[redacted]').trim());
  await cleanup();
 }
}catch(error){
 // Go assertions never print fixture credentials; defensively omit subprocess
 // output on failure (operators can rerun the focused test with their fixture).
 console.error('Database connector smoke failed: '+(error.code||'assertion/startup failure')+'.');
 if(error.stdout && fixturePassword) console.error(error.stdout.replaceAll(fixturePassword,'[redacted]'));
 process.exitCode=1;
}finally{await cleanup();}
