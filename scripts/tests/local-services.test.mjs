import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { plan } from '../local-services.mjs';

test('fresh service database selections never reuse legacy projects or ports', () => {
  const mongo=plan(), pg=plan('postgres');
  assert.equal(mongo.driver,'mongodb');
  assert.equal(pg.driver,'postgres');
  assert.notEqual(mongo.project,pg.project);
  const used=new Set([14200,18080,18081,15432,17017,17233,18233]);
  for(const p of [mongo,pg]) for(const port of Object.values(p.ports)) { assert(!used.has(port));used.add(port); }
  assert.throws(()=>plan('mysql'));
  assert.throws(()=>plan('../runloom-local'));
});
test('fresh topology mounts only owner credentials and no migration dependency', async () => {
  const script=await readFile(new URL('../local-services.mjs',import.meta.url),'utf8');
  assert(!script.includes('migrate-services'));
  assert(!script.includes('deploy/local.env'));
  assert(script.includes('ensureLocalConsumer'));
  assert(script.includes("'--no-deps','database-setup'"));
  const compose=await readFile(new URL('../../compose/compose.services.yaml',import.meta.url),'utf8');
  assert(compose.includes('secrets: !override []'));
  assert(compose.includes('source: auth-database-url'));
  assert(compose.includes('source: workflows-database-url'));
  assert(compose.includes('source: management-database-url'));
  assert(compose.includes('http://127.0.0.1:8080/readyz/vault'));
  assert(compose.includes('http://127.0.0.1:8081/readyz/vault'));
});
