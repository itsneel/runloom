// Only disposable, loopback-bound test containers. Never use installation data.
import {spawnSync} from 'node:child_process';
import {randomUUID} from 'node:crypto';
import {fileURLToPath} from 'node:url';
const root=fileURLToPath(new URL('../',import.meta.url));
const delay=ms=>new Promise(resolve=>setTimeout(resolve,ms));
function docker(args){const r=spawnSync('docker',args,{encoding:'utf8'});if(r.status!==0)throw new Error('Disposable authoring database operation failed');return r.stdout.trim();}
for(const driver of ['postgres','mongodb']){
 const name='runloom-authoring-test-'+randomUUID().slice(0,8),dbPort=driver==='postgres'?'5432':'27017';
 try{
  docker(['run','--detach','--name',name,'--label','runloom.test=authoring','-p',`127.0.0.1::${dbPort}`,...(driver==='postgres'?['-e','POSTGRES_PASSWORD=disposable-test-only','postgres:17-alpine']:['mongo:8','--replSet','rs0','--bind_ip_all'])]);
  const port=docker(['port',name,dbPort+'/tcp']).split(':').at(-1);
  let ready=false;for(let i=0;i<60;i++){if(spawnSync('docker',['exec',name,...(driver==='postgres'?['pg_isready','-U','postgres']:['mongosh','--quiet','--eval','db.adminCommand({ping:1})'])],{stdio:'ignore'}).status===0){ready=true;break}await delay(500)}if(!ready)throw new Error('Test database not ready');
  if(driver==='mongodb'){
   docker(['exec',name,'mongosh','--quiet','--eval','rs.initiate({_id:"rs0",members:[{_id:0,host:"localhost:27017"}]})']);
   ready=false;for(let i=0;i<60;i++){if(spawnSync('docker',['exec',name,'mongosh','--quiet','--eval','quit(db.hello().isWritablePrimary?0:1)'],{stdio:'ignore'}).status===0){ready=true;break}await delay(500)}if(!ready)throw new Error('Test replica set not ready');
  }
  const uri=driver==='postgres'?`postgres://postgres:disposable-test-only@127.0.0.1:${port}/postgres?sslmode=disable`:`mongodb://127.0.0.1:${port}/?directConnection=true&replicaSet=rs0`;
  const env={...process.env,RUNLOOM_TEST_POSTGRES_URL:'',RUNLOOM_TEST_MONGODB_URL:'',[driver==='postgres'?'RUNLOOM_TEST_POSTGRES_URL':'RUNLOOM_TEST_MONGODB_URL']:uri,RUNLOOM_TEST_ASSISTANT_DRIVER:driver,RUNLOOM_TEST_ASSISTANT_URL:uri,RUNLOOM_TEST_ASSISTANT_NAME:'runloom_assistant_authoring_test'};
  for(const [service,args] of [['workflows',['./tests','./internal/store','-run','Authoring|StoreContract|RollsBack']],['assistant',['./...']]]){
   const result=spawnSync('go',['test','-p','1','-race','-count=1',...args],{cwd:root+'server/'+service,env,stdio:'inherit'});if(result.status!==0)throw new Error(driver+' '+service+' authoring tests failed');
  }
  console.log('PASS guarded authoring and persistence: '+driver);
 }finally{docker(['rm','--force','--volumes',name])}
}
