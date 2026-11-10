// Local browser-test identities; never grant memberships to shared development accounts.
import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';

export async function organizationIdentity(base) {
  const tokenResponse = await fetch(base + '/auth/realms/master/protocol/openid-connect/token', {
    method: 'POST',
    body: new URLSearchParams({
      grant_type: 'password',
      client_id: 'admin-cli',
      username: process.env.RUNLOOM_TEST_KEYCLOAK_ADMIN || 'admin',
      password: process.env.RUNLOOM_TEST_KEYCLOAK_PASSWORD || 'development-only',
    }),
  });
  assert.equal(tokenResponse.status, 200, 'Local Keycloak fixture login');
  const { access_token: token } = await tokenResponse.json();
  const suffix = randomUUID();
  const prefix = 'Hierarchy browser test ' + suffix;
  const users = [];
  const call = async (path, method = 'GET', body) => {
    const result = await fetch(base + '/auth/admin/realms/runloom' + path, {
      method,
      headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    assert.ok(result.ok, `Keycloak fixture ${method} failed (${result.status})`);
    if (result.status === 204 || result.status === 201) return;
    return result.json();
  };
  async function cleanup() {
    const orgs = [];
    for (let first = 0; ; first += 100) {
      const page = await call(`/organizations?first=${first}&max=100&briefRepresentation=false`);
      orgs.push(...page.filter((org) => org.name.startsWith(prefix)));
      if (page.length < 100) break;
    }
    for (const org of orgs) await call(`/organizations/${org.id}`, 'DELETE');
    for (const user of users) await call(`/users/${user.id}`, 'DELETE');
  }
  try {
    for (const role of ['owner', 'member']) {
      const email = `hierarchy-${suffix}-${role}@example.test`;
      const password = randomUUID() + '!Aa1';
      await call('/users', 'POST', {
        username: email,
        email,
        enabled: true,
        emailVerified: true,
        firstName: 'Hierarchy',
        lastName: 'Test',
        credentials: [{ type: 'password', value: password, temporary: false }],
      });
      const found = await call('/users?exact=true&email=' + encodeURIComponent(email));
      assert.equal(found.length, 1);
      users.push({ id: found[0].id, email, password });
    }
    return { owner: users[0], member: users[1], prefix, cleanup };
  } catch (error) {
    await cleanup();
    throw error;
  }
}
