// Copy each project alone, then compile and test with no sibling source or network.
import assert from 'node:assert/strict';
import { cp, mkdtemp, readFile, readdir, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawnSync } from 'node:child_process';
const root=fileURLToPath(new URL('../',import.meta.url));
const projects=['server/auth','server/workflows','server/management','server/gateway','server/assistant','tools/database-setup'];
for(const project of projects) {
  const source=resolve(root,project), module=await readFile(resolve(source,'go.mod'),'utf8');
  assert(!/^replace\s/m.test(module),`${project}: source replacement is forbidden`);
  assert(!module.includes('github.com/example/runloom/server'),`${project}: old shared module`);
  if(['server/auth','server/management'].includes(project)) assert(!/go\.temporal\.io|quickjs|wazero/.test(module),`${project}: execution dependency`);
  async function scan(dir) {
    for(const e of await readdir(dir,{withFileTypes:true})) {
      if(['vendor','bin','.git','testdata'].includes(e.name)) continue;
      const path=resolve(dir,e.name);
      assert(!e.isSymbolicLink(),`${project}: source symlink`);
      if(e.isDirectory()) await scan(path);
      else if(e.name.endsWith('.go')) {
        const text=await readFile(path,'utf8');
        for(const match of text.matchAll(/"(github\.com\/example\/runloom\/[^\"]+)"/g)) {
          const own=module.match(/^module (\S+)/m)[1];
          assert(match[1].startsWith(own+'/') || ['github.com/example/runloom/sdk/go-auth','github.com/example/runloom/sdk/go-observability'].includes(match[1]),`${project}: sibling import ${match[1]}`);
        }
      }
    }
  }
  await scan(source);
  const temporary=await mkdtemp(resolve(tmpdir(),'runloom-isolated-'));
  try {
    const isolated=resolve(temporary,'project');
    await cp(source,isolated,{recursive:true,filter:p=>!p.split('/').some(s=>['bin','.git'].includes(s))});
    const env={...process.env,GOWORK:'off',GOPROXY:'off',GOMODCACHE:resolve(temporary,'module-cache')};
    for(const key of Object.keys(env)) if(key.startsWith('RUNLOOM_TEST_') || key.endsWith('_BROWSER')) delete env[key];
    for(const args of [['build','-mod=vendor','./...'],['test','-p','1','-mod=vendor','./...']]) {
      const result=spawnSync('go',args,{cwd:isolated,env,encoding:'utf8'});
      if(result.status!==0) {process.stderr.write(result.stdout||'');process.stderr.write(result.stderr||'');throw new Error(`${project}: isolated ${args[0]} failed`)}
    }
    console.log(`PASS ${project}: isolated offline build and tests; no siblings, workspace or module cache`);
  } finally { await rm(temporary,{recursive:true,force:true}); }
}
