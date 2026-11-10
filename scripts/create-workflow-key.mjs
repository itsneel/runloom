// Without a path, create/reuse the managed local key. Explicit paths are create-only.
import { open } from 'node:fs/promises';
import { randomBytes } from 'node:crypto';
import { ensureLocalWorkflowKey } from './local-workflow-key.mjs';

const path = process.env.RUNLOOM_SECRETS_KEY_PATH;
try {
  if (!path) {
    const key = await ensureLocalWorkflowKey();
    console.log(`Local workflow encryption key ${key.created ? 'created' : 'reused'} at ${key.path}. Back it up securely; its value is not printed.`);
  } else {
    const file = await open(path, 'wx', 0o600);
    try { await file.writeFile(randomBytes(32)); await file.sync(); }
    finally { await file.close(); }
    console.log('Workflow encryption key created. Back it up securely; use the same file on API and worker.');
  }
} catch { console.error('Cannot provision workflow key. Check local mode, directory ownership/permissions and any existing key. Existing keys are never overwritten; explicit output paths must not exist.'); process.exitCode = 1; }
