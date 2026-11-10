// Local-only bootstrap. Key bytes never enter logs, environment variables or source control.
import { randomBytes, randomUUID } from 'node:crypto';
import { chmod, link, lstat, mkdir, open, readFile, stat, unlink } from 'node:fs/promises';
import { homedir } from 'node:os';
import { dirname, isAbsolute, join, resolve } from 'node:path';

export function localWorkflowKeyPath(env = process.env, userDirectory = homedir()) {
  if (env.RUNLOOM_ENV && env.RUNLOOM_ENV !== 'local') throw new Error('Automatic workflow key setup is local-only.');
  const supplied = env.RUNLOOM_SECRETS_KEY_PATH;
  if (supplied && !isAbsolute(supplied)) throw new Error('RUNLOOM_SECRETS_KEY_PATH must be an absolute file path.');
  return { path: supplied || join(userDirectory, '.config', 'runloom', 'local', 'workflow-encryption-key'), managed: !supplied };
}

export async function ensureLocalWorkflowKey(env = process.env, userDirectory = homedir()) {
  const selected = localWorkflowKeyPath(env, userDirectory);
  const inspect = async () => {
    if (selected.managed && (await lstat(selected.path)).isSymbolicLink()) throw new Error('The managed workflow key must not be a symbolic link; supply external key files explicitly.');
    const metadata = await stat(selected.path);
    if (!metadata.isFile() || metadata.size !== 32 || (await readFile(selected.path)).length !== 32) {
      throw new Error('Existing workflow key is invalid; restore its original 32-byte file. It was not replaced.');
    }
    // Compose file secrets preserve host ownership. Grant only the file's group
    // read access; the managed parent directory remains private to this user.
    // Explicitly supplied files are never chmod/chown-ed by local startup.
    if (selected.managed && process.platform !== 'win32') {
      if (metadata.uid !== process.getuid()) throw new Error('The local workflow key belongs to another user; refusing to change it.');
      const parent = await stat(dirname(selected.path));
      if (parent.uid !== process.getuid() || (parent.mode & 0o077) !== 0) throw new Error('The managed workflow key directory must be private (0700) and owned by this user.');
      await chmod(selected.path, 0o640);
    }
    return { path: resolve(selected.path), gid: String(metadata.gid) };
  };
  try { return { ...await inspect(), created: false }; }
  catch (error) {
    if (error.code !== 'ENOENT') throw error;
    if (!selected.managed) throw new Error('The supplied workflow key file is missing; local startup will not generate a replacement.');
  }
  await mkdir(dirname(selected.path), { recursive: true, mode: 0o700 });
  const temporary = join(dirname(selected.path), `.workflow-key-${randomUUID()}`);
  const file = await open(temporary, 'wx', 0o600);
  let created = false;
  try {
    try { await file.writeFile(randomBytes(32)); await file.sync(); }
    finally { await file.close(); }
    // Publish a complete key atomically; never overwrite a winner from another startup.
    try { await link(temporary, selected.path); created = true; }
    catch (error) { if (error.code !== 'EEXIST') throw error; }
    return { ...await inspect(), created };
  } finally { await unlink(temporary); }
}
