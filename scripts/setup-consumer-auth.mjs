// Operator-only bootstrap. Reads a short-lived realm-admin token from a private
// file, creates a dedicated provisioner, and saves its secret without printing it.
import { randomBytes } from 'node:crypto';
import { constants } from 'node:fs';
import { open, readFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

export async function setup({ baseURL, realm, tokenFile, secretFile, clientID = 'runloom-provisioner', reuseCredential = false }) {
  const url = new URL(baseURL);
  if (url.protocol !== 'https:' && !(url.protocol === 'http:' && ['localhost', '127.0.0.1', '[::1]'].includes(url.hostname))) throw new Error('HTTPS required outside localhost');
  if (url.username || url.password || url.search || url.hash) throw new Error('Use a plain Keycloak base URL');
  const adminToken = (await readFile(tokenFile, 'utf8')).trim();
  if (!adminToken) throw new Error('Admin token file is empty');
  // Exclusive creation prevents overwriting an existing credential file.
  // Recovery reads the existing credential without writing or replacing it.
  const output = await open(secretFile, reuseCredential ? constants.O_RDONLY | constants.O_NOFOLLOW : 'wx', 0o600);
  let secret;
  try {
    if (reuseCredential) {
      if (!(await output.stat()).isFile()) throw new Error('Saved provisioner credential must be a regular file.');
      secret = (await output.readFile('utf8')).trim();
      if (!secret) throw new Error('Saved provisioner credential is empty.');
    } else secret = randomBytes(32).toString('base64url');
  } catch (error) { await output.close(); throw error; }
  const root = baseURL.replace(/\/$/, '') + '/admin/realms/' + encodeURIComponent(realm);
  async function request(method, path, body) {
    const response = await fetch(root + path, { method, redirect: 'error', signal: AbortSignal.timeout(10000), headers: { Authorization: 'Bearer ' + adminToken, 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body) });
    if (!response.ok) throw new Error(`Keycloak bootstrap failed (HTTP ${response.status}); inspect configuration, not response secrets`);
    return response;
  }
  try {
    const available = await (await request('GET', '/client-scopes')).json();
    for (const name of ['workflows:read', 'runs:start', 'runs:read', 'runs:cancel']) {
      if (!available.some(s => s.name === name)) await request('POST', '/client-scopes', { name, protocol: 'openid-connect', attributes: { 'include.in.token.scope': 'true', 'display.on.consent.screen': 'false' } });
    }
    const created = await request('POST', '/clients', { clientId: clientID, protocol: 'openid-connect', enabled: false, publicClient: false, clientAuthenticatorType: 'client-secret', secret, serviceAccountsEnabled: true, standardFlowEnabled: false, implicitFlowEnabled: false, directAccessGrantsEnabled: false, fullScopeAllowed: true, defaultClientScopes: ['basic', 'roles'], optionalClientScopes: [] });
    const id = created.headers.get('location')?.split('/').pop();
    if (!id) throw new Error('Keycloak omitted provisioner ID');
    const user = await (await request('GET', `/clients/${id}/service-account-user`)).json();
    const management = await (await request('GET', '/clients?clientId=realm-management')).json();
    if (!user.id || !management[0]?.id) throw new Error('Keycloak management identity missing');
    const managementID = management[0].id;
    const roles = [];
    for (const role of ['manage-clients', 'view-clients', 'query-clients']) roles.push(await (await request('GET', `/clients/${managementID}/roles/${role}`)).json());
    await request('POST', `/users/${user.id}/role-mappings/clients/${managementID}`, roles);
    if (!reuseCredential) {
      await output.writeFile(secret + '\n');
      await output.sync();
    }
    await request('PUT', `/clients/${id}`, { enabled: true });
    return { clientID, secretFile: resolve(secretFile) };
  } finally { await output.close(); }
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  const [baseURL, realm, tokenFile, secretFile] = process.argv.slice(2);
  if (!baseURL || !realm || !tokenFile || !secretFile) {
    console.error('Usage: node scripts/setup-consumer-auth.mjs KEYCLOAK_BASE_URL REALM ADMIN_TOKEN_FILE NEW_SECRET_FILE');
    process.exitCode = 1;
  } else {
    try { const result = await setup({ baseURL, realm, tokenFile, secretFile }); console.log(`Created ${result.clientID}. Provisioning secret saved to ${result.secretFile}; value was not printed.`); }
    catch { console.error('Bootstrap failed. Check provider permissions/configuration. Do not overwrite existing credentials; an incomplete client/file may need operator review.'); process.exitCode = 1; }
  }
}
