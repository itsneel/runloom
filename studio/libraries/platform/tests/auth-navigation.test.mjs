import test from 'node:test';
import assert from 'node:assert/strict';
import { authReturnUrl } from '../src/auth-navigation.ts';

test('retains internal destination and removes authentication response parameters', () => {
  assert.equal(authReturnUrl('https://studio.example/projects?project=123&code=secret&state=secret&session_state=x&iss=x&error=x&error_description=x&signedOut=1#workflow'), 'https://studio.example/projects?project=123#workflow');
});
test('returnTo is not followed or interpreted as a redirect destination', () => {
  const result = new URL(authReturnUrl('https://studio.example/?returnTo=https://evil.example'));
  assert.equal(result.origin, 'https://studio.example');
  assert.equal(result.pathname, '/');
});
