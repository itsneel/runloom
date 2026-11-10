// Local-only verification; the authenticated browser session launches live checks.
import { homedir } from 'node:os';
import { resolve } from 'node:path';
import { existsSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { plan } from './local-services.mjs';
const selected=plan(process.env.SERVICES_DATABASE || 'mongodb');
// Reuse an installed browser-test dependency, never build an application here.
const localPlaywright=fileURLToPath(new URL('../Website/node_modules/playwright/index.mjs',import.meta.url));
if(!process.env.RUNLOOM_PLAYWRIGHT_MODULE && existsSync(localPlaywright)) process.env.RUNLOOM_PLAYWRIGHT_MODULE=localPlaywright;
const localChrome='/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
if(!process.env.RUNLOOM_CHROME && process.platform==='darwin' && existsSync(localChrome)) process.env.RUNLOOM_CHROME=localChrome;
Object.assign(process.env, {
  RUNLOOM_UI_URL:`http://localhost:${selected.ports.UI_PORT}`,
  RUNLOOM_TEST_TEMPORAL_ADDRESS:`localhost:${selected.ports.TEMPORAL_PORT}`,
  RUNLOOM_TEST_NAMESPACE_PREFIX:selected.project,
  RUNLOOM_TEST_MANAGEMENT_URL:`http://localhost:${selected.ports.MANAGEMENT_PORT}`,
  RUNLOOM_TEST_SERVICE_DATABASE:selected.driver,
  RUNLOOM_TEST_SERVICE_CONFIG_DIR:resolve(homedir(),'.config/runloom/local/services',selected.project),
});
await import('./services-smoke.mjs');
