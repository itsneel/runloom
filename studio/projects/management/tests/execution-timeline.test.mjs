import test from 'node:test';
import assert from 'node:assert/strict';
import { executionTimeline } from '../src/app/features/runs/utils/execution-timeline.ts';

const t = (n) => new Date(1000 * n).toISOString();
test('waterfall preserves overlapping invocations and explicit empty payloads', () => {
  const history = {
    run: { temporal_status: 'running' },
    trace: {
      nodes: [
        { sequence: 1, step_id: 'branch', status: 'running', started_at: t(0) },
        {
          sequence: 2,
          step_id: 'task',
          parent: 1,
          status: 'completed',
          started_at: t(1),
          finished_at: t(3),
          input: false,
          output: null,
        },
        { sequence: 3, step_id: 'task', parent: 1, status: 'running', started_at: t(2) },
      ],
    },
  };
  const result = executionTimeline(history, 4000);
  assert.equal(result.span, '4 s');
  assert.deepEqual(
    result.rows.map((r) => [r.left, r.width]),
    [
      [0, 100],
      [25, 50],
      [50, 50],
    ],
  );
  assert.equal(result.rows[1].input, false);
  assert.equal(result.rows[1].output, null);
  history.run.temporal_status = 'terminated';
  assert.equal(executionTimeline(history).rows[2].duration, undefined);
  assert.equal(executionTimeline(history).rows[2].status, 'last observed running');
});
test('stored steps keep output and do not invent timings, failure details or inputs', () => {
  const run = {
    temporal_status: 'failed',
    state: {
      completed_steps: ['a'],
      steps: { b: { status: 'failed', output: null }, a: { status: 'completed', output: 0 } },
    },
  };
  const result = executionTimeline({ run });
  assert.equal(result.timed, false);
  assert.deepEqual(
    result.rows.map((r) => r.step_id),
    ['a', 'b'],
  );
  assert.equal(result.rows[0].output, 0);
  assert.equal(result.rows[1].output, undefined);
  assert.equal(result.rows[0].duration, undefined);
  assert.deepEqual(executionTimeline({ run, trace: { nodes: [] } }).rows, []);
});
