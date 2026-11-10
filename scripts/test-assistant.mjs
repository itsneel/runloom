// Disposable databases only. No current Runloom installation or data is changed.
import {spawn, spawnSync} from 'node:child_process';
import {randomUUID, randomBytes} from 'node:crypto';
import {mkdtemp, writeFile, rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {fileURLToPath} from 'node:url';
const root=fileURLToPath(new URL('../',import.meta.url));
function docker(args){const r=spawnSync('docker',args,{encoding:'utf8'});if(r.status!==0)throw new Error('Disposable assistant database operation failed');return r.stdout.trim();}
for(const driver of ['postgres','mongodb']){
  const name='runloom-assistant-test-'+randomUUID().slice(0,8);
  const directory=await mkdtemp(join(tmpdir(),'runloom-assistant-owner-test-'));
  const rootPassword='disposable-test-root-password-only';
  const port=driver==='postgres'?'5432':'27017';
  try{
    docker(['run','--detach','--name',name,'--label','runloom.test=assistant','-p',`127.0.0.1::${port}`,...(driver==='postgres'?['-e','POSTGRES_PASSWORD='+rootPassword,'postgres:17-alpine']:['-e','MONGO_INITDB_ROOT_USERNAME=root','-e','MONGO_INITDB_ROOT_PASSWORD='+rootPassword,'mongo:8'])]);
    const published=docker(['port',name,port+'/tcp']).split(':').at(-1);
    let healthy=false;for(let i=0;i<60;i++){const r=spawnSync('docker',['exec',name,...(driver==='postgres'?['pg_isready','-U','postgres']:['mongosh','--quiet','-u','root','-p',rootPassword,'--authenticationDatabase','admin','--eval','db.adminCommand({ping:1})'])],{stdio:'ignore'});if(r.status===0){healthy=true;break};await new Promise(resolve=>setTimeout(resolve,500));}
    if(!healthy)throw new Error('Test database did not become ready');
    if(driver==='mongodb') await new Promise(resolve=>setTimeout(resolve,2500)); // initialization restarts mongod after root creation
    // Exercise the actual database-setup image, including all cross-owner checks.
    const url=(user,password,db,hostPort=port)=>driver==='postgres'?`postgres://${user}:${password}@127.0.0.1:${hostPort}/${db}?sslmode=disable`:`mongodb://${user}:${password}@127.0.0.1:${hostPort}/${db}?authSource=${db}`;
    const write=(name,value)=>writeFile(join(directory,name),value,{mode:0o644});
    await write('database-admin-url',url(driver==='postgres'?'postgres':'root',rootPassword,driver==='postgres'?'postgres':'admin'));
    await write('database-setup.json',JSON.stringify({driver,assistant:true,adminURLFile:'/run/secrets/database-admin-url',directory:'/run/secrets'}));
    let ownerPassword;
    for(const role of ['auth','workflows','management','assistant']){
      const password=randomBytes(32).toString('base64url');
      await write(role+'-database-password',password);await write(role+'-database-url',url('runloom_'+role,password,'runloom_'+role));
      if(role==='assistant')ownerPassword=password;
    }
    docker(['run','--rm','--network','container:'+name,'-v',directory+':/run/secrets:ro','-v',join(directory,'database-setup.json')+':/etc/runloom/database-setup.json:ro',process.env.DATABASE_SETUP_IMAGE || 'runloom-database-setup:local']);
    const uri=url('runloom_assistant',ownerPassword,'runloom_assistant',published);
    const code=await new Promise(resolve=>{const p=spawn('go',['test','-race','-count=1','-mod=vendor','./...'],{cwd:root+'server/assistant',env:{...process.env,RUNLOOM_TEST_ASSISTANT_DRIVER:driver,RUNLOOM_TEST_ASSISTANT_URL:uri,RUNLOOM_TEST_ASSISTANT_NAME:'runloom_assistant'},stdio:'inherit'});p.on('exit',resolve)});
    if(code!==0)throw new Error(driver+' assistant tests failed');
    console.log('PASS assistant persistence, jobs and streaming: '+driver);
  }finally{docker(['rm','--force','--volumes',name]);await rm(directory,{recursive:true,force:true});}
}
