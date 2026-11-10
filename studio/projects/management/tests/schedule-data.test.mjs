import assert from 'node:assert/strict';
import test from 'node:test';
import { scheduleConfig } from '../src/app/features/schedules/utils/schedule-config.ts';

const draft = {
  name: 'Hourly',
  definitionId: 'main',
  version: 2,
  input: '{"hello":"world"}',
  paused: true,
  mode: 'interval',
  minutes: 60,
  cron: '0 9 * * 1-5',
  timeZone: 'Asia/Kolkata',
  at: '2030-01-01T12:00:00Z',
};
test('interval schedules pin the version and carry saved input, not calendar fields', () => {
  assert.deepEqual(scheduleConfig(draft), {
    name: 'Hourly',
    definition_id: 'main',
    version: 2,
    input: { hello: 'world' },
    paused: true,
    interval_seconds: 3600,
  });
});
test('calendar and one-time modes emit exactly one timing field', () => {
  const cron = scheduleConfig({ ...draft, mode: 'cron' });
  assert.equal(cron.cron, draft.cron);
  assert.equal(cron.time_zone, 'Asia/Kolkata');
  assert.equal(cron.interval_seconds, undefined);
  const once = scheduleConfig({ ...draft, mode: 'once' }, Date.parse('2029-01-01'));
  assert.equal(once.at, '2030-01-01T12:00:00.000Z');
  assert.equal(once.cron, undefined);
});
test('invalid inputs, versions, intervals, calendars and dates cannot be submitted', () => {
  for (const change of [
    { name: ' ' },
    { definitionId: '' },
    { version: 0 },
    { version: 1.5 },
    { input: '{' },
    { input: 'null' },
    { input: '[]' },
    { minutes: 0 },
    { minutes: 0.5 },
    { minutes: 525601 },
    { mode: 'cron', cron: '* *' },
    { mode: 'cron', timeZone: 'Not/AZone' },
    { mode: 'once', at: '' },
    { mode: 'once', at: '2020-01-01' },
  ]) {
    assert.throws(() => scheduleConfig({ ...draft, ...change }));
  }
});
