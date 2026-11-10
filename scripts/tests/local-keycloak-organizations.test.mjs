import assert from 'node:assert/strict';
import {test} from 'node:test';
import {ensureLocalOrganizations} from '../local-keycloak-organizations.mjs';

test('local organization setup preserves credentials, password login and MFA', async () => {
  const calls = [];
  const fetcher = async (url, options) => {
    const path = new URL(url).pathname;
    calls.push({path, method: options.method, body: options.body});
    let value;
    if (path.endsWith('/token')) value = {access_token: 'synthetic-test-token'};
    else if (path.endsWith('/clients')) value = [{id: 'client'}];
    else if (path.endsWith('/service-account-user')) value = {id: 'service'};
    else if (path.includes('/roles/')) value = {id: path.split('/').at(-1)};
    else if (path.endsWith('/executions') && options.method === 'GET') value = [
      {id: 'organization', providerId: 'organization', requirement: 'ALTERNATIVE'},
      {id: 'password', providerId: 'auth-username-password-form', requirement: 'REQUIRED'},
      {id: 'mfa', providerId: 'auth-otp-form', requirement: 'ALTERNATIVE'},
    ];
    else if (path.endsWith('/runloom') && options.method === 'GET') value = {browserFlow: 'browser'};
    return value === undefined ? new Response(null, {status: 204}) : Response.json(value);
  };
  await ensureLocalOrganizations('http://localhost:4200/auth', 'provisioner', fetcher);
  const writes = calls.filter(c => c.method === 'PUT' || c.method === 'POST');
  assert.ok(writes.some(c => c.body === '{"organizationsEnabled":true}'));
  assert.deepEqual(writes.filter(c => c.path.endsWith('/executions')).map(c => JSON.parse(c.body)), [{id: 'organization', requirement: 'DISABLED'}]);
  assert.ok(!calls.some(c => /client-secret|reset-password/.test(c.path)));
  assert.ok(!calls.some(c => c.path.includes('/executions/password') || c.path.includes('/executions/mfa')));
  await assert.rejects(ensureLocalOrganizations('https://keycloak.example.com', 'provisioner', fetcher), /loopback/);
});
