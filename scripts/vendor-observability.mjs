// Explicit maintainer operation; never called by normal builds or Dockerfiles.
// Builds local release archives, then pins independent vendored consumers.
import {mkdtemp, mkdir, cp, readFile, writeFile, rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {spawnSync} from 'node:child_process';
const root=fileURLToPath(new URL('../',import.meta.url));
const tmp=await mkdtemp(resolve(tmpdir(),'runloom-sdk-release-'));
const run=(cmd,args,cwd,env=process.env)=>{const r=spawnSync(cmd,args,{cwd,env,stdio:'inherit'});if(r.status!==0)throw new Error(`${cmd} failed`)};
try {
 for(const name of ['go-auth','go-observability']) {
  const module=`github.com/example/runloom/sdk/${name}`,version=name==='go-observability'?'v0.2.0':'v0.1.0';
  const staged=resolve(tmp,'stage',module+'@'+version), dest=resolve(tmp,'proxy',module,'@v');
  await cp(resolve(root,'sdk',name),staged,{recursive:true,filter:p=>!p.split('/').some(v=>['vendor','.git','bin'].includes(v))});
  await mkdir(dest,{recursive:true});
  await writeFile(resolve(dest,version+'.mod'),await readFile(resolve(staged,'go.mod')));
  await writeFile(resolve(dest,version+'.info'),JSON.stringify({Version:version,Time:name==='go-observability'?'2026-10-20T00:00:00Z':'2026-10-14T00:00:00Z'}));
  await writeFile(resolve(dest,'list'),version+'\n');
  run('zip',['-qr',resolve(dest,version+'.zip'),module+'@'+version],resolve(tmp,'stage'));
 }
 const env={...process.env,GOWORK:'off',GONOSUMDB:'github.com/example/runloom/sdk/*',GOPROXY:`file://${tmp}/proxy,https://proxy.golang.org`,GOMODCACHE:resolve(tmp,'cache')};
 for(const name of ['auth','workflows','management','gateway','assistant']) {
  const dir=resolve(root,'server',name);
  run('go',['mod','edit','-require=github.com/example/runloom/sdk/go-observability@v0.2.0'],dir,env);
  run('go',['mod','tidy'],dir,env);
  run('go',['mod','vendor'],dir,env);
 }
} finally {run('go',['clean','-modcache'],root,{...process.env,GOMODCACHE:resolve(tmp,'cache')});await rm(tmp,{recursive:true,force:true});}
