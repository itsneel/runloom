import test from 'node:test';
import assert from 'node:assert/strict';
import {parsePromptTags, promptTagsError} from '../src/app/features/prompts/models/prompt-tags.ts';

test('prompt tags trim, lowercase and deduplicate without losing multiword or Unicode labels', () => {
  assert.deepEqual(parsePromptTags(' Support, BILLING, support, customer care, 客服, ,'), ['support', 'billing', 'customer care', '客服']);
  assert.deepEqual(parsePromptTags(''), []);
});
test('prompt tag limits preserve valid Unicode and reject excessive or invalid labels', () => {
  assert.equal(promptTagsError(['😀'.repeat(40)]), undefined);
  assert.ok(promptTagsError(['😀'.repeat(41)]));
  assert.ok(promptTagsError(Array.from({length: 21}, (_, i) => 'tag' + i)));
  for (const value of ['', ' ', 'bad,tag', 'bad\u0000tag', 'bad\u0085tag']) assert.ok(promptTagsError([value]));
});
