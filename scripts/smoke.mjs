// Exercises the deployed UI proxy, API, Temporal worker and database.
// Creates uniquely named synthetic records and leaves them for inspection.
import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { setTimeout as delay } from 'node:timers/promises';

const base = (process.env.RUNLOOM_UI_URL || 'http://localhost:4200').replace(/\/$/, '');
const id = 'smoke-' + randomUUID();
const token = process.env.RUNLOOM_ACCESS_TOKEN_FILE
  ? readFileSync(process.env.RUNLOOM_ACCESS_TOKEN_FILE, 'utf8').trim()
  : process.env.RUNLOOM_ACCESS_TOKEN;
const team = process.env.RUNLOOM_TEAM_ID;
assert(token && team, 'Set RUNLOOM_ACCESS_TOKEN_FILE and RUNLOOM_TEAM_ID for a signed-in team editor/admin.');
let runID;
let completed = false;

async function request(path, body) {
  const response = await fetch(base + path, {
    method: body === undefined ? 'GET' : 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + token, 'X-Runloom-Team': team },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: AbortSignal.timeout(10000),
  });
  assert(response.ok, path + ': HTTP ' + response.status);
  return response.json();
}

async function waitFor(predicate) {
  for (let attempt = 0; attempt < 90; attempt++) {
    const run = await request('/api/v1/studio/runs/' + runID);
    if (predicate(run)) return run;
    assert(!['failed', 'cancelled', 'terminated', 'timed_out'].includes(run.temporal_status), 'Workflow ended: ' + run.temporal_status);
    await delay(500);
  }
  throw new Error('Timed out waiting for workflow ' + runID);
}

try {
  const page = await fetch(base, { signal: AbortSignal.timeout(10000) });
  assert(page.ok && (await page.text()).includes('<app-root>'), 'Studio HTML did not load');
  const license = await fetch(base + '/LICENSE', { signal: AbortSignal.timeout(10000) });
  assert(license.ok && (await license.text()).includes('MIT License'), 'UI license is missing');
  const health = await request('/api/v1/health');
  assert.equal(health.temporal, true);
  assert.equal(health.database, true);
  const catalog = await request('/api/v1/studio/catalog');
  assert.deepEqual(catalog.activities.map(activity => activity.name), ['runloom.echo', 'runloom.compose', 'runloom.sum', 'runloom.llm']);

  await request('/api/v1/studio/definitions', {
    schema_version: '1.0', id, name: 'Deployment smoke test', version: 0,
    description: 'Synthetic deployment verification', trigger: { type: 'manual' },
    input_schema: { type: 'object' },
    steps: [
      { id: 'echo', type: 'activity', activity: 'runloom.echo', with: { value: '${input.value}' } },
      { id: 'timer', type: 'wait', seconds: 1 },
      { id: 'callback', type: 'callback', callback_key: 'ready', prompt: 'Wait for callback', timeout_seconds: 120 },
      { id: 'signal', type: 'signal', signal_name: 'smoke.ready', prompt: 'Wait for signal', timeout_seconds: 120 },
      { id: 'pause', type: 'pause', message: 'Deployment checkpoint' },
    ],
    output: '${steps.echo.output}',
  });
  const published = await request('/api/v1/studio/definitions/' + id + '/publish', {});
  assert.equal(published.version, 1);
  const started = await request('/api/v1/studio/runs', { definition_id: id, input: { value: 'deployment-ok' } });
  runID = started.workflow_id;
  await waitFor(run => run.state?.status === 'waiting_callback');
  await request('/api/v1/studio/callbacks/' + runID + '/callback', { payload: { received: true }, actor: 'deployment-smoke' });
  await waitFor(run => run.state?.status === 'waiting_signal');
  await request('/api/v1/studio/runs/' + runID + '/signals/smoke.ready', { payload: { received: true }, actor: 'deployment-smoke' });
  await waitFor(run => run.state?.status === 'paused');
  await request('/api/v1/studio/runs/' + runID + '/resume', { step_id: 'pause', payload: {}, actor: 'deployment-smoke' });
  await waitFor(run => run.temporal_status === 'completed');
  completed = true;
  const history = await request('/api/v1/studio/runs/' + runID + '/history');
  assert.equal(history.run.input.value, 'deployment-ok');
  assert.equal(history.run.output.value, 'deployment-ok');
  for (const type of ['workflow.started', 'callback.received', 'signal.received', 'workflow.resume_requested']) {
    assert(history.events.some(event => event.event_type === type), 'Missing audit event: ' + type);
  }
  const commits = await request('/api/v1/studio/definitions/' + id + '/history');
  assert(commits.commits.some(commit => commit.kind === 'publish'), 'Publish history is missing');
  console.log('PASS: UI, API, ' + (health.database_driver || 'service databases') + ', publish history, activity, timer, callback, signal, pause/resume and audit.');
  console.log('Synthetic definition: ' + id + '; workflow: ' + runID);
} finally {
  if (runID && !completed) {
    await request('/api/v1/studio/runs/' + runID + '/cancel', {}).catch(() => {});
  }
}
