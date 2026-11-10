import test from 'node:test';
import assert from 'node:assert/strict';
import {loadTypeScript} from './helpers/load-typescript.mjs';

const {loginLanding} = loadTypeScript(new URL('../src/app/routing/login-landing.ts', import.meta.url));

test('platform staff and administrators land in the console from generic entry URLs', () => {
  for (const role of ['platform_staff', 'platform_admin']) {
    for (const path of ['', '/', '/workflows', '/workflows/', '/?welcome=true']) {
      assert.equal(loginLanding(path, role), '/platform/health');
    }
  }
});

test('tenant roles and explicit deep links are not redirected', () => {
  for (const role of ['', 'member', 'admin', 'owner']) assert.equal(loginLanding('/', role), undefined);
  for (const path of ['/platform/staff', '/platform/organizations', '/management/teams', '/workflows/definitions/abc', '/workflows/projects/abc']) {
    assert.equal(loginLanding(path, 'platform_staff'), undefined);
  }
});
