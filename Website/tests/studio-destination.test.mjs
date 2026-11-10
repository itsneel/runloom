import test from 'node:test';
import assert from 'node:assert/strict';
import { studioDestination } from '../src/app/studio-destination.ts';

test('hosting metadata connects both website links without changing the default destination', () => {
  assert.equal(studioDestination('http://localhost:4200', 'http://localhost:24200/'), 'http://localhost:4200/');
  assert.equal(studioDestination(undefined, 'http://localhost:24200/'), 'http://localhost:24200/');
  assert.equal(studioDestination('https://studio.example.test/', ''), 'https://studio.example.test/');
});

test('unsafe or invalid destinations fall back to the configured site destination', () => {
  for (const value of ['javascript:alert(1)', 'data:text/html,test', '//example.test', 'not a url', 'https://user:password@example.test']) {
    assert.equal(studioDestination(value, 'fallback'), 'fallback');
  }
});
