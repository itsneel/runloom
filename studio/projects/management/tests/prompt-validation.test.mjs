import test from 'node:test';
import assert from 'node:assert/strict';
import { promptFieldErrors, suggestedPromptID } from '../src/app/features/prompts/prompt-validation.ts';

const entry = () => ({id: 'greeting', name: 'Greeting', template: {text: 'Hello', instructions: '', variables: []}});
test('generated prompt IDs are valid for empty, Unicode, numeric and long names', () => {
  for (const name of ['', 'Customer Summary', '123 greeting', 'Café', '你好', 'a'.repeat(200)]) {
    const id = suggestedPromptID(name, 'a1b2c3d4');
    assert.match(id, /^[a-z][a-z0-9_-]{0,63}$/);
    assert.ok(id.endsWith('-a1b2c3d4'));
  }
  assert.equal(suggestedPromptID('Customer Summary', 'a1b2c3d4'), 'customer-summary-a1b2c3d4');
});
test('missing or invalid identifiers never form a publish URL', () => {
  for (const id of ['', 'has spaces', 'UPPER', '/path', 'a'.repeat(65)]) assert.ok(promptFieldErrors({...entry(), id}).id);
  assert.deepEqual(promptFieldErrors(entry()), {});
});
test('required text and UTF-8 byte limits match backend validation', () => {
  assert.ok(promptFieldErrors({...entry(), name: '   '}).name);
  assert.ok(promptFieldErrors({...entry(), name: '😀'.repeat(31)}).name);
  assert.ok(promptFieldErrors({...entry(), template: {text: ' ', instructions: ''}}).text);
  assert.ok(promptFieldErrors({...entry(), template: {text: '😀'.repeat(4001), instructions: ''}}).text);
  assert.ok(promptFieldErrors({...entry(), template: {text: 'Hello', instructions: 'a'.repeat(16001)}}).instructions);
});
