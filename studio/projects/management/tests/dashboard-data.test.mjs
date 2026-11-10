import test from 'node:test';
import assert from 'node:assert/strict';
import { sorted, withinDates } from '../src/app/shared/utils/dashboard-data.ts';
import {
  runStatus,
  isTest,
  duration,
  runTags,
} from '../src/app/features/runs/utils/run-history.utils.ts';

test('run tags preserve false and draft zero and omit absent metadata', () => {
  assert.deepEqual(runTags({}), []);
  const tags = runTags({
    metadata: {
      project_id: 'project',
      definition_version: 0,
      is_test: false,
      source: 'manual',
      snapshot_id: '<script>',
    },
  });
  assert.deepEqual(
    tags.map((t) => t.value),
    ['project', '0', 'manual', 'false', '<script>'],
  );
  assert.equal(tags[0].label, 'Project');
  assert(isTest({ metadata: { is_test: true }, trigger_type: 'child' }));
});
test('closed engine state takes precedence; open pauses stay visible', () => {
  assert.equal(runStatus({ temporal_status: 'failed', state: { status: 'running' } }), 'failed');
  assert.equal(
    runStatus({ temporal_status: 'running', state: { status: 'paused_budget' } }),
    'paused_budget',
  );
  assert.equal(runStatus({ temporal_status: 'running', state: null }), 'running');
});
test('test classifications distinguish draft tests, child tests and normal executions', () => {
  for (const trigger_type of ['child_test', 'draft_test']) assert(isTest({ trigger_type }));
  assert(isTest({ trigger_type: 'manual', state: { is_test: true } }));
  assert(!isTest({ trigger_type: 'schedule' }));
});
test('sorting preserves input and handles numbers and natural names', () => {
  const rows = [
    { name: 'Prompt 10', n: 10 },
    { name: 'Prompt 2', n: 2 },
  ];
  assert.equal(sorted(rows, (r) => r.name, true)[0].n, 2);
  assert.equal(sorted(rows, (r) => r.n, false)[0].n, 10);
  assert.equal(rows[0].n, 10);
});
test('duration and inclusive UTC dates avoid guessing live completion', () => {
  assert.equal(duration({ created_at: '2026-10-01T00:00:00Z' }), -1);
  assert.equal(
    duration({ created_at: '2026-10-01T00:00:00Z', completed_at: '2026-10-01T00:00:01Z' }),
    1000,
  );
  assert(withinDates('2026-10-01T23:59:59.999Z', '2026-10-01', '2026-10-01'));
  assert(!withinDates('2026-10-02T00:00:00Z', '2026-10-01', '2026-10-01'));
});
