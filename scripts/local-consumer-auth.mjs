// Shared by local Compose and kind setup. Never log provider bodies or credentials.
import { chmod, lstat, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { tmpdir } from 'node:os';
import { setup } from './setup-consumer-auth.mjs';
import { ensureLocalOrganizations } from './local-keycloak-organizations.mjs';

export async function ensureLocalConsumer({ baseURL, secretFile, storedSecret, managed = true, clientID = 'runloom-provisioner', organizations = false }, dependencies = {}) {
  const url = new URL(baseURL);
  if (url.protocol !== 'http:' || !['127.0.0.1', 'localhost', '[::1]'].includes(url.hostname) || url.username || url.password || url.search || url.hash) throw new Error('Local consumer setup requires a loopback Keycloak URL.');
  const fetcher = dependencies.fetch || fetch;
  async function request(path, options = {}, stage = 'service-account setup') {
    let response;
    try { response = await fetcher(baseURL + path, { ...options, redirect: 'error', signal: AbortSignal.timeout(10000) }); }
    catch { throw new Error('Local Keycloak is unreachable; service-account setup did not complete.'); }
    if (!response.ok) {
      const error = new Error(`Local ${stage} failed (HTTP ${response.status}); credentials and response omitted.`);
      error.status = response.status;
      throw error;
    }
    return response;
  }
  let file;
  try { file = await lstat(secretFile); }
  catch (error) { if (error.code !== 'ENOENT') throw error; }
  if (file && (!file.isFile() || file.isSymbolicLink())) throw new Error('Provisioner credential must be a regular file, not a symlink.');
  if (!managed && !file) throw new Error('Supplied provisioner credential file is missing; it will not be generated or replaced.');
  let secret = storedSecret ?? (file ? (await readFile(secretFile, 'utf8')).trim() : undefined);
  let created = false;
  // Reuse needs only the dedicated credential, not the bootstrap administrator.
  async function bootstrap(reuseCredential = false) {
    const login = await (await request('/realms/master/protocol/openid-connect/token', {
      method: 'POST', body: new URLSearchParams({ grant_type: 'password', client_id: 'admin-cli', username: 'admin', password: 'development-only' }),
    }, 'Keycloak bootstrap administrator login')).json();
    if (!login.access_token) throw new Error('Local administrator token missing.');
    const clients = await (await request('/admin/realms/runloom/clients?clientId=' + encodeURIComponent(clientID), {
      headers: { Authorization: 'Bearer ' + login.access_token },
    })).json();
    if (clients.length) throw new Error(reuseCredential
      ? 'Saved provisioner credential was rejected (HTTP 401), but the Keycloak client already exists. Restore its matching credential; no client or credential was changed.'
      : 'Provisioner already exists but its original credential is missing. Restore it; no credential was rotated.');
    await mkdir(dirname(secretFile), { recursive: true, mode: 0o700 });
    const temporary = await mkdtemp(join(tmpdir(), 'runloom-consumer-bootstrap-'));
    try {
      const tokenFile = join(temporary, 'admin-token');
      await writeFile(tokenFile, login.access_token, { mode: 0o600, flag: 'wx' });
      await (dependencies.setup || setup)({ baseURL, realm: 'runloom', tokenFile, secretFile, clientID, reuseCredential });
      secret = (await readFile(secretFile, 'utf8')).trim();
      created = true;
    } catch {
      throw new Error('Provisioner bootstrap failed. Inspect the existing client and private credential file before retrying; nothing was rotated.');
    } finally {
      login.access_token = '';
      await rm(temporary, { recursive: true, force: true });
    }
  }
  if (secret === undefined) await bootstrap();
  if (!secret) throw new Error('Provisioner credential is empty; no credential was replaced.');
  const authenticate = async () => (await request('/realms/runloom/protocol/openid-connect/token', {
    method: 'POST', body: new URLSearchParams({ grant_type: 'client_credentials', client_id: clientID, client_secret: secret }),
  }, 'provisioner credential verification')).json();
  let token;
  try { token = await authenticate(); }
  catch (error) {
    // Fresh identity storage can lack a client whose host backup still exists.
    // Restore only a missing client, retaining the saved key.
    if (error.status !== 401 || !managed || storedSecret !== undefined || !file) throw error;
    await bootstrap(true);
    token = await authenticate();
  }
  secret = '';
  if (!token.access_token) throw new Error('Provisioner token missing.');
  for (const resource of ['client-scopes', 'clients']) await request(`/admin/realms/runloom/${resource}`, { headers: { Authorization: 'Bearer ' + token.access_token } });
  token.access_token = '';
  if (organizations && managed) await (dependencies.organizations || ensureLocalOrganizations)(baseURL, clientID, fetcher);
  file = await lstat(secretFile).catch(error => { if (error.code !== 'ENOENT') throw error; });
  if (managed && file) {
    if (!file.isFile() || file.uid !== process.getuid?.()) throw new Error('Managed provisioner file must be owned by the local user.');
    await chmod(secretFile, 0o640);
  }
  return { path: secretFile, gid: file ? String(file.gid) : undefined, created };
}
