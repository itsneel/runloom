import fs from 'node:fs/promises';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import assert from 'node:assert/strict';
import SwaggerParser from '@apidevtools/swagger-parser';
import {studioSpec} from './studio-spec.mjs';
import {platformSpec} from './platform-spec.mjs';

const here=path.dirname(fileURLToPath(import.meta.url)), root=path.resolve(here,'../..');
const api=path.resolve(here,'../workflows/internal/api'), check=process.argv.includes('--check');
const readJSON=async file=>JSON.parse(await fs.readFile(file,'utf8'));
const workflow=await readJSON(path.join(root,'spec/workflow.schema.json'));
const schedule=await readJSON(path.join(root,'spec/schedule.schema.json'));
// Imported schema references must resolve inside the OpenAPI document, not its root.
function embedSchema(schema,name){
  const result=JSON.parse(JSON.stringify(schema).replaceAll('"#/$defs/','"#/components/schemas/'+name+'/$defs/'));
  delete result.$schema; delete result.$id; return result;
}
const spec=studioSpec(embedSchema(workflow,'WorkflowDefinition'),embedSchema(schedule,'ScheduleRequest'));
// A data snapshot keeps combined-route tests independent of sibling source trees.
const authRoutesSource=await fs.readFile(path.resolve(here,'../auth/internal/api/gateway.go'),'utf8');
const authRoutes=[...authRoutesSource.matchAll(/HandleFunc\("(GET|POST|PUT|PATCH|DELETE) \/api\/v1\/studio([^\"]*)"/g)].map(match=>match[1].toLowerCase()+' '+match[2]).sort();
const authRoutesPath=path.join(api,'testdata/auth-studio-routes.json'), authRoutesJSON=JSON.stringify(authRoutes,null,2)+'\n';
if(check)assert.equal(await fs.readFile(authRoutesPath,'utf8'),authRoutesJSON,'Auth Studio route snapshot is stale');
else {await fs.mkdir(path.dirname(authRoutesPath),{recursive:true});await fs.writeFile(authRoutesPath,authRoutesJSON);}
const encoded=JSON.stringify(spec,null,2)+'\n', specPath=path.join(api,'studio.openapi.json');
if(check)assert.equal(await fs.readFile(specPath,'utf8'),encoded,'Studio OpenAPI is stale; run npm run build in server/api-docs');
else await fs.writeFile(specPath,encoded);
const platformPath=path.resolve(here,'../auth/internal/api/platform.openapi.json');
const platformEncoded=JSON.stringify(platformSpec(),null,2)+'\n';
if(check)assert.equal(await fs.readFile(platformPath,'utf8'),platformEncoded,'Platform OpenAPI is stale');
else await fs.writeFile(platformPath,platformEncoded);
await SwaggerParser.validate(platformPath);
const dist=path.join(here,'node_modules/swagger-ui-dist');
for(const name of ['swagger-ui-bundle.js','swagger-ui-bundle.js.LICENSE.txt','swagger-ui.css','LICENSE','NOTICE']){
  const source=await fs.readFile(path.join(dist,name));
  const target=path.join(api,'docs',name);
  if(check)assert.deepEqual(await fs.readFile(target),source,`Vendored Swagger asset differs: ${name}`);
  else await fs.writeFile(target,source);
}
for(const name of ['studio','consumer']){
  await SwaggerParser.validate(path.join(api,`${name}.openapi.json`));
  console.log(`PASS ${name} OpenAPI validation`);
}
console.log(`PASS Swagger UI assets and Studio contract ${check?'match committed output':'generated'}`);
