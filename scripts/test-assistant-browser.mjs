// Existing identity login; chat data lives in disposable databases. Opt-in
// AUTHORING mode creates isolated smoke drafts, never edits existing workflows.
import {createServer, request as httpRequest} from 'node:http';
import {spawn, spawnSync} from 'node:child_process';
import {mkdtemp, writeFile, rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {randomUUID} from 'node:crypto';
import {fileURLToPath} from 'node:url';
const root=fileURLToPath(new URL('../',import.meta.url));
const origin=process.env.RUNLOOM_UI_URL || 'http://localhost:4200';
const unconfigured=process.env.RUNLOOM_ASSISTANT_TEST_DISABLED==='1';
if(!['localhost','127.0.0.1'].includes(new URL(origin).hostname))throw new Error('Browser smoke requires a local installation');
function docker(args){const r=spawnSync('docker',args,{encoding:'utf8'});if(r.status!==0)throw new Error('Disposable database operation failed');return r.stdout.trim();}
const listen=server=>new Promise(resolve=>server.listen(0,'127.0.0.1',()=>resolve(server.address().port)));
const delay=ms=>new Promise(resolve=>setTimeout(resolve,ms));
const provider=createServer(async(req,res)=>{
 let raw='';for await(const chunk of req)raw+=chunk;
 res.setHeader('Content-Type','text/event-stream');
 const body=JSON.parse(raw || '{}');
 if(process.env.RUNLOOM_ASSISTANT_TEST_AUTHORING==='1' && body.messages?.at(-1)?.content==='Create guarded smoke child'){
  const args=JSON.stringify({name:'Guarded smoke child',steps_json:JSON.stringify([{id:'wait',type:'wait',seconds:1},{id:'done',type:'end'}]),before_step_id:'done'});
  res.end('data: '+JSON.stringify({choices:[{delta:{tool_calls:[{index:0,id:'smoke-call',type:'function',function:{name:'propose_child_workflow',arguments:args}}]},finish_reason:'tool_calls'}]})+'\n\ndata: [DONE]\n\n');return;
 }
 for(const text of ['Test response: ','we can plan ','the onboarding ','steps together.']){
  if(res.destroyed)return;
  res.write('data: '+JSON.stringify({choices:[{delta:{content:text}}]})+'\n\n');await delay(1200);
 }
 res.end('data: {"choices":[{"delta":{},"finish_reason":"stop"}]}\n\ndata: [DONE]\n\n');
});
const modelPort=await listen(provider);
try{
 for(const driver of ['postgres','mongodb']){
  const name='runloom-assistant-browser-'+randomUUID().slice(0,8),directory=await mkdtemp(join(tmpdir(),'runloom-assistant-browser-'));
  let app,proxy;
  try{
   const dbPort=driver==='postgres'?'5432':'27017';
   docker(['run','--detach','--name',name,'-p',`127.0.0.1::${dbPort}`,...(driver==='postgres'?['-e','POSTGRES_PASSWORD=disposable-test-only','-e','POSTGRES_DB=runloom_assistant_test','postgres:17-alpine']:['mongo:8'])]);
   const port=docker(['port',name,dbPort+'/tcp']).split(':').at(-1);
   for(let i=0;i<60;i++){if(spawnSync('docker',['exec',name,...(driver==='postgres'?['pg_isready','-U','postgres']:['mongosh','--quiet','--eval','db.adminCommand({ping:1})'])],{stdio:'ignore'}).status===0)break;await delay(500);}
   const socket=createServer();const appPort=await listen(socket);await new Promise(resolve=>socket.close(resolve));
   await writeFile(join(directory,'database-url'),driver==='postgres'?`postgres://postgres:disposable-test-only@127.0.0.1:${port}/runloom_assistant_test?sslmode=disable`:`mongodb://127.0.0.1:${port}`,{mode:0o600});
   await writeFile(join(directory,'config.yaml'),JSON.stringify({
    RUNLOOM_API_ADDRESS:`127.0.0.1:${appPort}`,RUNLOOM_DATABASE_DRIVER:driver,RUNLOOM_DATABASE_NAME:'runloom_assistant_test',RUNLOOM_DATABASE_URL_FILE:join(directory,'database-url'),
    RUNLOOM_AUTH_URL:origin,RUNLOOM_WORKFLOWS_URL:origin,RUNLOOM_OIDC_ISSUER:origin+'/auth/realms/runloom',RUNLOOM_OIDC_JWKS_URL:origin+'/auth/realms/runloom/protocol/openid-connect/certs',RUNLOOM_OIDC_CLIENT_ID:'runloom',
    RUNLOOM_ASSISTANT_PROVIDER:unconfigured?'disabled':'openai-compatible',RUNLOOM_ASSISTANT_BASE_URL:`http://127.0.0.1:${modelPort}`,RUNLOOM_ASSISTANT_MODEL:'local-test-fixture',RUNLOOM_ASSISTANT_ALLOW_HTTP:'true',RUNLOOM_ASSISTANT_MAX_TOKENS:'128'
   }),{mode:0o600});
   app=spawn(root+'server/assistant/bin/runloom-assistant',[],{env:{...process.env,RUNLOOM_CONFIG_FILE:join(directory,'config.yaml')},stdio:'ignore'});
   let ready=false;for(let i=0;i<160;i++){try{if((await fetch(`http://127.0.0.1:${appPort}/readyz`)).ok){ready=true;break}}catch{}if(app.exitCode!==null)break;await delay(250)}if(!ready)throw new Error('Assistant failed readiness');
   // Test-only CORS proxy lets production Studio assets use an isolated backend.
   // Authorization headers reach the real token verifier and Auth service.
   proxy=createServer((req,res)=>{
    res.setHeader('Access-Control-Allow-Origin',origin);res.setHeader('Access-Control-Allow-Headers','Authorization, Content-Type');res.setHeader('Access-Control-Allow-Methods','GET, POST, OPTIONS');
    if(req.method==='OPTIONS'){res.end();return}
    const upstream=httpRequest({hostname:'127.0.0.1',port:appPort,path:req.url,method:req.method,headers:req.headers},reply=>{res.writeHead(reply.statusCode,{...reply.headers,'Access-Control-Allow-Origin':origin});reply.pipe(res)});
    upstream.on('error',()=>{res.statusCode=502;res.end()});req.pipe(upstream);res.on('close',()=>upstream.destroy());
   });
   const proxyPort=await listen(proxy);
   const code=await new Promise(resolve=>{const child=spawn(process.execPath,[root+'studio/projects/host/tests/assistant.browser.mjs'],{env:{...process.env,RUNLOOM_UI_URL:origin,RUNLOOM_ASSISTANT_TEST_URL:`http://127.0.0.1:${proxyPort}`},stdio:'inherit'});child.on('exit',resolve)});
   if(code!==0)throw new Error(driver+' browser check failed');
   console.log('PASS production Angular chat + real Keycloak + assistant + '+driver+(unconfigured?' (no model configured).':' (synthetic model stream).'));
  }finally{
   proxy?.closeAllConnections();proxy?.close();if(app){app.kill('SIGTERM');await Promise.race([new Promise(resolve=>app.once('exit',resolve)),delay(5000)]);if(app.exitCode===null)app.kill('SIGKILL')}
   docker(['rm','--force','--volumes',name]);await rm(directory,{recursive:true,force:true});
  }
 }
}finally{provider.closeAllConnections();provider.close();}
