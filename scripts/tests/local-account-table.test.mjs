import test from 'node:test';
import assert from 'node:assert/strict';
import {localAccountTable} from '../local-account-table.mjs';

function accounts() {
  const defaults = {created: false, reset: false, default_password: 'public-local-default'};
  return {...defaults, email: 'admin@runloom.test', organization: 'Runloom', team: 'General',
    member: {...defaults, email: 'member@runloom.test', default_org_role: 'member', default_team_role: 'editor'},
    platform_admin: {...defaults, email: 'platform-admin@runloom.test', role: 'platform_admin'},
    platform_staff: {...defaults, email: 'platform-staff@runloom.test', role: 'platform_staff'},
  };
}

test('four Runloom accounts and Keycloak admin use one aligned ASCII table', () => {
  const output = localAccountTable(accounts(), 'http://localhost:4200/');
  const lines = output.split('\n').filter(line => /^[+|]/.test(line));
  assert.equal(lines.length, 9);
  assert.equal(new Set(lines.map(line => line.length)).size, 1);
  assert.equal(output.match(/Existing passwords are preserved/g).length, 1);
  assert.match(output, /admin@runloom.test\s+\| owner \/ admin/);
  assert.match(output, /platform-admin@runloom.test\s+\| platform_admin\s+\| -\s+\| -/);
  assert.match(output, /Platform console: http:\/\/localhost:4200\/platform/);
  assert.equal(output.match(/\| Email/g).length, 1);
  assert.match(output, /\| admin\s+\| Keycloak admin\s+\| -\s+\| -\s+\| development-only\s+\|/);
  assert.match(output, /Keycloak admin: http:\/\/localhost:4200\/auth\/admin\/master\/console\//);
  assert.match(output, /Login realm: master; select runloom/);
});

test('explicit reset and revoked access are reported truthfully', () => {
  const data = accounts();
  data.platform_staff.role = '';
  data.reset = true;
  const output = localAccountTable(data, 'http://localhost:35200');
  assert.match(output, /none \(revoked\)/);
  assert.match(output, /Explicit reset: Runloom local default passwords restored/);
  assert.match(output, /Keycloak credentials are unchanged/);
  assert.doesNotMatch(output, /Existing passwords are preserved/);
});

test('Keycloak console uses the same public origin for each Compose database stack', () => {
  for (const port of [34200, 35200]) {
    const output = localAccountTable(accounts(), `http://localhost:${port}`);
    assert.ok(output.includes(`Keycloak admin: http://localhost:${port}/auth/admin/master/console/`));
  }
});

test('incomplete results fail instead of showing invented platform access', () => {
  assert.throws(() => localAccountTable({}, 'http://localhost:4200'), /Incomplete/);
  const data = accounts(); data.platform_staff.role = 'owner';
  assert.throws(() => localAccountTable(data, 'http://localhost:4200'), /Invalid/);
});
