import test from 'node:test';
import assert from 'node:assert/strict';
import { secretNameError, secretValueError } from '../src/app/features/secrets/secret-validation.ts';

test('names explain leading, trailing and embedded whitespace without silently trimming it', () => {
  for (const name of ['api key', ' api-key', 'api-key ', 'api\tkey', 'api\u00a0key'])
    assert.match(secretNameError(name), /Spaces are not allowed/);
});

test('name and value validation match the vault contract without exposing values', () => {
  for (const name of ['', 'API', '9api', 'api_key', 'a'.repeat(65)]) assert.ok(secretNameError(name));
  for (const name of ['api', 'billing-api', 'a1', 'a'.repeat(64)]) assert.equal(secretNameError(name), '');
  for (const value of ['', 'x\ny', 'x\ry', 'x\0y', 'a'.repeat(8193), '😀'.repeat(2049)]) {
    const error = secretValueError(value);
    assert.ok(error);
    if (value) assert.ok(!error.includes(value));
  }
  for (const value of ['a token with spaces', ' keep exact whitespace ', 'a'.repeat(8192), '😀'.repeat(2048)]) assert.equal(secretValueError(value), '');
});
