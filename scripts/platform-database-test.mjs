// Tests use a uniquely named disposable schema/database and leave application data intact.
import {readFileSync} from 'node:fs';
import {homedir} from 'node:os';
import {resolve} from 'node:path';
import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
import {plan} from './local-services.mjs';
const selected=plan(process.env.SERVICES_DATABASE || 'mongodb');
const password=readFileSync(resolve(homedir(),'.config/runloom/local/services',selected.project,'database-root-password'),'utf8').trim();
const env={...process.env}; delete env.RUNLOOM_TEST_MONGODB_URL; delete env.RUNLOOM_TEST_POSTGRES_URL;
if(selected.driver==='postgres') env.RUNLOOM_TEST_POSTGRES_URL=`postgres://runloom_root:${encodeURIComponent(password)}@127.0.0.1:${selected.ports.POSTGRES_PORT}/postgres?sslmode=disable`;
else env.RUNLOOM_TEST_MONGODB_URL=`mongodb://runloom_root:${encodeURIComponent(password)}@127.0.0.1:${selected.ports.MONGODB_PORT}/admin?replicaSet=runloom&directConnection=true`;
const result=spawnSync('go',['test','-race','-count=1','./internal/localadmin','-run','TestDatabaseBootstrap'],{cwd:fileURLToPath(new URL('../server/auth/',import.meta.url)),env,encoding:'utf8',timeout:180000});
if(result.status!==0) { console.error('Platform database contract failed; credentials withheld.'); process.exitCode=1; }
else console.log(`PASS ${selected.driver}: persisted platform grants/revocation, idempotent bootstrap and concurrent last-admin protection across two database clients.`);
